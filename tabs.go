package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// Tabs are data, not code. A tab names a purpose, picks a renderer by `type`,
// and owns its own `state`; the browser has no fixed list. That is what lets an
// agent open a board for whatever it needs to show — a plan, a chart, a
// question, a conversation with another agent — instead of choosing from five.
//
// Three guarantees are enforced here rather than left to convention, because an
// agent that forgets them would destroy the user's work:
//
//  1. An agent cannot delete a tab. A write that drops one has the tab restored
//     and turned into a removal REQUEST that the human answers.
//  2. An agent cannot clear a tab's `touched` marker. That marker is what raises
//     the dot on the tab and the banner inside it, and only the human dismissing
//     it takes it down — otherwise a later agent write could hide the fact that
//     an earlier one changed something.
//  4. An agent cannot clear another actor's read state (`seen`): it may stamp
//     its own key and nobody else's.
//  3. An agent cannot un-read a chat message. Once a session has acked a message
//     the human's edit/delete window closes, so dropping the ack would reopen a
//     window on something already acted on. Acks are carried forward.

type tab struct {
	ID    string          `json:"id"`
	Key   string          `json:"key,omitempty"` // optional stable handle for idempotent upsert
	Name  string          `json:"name"`
	Type  string          `json:"type"`
	State json.RawMessage `json:"state,omitempty"`

	// StateFrom lets a tab render another tab's state with a different type —
	// a kanban and a DAG over one set of nodes, for instance.
	StateFrom string `json:"stateFrom,omitempty"`

	// Note is what this tab is FOR, in the human's words: the intent an agent
	// cannot infer from the contents. A kanban of eight cards does not say whether
	// it is a wish list or a commitment; a markup tab does not say what the human
	// was actually looking for. Free text, theirs to write, ours to read before
	// acting on the tab.
	Note string `json:"note,omitempty"`

	Touched        *touchMark  `json:"touched,omitempty"`
	PendingRemoval *removalAsk `json:"pendingRemoval,omitempty"`

	// Seen is per-actor read state: {"human":"…","agent-2":"…"}. `touched` answers
	// "has the HUMAN looked", which is all one marker can answer — with two
	// sessions and a human on one board, agent-2 dismissing something erased the
	// signal agent-1 left. Each actor stamps its own key and cannot clear another's
	// (enforced below), so "changed since I last looked" is answerable per actor
	// without anyone stepping on anyone.
	Seen map[string]string `json:"seen,omitempty"`
}

type touchMark struct {
	By   string `json:"by"`
	At   string `json:"at"`
	Note string `json:"note,omitempty"`
}

type removalAsk struct {
	By     string `json:"by"`
	At     string `json:"at"`
	Reason string `json:"reason,omitempty"`
}

type board struct {
	Tabs []tab `json:"tabs"`
}

func isHuman(by string) bool { return by == "human" }

// reconcileTabs applies the guarantees above and stamps `touched` on every
// tab an agent actually changed. Returns the tab list to persist.
func reconcileTabs(currentRaw, incomingRaw []byte, by string) ([]tab, error) {
	var cur, inc board
	if len(currentRaw) > 0 {
		if err := json.Unmarshal(currentRaw, &cur); err != nil {
			return nil, fmt.Errorf("current board unreadable: %w", err)
		}
	}
	if err := json.Unmarshal(incomingRaw, &inc); err != nil {
		return nil, fmt.Errorf("incoming board unreadable: %w", err)
	}

	// A human write is taken as-is: they may dismiss markers, answer removal
	// requests, delete tabs, anything.
	if isHuman(by) {
		return inc.Tabs, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	before := map[string]tab{}
	order := []string{}
	for _, t := range cur.Tabs {
		before[t.ID] = t
		order = append(order, t.ID)
	}
	after := map[string]tab{}
	for _, t := range inc.Tabs {
		after[t.ID] = t
	}

	out := make([]tab, 0, len(inc.Tabs)+2)

	for _, t := range inc.Tabs {
		prev, existed := before[t.ID]

		if !existed {
			// A brand new tab: mark it so the human sees it arrived.
			if t.Touched == nil {
				t.Touched = &touchMark{By: by, At: now, Note: "new tab"}
			} else {
				t.Touched.By, t.Touched.At = by, now
			}
			out = append(out, t)
			continue
		}

		// Guarantee 2: only the human clears a marker. Carry the old one forward
		// if this write tried to drop it.
		if t.Touched == nil && prev.Touched != nil {
			t.Touched = prev.Touched
		}

		// Guarantee 3: an agent cannot un-read a message. A chat message carries
		// an ack once a session has consumed it, and the human's edit/delete
		// window closes at that point — so an agent that dropped the ack could
		// reopen a window on a message it had already acted on. Carried forward
		// for the same reason as `touched`.
		if state, changed := carryAcks(prev.State, t.State); changed {
			t.State = state
		}

		changed := !jsonEqual(prev.State, t.State) ||
			prev.Name != t.Name ||
			prev.Type != t.Type ||
			prev.StateFrom != t.StateFrom

		if changed {
			note := ""
			if t.Touched != nil {
				note = t.Touched.Note
			}
			t.Touched = &touchMark{By: by, At: now, Note: note}
		}
		out = append(out, t)
	}

	// Guarantee 1: restore anything the agent dropped, as a request instead.
	for _, id := range order {
		if _, kept := after[id]; kept {
			continue
		}
		gone := before[id]
		if gone.PendingRemoval == nil {
			gone.PendingRemoval = &removalAsk{By: by, At: now, Reason: "removal requested by an agent write"}
		}
		if gone.Touched == nil {
			gone.Touched = &touchMark{By: by, At: now, Note: "removal requested"}
		}
		log.Printf("tab %q (%s) was dropped by %s — restored as a removal request", gone.Name, id, by)
		out = insertAt(out, gone, indexOf(order, id))
	}

	return out, nil
}

func jsonEqual(a, b json.RawMessage) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return string(a) == string(b)
	}
	ab, err1 := json.Marshal(x)
	bb, err2 := json.Marshal(y)
	if err1 != nil || err2 != nil {
		return false
	}
	return string(ab) == string(bb)
}

