// receipts.go — what the browser reports it actually drew.
//
//	POST /rendered                  one tab's mount receipt, from aboard.html
//	aboard rendered [tab]           print what was recorded
//	aboard wait --for "rendered <id>"
//
// Nothing today tells a session what the browser DID with the document it wrote.
// `aboard apply` prints `applied` and exits 0 for a tree that draws an empty box,
// so the two symptoms are an agent declaring a tab ready and the human finding it
// wrong, and a hand-kept "genuinely unproven" list that nothing updates on its own.
//
// **This is not the DOM sweep**, which was measured and abandoned on the spike
// (collect every `button[title]`, match it against free-text `gestures`, ~4 real
// gaps in 23 candidates — a check with that ratio gets muted). What is recorded
// here is ids that are ALREADY machine-declared in views/*.spec.json: the control
// ids a renderer drew, the ones a human actually pressed, and the markers a
// renderer put on screen because it did not recognise something. Nothing is
// scraped and nothing is matched against prose.
//
// Sidecar, under .aboard/run/, never in the state document — per-viewer state,
// the same rule that keeps selection, zoom and chat drafts out of the board. It
// REPORTS; it does not act, and it never writes a tab.
//
// Two limits are printed by the command itself rather than only noted here,
// because they are what stops a receipt being read as a proof:
//
//   - no receipt means nobody had the tab OPEN, not that it failed to render;
//   - a recorded control was REACHED, never that it behaved correctly.

package aboard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// maxReceiptBody caps one posted receipt: a handful of short ids.
const maxReceiptBody = 64 << 10

// maxReceiptIDs caps how many distinct ids one tab's receipt remembers. A tab
// whose renderer draws a control per row could otherwise grow this file without
// bound, and the answer "these ids were drawn" stops being useful long before
// then anyway.
const maxReceiptIDs = 200

// Receipt is one tab as the browser last drew it.
type Receipt struct {
	Tab  string `json:"tab"            yaml:"tab"`
	Type string `json:"type,omitempty" yaml:"type,omitempty"`
	At   string `json:"at"             yaml:"at"`
	// Mounts is how many times the shell has mounted this tab since the file
	// was created. A tab that mounts and never gains a control is a different
	// story from one nobody has opened.
	Mounts int `json:"mounts" yaml:"mounts"`
	// Mount is INPUT ONLY: whether this post is a mount or a press report. Two
	// reasons a receipt is sent and only one of them is a mount, so counting
	// every post as one would make "mounted 9×" mean "somebody clicked 8 times".
	Mount bool `json:"mount,omitempty" yaml:"-"`
	// Controls are the DECLARED control ids present on screen at the last mount.
	Controls []string `json:"controls,omitempty" yaml:"controls,omitempty"`
	// Undeclared are control ids the renderer built that no spec declares —
	// `controlsFor` draws them as `?id` with data-undeclared, and this is that
	// marker reaching the agent instead of only the human.
	Undeclared []string `json:"undeclared,omitempty" yaml:"undeclared,omitempty"`
	// Unknown are the `ui` markers: a component type the catalog does not hold,
	// or a prop the renderer refused. The gallery's deliberate `sparkline` is
	// expected to appear here forever; that is the marker working.
	Unknown []string `json:"unknown,omitempty" yaml:"unknown,omitempty"`
	// Fired counts control ids somebody actually invoked, id -> times.
	Fired map[string]int `json:"fired,omitempty" yaml:"fired,omitempty"`
}

// receiptStore is the sidecar file, read and rewritten whole under one lock.
//
// Whole-file rather than append-only, unlike the journal: this is a CURRENT
// picture keyed by tab, not a history, and an append-only version of it would
// need compaction to answer the only question anybody asks of it.
type receiptStore struct {
	mu   sync.Mutex
	dir  string
	path string
}

func newReceiptStore(root Root, name string) *receiptStore {
	return &receiptStore{dir: root.RunDir(), path: root.RenderedFile(name)}
}

// record folds one posted receipt into the file and returns the merged row.
func (r *receiptStore) record(in Receipt) Receipt {
	r.mu.Lock()
	defer r.mu.Unlock()

	all := readReceipts(r.path)
	prev := all[in.Tab]

	mounts := prev.Mounts
	if in.Mount {
		mounts++
	}
	out := Receipt{
		Tab:        in.Tab,
		Type:       firstNonEmpty(in.Type, prev.Type),
		At:         time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		Mounts:     mounts,
		Controls:   capIDs(in.Controls),
		Undeclared: capIDs(in.Undeclared),
		Unknown:    capIDs(in.Unknown),
		Fired:      map[string]int{},
	}
	// Controls, undeclared and unknown describe THIS mount and are replaced:
	// a marker that was fixed must stop being reported, or the receipt becomes a
	// list of things that were once wrong. Fired counts accumulate, because a
	// press that happened did happen.
	maps.Copy(out.Fired, prev.Fired)
	for id, n := range in.Fired {
		if n <= 0 {
			continue
		}
		if _, seen := out.Fired[id]; !seen && len(out.Fired) >= maxReceiptIDs {
			continue
		}
		out.Fired[id] += n
	}
	if len(out.Fired) == 0 {
		out.Fired = nil
	}
	all[in.Tab] = out

	if err := os.MkdirAll(r.dir, 0o755); err == nil {
		if body, err := json.MarshalIndent(all, "", "  "); err == nil {
			//nolint:gosec // 0o644 is the board's repo-wide file-mode policy; see the note in init.go
			_ = os.WriteFile(r.path, append(body, '\n'), 0o644)
		}
	}
	return out
}

