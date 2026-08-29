package aboard

import (
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// The write path under simultaneous callers, and the SSE/watch fanout under a
// client that hangs up mid-broadcast. Both were reproduced against d6c2f84 and
// both are invisible to single-session use, which is why they survived: the
// first needs two writers inside one file-write, the second needs a
// disconnection inside the window between copying the subscriber list and
// sending to it.

// N concurrent posts off ONE base document, released together.
//
// Before the write lock, the read → compare-and-set → reconcile → write span had
// no mutual exclusion at all: every racer read the same `updatedAt`, every CAS
// passed, every writer got 200, and the last rename won. The losers were told
// their write had landed and the journal recorded it as if it had — the one
// failure mode a compare-and-set exists to prevent, reported as success.
func TestConcurrentPostsProduceExactlyOneWinner(t *testing.T) {
	const writers = 12

	srv := testServer(t, twoTabs)

	type result struct {
		code int
		text string
	}
	results := make([]result, writers)

	start := make(chan struct{})
	var ready, done sync.WaitGroup
	ready.Add(writers)
	done.Add(writers)

	for i := range writers {
		go func() {
			defer done.Done()
			text := "writer-" + string(rune('a'+i))
			body := `{"version":1,"nextId":3,"__base":"T0","__by":"agent-1","tabs":[
			  {"id":"ab1","name":"Plan","type":"notes","state":{"text":"` + text + `"}},
			  {"id":"ab2","name":"Queue","type":"notes","state":{"text":"two"}}
			]}`
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/aboard.json", strings.NewReader(body))
			ready.Done()
			<-start // the barrier: everyone is parsed, built and waiting
			srv.postState(rec, req)
			results[i] = result{code: rec.Code, text: text}
		}()
	}

	ready.Wait()
	close(start)
	done.Wait()

	var winners []result
	for _, r := range results {
		switch r.code {
		case http.StatusOK:
			winners = append(winners, r)
		case http.StatusConflict:
		default:
			t.Errorf("unexpected status %d", r.code)
		}
	}
	if len(winners) != 1 {
		t.Fatalf("%d writers returned 200, want exactly 1 — the rest must be refused with 409", len(winners))
	}

	// The document on disk is the winner's, whole. A second writer landing after
	// it would leave the file holding text nobody was told had won.
	tabs := srv.readTabs(t)
	if got := string(tabByID(t, tabs, "ab1").State); !strings.Contains(got, winners[0].text) {
		t.Errorf("state file holds %s, but the only 200 was %q", got, winners[0].text)
	}

	// And the record agrees with the disk. A journal entry per racer would mean
	// the one place a session looks to answer "who changed this while I was
	// thinking?" reports writes that are not in the file.
	entries, _, err := JournalEntries(t.Context(), srv.root, "", 100, DefaultInvocation)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("%d journal entries for %d concurrent posts, want 1", len(entries), writers)
	}
}

// A second write that arrives while the first is still inside the critical
// section must be refused, not queued and applied on top: it was built on a
// document that no longer exists by the time it is its turn.
func TestASecondWriterOffTheSameBaseIsRefused(t *testing.T) {
	srv := testServer(t, twoTabs)

	first := srv.postDocument(t, `{"version":1,"nextId":3,"__base":"T0","__by":"agent-1","tabs":[
	  {"id":"ab1","name":"Plan","type":"notes","state":{"text":"first"}},
	  {"id":"ab2","name":"Queue","type":"notes","state":{"text":"two"}}
	]}`)
	if first.Code != http.StatusOK {
		t.Fatalf("the first write: status %d: %s", first.Code, first.Body)
	}

	second := srv.postDocument(t, `{"version":1,"nextId":3,"__base":"T0","__by":"agent-2","tabs":[
	  {"id":"ab1","name":"Plan","type":"notes","state":{"text":"second"}},
	  {"id":"ab2","name":"Queue","type":"notes","state":{"text":"two"}}
	]}`)
	if second.Code != http.StatusConflict {
		t.Fatalf("the second write off the same base: status %d, want 409", second.Code)
	}

	raw, err := os.ReadFile(srv.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "first") {
		t.Error("the refused write reached disk anyway")
	}
}

// discardWriter is an http.ResponseWriter that keeps nothing. These two tests
// broadcast in a tight loop to widen a window measured in instructions, and a
// ResponseRecorder would grow a buffer for every frame the clients drain.
type discardWriter struct{ header http.Header }

func (d *discardWriter) Header() http.Header {
	if d.header == nil {
		d.header = http.Header{}
	}
	return d.header
}
func (d *discardWriter) Write(p []byte) (int, error) { return len(p), nil }
func (d *discardWriter) WriteHeader(int)             {}
func (d *discardWriter) Flush()                      {}

// streamRace runs `serve` for many clients that hang up at staggered moments
// while `broadcast` fires continuously, and returns when everything has stopped.
// The failure it hunts is a panic in the BROADCASTER's goroutine, which takes the
// test binary — and, in production, the server — down with it.
func streamRace(t *testing.T, serve func(http.ResponseWriter, *http.Request), broadcast func()) {
	t.Helper()

	const clients = 60

	stop := make(chan struct{})
	var pump sync.WaitGroup
	pump.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
				broadcast()
			}
		}
	})

	var conns sync.WaitGroup
	for i := range clients {
		conns.Go(func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req := httptest.NewRequest(http.MethodGet, "/stream", http.NoBody).WithContext(ctx)

			served := make(chan struct{})
			go func() {
				defer close(served)
				serve(&discardWriter{}, req)
			}()

			// Stagger the hang-ups across the broadcast loop rather than firing
			// them together: the window is between copying the subscriber list
			// and sending to it, so the disconnections have to land all over it.
			time.Sleep(time.Duration(i%20) * 100 * time.Microsecond)
			cancel()
			<-served
		})
	}

	conns.Wait()
	close(stop)
	pump.Wait()
}

