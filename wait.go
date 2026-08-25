// wait.go — the notify channel.
//
//	GET  /wait?for=poke&timeout=600&by=agent-1   block until poked (long poll)
//	POST /poke                                   release every waiting session
//	GET  /waiters                                who is waiting right now
//
// Why this exists rather than an agent polling board.json: a session that wants
// an answer from the human has no way to know when the answer arrived, so it
// either spins in a loop or asks and hopes. Here it blocks on one request, and
// the human's button releases it.
//
// Why the human's click and not a predicate: a wait on "the form is answered"
// fires halfway through a thought — three fields in, mind changed, two more to
// go. An explicit "I am done, go" is a better event than anything an agent can
// infer, so the button is the trigger and the predicate vocabulary can stay
// empty until something actually needs it.
//
// Liveness is the connection itself. A waiter is an open request, so it cannot
// go stale: if the session dies, the TCP connection dies with it and the count
// drops. Nothing to heartbeat, nothing to expire, no registry file to clean up
// — which is the whole reason the button can honestly claim someone is there.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// How long a single -wait may block. A cap exists so a forgotten session cannot
// hold a goroutine and a connection open forever.
const (
	waitDefault = 10 * time.Minute
	waitMax     = time.Hour
)

// pokeEvent is what a released waiter prints on stdout. `event` distinguishes a
// real release from giving up, so a script can branch without parsing prose.
type pokeEvent struct {
	Event string `json:"event"` // "poke" | "timeout"
	At    string `json:"at,omitempty"`
	By    string `json:"by,omitempty"`
	Note  string `json:"note,omitempty"`
}

// waiter is one blocked session. The exported fields are what /waiters reports,
// so the button can say who it would wake, since when, how long it will keep
// waiting, and what it is waiting for.
//
// `Until` is absolute rather than a remaining duration: the browser recomputes
// the countdown locally from it, so the label stays honest between polls instead
// of ageing out. Server and page are on the same machine, so no clock skew.
type waiter struct {
	ID      int64  `json:"id"`
	By      string `json:"by"`
	For     string `json:"for"`
	pred    predicate
	Since   string `json:"since"`
	Until   string `json:"until"`
	Timeout int    `json:"timeout"` // seconds, as asked for
	Note    string `json:"note,omitempty"`

	ch chan pokeEvent
}

type waitHub struct {
	mu     sync.Mutex
	seq    int64
	active map[int64]*waiter
	last   *pokeEvent
}

func newWaitHub() *waitHub {
	return &waitHub{active: map[int64]*waiter{}}
}

func (h *waitHub) add(by, forWhat, note string, pred predicate, timeout time.Duration) *waiter {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	now := time.Now().UTC()
	w := &waiter{
		ID:      h.seq,
		By:      by,
		For:     forWhat,
		pred:    pred,
		Since:   now.Format(time.RFC3339),
		Until:   now.Add(timeout).Format(time.RFC3339),
		Timeout: int(timeout.Seconds()),
		Note:    note,
		// Buffered so release never blocks on a waiter that is already leaving.
		ch: make(chan pokeEvent, 1),
	}
	h.active[w.ID] = w
	return w
}

func (h *waitHub) remove(id int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.active, id)
}

func (h *waitHub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.active)
}

// release wakes everyone and returns how many were actually reached, which is
// what the button reports. Waiters are dropped from the set here rather than
// waiting for each handler to unregister, so two clicks in a row cannot claim
// the same session twice.
func (h *waitHub) release(by, note string) (int, pokeEvent) {
	ev := pokeEvent{
		Event: "poke",
		At:    time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		By:    by,
		Note:  note,
	}
	h.mu.Lock()
	targets := make([]*waiter, 0, len(h.active))
	for id, w := range h.active {
		targets = append(targets, w)
		delete(h.active, id)
	}
	h.last = &ev
	h.mu.Unlock()

	released := 0
	for _, w := range targets {
		select {
		case w.ch <- ev:
			released++
		default:
		}
	}
	return released, ev
}

func (h *waitHub) snapshot() ([]waiter, *pokeEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	list := make([]waiter, 0, len(h.active))
	for _, w := range h.active {
		list = append(list, *w)
	}
	last := h.last
	return list, last
}

/* ---------- predicates ---------- */