// readReceipts reads the sidecar. A file that does not exist is an empty set,
// not an error: no viewer has reported anything yet, which is an answer.
func readReceipts(path string) map[string]Receipt {
	out := map[string]Receipt{}
	body, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	if json.Unmarshal(body, &out) != nil {
		return map[string]Receipt{}
	}
	return out
}

// capIDs bounds and sorts an incoming id list, and drops anything that is not a
// plain identifier. The ids come from a page, so they are input: this file is
// read back by a terminal, and a control id is a short token by construction.
func capIDs(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, raw := range in {
		id := strings.TrimSpace(raw)
		if id == "" || len(id) > 80 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
		if len(out) >= maxReceiptIDs {
			break
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

/* ---------- endpoint ---------- */

func (s *server) handleRendered(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxReceiptBody))
	if err != nil {
		s.writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{wireError: "receipt too large"})
		return
	}
	var in Receipt
	if len(body) == 0 || json.Unmarshal(body, &in) != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{wireError: "expected a mount receipt"})
		return
	}
	// The tab id becomes a KEY in a file a terminal prints, and it is the string
	// a `rendered <id>` predicate is compared against. Validated with the same
	// expression a sidecar log's filename is, so one rule covers both.
	if !validTabFileID(in.Tab) {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{wireError: msgTabPlainID})
		return
	}
	got := s.receipts.record(in)

	// A session can block until a tab is actually drawn. Released here rather
	// than from the write path, because a receipt is not a write: nothing about
	// the board document changed, and folding it into the journal would put
	// per-viewer state into the record of what the board says.
	if released := s.waits.releaseRendered(in.Tab); released > 0 {
		s.opts.Log().Printf("released %d waiting session(s): %s rendered", released, in.Tab)
		s.broadcastWaiters()
	}
	s.writeJSON(w, http.StatusOK, map[string]any{wireOK: true, "mounts": got.Mounts})
}

/* ---------- CLI ---------- */

// Rendered reads the receipts for one tab, or for every tab when `tab` is empty.
//
// Straight off the sidecar file, with no server involved: the file is written by
// the server but READING it is a file read, exactly as `journal` and `export`
// are. A session asking "did the browser draw this" after the board was stopped
// is asking a question the answer to which is already on disk.
func Rendered(_ context.Context, root Root, name, tab string) ([]Receipt, error) {
	all := readReceipts(root.RenderedFile(name))
	out := []Receipt{}
	for _, id := range sortedKeysOf(all) {
		if tab != "" && id != tab {
			continue
		}
		out = append(out, all[id])
	}
	return out, nil
}

func sortedKeysOf(m map[string]Receipt) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// RenderedHuman prints the receipts, and then the two things a receipt does not
// prove. The limits are in the OUTPUT and not only in the docs because this is a
// command whose whole product is a claim about evidence, and a reader who takes
// it for more than it is will stop looking at the board.
func RenderedHuman(tab string, list []Receipt) string {
	var b strings.Builder
	if len(list) == 0 {
		if tab != "" {
			fmt.Fprintf(&b, "no mount receipt for %s\n", tab)
		} else {
			b.WriteString("no mount receipts on this board\n")
		}
	}
	for i := range list {
		r := &list[i]
		fmt.Fprintf(&b, "%s  %s  mounted %d×  last %s\n", r.Tab, orDash(r.Type), r.Mounts, r.At)
		fmt.Fprintf(&b, "  controls drawn : %s\n", joinOr(r.Controls, "none"))
		if len(r.Undeclared) > 0 {
			fmt.Fprintf(&b, "  UNDECLARED     : %s  (rendered as \"?id\" — declare it in views/<type>.spec.json)\n",
				strings.Join(r.Undeclared, ", "))
		}
		if len(r.Unknown) > 0 {
			fmt.Fprintf(&b, "  UNKNOWN MARKER : %s  (the renderer drew a marker instead of the thing)\n",
				strings.Join(r.Unknown, ", "))
		}
		if len(r.Fired) > 0 {
			fmt.Fprintf(&b, "  pressed        : %s\n", firedList(r.Fired))
		}
	}
	b.WriteString("\nWhat this is not evidence of, both of them on purpose:\n")
	b.WriteString("  · no receipt means nobody had the tab OPEN in a browser — not that it failed to render.\n")
	b.WriteString("  · a control listed here was REACHED. It says nothing about whether it behaved correctly.\n")
	return b.String()
}

func firedList(fired map[string]int) string {
	ids := make([]string, 0, len(fired))
	for id := range fired {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, fmt.Sprintf("%s ×%d", id, fired[id]))
	}
	return strings.Join(out, ", ")
}
