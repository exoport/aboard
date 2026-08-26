// requests.go — the human's notes to an agent, from the terminal.
//
//	aboard requests                      what is waiting on you, oldest first
//	aboard requests --tab bb14           just that tab's
//	aboard requests --all                the done ones too
//	aboard requests done bb199 --by agent-1 --note "redrew it"
//
// Everything else on a board flows one way: an agent shows the human something
// and reads back what they changed. A request is the other direction — the human
// pointing at a tab and saying "this is wrong, fix it" — and it needs a channel
// an agent can find without being told where to look, because the whole point is
// that it arrives while nobody is watching for it.
//
// So it is a LIST command with no argument, and `aboard status` prints the count:
// the two things a resuming session already runs. A request nobody discovers is a
// request that was not made.
//
// The listing needs no server (it falls back to the state file); stamping one
// does, for the same reason `apply` does — a direct write has no compare-and-set,
// and this write lands on a document the human is editing in a browser at the
// same moment, by construction.

package aboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// Request is one of the human's notes, as the CLI reports it: the note itself,
// plus which tab it is about.
//
// The tab's NAME travels with its id and that is not decoration. An id is enough
// coming from the human and not enough going to them — and it is not enough going
// to an agent either, mid-task, three tabs into a board it opened yesterday.
// "Architecture (bb14)" is a thing to act on; "bb14" is a lookup.
type Request struct {
	ID      string        `json:"id"                yaml:"id"`
	Tab     string        `json:"tab"               yaml:"tab"`
	TabName string        `json:"tabName,omitempty" yaml:"tabName,omitempty"`
	At      string        `json:"at,omitempty"      yaml:"at,omitempty"`
	By      string        `json:"by,omitempty"      yaml:"by,omitempty"`
	Text    string        `json:"text"              yaml:"text"`
	Done    *RequestStamp `json:"done,omitempty"    yaml:"done,omitempty"`
}

// RequestStamp is an agent saying it acted: who, when, and optionally a line
// about what it did. The human reads it struck through the note on the board.
type RequestStamp struct {
	By   string `json:"by"             yaml:"by"`
	At   string `json:"at"             yaml:"at"`
	Note string `json:"note,omitempty" yaml:"note,omitempty"`
}

// requestDoc is the shape both readers below want out of a board document: tabs
// with their requests and nothing else. Declared once rather than inline twice,
// because the two would then be free to disagree about a field name.
type requestDoc struct {
	Tabs []struct {
		ID       string       `json:"id"`
		Name     string       `json:"name"`
		Requests []requestAsk `json:"requests"`
	} `json:"tabs"`
}

// ListRequests reads the board and returns the human's notes, oldest first.
//
// `tab` narrows to one tab; `all` includes the ones already stamped done. The
// default is PENDING ONLY, because the question this command answers is "is
// anything waiting on me" and a list that grows for ever answers it worse every
// week.
func ListRequests(ctx context.Context, root Root, name, tab string, all bool) ([]Request, error) {
	body, err := currentDocument(ctx, root, name)
	if err != nil {
		return nil, err
	}
	var doc requestDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("the board document could not be read: %w", err)
	}

	out := []Request{}
	for _, t := range doc.Tabs {
		if tab != "" && t.ID != tab && t.Name != tab {
			continue
		}
		for _, ask := range t.Requests {
			if ask.Done != nil && !all {
				continue
			}
			entry := Request{ID: ask.ID, Tab: t.ID, TabName: t.Name, At: ask.At, By: ask.By, Text: ask.Text}
			if ask.Done != nil {
				entry.Done = &RequestStamp{By: ask.Done.By, At: ask.Done.At, Note: ask.Done.Note}
			}
			out = append(out, entry)
		}
	}
	// Oldest first: the order they were asked in is the order to work through
	// them in, and it is the opposite of the journal's, which is newest-first
	// because that one is a log. `at` sorts lexically because it is RFC 3339 in
	// UTC; the id is the tie-break, and it is monotonic, so two notes written in
	// the same second still come back in the order they were typed.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].At != out[j].At {
			return out[i].At < out[j].At
		}
		return idCounter(out[i].ID) < idCounter(out[j].ID)
	})
	return out, nil
}

// PendingRequests counts the notes still waiting, for `aboard status`.
//
// It reads the state file directly and answers 0 for anything it cannot read: it
// is one line of a report about something else, and a status command that fails
// because it could not count something would be a worse command than one that
// undercounts.
func PendingRequests(root Root, name string) int {
	body, err := os.ReadFile(root.StateFile(name))
	if err != nil {
		return 0
	}
	var doc requestDoc
	if json.Unmarshal(body, &doc) != nil {
		return 0
	}
	pending := 0
	for _, t := range doc.Tabs {
		for _, ask := range t.Requests {
			if ask.Done == nil {
				pending++
			}
		}
	}
	return pending
}