// The vocabulary is deliberately tiny. Four forms cover what an agent actually
// waits for, and each one is checkable against a single write:
//
//	poke                the human pressed Notify (or another session -poked)
//	change              any accepted write at all
//	tab bb71            that tab changed
//	answer bb15         that tab changed AND a human made the change
//	node bb58=done      that node reached that status
//
// An unknown form is refused at request time rather than accepted and never
// fired: the caller learns immediately instead of after the timeout. That is the
// whole reason not to grow this into an expression language — every form here can
// fail loudly, and a grammar cannot.
type predicate struct {
	kind  string // "poke" | "change" | "tab" | "answer" | "node"
	id    string
	value string
}

func parsePredicate(raw string) (predicate, error) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return predicate{kind: "poke"}, nil
	}
	switch fields[0] {
	case "poke", "change":
		if len(fields) > 1 {
			return predicate{}, fmt.Errorf("%q takes no argument", fields[0])
		}
		return predicate{kind: fields[0]}, nil
	case "tab", "answer":
		if len(fields) != 2 {
			return predicate{}, fmt.Errorf("%s needs one id, e.g. %q", fields[0], fields[0]+" bb71")
		}
		return predicate{kind: fields[0], id: fields[1]}, nil
	case "node":
		if len(fields) != 2 || !strings.Contains(fields[1], "=") {
			return predicate{}, errors.New(`node needs id=status, e.g. "node bb58=done"`)
		}
		parts := strings.SplitN(fields[1], "=", 2)
		return predicate{kind: "node", id: parts[0], value: parts[1]}, nil
	default:
		return predicate{}, fmt.Errorf("unknown predicate %q — try poke, change, tab <id>, answer <id>, node <id>=<status>", fields[0])
	}
}

// matches reports whether this write is the one the waiter asked about. `doc` is
// the document as written, `entry` the summary of what changed in it.
func (p predicate) matches(doc []byte, entry journalEntry) bool {
	switch p.kind {
	case "poke":
		return false // only an explicit poke releases those
	case "change":
		return len(entry.Tabs) > 0
	case "tab":
		return containsString(entry.Tabs, p.id)
	case "answer":
		// A human answering is the case worth waiting on; an agent rewriting the
		// same tab is not an answer to anything.
		return isHuman(entry.By) && containsString(entry.Tabs, p.id)
	case "node":
		return nodeHasStatus(doc, p.id, p.value)
	}
	return false
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// nodeHasStatus walks the document generically rather than knowing which tab or
// which renderer holds the node — a node lives in a dag, a kanban, or a block
// inside a stack, and a waiter should not have to say which.
func nodeHasStatus(doc []byte, id, status string) bool {
	var root any
	if json.Unmarshal(doc, &root) != nil {
		return false
	}
	found := false
	var walk func(any)
	walk = func(v any) {
		if found {
			return
		}
		switch t := v.(type) {
		case map[string]any:
			if gotID, ok := t["id"].(string); ok && gotID == id {
				if gotStatus, ok := t["status"].(string); ok && gotStatus == status {
					found = true
					return
				}
			}
			for _, child := range t {
				walk(child)
			}
		case []any:
			for _, child := range t {
				walk(child)
			}
		}
	}
	walk(root)
	return found
}

// releaseMatching wakes every session whose predicate this write satisfies.
// Called from the write path, so a waiting agent hears about the change that
// concerns it without polling for it.
func (h *waitHub) releaseMatching(doc []byte, entry journalEntry) int {
	ev := pokeEvent{
		Event: "change",
		At:    entry.At,
		By:    entry.By,
		Note:  joinOr(entry.Tabs, ""),
	}
	h.mu.Lock()
	targets := make([]*waiter, 0, len(h.active))
	for id, w := range h.active {
		if w.pred.matches(doc, entry) {
			targets = append(targets, w)
			delete(h.active, id)
		}
	}
	h.mu.Unlock()

	released := 0
	for _, w := range targets {
		select {
		case w.ch <- ev:
			released++
		default:
		}
	}
	return released
}

/* ---------- endpoints ---------- */

func (s *server) handleWait(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	forWhat := q.Get("for")
	if forWhat == "" {
		forWhat = "poke"
	}
	pred, err := parsePredicate(forWhat)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": err.Error(),
			"got":   forWhat,
		})
		return
	}

	timeout := waitDefault
	if raw := q.Get("timeout"); raw != "" {
		secs, err := strconv.Atoi(raw)
		if err != nil || secs <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "timeout must be whole seconds"})
			return
		}
		timeout = time.Duration(secs) * time.Second
	}
	if timeout > waitMax {
		timeout = waitMax
	}

	by := q.Get("by")
	if by == "" {
		by = "agent"
	}

	// Why this session is waiting, so the button can say more than a name. A
	// waiting agent should always fill this in: "waiting on form 15" is a
	// reason to press the button, a bare label is a mystery.
	note := q.Get("note")
	if len(note) > 140 {
		note = note[:140]
	}

	wt := s.waits.add(by, forWhat, note, pred, timeout)
	s.broadcastWaiters()
	defer func() {
		s.waits.remove(wt.ID)
		s.broadcastWaiters()
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case ev := <-wt.ch:
		writeJSON(w, http.StatusOK, ev)
	case <-timer.C:
		writeJSON(w, http.StatusOK, pokeEvent{Event: "timeout"})
	case <-r.Context().Done():
		// The caller hung up. Nothing to write; the deferred remove drops the
		// waiter and the button's count falls on its own.
	}
}

