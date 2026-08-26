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
type JournalEntry struct {
	At     string                     `json:"at"`
	By     string                     `json:"by"`
	Origin string                     `json:"origin,omitempty"`
	Tabs   []string                   `json:"tabs"`
	Names  map[string]string          `json:"names,omitempty"`
	NextID int                        `json:"nextId,omitempty"`
	Before map[string]json.RawMessage `json:"before,omitempty"`
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

// tail returns the last `limit` entries, oldest first. Reads the whole file: at
// the rotation cap this is a few MB, and a journal viewer asks for it once.
func (j *journal) tail(limit int) []json.RawMessage {
	j.mu.Lock()
	path := j.path
	j.mu.Unlock()

	f, err := os.Open(path)
	if err != nil {
		return []json.RawMessage{}
	}
	defer func() { _ = f.Close() }()

	out := make([]json.RawMessage, 0, limit)
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
func changeSummary(currentRaw []byte, next []tab, by, origin string) JournalEntry {
	entry := JournalEntry{
		At:     time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		By:     by,
		Origin: origin,
		Tabs:   []string{},
		Names:  map[string]string{},
		Before: map[string]json.RawMessage{},
	}

	var cur board
	if len(currentRaw) > 0 {
		_ = json.Unmarshal(currentRaw, &cur)
	}
	before := map[string]tab{}
	for i := range cur.Tabs {
		before[cur.Tabs[i].ID] = cur.Tabs[i]
	}

	for i := range next {
		t := &next[i]
		prev, existed := before[t.ID]
		// The same comparison reconcileTabs makes, deliberately: one of them
		// raises the dot on the tab and the other writes the journal line, and a
		// change that gets one without the other is a change the human can see but
		// not trace, or trace but not see.
		//
		// `note` was missing. A note is the human's own sentence about what a tab
		// is for, an agent can overwrite it in a normal write, and the journal —
		// the thing you go to when you ask "who changed this while I was
		// thinking?" — had no record of it at all.
		//
		// `pendingRemoval` was missing too, and it was worse: a write that DROPS a
		// tab changes nothing else about it, so an agent asking to delete
		// something produced a banner on the human's screen and not one line
		// anywhere else. `aboard journal` said nothing, `aboard watch` emitted
		// nothing, and `aboard wait --for "tab bb126"` waited for a change that
		// had already happened. Found by dropping a tab with a bare POST and
		// looking at the journal afterwards.
		if existed && jsonEqual(prev.State, t.State) &&
			prev.Name == t.Name && prev.Type == t.Type &&
			prev.StateFrom == t.StateFrom && prev.Note == t.Note &&
			sameRemovalAsk(prev.PendingRemoval, t.PendingRemoval) {
			continue
		}
		entry.Tabs = append(entry.Tabs, t.ID)
		entry.Names[t.ID] = t.Name
		if existed && len(prev.State) > 0 {
			entry.Before[t.ID] = prev.State
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
const (
	JournalFromServer = "server"
	JournalFromDisk   = "disk"
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
// The source is returned rather than printed here so the caller decides where it
// says so: on stderr in human mode, and nowhere at all in json mode, where a
// prose line would be something for jq to choke on.
func JournalEntries(ctx context.Context, root Root, name string, limit int) ([]JournalEntry, string, error) {
	inst, err := RunningInstance(root, name)
	if err != nil {
		entries, derr := journalFromDisk(root, limit)
		if derr != nil {
			return nil, JournalFromDisk, derr
		}
		return entries, JournalFromDisk, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/journal?limit=%d", inst.URL, limit), http.NoBody)
	if err != nil {
		return nil, JournalFromServer, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, JournalFromServer, fmt.Errorf("reading the journal from %s: %w", inst.URL, err)
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
	for _, e := range entries {
		labels := make([]string, 0, len(e.Tabs))
		for _, id := range e.Tabs {
			if n := e.Names[id]; n != "" {
				labels = append(labels, fmt.Sprintf("%s (%s)", id, n))
				continue
			}
			labels = append(labels, id)
		}
		fmt.Fprintf(&b, "%s  %-16s %s\n", e.At, e.By, joinOr(labels, "no tab changed"))
	}
	return b.String()
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
