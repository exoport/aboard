// journal.go — what changed, who changed it, and what it said before.
//
//	GET /journal?limit=100   recent entries, newest last
//	GET /watch               the same entries as they happen, one JSON line each
//	aboard journal            print recent entries
//	aboard watch              follow them
//
// Why: with two sessions and a human writing one document, "who changed the plan
// while I was thinking?" had no answer except git archaeology over a file that
// changes constantly. git gives coarse history of aboard.json across commits; this
// gives per-write granularity, the author, and — for a tab that changed — the
// state it held BEFORE, which is the unit you would actually want to restore.
//
// Every accepted write already funnels through one function, so this is one
// append at a choke point that cannot be bypassed: an agent that forgets to
// journal is not a thing that can happen.
//
// Bounded from the start, because an append-only file that nobody prunes is the
// same bug as a log tab inside aboard.json: rotate at a size cap, keep one older
// generation, and store only the tabs a write actually touched.

package aboard

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	journalMaxBytes = 16 << 20 // rotate past 16 MiB
	journalKeep     = 1        // how many rotated generations to keep
)

// JournalEntry is one accepted write. `Before` holds the previous state of each
// tab that changed, keyed by tab id — absent for a tab that was created.
//
// `Label` and `Warnings` are the two things a journal entry could never answer.
//
// Label is WHY: an entry said who and what, never what the write was for, so
// "the write that broke the gallery" could not be found in a long journal
// without reading every payload. It is the caller's own line (`apply --label`),
// threaded exactly like `__by` and `__base` — stripped off the payload before the
// document is stored, so it lives on the record of the write and never in the
// board.
//
// Warnings is WHAT WAS WRONG WITH IT: the write-time checks, which until now only
// ever reached the actor who ran the CLI. A browser write, a raw POST, or an
// `apply` whose stderr nobody read all produced an empty box that only the human
// ever found — which is backwards, since the agent is the one still holding the
// context to fix it. Keyed by tab id, and only for the tabs the write touched.
//
// Neither is written into a tab's own `state`, deliberately: the board document
// is the content, and a note about a write is not content. It also means a
// warning cannot be laundered into the record by a later write that copies the
// tab forward.
type JournalEntry struct {
	At     string `json:"at"               yaml:"at"`
	By     string `json:"by"               yaml:"by"`
	Origin string `json:"origin,omitempty" yaml:"origin,omitempty"`
	// Rev is the revision this write PRODUCED — the compare-and-set token the
	// board carried once it landed. Recorded because a reader with a base rev
	// has no other way to ask "which tabs moved since I read the document":
	// `at` is a millisecond clock, which is exactly the token this project
	// stopped trusting (see revision, server.go). Absent — zero — on entries
	// written before this landed, and a reader must treat 0 as "unknown", never
	// as "revision zero".
	Rev      int                        `json:"rev,omitempty"      yaml:"rev,omitempty"`
	Label    string                     `json:"label,omitempty"    yaml:"label,omitempty"`
	Tabs     []string                   `json:"tabs"               yaml:"tabs"`
	Names    map[string]string          `json:"names,omitempty"    yaml:"names,omitempty"`
	NextID   int                        `json:"nextId,omitempty"   yaml:"nextId,omitempty"`
	Warnings map[string][]string        `json:"warnings,omitempty" yaml:"warnings,omitempty"`
	Before   map[string]json.RawMessage `json:"before,omitempty"   yaml:"before,omitempty"`
}

type journal struct {
	mu sync.Mutex
	// dir and path both come from Root, so this file never joins one itself.
	dir  string
	path string
}

func newJournal(root Root) *journal {
	return &journal{dir: root.RunDir(), path: root.JournalFile()}
}