func (s *server) handlePoke(w http.ResponseWriter, r *http.Request) {
	var body struct {
		By   string `json:"by"`
		Note string `json:"note"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body)
	}
	if body.By == "" {
		body.By = "human"
	}

	released, ev := s.waits.release(body.By, body.Note)
	s.broadcastWaiters()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"released": released,
		"at":       ev.At,
		"by":       ev.By,
	})
}

func (s *server) handleWaiters(w http.ResponseWriter, _ *http.Request) {
	list, last := s.waits.snapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"waiting":  len(list),
		"waiters":  list,
		"lastPoke": last,
	})
}

// The UI enables its notify button from this, so it has to arrive without a
// poll. Sent on the same SSE stream as state changes and told apart by the key:
// a state change carries `origin`, this carries `waiters`.
func (s *server) broadcastWaiters() {
	b, err := json.Marshal(map[string]int{"waiters": s.waits.count()})
	if err != nil {
		return
	}
	s.fanout(string(b))
}

/* ---------- CLI ---------- */

// waitCLI blocks until the board is poked. Exit 0 means poked, 3 means the
// timeout ran out — distinguishable so a script can tell "the human said go"
// from "nobody came".
func waitCLI(name, by, forWhat, note string, timeout time.Duration) (int, error) {
	inst, err := runningInstance(name)
	if err != nil {
		return 1, err
	}
	if timeout <= 0 {
		timeout = waitDefault
	}
	if timeout > waitMax {
		timeout = waitMax
	}

	url := fmt.Sprintf("%s/wait?for=%s&timeout=%d&by=%s&note=%s",
		inst.URL, neturl.QueryEscape(forWhat), int(timeout.Seconds()),
		neturl.QueryEscape(by), neturl.QueryEscape(note))

	// Give the HTTP client more rope than the server's own timeout, so a clean
	// "timeout" answer always beats a client-side abort.
	client := &http.Client{Timeout: timeout + 30*time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return 1, fmt.Errorf("waiting on %s: %w", inst.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	var ev pokeEvent
	if err := json.NewDecoder(resp.Body).Decode(&ev); err != nil {
		return 1, fmt.Errorf("unreadable answer from %s", inst.URL)
	}
	if resp.StatusCode != http.StatusOK {
		return 1, fmt.Errorf("board returned %d", resp.StatusCode)
	}

	out, err := json.Marshal(ev)
	if err != nil {
		return 1, err
	}
	fmt.Println(string(out))

	if ev.Event == "poke" {
		return 0, nil
	}
	return 3, nil
}

// pokeCLI is the same gesture as the button, for an agent that wants to hand
// off to another session without going through the browser.
func pokeCLI(name, by, note string) error {
	inst, err := runningInstance(name)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]string{"by": by, "note": note})
	if err != nil {
		return err
	}
	resp, err := http.Post(inst.URL+"/poke", "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("posting to %s: %w", inst.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	var got struct {
		Released int `json:"released"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("board returned %d", resp.StatusCode)
	}
	fmt.Printf("poked %s — released %d waiting session%s\n",
		inst.URL, got.Released, plural(got.Released))
	return nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