func indexOf(ids []string, id string) int {
	for i, v := range ids {
		if v == id {
			return i
		}
	}
	return len(ids)
}

func insertAt(list []tab, t tab, at int) []tab {
	if at > len(list) {
		at = len(list)
	}
	list = append(list, tab{})
	copy(list[at+1:], list[at:])
	list[at] = t
	return list
}

// carryAcks re-applies any ack that the incoming state dropped, for a chat
// anywhere in a tab's state — a chat tab, or a chat block inside a stack.
//
// Shape-tolerant on purpose: it looks for arrays of objects that carry both an
// `id` and an `ackBy`, rather than knowing where chats live. A renderer that
// grows the same concept gets this for free, and one that does not is untouched.
func carryAcks(prevRaw, nextRaw json.RawMessage) (json.RawMessage, bool) {
	if len(prevRaw) == 0 || len(nextRaw) == 0 {
		return nextRaw, false
	}
	var prev, next any
	if json.Unmarshal(prevRaw, &prev) != nil || json.Unmarshal(nextRaw, &next) != nil {
		return nextRaw, false
	}

	acks := map[string]map[string]any{}
	collectAcks(prev, acks)
	if len(acks) == 0 {
		return nextRaw, false
	}

	changed := restoreAcks(next, acks)
	if !changed {
		return nextRaw, false
	}
	out, err := json.Marshal(next)
	if err != nil {
		return nextRaw, false
	}
	return out, true
}

func collectAcks(v any, into map[string]map[string]any) {
	switch t := v.(type) {
	case map[string]any:
		id, hasID := t["id"].(string)
		if hasID {
			if by, ok := t["ackBy"].(string); ok && by != "" {
				entry := map[string]any{"ackBy": by}
				if at, ok := t["ackAt"].(string); ok {
					entry["ackAt"] = at
				}
				into[id] = entry
			}
		}
		for _, child := range t {
			collectAcks(child, into)
		}
	case []any:
		for _, child := range t {
			collectAcks(child, into)
		}
	}
}

func restoreAcks(v any, acks map[string]map[string]any) bool {
	changed := false
	switch t := v.(type) {
	case map[string]any:
		if id, ok := t["id"].(string); ok {
			if ack, known := acks[id]; known {
				if by, ok := t["ackBy"].(string); !ok || by == "" {
					for k, val := range ack {
						t[k] = val
					}
					changed = true
				}
			}
		}
		for _, child := range t {
			if restoreAcks(child, acks) {
				changed = true
			}
		}
	case []any:
		for _, child := range t {
			if restoreAcks(child, acks) {
				changed = true
			}
		}
	}
	return changed
}

// mergeSeen keeps every actor's read stamp except the writer's own, which the
// writer is free to move. A write that drops the map entirely (most writes, since
// most agents never touch it) leaves everyone's stamps intact.
func mergeSeen(prev, next map[string]string, by string) map[string]string {
	if len(prev) == 0 {
		return next
	}
	out := map[string]string{}
	for actor, at := range prev {
		out[actor] = at
	}
	for actor, at := range next {
		// Only the writer may set the writer's own stamp; anything else it claims
		// about another actor is ignored rather than trusted.
		if actor == by {
			out[actor] = at
		}
	}
	return out
}
