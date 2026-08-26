// history.go — what a tab said before, and how to put it back.
//
//	GET /history?tab=<id>&limit=N   one tab's recorded prior states
//	aboard history <tab>            list them, with who wrote each one
//	aboard history <tab> --at N     print a document `aboard apply` accepts
//
// The journal already records, for every accepted write, the state each changed
// tab held BEFORE it — which is the unit somebody undoing a bad write actually
// wants. Until now that was reachable only by parsing `.aboard/run/journal.jsonl`
// by hand, so the board's only recovery path was one nobody could use in a hurry.
//
// Two properties are load-bearing and neither is obvious:
//
//   - **A restore is a whole document.** A journal entry holds one tab's inner
//     `state` blob. Submitting that on its own as `{"tabs":[{…}]}` would be a
//     document that DROPS every other tab, and the server would dutifully turn
//     each one into a removal request on the human's screen. So `--at N` merges
//     the old state onto a freshly read full document and prints THAT — the same
//     shape of mistake an absent `__by` used to make, avoided in the same way.
//
//   - **History ends, and the listing says where.** Rotation keeps one older
//     generation, so a tab's past is bounded and the boundary moves. A listing
//     that just stopped would read as "this tab has only ever been written three
//     times", which is a different and wrong sentence.

package aboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// historyScan is how many journal entries a history read looks through. The
// journal is capped at 16 MiB per generation, so this is a bound on the WORK
// rather than a policy about the record: a listing that had to decode every line
// of two full generations to show ten versions would be the slowest command here.
//
// A scan that hits the cap says so in its own output rather than pretending the
// record ends at the cap.
const historyScan = 2000

// defaultHistoryLimit is how many versions `aboard history` prints unasked.
const defaultHistoryLimit = 20

// HistoryVersion is one recorded prior state of a tab: what it said, when it
// stopped saying it, and who replaced it.
//
// `At`, `By` and `Rev` describe the write that OVERWROTE this state, not the one
// that produced it — the journal records a change, and the state it carries is
// the one being left behind. Saying "written by" here would name the wrong actor
// for every entry, so the human listing says "replaced by".
type HistoryVersion struct {
	N      int             `json:"n"                yaml:"n"`
	At     string          `json:"at"               yaml:"at"`
	By     string          `json:"by"               yaml:"by"`
	Origin string          `json:"origin,omitempty" yaml:"origin,omitempty"`
	Rev    int             `json:"rev,omitempty"    yaml:"rev,omitempty"`
	Name   string          `json:"name,omitempty"   yaml:"name,omitempty"`
	Bytes  int             `json:"bytes"            yaml:"bytes"`
	State  json.RawMessage `json:"state"            yaml:"-"`
}

// TabHistory is one tab's past as the kept journal holds it, newest first.
type TabHistory struct {
	Tab      string           `json:"tab"      yaml:"tab"`
	Versions []HistoryVersion `json:"versions" yaml:"versions"`
	// Scanned is how many journal entries were read to build this.
	Scanned int `json:"scanned" yaml:"scanned"`
	// Oldest is the timestamp of the oldest entry scanned — where the record
	// itself begins, whether or not it mentions this tab.
	Oldest string `json:"oldest,omitempty" yaml:"oldest,omitempty"`
	// Truncated reports that the scan hit its cap, so entries older than
	// `Oldest` exist and were not read. Distinct from the record simply ending.
	Truncated bool `json:"truncated" yaml:"truncated"`
	// Limited reports that --limit cut the list, so there are more versions in
	// the range that was scanned.
	Limited bool `json:"limited" yaml:"limited"`
	// Source is JournalFromServer, JournalFromDisk or JournalFromDiskStale, so
	// the caller can say where the answer came from.
	Source string `json:"source,omitempty" yaml:"source,omitempty"`
	// Ends is EndsAt() as a field, so a consumer that only has the JSON — the
	// browser's change banner — says the same sentence about where the record
	// stops as the terminal does. A fact the human form states and the machine
	// form omitted would be the two halves disagreeing in the one place it
	// matters.
	Ends string `json:"ends" yaml:"ends"`
}