func (j *journal) append(entry JournalEntry) {
	body, err := json.Marshal(entry)
	if err != nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()

	if err := os.MkdirAll(j.dir, 0o755); err != nil {
		return
	}
	if info, err := os.Stat(j.path); err == nil && info.Size() > journalMaxBytes {
		j.rotateLocked()
	}
	f, err := os.OpenFile(j.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.Write(append(body, '\n'))
}

func (j *journal) rotateLocked() {
	for i := journalKeep; i >= 1; i-- {
		older := fmt.Sprintf("%s.%d", j.path, i)
		if i == journalKeep {
			_ = os.Remove(older)
		}
		if i > 1 {
			_ = os.Rename(fmt.Sprintf("%s.%d", j.path, i-1), older)
		}
	}
	_ = os.Rename(j.path, j.path+".1")
}

// tail returns the last `limit` entries, oldest first, across the kept
// generations.
//
// It read only journal.jsonl, so the whole point of keeping a generation was
// lost: the instant the file rotated, `aboard journal --limit 40` on a board
// that had just written its first entry showed ONE line and the other forty
// sat readable in journal.jsonl.1 with nothing willing to open them. Rotation
// existed to bound the file, not to make history disappear at 16 MiB.
//
// Oldest generation first, so the concatenation is in time order and the
// existing "keep the last `limit`" trim still means what it says. Reads the
// whole of both: at the cap that is ~32 MB worst case, and a journal viewer
// asks once.
func (j *journal) tail(limit int) []json.RawMessage {
	j.mu.Lock()
	path := j.path
	j.mu.Unlock()

	// Oldest kept generation first, then the live file.
	paths := make([]string, 0, journalKeep+1)
	for i := journalKeep; i >= 1; i-- {
		paths = append(paths, fmt.Sprintf("%s.%d", path, i))
	}
	paths = append(paths, path)

	out := make([]json.RawMessage, 0, limit)
	for _, p := range paths {
		out = appendJournalLines(out, p, limit)
	}
	return out
}

// appendJournalLines reads one generation onto `out`, keeping at most `limit`.
// A missing file is not an error: journal.jsonl.1 does not exist until the first
// rotation, which is the normal case for the life of most boards.
func appendJournalLines(out []json.RawMessage, path string, limit int) []json.RawMessage {
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer func() { _ = f.Close() }()

	// 8 MiB, not maxJournalLine (4 MiB): unifying them would SHRINK this reader,
	// and a scanner that hits its cap stops silently. Whatever is already on disk
	// stays readable.
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for scan.Scan() {
		line := scan.Bytes()
		if len(line) == 0 {
			continue
		}
		cp := make([]byte, len(line))
		copy(cp, line)
		out = append(out, cp)
		if limit > 0 && len(out) > limit {
			out = out[1:]
		}
	}
	return out
}

/* ---------- what changed ---------- */

// changeSummary compares the document on disk with the one about to replace it.
// Reported by /watch, recorded in the journal, and evaluated against waiting
// sessions' predicates — three features off one comparison.
//
// The byte-level form, kept because the tests state "the marker and the journal
// agree on what changed" against two documents. The write path does not call it:
// reconcileDoc has already made this comparison and recorded the answer on each
// tab, so summarise only has to read the flag. Making it twice cost a full
// canonicalisation of every tab's state on the board, per write.
func changeSummary(currentRaw []byte, next []tab, by, origin string) JournalEntry {
	cur, err := decodeDocument(currentRaw)
	if err != nil {
		cur = emptyDoc()
	}
	tabs := make([]docTab, len(next))
	for i := range next {
		tabs[i] = docTab{tab: next[i], maxID: -1}
		tabs[i].changed = changedAgainst(cur, &tabs[i])
	}
	return summarise(cur, tabs, by, origin)
}

// changedAgainst is the journal's question about one tab.
//
// `note` was missing from it. A note is the human's own sentence about what a tab
// is for, an agent can overwrite it in a normal write, and the journal — the thing
// you go to when you ask "who changed this while I was thinking?" — had no record
// of it at all.
//
// `pendingRemoval` was missing too, and it was worse: a write that DROPS a tab
// changes nothing else about it, so an agent asking to delete something produced
// a banner on the human's screen and not one line anywhere else. `aboard journal`
// said nothing, `aboard watch` emitted nothing, and `aboard wait --for "tab bb126"`
// waited for a change that had already happened. Found by dropping a tab with a
// bare POST and looking at the journal afterwards.
func changedAgainst(cur *stateDoc, t *docTab) bool {
	j, existed := cur.byID[t.ID]
	if !existed {
		return true
	}
	prev := &cur.tabs[j]
	return !sameState(prev, t) ||
		prev.Name != t.Name ||
		prev.Type != t.Type ||
		prev.StateFrom != t.StateFrom ||
		prev.Note != t.Note ||
		!sameRemovalAsk(prev.PendingRemoval, t.PendingRemoval)
}

// summarise turns an already-made comparison into the entry its three consumers
// share. `Before` holds the previous state of each changed tab, which is the unit
// somebody restoring by hand would actually want.
func summarise(cur *stateDoc, next []docTab, by, origin string) JournalEntry {
	entry := JournalEntry{
		At:     time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		By:     by,
		Origin: origin,
		Tabs:   []string{},
		Names:  map[string]string{},
		Before: map[string]json.RawMessage{},
	}
	for i := range next {
		t := &next[i]
		if !t.changed {
			continue
		}
		entry.Tabs = append(entry.Tabs, t.ID)
		entry.Names[t.ID] = t.Name
		if j, existed := cur.byID[t.ID]; existed && len(cur.tabs[j].State) > 0 {
			entry.Before[t.ID] = cur.tabs[j].State
		}
	}
	return entry
}

// sameRemovalAsk compares two removal requests, either of which may be absent.
// Absent-to-present and present-to-absent are both changes: the first is an
// agent asking, the second is the human answering, and both belong in the
// record.
func sameRemovalAsk(a, b *removalAsk) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

/* ---------- endpoints ---------- */

func (s *server) handleJournal(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := fmt.Sscanf(raw, "%d", &limit); n != 1 || err != nil || limit <= 0 {
			limit = 100
		}
	}
	// Entries are already JSON; splice them rather than decode and re-encode, so
	// a viewer sees exactly what was recorded.
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, `{"entries":[`)
	for i, raw := range s.journal.tail(limit) {
		if i > 0 {
			_, _ = io.WriteString(w, ",")
		}
		_, _ = w.Write(raw)
	}
	_, _ = io.WriteString(w, `]}`)
}

