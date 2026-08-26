// merge.go — what `aboard apply` does with a 409 instead of giving up.
//
// Compare-and-set is whole-document, so ANY concurrent write conflicts with ANY
// other: the human dismissing a notice in the browser refuses an agent's write to
// a different tab entirely. The browser has refused to lose the human's work over
// that since aboard.html grew mergeOntoFresh; `apply` handed back one sentence and
// discarded the agent's whole document — built from a board it can no longer read,
// by the one actor still holding the context that produced it.
//
// So a 409 is now a merge, once:
//
//  1. re-read the live document;
//  2. ask the journal which tabs moved since the base we started from, and what
//     each of them said AT that base (`before` on the first entry that touched it);
//  3. keep the server's version of every tab it moved, and our version of every
//     tab it did not;
//  4. if we changed a tab the server also changed, NAME IT AND STOP. That is the
//     browser's rule, kept deliberately: a silent same-tab merge is how one of the
//     two edits disappears with a 200 to say it went well.
//
// One retry, so a busy board cannot spin apply forever, and the second refusal is
// reported exactly as the first one used to be.
//
// This mirrors mergeOntoFresh rather than calling into it — the CLI has no
// baseline snapshot and no DOM. What it has instead is the journal, which is a
// better source: the browser compares against what it loaded, and this compares
// against what the SERVER recorded, so it cannot be wrong about who moved what.

package aboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"sort"
	"strconv"
	"strings"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

// ErrCollision is a conflict the merge will not resolve on its own, and every
// use of it wraps a sentence naming the tab and saying which kind it is.
//
// The sentence is deliberately not "both sides changed the same tab", which was
// the first wording and was a lie in one of the three cases: the name/type/note
// comparison below is against the FRESH tab rather than against our base, so a
// tab somebody else RENAMED while we wrote to a different one lands here having
// been touched by exactly one side. An error that asserts something untrue about
// what the caller did is worse than a vaguer one — it sends them looking for an
// edit of their own that is not there.
var ErrCollision = errors.New("the merge stopped rather than pick a winner")

// errUnmergeable is a 409 the merge cannot reason about — a timestamp base, a
// journal that has rotated past our base. Reported as the plain conflict it is.
var errUnmergeable = errors.New("this conflict cannot be merged")

// serverManagedFields are the root keys the server stamps on every accepted
// write. A caller's copies of them are ignored on the merge, because the fresh
// document's are the true ones by definition (see commitState).
var serverManagedFields = map[string]bool{
	keyTabs: true, keyRev: true, keyNextID: true,
	keyVersion: true, keyUpdatedAt: true, keyLastEditedBy: true,
}

// mergeResult is what a successful merge produced, for the sentence the caller
// prints. `Kept` names the tabs whose server-side version we took.
type mergeResult struct {
	doc  map[string]any
	base string
	kept []string
}

// mergeOnConflict rebuilds the write against the document as it is now.
//
// `ours` is the document the caller submitted, with the `__` control keys already
// stripped; `base` is the revision it was built from.
func mergeOnConflict(ctx context.Context, inst Instance, ours map[string]any, base string) (mergeResult, error) {
	baseRev, err := strconv.Atoi(strings.TrimSpace(base))
	if err != nil {
		// A timestamp base belongs to a board written before the revision counter
		// landed. There is no way to ask the journal "since when", so there is no
		// way to know which tabs moved — and a merge that guessed would be the
		// silent loss this whole path exists to prevent.
		return mergeResult{}, fmt.Errorf("%w: the base is %q, not a revision, so the journal cannot say which tabs moved since", errUnmergeable, base)
	}

	freshRaw, err := fetchDocument(ctx, inst.URL)
	if err != nil {
		return mergeResult{}, fmt.Errorf("%w: re-reading the board: %w", errUnmergeable, err)
	}
	var fresh map[string]any
	if err := jsonv2.Unmarshal(freshRaw, &fresh); err != nil {
		return mergeResult{}, fmt.Errorf("%w: the board document does not parse: %w", errUnmergeable, err)
	}

	moved, err := tabsMovedSince(ctx, inst.URL, baseRev)
	if err != nil {
		return mergeResult{}, err
	}

	// A root field of our own that the merge would drop. Named rather than
	// silently lost: the board defines no root key beyond the five the server
	// stamps, so anything else is something this caller invented and cares about.
	for _, key := range sortedRootKeys(ours) {
		if serverManagedFields[key] || strings.HasPrefix(key, "__") {
			continue
		}
		if !sameJSON(mustJSON(ours[key]), mustJSON(fresh[key])) {
			return mergeResult{}, fmt.Errorf("%w: this write also changes the root field %q, and the merge only re-applies tabs — re-read the board, redo the edit, apply again", ErrCollision, key)
		}
	}

	tabs, kept, err := mergeTabs(ours, fresh, moved)
	if err != nil {
		return mergeResult{}, err
	}

	merged := map[string]any{}
	maps.Copy(merged, fresh)
	merged[keyTabs] = tabs

	return mergeResult{doc: merged, base: freshRevToken(fresh), kept: kept}, nil
}

