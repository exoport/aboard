package aboard

import (
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"time"

	jsonv2 "github.com/go-json-experiment/json"
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
//     and turned into a removal REQUEST that the human answers — and an agent
//     write cannot clear that request either, whoever raised it.
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
	ID   string `json:"id"`
	Key  string `json:"key,omitempty"` // optional stable handle for idempotent upsert
	Name string `json:"name"`
	Type string `json:"type"`
	// STILL a json.RawMessage, and deliberately, even though the codec is now
	// encoding/json/v2: v2 special-cases encoding/json.RawMessage onto exactly the
	// same raw-value fast path as its own jsontext.Value — measured at 4.5 ms
	// against 5.0 ms decoding 5 000 tabs, i.e. no difference worth a type change
	// that would ripple into every fixture in the tests.
	//
	// `omitzero` and not `omitempty`, which under v2 means something else: v2
	// omits a value that ENCODES to empty, so a tab holding a deliberate
	// `"state": {}` would silently lose the key. `omitzero` omits the zero value —
	// no state at all — which is what v1's `omitempty` did for a []byte and what
	// every reader of this file already assumes.
	State json.RawMessage `json:"state,omitzero"`

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

// The two actor names every guarantee in this file keys off. Written once
// because both mistakes fail OPEN and therefore silently: a `by` that is not
// actorHuman is treated as an agent, and an absent one is stamped actorUnknown,
// which has an agent's powers and no more. A typo in either literal would hand
// out or withhold powers with nothing to report it.
const (
	actorHuman   = "human"
	actorUnknown = "unknown"
)

// The two tab types the ENGINE has to know by name. Every other type is opaque
// to Go — a name and a state blob the renderer owns — which is what "tabs are
// data" means. These two are not: a stack's blocks are a second level of
// (type, state) pairs the write checker and export must walk, and an html tab is
// served from its own sandboxed route.
const (
	tabTypeStack = "stack"
	tabTypeHTML  = "html"
)

func isHuman(by string) bool { return by == actorHuman }

// reconcileTabs applies the guarantees above and stamps `touched` on every
// tab an agent actually changed. Returns the tab list to persist.
//
// The byte-level form: the tests specify the guarantees against two documents,
// which is the right level for a rule about what a WRITE may do. The write path
// itself calls reconcileDoc with the documents it already holds parsed — the
// current one has not been re-read or re-parsed since it was accepted (see
// document.go).
//
// `logger` is an argument rather than a package-level log.Printf because a host
// embedding this tree chooses where its logs go (Options.Logger, see aboard.go),
// and the one line this function writes — an agent tried to delete a tab — is
// exactly the line that host wants. Options.Log() is never nil, so callers pass
// it straight through.
func reconcileTabs(currentRaw, incomingRaw []byte, by string, logger *log.Logger) ([]tab, error) {
	cur, err := decodeDocument(currentRaw)
	if err != nil {
		return nil, fmt.Errorf("current board unreadable: %w", err)
	}
	inc, err := decodeDocument(incomingRaw)
	if err != nil {
		return nil, fmt.Errorf("incoming board unreadable: %w", err)
	}
	return reconcileDoc(cur, inc, by, logger).tabList(), nil
}

// reconcilePlan is one pass over an incoming write: the tabs to persist, each
// carrying whether this write changed it.
//
// That flag is the whole reason the plan exists. reconcileTabs and changeSummary
// used to make the SAME comparison independently — canonicalising every tab's
// state on the board, twice per write — and the comment on each said so, because
// a change that gets one without the other is a change the human can see but not
// trace, or trace but not see. One comparison, one flag, both consumers.
type reconcilePlan struct {
	tabs []docTab
}

func (p *reconcilePlan) tabList() []tab {
	out := make([]tab, len(p.tabs))
	for i := range p.tabs {
		out[i] = p.tabs[i].tab
	}
	return out
}

func reconcileDoc(cur, inc *stateDoc, by string, logger *log.Logger) *reconcilePlan {
	human := isHuman(by)
	now := time.Now().UTC().Format(time.RFC3339)

	out := make([]docTab, 0, len(inc.tabs)+2)
	for i := range inc.tabs {
		t := inc.tabs[i]
		j, existed := cur.byID[t.ID]

		if !existed {
			if !human {
				// A brand new tab: mark it so the human sees it arrived.
				if t.Touched == nil {
					t.Touched = &touchMark{By: by, At: now, Note: "new tab"}
				} else {
					t.Touched.By, t.Touched.At = by, now
				}
				// Guarantee 4 applies to a tab being CREATED as much as to one
				// being changed, and this branch skipped it entirely: a new tab
				// could arrive carrying `seen: {"human": "…"}`, so the dot the
				// human relies on to notice it was pre-extinguished by the write
				// that made it. There is no previous map by definition, which is
				// exactly why the filter has to run rather than be short-circuited.
				t.Seen = mergeSeen(nil, t.Seen, by)
			}
			t.changed = true
			out = append(out, t)
			continue
		}

		prev := &cur.tabs[j]
		if !human {
			// Guarantee 2: only the human clears a marker. Carry the old one
			// forward if this write tried to drop it.
			if t.Touched == nil && prev.Touched != nil {
				t.Touched = prev.Touched
			}

			// Guarantee 1, the other half: only the human answers a removal
			// request. The restore branch below covers a tab an agent DROPPED;
			// this covers the far commoner case of an agent carrying the whole
			// document through a read-modify-write with the field simply absent,
			// because nothing it did was about that tab. `pendingRemoval` was
			// taken verbatim, so a routine write by agent-2 cancelled agent-1's
			// request and the human's banner vanished with no record that it had
			// ever been raised — the same shape as `touched`, and it is carried
			// forward for the same reason.
			if t.PendingRemoval == nil && prev.PendingRemoval != nil {
				t.PendingRemoval = prev.PendingRemoval
			}

			// Guarantee 4: an agent may move its OWN read stamp and nobody
			// else's. Most writes drop `seen` entirely, having never looked at
			// it, and that must not erase what the other actors recorded — with
			// two sessions and a human on one board, "changed since I last
			// looked" is a per-actor question and one actor's answer is not
			// another's to clear.
			//
			// This is where mergeSeen had zero call sites: the function was
			// written, tested by eye and never wired in, so the guarantee existed
			// in the comment at the top of this file and nowhere in the code.
			// That is the worst shape a guarantee can have — documented, believed,
			// and absent.
			t.Seen = mergeSeen(prev.Seen, t.Seen, by)
		}

		same := sameState(prev, &t)

		// Guarantee 3: an agent cannot un-read a message. A chat message carries
		// an ack once a session has consumed it, and the human's edit/delete
		// window closes at that point — so an agent that dropped the ack could
		// reopen a window on a message it had already acted on. Carried forward
		// for the same reason as `touched`.
		//
		// Only for a tab that looks changed, and that ordering is deliberate: a
		// tab whose state is identical cannot have dropped an ack, so the whole
		// walk is skipped for every tab a write did not touch. The comparison is
		// then made AGAIN on the restored state, because a write whose only
		// difference was a missing ack has, after the carry, changed nothing —
		// and must not raise a dot saying it did.
		if !human && !same {
			if state, carried := carryAcks(prev.State, t.State); carried {
				t.State, t.norm, t.maxID = state, nil, -1
				same = sameState(prev, &t)
			}
		}

		// `note` is in the comparison for the same reason it is in the journal:
		// it is human-authored intent — what this tab is FOR, in their words — and
		// an agent rewriting it is exactly the kind of change the dot exists to
		// announce. When the two comparisons disagreed, a note-only write was
		// journaled and left no marker, so the human's own sentence could be
		// replaced with nothing on screen to say so.
		meta := prev.Name == t.Name &&
			prev.Type == t.Type &&
			prev.StateFrom == t.StateFrom &&
			prev.Note == t.Note

		if !human && (!same || !meta) {
			note := ""
			if t.Touched != nil {
				note = t.Touched.Note
			}
			t.Touched = &touchMark{By: by, At: now, Note: note}
		}

		// What the JOURNAL records, which is wider than what raises a dot by
		// exactly one field: a write that drops a tab changes nothing else about
		// it, so an agent asking to delete something used to produce a banner on
		// the human's screen and not one line anywhere else.
		t.changed = !same || !meta || !sameRemovalAsk(prev.PendingRemoval, t.PendingRemoval)

		// Nothing that could hold an id moved, so the largest one is the largest
		// one it had. This is what stops the id allocator walking the board.
		if same {
			t.maxID = prev.idHigh()
		}
		out = append(out, t)
	}

	// A human write is taken as-is: they may dismiss markers, answer removal
	// requests, delete tabs, anything.
	if human {
		return &reconcilePlan{tabs: out}
	}

	// Guarantee 1: restore anything the agent dropped, as a request instead.
	for i := range cur.tabs {
		id := cur.tabs[i].ID
		if _, kept := inc.byID[id]; kept {
			continue
		}
		gone := cur.tabs[i]
		asked := gone.PendingRemoval
		if gone.PendingRemoval == nil {
			gone.PendingRemoval = &removalAsk{By: by, At: now, Reason: "removal requested by an agent write"}
		}
		if gone.Touched == nil {
			gone.Touched = &touchMark{By: by, At: now, Note: "removal requested"}
		}
		gone.changed = !sameRemovalAsk(asked, gone.PendingRemoval)
		logger.Printf("tab %q (%s) was dropped by %s — restored as a removal request", gone.Name, id, by)
		out = insertAt(out, gone, i)
	}

	return &reconcilePlan{tabs: out}
}

func insertAt(list []docTab, t docTab, at int) []docTab {
	if at > len(list) {
		at = len(list)
	}
	list = append(list, docTab{})
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
	if jsonv2.Unmarshal(prevRaw, &prev) != nil || jsonv2.Unmarshal(nextRaw, &next) != nil {
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
	// writeOptions, because this result is written to the state file: v2 shuffles
	// map keys unless told not to, and a state blob whose keys moved on every
	// ack carry would read as a change nobody made.
	out, err := jsonv2.Marshal(next, writeOptions)
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
					maps.Copy(t, ack)
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
//
// The filter runs whether or not there was a previous map, and that is the fix:
// it used to short-circuit on `len(prev) == 0` and hand back whatever the write
// contained, so on a tab that had never carried a `seen` map an agent could
// PLANT one — `{"human": "<a time in the future>"}` — and the human's "changed
// since I last looked" dot would never light for that tab again. Guarantee 4 is
// "an agent may set its own key and nobody else's", and a guarantee with a
// condition on it is not one. Tab CREATION had the same hole from the other
// direction and is now routed through here too.
func mergeSeen(prev, next map[string]string, by string) map[string]string {
	if len(prev) == 0 && len(next) == 0 {
		return next
	}
	out := map[string]string{}
	maps.Copy(out, prev)
	for actor, at := range next {
		// Only the writer may set the writer's own stamp; anything else it claims
		// about another actor is ignored rather than trusted.
		if actor == by {
			out[actor] = at
		}
	}
	return out
}