// handleWatch streams change summaries as JSON lines until the caller hangs up.
// Not SSE: the consumer is a shell pipeline, and `data: ` prefixes would just be
// something for jq to strip.
func (s *server) handleWatch(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch := make(chan string, 8)
	s.mu.Lock()
	s.watchers[ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.watchers, ch)
		close(ch)
		s.mu.Unlock()
	}()

	for {
		select {
		case <-r.Context().Done():
			return
		case line := <-ch:
			if _, err := fmt.Fprintln(w, line); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// notifyWatchers pushes one change to every -watch consumer. Non-blocking: a
// wedged reader must not stall a write.
func (s *server) notifyWatchers(entry JournalEntry) {
	// The previous states are for the journal on disk, not for a live stream —
	// a watcher wants to know THAT something changed, then re-read the board.
	slim := entry
	slim.Before = nil
	body, err := json.Marshal(slim)
	if err != nil {
		return
	}
	// Under the lock, exactly as fanout does and for the same reason: handleWatch
	// unsubscribes by deleting its channel and closing it, so sending to a copy
	// taken before the lock was released is a send on a closed channel — a panic,
	// here on the goroutine of whoever just wrote to the board.
	s.mu.Lock()
	defer s.mu.Unlock()

	for ch := range s.watchers {
		select {
		case ch <- string(body):
		default:
		}
	}
}

/* ---------- CLI ---------- */

// Where a journal listing came from, reported so the human form can say.
//
// Two disk cases, not one, because they are different sentences to a reader.
// JournalFromDisk is "nothing is running here", which is ordinary.
// JournalFromDiskStale is "there is a record of a board and it does not answer" —
// a crashed server, a stale instance file — and that is worth saying out loud,
// because the next command the reader runs will hit the same dead port.
const (
	JournalFromServer    = "server"
	JournalFromDisk      = "disk"
	JournalFromDiskStale = "disk-stale"
)

// JournalEntries reads recent writes, from the running board if there is one and
// from the file on disk if there is not.
//
// The disk fallback exists because of the resume protocol: status, capabilities,
// journal, in that order, as the first three commands a session runs after a
// context clear. The first two answer with no server — that is the whole point of
// them — and the third used to exit 1, so the documented sequence failed in any
// project whose board happened to be stopped. The journal is an append-only FILE;
// nothing about reading it needs a server, exactly as `export` needs none.
//
// The fallback covers the DEAD BOARD as well as the absent one. It used to fire
// only when the instance FILE was unreadable, so the commonest real case — a
// server that crashed or was killed, leaving its record behind — dialled a port
// nobody was listening on and exited 1 with a connection error, while
// journal.jsonl sat readable beside it. That is the third command of the resume
// protocol failing in exactly the situation a session resumes into.
//
// The source is returned rather than printed here so the caller decides where it
// says so: on stderr in human mode, and nowhere at all in json mode, where a
// prose line would be something for jq to choke on.
func JournalEntries(ctx context.Context, root Root, name string, limit int) ([]JournalEntry, string, error) {
	inst, err := RunningInstance(root, name)
	if err != nil {
		return journalDiskAnswer(root, limit, JournalFromDisk)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/journal?limit=%d", inst.URL, limit), http.NoBody)
	if err != nil {
		return nil, JournalFromServer, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// A cancelled context is not a dead board — it is the caller leaving —
		// and answering it from disk would report success for a command the user
		// interrupted. Everything else is the transport failing: the record
		// points somewhere nothing answers, so read the file instead and say
		// which it was, because hiding it leaves the reader believing a dead
		// board is alive.
		if ctx.Err() != nil {
			return nil, JournalFromServer, fmt.Errorf("reading the journal from %s: %w", inst.URL, err)
		}
		return journalDiskAnswer(root, limit, JournalFromDiskStale)
	}
	defer func() { _ = resp.Body.Close() }()

	var payload struct {
		Entries []JournalEntry `json:"entries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, JournalFromServer, fmt.Errorf("unreadable journal: %w", err)
	}
	return payload.Entries, JournalFromServer, nil
}

// journalDiskAnswer is the disk read plus its label, so both fallbacks return
// the same shape and neither can forget to say where the answer came from.
func journalDiskAnswer(root Root, limit int, source string) ([]JournalEntry, string, error) {
	entries, err := journalFromDisk(root, limit)
	if err != nil {
		return nil, source, err
	}
	return entries, source, nil
}

// maxJournalLine caps one journal line. An entry is a summary — ids and a
// sentence — so anything near this is a corrupt file, not a long day.
const maxJournalLine = 4 << 20

// journalFromDisk reads the same file the server appends to, through the same
// tail() the endpoint uses — so the two answers cannot differ in shape, only in
// how fresh they are.
//
// A journal file that does not exist yet is an empty list, not an error: a board
// that has never been written to has nothing to report, and that is an answer.
func journalFromDisk(root Root, limit int) ([]JournalEntry, error) {
	raw := newJournal(root).tail(limit)
	out := make([]JournalEntry, 0, len(raw))
	for i, line := range raw {
		var entry JournalEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return nil, fmt.Errorf("%s line %d is not a journal entry: %w", root.JournalFile(), i+1, err)
		}
		out = append(out, entry)
	}
	return out, nil
}

// JournalHuman is the one-line-per-write form the terminal shows.
func JournalHuman(entries []JournalEntry) string {
	if len(entries) == 0 {
		return "no journal entries yet\n"
	}
	var b strings.Builder
	// Indexed, not copied: JournalEntry carries three maps and a slice now, and
	// a value range copies all of it per entry for a loop that only reads.
	for i := range entries {
		e := &entries[i]
		labels := make([]string, 0, len(e.Tabs))
		for _, id := range e.Tabs {
			if n := e.Names[id]; n != "" {
				labels = append(labels, fmt.Sprintf("%s (%s)", id, n))
				continue
			}
			labels = append(labels, id)
		}
		fmt.Fprintf(&b, "%s  %-16s %s\n", e.At, e.By, joinOr(labels, "no tab changed"))
		// Both continuation lines are indented under the write they belong to,
		// because the entry line is already three columns wide and a label or a
		// warning appended to it would push the tab list off the terminal — the
		// column a reader scans.
		if e.Label != "" {
			fmt.Fprintf(&b, "%*s  %s\n", len(e.At), "", e.Label)
		}
		for _, id := range sortedWarningTabs(e.Warnings) {
			for _, w := range e.Warnings[id] {
				fmt.Fprintf(&b, "%*s  ⚠ %s\n", len(e.At), "", w)
			}
		}
	}
	return b.String()
}

// sortedWarningTabs orders the warning groups so two runs of `aboard journal`
// print the same thing. Map order is randomised in Go, and an output that
// reshuffles itself between runs is one a reader cannot diff.
func sortedWarningTabs(warnings map[string][]string) []string {
	ids := make([]string, 0, len(warnings))
	for id := range warnings {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Watch streams every change as JSON lines until the connection ends or the
// context is cancelled.
//
// The context is the whole point of the signature. /watch is a stream that never
// closes by design, so a plain http.Get blocked in Read forever and Ctrl-C did
// nothing: the signal cancelled a context nobody had handed to the request, and
// the process sat there until it was killed. `timeout -s INT 3 aboard watch` was
// still alive at five seconds.
//
// Cancellation is a CLEAN exit here, not a failure — the caller asked it to
// stop — so the context errors are swallowed rather than reported as a broken
// connection.
func Watch(ctx context.Context, root Root, name string, out io.Writer) error {
	inst, err := RunningInstance(root, name)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, inst.URL+"/watch", http.NoBody)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("watching %s: %w", inst.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Unbuffered line-by-line copy, so a shell pipeline sees each change as it
	// happens rather than when a buffer fills.
	scan := bufio.NewScanner(resp.Body)
	scan.Buffer(make([]byte, 0, 64<<10), maxJournalLine)
	for scan.Scan() {
		fmt.Fprintln(out, scan.Text())
	}
	if ctx.Err() != nil {
		return nil
	}
	return scan.Err()
}

func joinOr(items []string, empty string) string {
	if len(items) == 0 {
		return empty
	}
	return strings.Join(items, ", ")
}