// movedTab is one tab the server changed since our base: whether it existed at
// that base at all, and what its state was then.
type movedTab struct {
	existed bool
	state   json.RawMessage
}

// tabsMovedSince asks the journal which tabs changed after a revision, and what
// each of them held at it.
//
// The state comes from the EARLIEST qualifying entry, not the latest: `before` on
// entry N is the state that entry replaced, so the earliest one after our base is
// the only entry holding the document as WE read it.
//
// A write that changed no tab is not journalled at all, so a gap between our base
// and the live revision is not evidence of a missing record — it is a write that
// touched nothing, which is exactly what "no tab moved" means.
func tabsMovedSince(ctx context.Context, base string, sinceRev int) (map[string]movedTab, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/journal?limit=%d", base, historyScan), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errUnmergeable, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: reading the journal: %w", errUnmergeable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var payload struct {
		Entries []JournalEntry `json:"entries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("%w: unreadable journal: %w", errUnmergeable, err)
	}

	entries := payload.Entries
	// The scan cap is a bound on work, and hitting it means the window between
	// our read and our write is not fully in view. Refusing beats guessing.
	if len(entries) >= historyScan && len(entries) > 0 && entries[0].Rev > sinceRev {
		return nil, fmt.Errorf("%w: more than %d writes landed since rev %d, so the journal tail no longer covers the whole window",
			errUnmergeable, historyScan, sinceRev)
	}

	moved := map[string]movedTab{}
	for i := range entries {
		e := &entries[i]
		if e.Rev <= sinceRev {
			continue
		}
		for _, id := range e.Tabs {
			if _, seen := moved[id]; seen {
				continue
			}
			state, existed := e.Before[id]
			moved[id] = movedTab{existed: existed, state: state}
		}
	}
	return moved, nil
}

// mergeTabs builds the tab list of the merged document.
//
// Our order, not the server's: the tabs a caller submits are the ones it means
// to have, in the order it means to have them, and the only thing being taken
// from the fresh document is the CONTENT of tabs it moved.
func mergeTabs(ours, fresh map[string]any, moved map[string]movedTab) (tabs []any, kept []string, err error) {
	freshByID := map[string]map[string]any{}
	freshOrder := []string{}
	for _, raw := range tabList(fresh) {
		if t, id := asTab(raw); id != "" {
			freshByID[id] = t
			freshOrder = append(freshOrder, id)
		}
	}

	out := []any{}
	kept = []string{}
	mine := map[string]bool{}

	for _, raw := range tabList(ours) {
		t, id := asTab(raw)
		if id == "" {
			// Not a tab object. Pass it through untouched and let the server's own
			// refusal be the one that speaks: inventing a second opinion here would
			// mean two different messages for one malformed document.
			out = append(out, raw)
			continue
		}
		mine[id] = true
		gone, serverTouched := moved[id]
		if !serverTouched {
			out = append(out, raw)
			continue
		}
		live, stillThere := freshByID[id]
		if !stillThere {
			// The server touched it and it is no longer in the document: the human
			// answered a removal request. Ours would resurrect it.
			return nil, nil, fmt.Errorf("%w: %s was removed on the board while you were writing, and your document still carries it — re-read the board and decide", ErrCollision, id)
		}
		if !gone.existed {
			return nil, nil, fmt.Errorf("%w: %s was created on the board while you were writing a tab with the same id — re-read the board, redo the edit, apply again", ErrCollision, id)
		}
		if err := ourTabIsUnchanged(id, t, live, gone.state); err != nil {
			return nil, nil, err
		}
		out = append(out, live)
		kept = append(kept, id)
	}

	// A tab the server CREATED since our base is not ours to drop: our document
	// simply predates it, and omitting it would be read as a removal request.
	for _, id := range freshOrder {
		if mine[id] {
			continue
		}
		if _, serverTouched := moved[id]; serverTouched {
			out = append(out, freshByID[id])
			kept = append(kept, id)
		}
	}
	sort.Strings(kept)
	return out, kept, nil
}

// ourTabIsUnchanged decides whether our copy of a server-moved tab is one we
// left alone, and returns the collision when it is not.
//
// Two questions, and they are answered from different sources on purpose:
//
//   - `state` is compared against what the JOURNAL says the tab held at our base.
//     That is the only comparison that can distinguish "I changed this" from "the
//     server changed this", because it is the only record of the third document.
//   - name, type, note and stateFrom are compared against the FRESH tab, because
//     the journal does not carry them. That is a weaker test and it can produce a
//     FALSE collision: if the human renamed a tab we did not touch, our copy
//     disagrees with the fresh one through no fault of ours and the merge stops.
//     Stopping is the safe half of that trade — the caller re-reads and applies
//     again — where the alternative silently discards whichever rename lost.
func ourTabIsUnchanged(id string, ours, live map[string]any, atBase json.RawMessage) error {
	if !sameJSON(mustJSON(ours["state"]), atBase) {
		return fmt.Errorf("%w: %s changed on the board while you were changing it — re-read the board, redo the edit, apply again", ErrCollision, id)
	}
	for _, field := range []string{keyName, keyType, keyNote, keyStateFrom} {
		if !sameJSON(mustJSON(ours[field]), mustJSON(live[field])) {
			return fmt.Errorf("%w: %s — its %s differs from the board's and the journal does not record which side changed it; re-read the board, redo the edit, apply again",
				ErrCollision, id, field)
		}
	}
	return nil
}

/* ---------- small readers ---------- */

func tabList(doc map[string]any) []any {
	list, _ := doc[keyTabs].([]any)
	return list
}

func asTab(raw any) (tab map[string]any, id string) {
	t, ok := raw.(map[string]any)
	if !ok {
		return nil, ""
	}
	got, _ := t["id"].(string)
	return t, got
}

func sortedRootKeys(doc map[string]any) []string {
	keys := make([]string, 0, len(doc))
	for k := range doc {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// freshRevToken renders the fresh document's `rev` as a compare-and-set base,
// the way applyBase does. An absent one leaves the base empty, which the server
// treats as an unconditional write — and that is right here, because a board
// with no rev has nothing to compare against.
func freshRevToken(doc map[string]any) string {
	switch rev := doc[keyRev].(type) {
	case float64:
		return strconv.Itoa(int(rev))
	case string:
		return strings.TrimSpace(rev)
	}
	return ""
}

// mustJSON marshals a decoded value back to bytes for comparison. A value that
// came out of a JSON decoder always marshals, so a failure here is nil, which
// compares equal to nil and unequal to everything else — the conservative answer.
func mustJSON(v any) []byte {
	if v == nil {
		return nil
	}
	body, err := jsonv2.Marshal(v, writeOptions)
	if err != nil {
		return nil
	}
	return body
}

// sameJSON is order-insensitive equality for two encoded values.
//
// Deliberately NOT canonicalEqual: that one feeds the counters document_test.go
// asserts on, and those counters describe the SERVER's write path. A client-side
// comparison landing in them would make a claim about the write path that the
// write path did not make.
func sameJSON(a, b []byte) bool {
	if len(a) == 0 || len(b) == 0 {
		return len(a) == len(b)
	}
	if bytes.Equal(a, b) {
		return true
	}
	x, y := jsontext.Value(a).Clone(), jsontext.Value(b).Clone()
	if x.Canonicalize() != nil || y.Canonicalize() != nil {
		return false
	}
	return bytes.Equal(x, y)
}