// An SSE client that disconnects while broadcasts are in flight.
//
// `fanout` copied the subscriber channels under the lock, released it, then sent
// — and `events`' own defer deletes the channel from the map AND closes it. A
// client that hung up inside that window had its channel closed before the send
// reached it, and a send on a closed channel panics. `watch()` is a bare
// goroutine with no recover, so the process died outright: the board vanished
// and `aboard status` reported a stale record.
func TestFanoutSurvivesAClientDisconnecting(t *testing.T) {
	srv := testServer(t, twoTabs)
	streamRace(t, srv.events, func() { srv.fanout(`{"origin":"race"}`) })
}

// The journal watcher is the same shape and needed the same fix: `/watch` closes
// its channel on the way out while `notifyWatchers` sends to a copy of the map.
func TestNotifyWatchersSurvivesAWatcherDisconnecting(t *testing.T) {
	srv := testServer(t, twoTabs)
	entry := JournalEntry{At: "T", By: "agent-1", Tabs: []string{"ab1"}}
	streamRace(t, srv.handleWatch, func() { srv.notifyWatchers(entry) })
}

// Defence in depth, asserted so it cannot be quietly removed: a panic in a
// long-lived background goroutine is logged through Options.Logger rather than
// killing the server. Nothing in the fanout path panics any more — that is the
// point of the fix above — but the state watcher and the UI watcher are bare
// goroutines with no HTTP handler above them to recover, so the next panic
// anyone introduces there would be fatal instead of loud.
func TestGuardLogsAPanicInsteadOfCrashing(t *testing.T) {
	var logged strings.Builder
	srv := testServer(t, twoTabs)
	srv.opts = Options{Logger: log.New(&logged, "", 0)}

	srv.guard("test watcher", func() { panic("boom") })

	out := logged.String()
	if !strings.Contains(out, "test watcher") || !strings.Contains(out, "boom") {
		t.Errorf("the panic was not reported through the logger: %q", out)
	}
}

// guard existing is not the deliverable — the two watchers actually going
// through it is, and the test above passes either way. Reverting the two
// launches in Serve to a bare `go srv.watch()` leaves the whole suite green,
// which makes the recover exactly the kind of protection that gets refactored
// away by someone tidying an unfamiliar line.
//
// So the rule is asserted where it can be broken: a goroutine started on the
// server, in this package, goes through guard. It is deliberately a rule about
// the SHAPE and not a list of two names, because the failure mode is a third
// long-lived goroutine added later by someone who never read this file.
func TestEveryServerGoroutineRunsUnderGuard(t *testing.T) {
	launch := regexp.MustCompile(`(?m)^[\t ]*go\s+(srv|s)\.(\w+)`)

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range launch.FindAllStringSubmatch(string(body), -1) {
			found++
			if m[2] != "guard" {
				t.Errorf("%s starts `go %s.%s(…)` directly; a bare goroutine that panics takes the server with it — start it as `go %s.guard(%q, %s.%s)`", name, m[1], m[2], m[1], m[2], m[1], m[2])
			}
		}
	}
	if found == 0 {
		t.Error("no `go srv.…` launch found at all; this test has stopped watching anything")
	}
}

// The audit that goes with the lock, kept honest mechanically: the state file is
// written by exactly one thing in this process, and that thing holds writeMu.
//
// Every other writer in the tree works on a DIFFERENT file — uploads
// (`upload.go`), the sidecar logs (`logs.go`), the instance record
// (`server.go`) — or runs in another process before this one starts (`init`,
// which refuses outright if a board is already there). `apply` posts to the
// running board rather than writing the file, so an agent's write queues behind
// the browser's on this same lock. A second call to writeAtomic would be a
// second writer, and the reason to notice it is that the damage is invisible:
// the loser is told its write landed.
func TestTheStateFileHasOneWriter(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	callers := map[string]int{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(string(body), ".writeAtomic("); n > 0 {
			callers[name] = n
		}
	}
	if len(callers) != 1 || callers["server.go"] != 1 {
		t.Errorf("writeAtomic is called from %v; the state file has exactly one writer, inside commitState", callers)
	}

	body, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "s.writeMu.Lock()") {
		t.Error("the write path no longer takes writeMu")
	}

	// Counting calls to writeAtomic only catches a second writer that reuses
	// writeAtomic — which is the tidy way to add one, and not the likely one. A
	// handler that reaches for os.WriteFile on s.stateFile bypasses both the
	// rename and the lock while every existing assertion here stays green, so
	// that shape is refused by name.
	direct := regexp.MustCompile(`os\.(WriteFile|Create|OpenFile|Truncate)\(\s*s\.stateFile`)
	renames := 0
	for name := range callersDir(t) {
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if loc := direct.FindString(string(body)); loc != "" {
			t.Errorf("%s writes the state file directly (%s…); it must go through writeAtomic, which renames and is the only thing holding writeMu", name, loc)
		}
		renames += strings.Count(string(body), "os.Rename(tmpName, s.stateFile)")
	}
	if renames != 1 {
		t.Errorf("%d renames onto the state file; exactly one, inside writeAtomic", renames)
	}
}

// callersDir is the package's own non-test sources, which three audits here read
// as text. Reading source is the only way to assert "nothing ELSE does this":
// a runtime test can only observe the callers that ran.
func callersDir(t *testing.T) map[string]struct{} {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]struct{}{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files[name] = struct{}{}
	}
	return files
}
