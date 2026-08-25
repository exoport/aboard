// journal.go — what changed, who changed it, and what it said before.
//
//	GET /journal?limit=100   recent entries, newest last
//	GET /watch               the same entries as they happen, one JSON line each
//	board journal            print recent entries
//	board watch              follow them
//
// Why: with two sessions and a human writing one document, "who changed the plan
// while I was thinking?" had no answer except git archaeology over a file that
// changes constantly. git gives coarse history of board.json across commits; this
// gives per-write granularity, the author, and — for a tab that changed — the
// state it held BEFORE, which is the unit you would actually want to restore.
//
// Every accepted write already funnels through one function, so this is one
// append at a choke point that cannot be bypassed: an agent that forgets to
// journal is not a thing that can happen.
//
// Bounded from the start, because an append-only file that nobody prunes is the
// same bug as a log tab inside board.json: rotate at a size cap, keep one older
// generation, and store only the tabs a write actually touched.
package aboard

import (
	"bufio"
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
	for _, t := range cur.Tabs {
		before[t.ID] = t
	}

	for _, t := range next {
		prev, existed := before[t.ID]
		if existed && jsonEqual(prev.State, t.State) &&
			prev.Name == t.Name && prev.Type == t.Type && prev.StateFrom == t.StateFrom {
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
	s.mu.Lock()
	targets := make([]chan string, 0, len(s.watchers))
	for ch := range s.watchers {
		targets = append(targets, ch)
	}
	s.mu.Unlock()

	for _, ch := range targets {
		select {
		case ch <- string(body):
		default:
		}
	}
}

/* ---------- CLI ---------- */

// JournalEntries reads recent writes from the running board. Returned rather
// than printed so `--output-format json` and the human form render the same
// values.
func JournalEntries(root Root, name string, limit int) ([]JournalEntry, error) {
	inst, err := RunningInstance(root, name)
	if err != nil {
		return nil, err
	}
	resp, err := http.Get(fmt.Sprintf("%s/journal?limit=%d", inst.URL, limit))
	if err != nil {
		return nil, fmt.Errorf("reading the journal from %s: %w", inst.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	var payload struct {
		Entries []JournalEntry `json:"entries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("unreadable journal: %w", err)
	}
	return payload.Entries, nil
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

// Watch streams every change as JSON lines until the connection ends.
func Watch(root Root, name string, out io.Writer) error {
	inst, err := RunningInstance(root, name)
	if err != nil {
		return err
	}
	resp, err := http.Get(inst.URL + "/watch")
	if err != nil {
		return fmt.Errorf("watching %s: %w", inst.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Unbuffered line-by-line copy, so a shell pipeline sees each change as it
	// happens rather than when a buffer fills.
	scan := bufio.NewScanner(resp.Body)
	scan.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for scan.Scan() {
		fmt.Fprintln(out, scan.Text())
	}
	return scan.Err()
}

func joinOr(items []string, empty string) string {
	if len(items) == 0 {
		return empty
	}
	out := items[0]
	for _, s := range items[1:] {
		out += ", " + s
	}
	return out
}