// RequestsHuman renders the listing the way the terminal shows it.
//
// Two lines per note, the text on its own: a request is a SENTENCE the human
// wrote, and squeezing it into a column beside four other fields is how it ends
// up truncated in the one command whose job is to deliver it.
func RequestsHuman(list []Request, tab string, all bool, name string) string {
	if len(list) == 0 {
		where := "on this board"
		if tab != "" {
			where = "on " + tab
		}
		if all {
			return fmt.Sprintf("no requests %s\n", where)
		}
		return fmt.Sprintf("nothing pending %s — the human has not asked for anything\n", where)
	}

	var b strings.Builder
	pending := 0
	for i := range list {
		if list[i].Done == nil {
			pending++
		}
	}
	fmt.Fprintf(&b, "%d pending, %d listed\n", pending, len(list))
	for _, r := range list {
		head := r.ID + "  " + orUnnamed(r.TabName) + " (" + r.Tab + ")"
		if r.At != "" {
			head += "  " + r.At
		}
		if r.Done != nil {
			head += "  ✓ " + r.Done.By
			if r.Done.At != "" {
				head += " · " + r.Done.At
			}
			if r.Done.Note != "" {
				head += " · " + r.Done.Note
			}
		}
		fmt.Fprintf(&b, "  %s\n      %s\n", head, r.Text)
	}
	if pending > 0 {
		// A command in output is a claim, and this one is spliced with --name for
		// the same reason the change banner's restore line is: the left half reads
		// one board and the right half writes whichever it was told about, so a
		// half-qualified line stamps the right note on the wrong board.
		fmt.Fprintf(&b, "\nsay you did one with:  aboard requests done %s%s --by agent-1 --note \"what you did\"\n",
			firstPending(list), nameFlagFor(name))
	}
	return b.String()
}

func orUnnamed(s string) string {
	if s == "" {
		return "(unnamed)"
	}
	return s
}

func firstPending(list []Request) string {
	for _, r := range list {
		if r.Done == nil {
			return r.ID
		}
	}
	return "<request-id>"
}

// nameFlagFor is ` --name <board>` for a named board and nothing for the default
// one, for splicing into a command that output is telling somebody to run.
//
// Shared with `aboard history`, whose restore line found this first: BOTH halves
// of a pipeline need it and it is easy to carry into only the first, so the right
// document lands on the wrong board.
func nameFlagFor(name string) string {
	if name == "" {
		return ""
	}
	return " --name " + name
}

// CompleteRequest stamps one request done and writes it back through the running
// board.
//
// A thin `apply`: read the whole document, change one field, post it with the
// `rev` it came with. It does NOT merge on a 409 — that path exists for a caller
// who built a document and can rebuild it, and this one can simply be run again,
// which is a better answer than a merge nobody asked for.
func CompleteRequest(ctx context.Context, root Root, name, id, by, note string, out io.Writer) error {
	if by == actorHuman {
		// The same refusal `apply` makes, and for the same reason: "human" is the
		// key every guarantee turns on, and a request the human stamped for
		// themselves is not feedback, it is a record with nobody in it.
		return errors.New(`--by human is refused: a done stamp says which SESSION acted on the note, and the human answers their own requests by deleting them. Use agent-1, agent-2 or agent-<role>`)
	}

	inst, err := RunningInstance(root, name)
	if err != nil {
		return err
	}
	body, err := fetchDocument(ctx, inst.URL)
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("the board document could not be read: %w", err)
	}

	tabID, already, err := stampRequest(doc, id, by, note)
	if err != nil {
		return err
	}
	if already != nil {
		// Not an error. Re-running a command that has already had its effect is
		// how scripts are written, and an agent stamping a note twice has done
		// nothing wrong — the second stamp is refused by guarantee 5 anyway, so
		// saying so here is the honest version of the same answer.
		fmt.Fprintf(out, "%s was already stamped by %s at %s — nothing written\n", id, already.By, already.At)
		return nil
	}

	code, got, err := postDocument(ctx, inst.URL, doc, applyBase(doc), by, "requests done "+id)
	if err != nil {
		return err
	}
	switch code {
	case http.StatusOK:
		fmt.Fprintf(out, "%s stamped done by %s on %s\n", id, by, tabID)
		return nil
	case http.StatusConflict:
		return fmt.Errorf("the board changed while this was being stamped (%s) — run the same command again", strings.TrimSpace(got))
	default:
		return fmt.Errorf("board returned %d: %s", code, strings.TrimSpace(got))
	}
}

// stampRequest finds one request in a decoded document and adds a `done` to it.
//
// It works on the map rather than on the typed document on purpose: this write
// posts back everything it read, and decoding through the engine's own structs
// would drop any root key or tab field they do not know about — which is exactly
// the shape of bug that makes a small command dangerous on someone else's board.
func stampRequest(doc map[string]any, id, by, note string) (tabID string, already *RequestStamp, err error) {
	tabs, _ := doc[keyTabs].([]any)
	for _, raw := range tabs {
		tab, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		asks, _ := tab[keyRequests].([]any)
		for _, rawAsk := range asks {
			ask, ok := rawAsk.(map[string]any)
			if !ok || ask[idKey] != id {
				continue
			}
			owner, _ := tab[idKey].(string)
			if done, ok := ask["done"].(map[string]any); ok && done != nil {
				stamp := &RequestStamp{}
				stamp.By, _ = done[keyBy].(string)
				stamp.At, _ = done[keyAt].(string)
				stamp.Note, _ = done[keyNote].(string)
				return owner, stamp, nil
			}
			done := map[string]any{
				keyBy: by,
				keyAt: time.Now().UTC().Format(time.RFC3339),
			}
			if note != "" {
				done[keyNote] = note
			}
			ask["done"] = done
			return owner, nil, nil
		}
	}
	return "", nil, fmt.Errorf("no request %q on this board — `aboard requests --all` lists them, and the id is the one printed beside the note", id)
}