// historyFrom filters journal entries down to one tab's versions.
//
// ONE implementation, shared by the HTTP handler and the CLI, because they read
// the same file through different doors and an answer that differed by door
// would be the worst kind of drift — invisible until somebody compared them.
//
// `entries` arrive oldest first (tail()'s order); versions come out newest
// first, numbered from 1, because 1 is the undo everybody wants.
func historyFrom(entries []JournalEntry, tab string, limit int) TabHistory {
	out := TabHistory{Tab: tab, Versions: []HistoryVersion{}, Scanned: len(entries)}
	if len(entries) > 0 {
		out.Oldest = entries[0].At
	}
	n := 0
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		state, ok := e.Before[tab]
		if !ok {
			// The tab changed in this write but held no state before it — a tab
			// being CREATED. There is nothing to restore, and listing it as a
			// version with an empty body would offer a restore that blanks it.
			continue
		}
		n++
		if limit > 0 && n > limit {
			out.Limited = true
			break
		}
		out.Versions = append(out.Versions, HistoryVersion{
			N: n, At: e.At, By: e.By, Origin: e.Origin, Rev: e.Rev,
			Name: e.Names[tab], Bytes: len(state), State: state,
		})
	}
	return out
}

/* ---------- endpoint ---------- */

// handleHistory answers the browser's "what did this tab say before?".
func (s *server) handleHistory(w http.ResponseWriter, r *http.Request) {
	tab := strings.TrimSpace(r.URL.Query().Get("tab"))
	if tab == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":  "tab is required",
			"reason": "GET /history?tab=<id> — history is per tab, and a whole-board history is what /journal already is",
		})
		return
	}
	limit := defaultHistoryLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := fmt.Sscanf(raw, "%d", &limit); n != 1 || err != nil || limit <= 0 {
			limit = defaultHistoryLimit
		}
	}
	entries := make([]JournalEntry, 0, historyScan)
	for _, raw := range s.journal.tail(historyScan) {
		var e JournalEntry
		if json.Unmarshal(raw, &e) != nil {
			continue
		}
		entries = append(entries, e)
	}
	got := historyFrom(entries, tab, limit)
	got.Truncated = len(entries) >= historyScan
	got.Source = JournalFromServer
	got.Ends = got.EndsAt()
	s.writeJSON(w, http.StatusOK, got)
}

/* ---------- CLI ---------- */

// History reads one tab's recorded past, from the running board when there is
// one and from the journal file when there is not — the same fallback
// JournalEntries makes, and for the same reason: reading an append-only file
// needs no server, and a session resuming into a stopped board still has to be
// able to ask what happened.
func History(ctx context.Context, root Root, name, tab string, limit int) (TabHistory, error) {
	entries, source, err := JournalEntries(ctx, root, name, historyScan)
	if err != nil {
		return TabHistory{}, err
	}
	got := historyFrom(entries, tab, limit)
	got.Truncated = len(entries) >= historyScan
	got.Source = source
	got.Ends = got.EndsAt()
	return got, nil
}

// Human is the listing the terminal shows.
//
// It ends with where the record ends, always — including when the answer is
// "nothing". A bare empty list reads as "this tab has never changed", which is
// indistinguishable from "everything about it has rotated away", and those call
// for opposite next moves.
func (h TabHistory) Human() string {
	var b strings.Builder
	if len(h.Versions) == 0 {
		fmt.Fprintf(&b, "no recorded history for %s\n", h.Tab)
	} else {
		fmt.Fprintf(&b, "%s — %d recorded version%s, newest first\n", h.Tab, len(h.Versions), plural(len(h.Versions)))
		for _, v := range h.Versions {
			label := v.Name
			if label != "" {
				label = " (" + label + ")"
			}
			fmt.Fprintf(&b, "  %2d  %s  replaced by %-16s %6d bytes%s\n", v.N, v.At, v.By, v.Bytes, label)
		}
		fmt.Fprintf(&b, "\nrestore one with:  aboard history %s --at 1 | aboard apply --by agent-1\n", h.Tab)
	}
	fmt.Fprintf(&b, "\n%s\n", h.EndsAt())
	return b.String()
}

// EndsAt is the sentence about where the record stops. Its own method because
// the JSON form carries the facts and the human form has to say them out loud,
// and a reader of either must reach the same conclusion.
func (h TabHistory) EndsAt() string {
	switch {
	case h.Scanned == 0:
		return "the journal is empty — nothing has been written to this board since it was created, or the journal file was removed"
	case h.Truncated:
		return fmt.Sprintf("history ends here because the scan stopped at %d entries; older writes are in the journal and were not read", h.Scanned)
	default:
		return fmt.Sprintf("history ends at %s — the journal keeps one rotated generation, so nothing before that survives", h.Oldest)
	}
}

// ErrNoSuchVersion is what Restore returns for an --at that names nothing.
var ErrNoSuchVersion = errors.New("no such version")

// Restore builds the document that puts one recorded state back: the CURRENT
// document, whole, with that one tab's state replaced.
//
// Merged onto a fresh read rather than printed on its own, and this is the whole
// risk of the feature. A journal entry holds one tab's `state`; wrapping it as
// `{"tabs":[{"id":…,"state":…}]}` is a legal document that says the board has one
// tab, and `reconcileTabs` would answer it by raising a removal request on every
// other tab the human owns. The restore is a normal write of a normal document
// with one field different, and it carries the fresh document's `rev`, so it is
// refused rather than clobbering if somebody wrote while you were reading.
func Restore(ctx context.Context, root Root, name, tab string, at int, out io.Writer) error {
	got, err := History(ctx, root, name, tab, 0)
	if err != nil {
		return err
	}
	if at <= 0 || at > len(got.Versions) {
		return fmt.Errorf("%w: --at %d, and %s has %d recorded version%s (%s)",
			ErrNoSuchVersion, at, tab, len(got.Versions), plural(len(got.Versions)), got.EndsAt())
	}
	want := got.Versions[at-1]

	raw, err := currentDocument(ctx, root, name)
	if err != nil {
		return err
	}
	doc, err := decodeDocument(raw)
	if err != nil {
		return fmt.Errorf("the current board document does not parse: %w", err)
	}
	i, ok := doc.byID[tab]
	if !ok {
		// Deliberately refused rather than rebuilt. The journal records a tab's
		// NAME and its state, never its `type` — so a tab reconstructed from it
		// would mount as "No renderer for type" and the restore would look like a
		// second failure. Recreating the tab is one gesture in the browser, and
		// then this command works.
		return fmt.Errorf("tab %s is not on the board any more — the journal has its state but not its `type`, "+
			"so this cannot rebuild it: recreate the tab (same id), then run this again", tab)
	}
	doc.tabs[i].State = want.State

	body, err := doc.marshalIndent()
	if err != nil {
		return err
	}
	_, err = out.Write(append(body, '\n'))
	return err
}

// currentDocument reads the board as it stands: from the running server when
// there is one, from the state file when there is not.
//
// The server first, because it is the only reader that cannot see a torn
// document — the file is replaced by rename, and a read that straddles one gets
// a complete but superseded copy. Either way the restore carries a `rev` and a
// stale one is refused by compare-and-set, so the fallback is safe rather than
// merely convenient.
func currentDocument(ctx context.Context, root Root, name string) ([]byte, error) {
	if inst, err := RunningInstance(root, name); err == nil {
		if body, err := fetchDocument(ctx, inst.URL); err == nil {
			return body, nil
		}
	}
	body, err := os.ReadFile(root.StateFile(name))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", root.StateFile(name), err)
	}
	return body, nil
}

// fetchDocument is GET /aboard.json, as bytes.
func fetchDocument(ctx context.Context, base string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/aboard.json", http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("board returned %d for /aboard.json", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
}
