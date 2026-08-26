package aboard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// The board has no authentication, so the only thing standing between it and a
// page in the developer's browser is that the page cannot reach it — and a
// cross-origin POST reaches it perfectly well. Reproduced in headless Chromium
// from another local origin: the board was wiped, and a pure deletion writes no
// journal entry, so nothing survived to restore from.
func TestACrossSiteWriteIsRefused(t *testing.T) {
	srv := testServer(t, twoTabs)
	body := `{"tabs":[],"__by":"human"}`

	for _, tc := range []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"a page on another origin", map[string]string{"Origin": "http://evil.example:1234"}, http.StatusForbidden},
		{"the browser says cross-site", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"a sandboxed frame (opaque origin)", map[string]string{"Origin": "null"}, http.StatusForbidden},
		{"the board's own page", map[string]string{"Origin": "http://localhost:4000", "Sec-Fetch-Site": "same-origin"}, http.StatusOK},
		{"same-site", map[string]string{"Sec-Fetch-Site": "same-site"}, http.StatusOK},
		{"a typed URL", map[string]string{"Sec-Fetch-Site": "none"}, http.StatusOK},
		{"curl and apply, no Origin at all", nil, http.StatusOK},
	} {
		req := httptest.NewRequest(http.MethodPost, "/aboard.json", strings.NewReader(body))
		req.Host = "localhost:4000"
		for k, v := range tc.headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		srv.route(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s: status %d, want %d (%s)", tc.name, rec.Code, tc.want, strings.TrimSpace(rec.Body.String()))
		}
		if tc.want == http.StatusForbidden && !strings.Contains(rec.Body.String(), "refused") {
			t.Errorf("%s: the refusal does not name a reason: %s", tc.name, rec.Body)
		}
	}
}

// A READ is not exempt from the host check, which is the whole point of it: the
// bind is loopback, but any hostname that resolves to loopback reaches it, and a
// page served from that name is then same-origin with the board — able to read
// /aboard.json, /journal and /health, which discloses the absolute project path
// and the pid.
func TestOnlyLoopbackHostsAreAnswered(t *testing.T) {
	srv := testServer(t, twoTabs)

	for _, tc := range []struct {
		host string
		want int
	}{
		{"localhost:41000", http.StatusOK},
		{"127.0.0.1:41000", http.StatusOK},
		{"[::1]:41000", http.StatusOK},
		{"localhost", http.StatusOK},
		{"127.0.0.1", http.StatusOK},
		{"rebind.evil.example:41000", http.StatusForbidden},
		{"aboard.local", http.StatusForbidden},
		{"", http.StatusForbidden},
	} {
		for _, path := range []string{"/aboard.json", "/health"} {
			req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
			req.Host = tc.host
			rec := httptest.NewRecorder()
			srv.route(rec, req)
			if rec.Code != tc.want {
				t.Errorf("Host %q %s: status %d, want %d", tc.host, path, rec.Code, tc.want)
			}
		}
	}
}

// An explicit --port (or PORT) used to skip duplicate detection entirely: the
// early return happened before anything asked who held the port. A second server
// then ran on the same state file and rewrote instance.json to point at itself,
// so killing it aimed every client command at a dead port while a healthy board
// went on serving.
func TestAnExplicitPortStillRefusesADuplicate(t *testing.T) {
	dir := t.TempDir()
	root := Root(dir)

	// A board that answers /health for THIS project, on a real port.
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(Instance{
			App: HostStandalone, Project: root.String(), URL: "http://localhost:1", PID: 4242,
		})
	}))
	defer stub.Close()
	port := serverPort(t, stub.URL)

	srv := &server{opts: Options{Logger: log.New(io.Discard, "", 0)}, root: root}
	_, _, err := srv.listen(context.Background(), port, root, "")
	if err == nil {
		t.Fatal("--port started a second server for a project that already had one")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("the refusal does not say a board is already running: %v", err)
	}
	if !strings.Contains(err.Error(), "4242") {
		t.Errorf("the refusal does not name the process to look at: %v", err)
	}
}

// A stranger on an explicitly requested port is not a duplicate — it is somebody
// else's server, and the bind failure is the honest report.
func TestAnExplicitPortHeldByAStrangerIsAPlainBindFailure(t *testing.T) {
	dir := t.TempDir()
	root := Root(dir)
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not a board", http.StatusTeapot)
	}))
	defer stub.Close()

	srv := &server{opts: Options{Logger: log.New(io.Discard, "", 0)}, root: root}
	_, _, err := srv.listen(context.Background(), serverPort(t, stub.URL), root, "")
	if err == nil {
		t.Fatal("binding a port somebody else holds succeeded")
	}
	if strings.Contains(err.Error(), "already running") {
		t.Errorf("a stranger was reported as this project's own board: %v", err)
	}
}

// The instance file, the state file and everything else under .aboard/ are
// joined from the board NAME, so a name that is a path escapes the project.
// `--name '../../../../evil'` wrote both files outside the tree and reported
// success — uncovered by .gitignore, invisible to `status`.
func TestBoardNamesThatEscapeThePathAreRefused(t *testing.T) {
	for _, name := range []string{
		"../../../../evil", "..", ".", "a/b", `a\b`, ".hidden", "-flag",
		"with space", strings.Repeat("x", 65), "eviĺ",
	} {
		if err := ValidateBoardName(name); err == nil {
			t.Errorf("%q was accepted as a board name", name)
		}
	}
	for _, name := range []string{"", "review", "agent-2", "a.b_c-1", "X", strings.Repeat("x", 64)} {
		if err := ValidateBoardName(name); err != nil {
			t.Errorf("%q is a reasonable board name and was refused: %v", name, err)
		}
	}
}

// And the refusal happens before any path is joined, which is what makes it a
// fix rather than a warning: this is the end-to-end shape of the escape.
func TestAnEscapingNameWritesNothing(t *testing.T) {
	dir := t.TempDir()
	root := Root(dir)
	name := "../../../../evil"
	if err := ValidateBoardName(name); err == nil {
		t.Fatalf("the name was accepted; %s would have been written", root.StateFile(name))
	}
	outside := root.StateFile(name)
	if _, err := os.Stat(outside); err == nil {
		t.Fatalf("%s exists — a previous run of this escape left a file behind", outside)
	}
}

// writeInstanceRecord puts a discovery record on disk for a board that may or
// may not be listening — which is the whole subject of the two tests below.
func writeInstanceRecord(t *testing.T, root Root, name string, port, pid int) {
	t.Helper()
	if err := os.MkdirAll(root.RunDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(Instance{
		App: HostStandalone, Project: root.String(), Name: name,
		Port: port, URL: fmt.Sprintf("http://localhost:%d", port), PID: pid,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root.InstanceFile(name), body, 0o644); err != nil {
		t.Fatal(err)
	}
}

// aFreePort is a port nothing is listening on: bound to find out which, then
// released. Racy in principle and unavoidable — the question "what would a
// second server find free" has no non-racy form — and the window is a test's
// own microseconds.
func aFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("a tcp listener reported %T", ln.Addr())
	}
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr.Port
}

// boardStub answers /health as this project's board of this name, on a real
// port, so a probe of it is a probe of a live board.
func boardStub(t *testing.T, root Root, name string, pid int) *httptest.Server {
	t.Helper()
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(Instance{
			App: HostStandalone, Project: root.String(), Name: name,
			URL: "http://localhost:1", PID: pid,
		})
	}))
	t.Cleanup(stub.Close)
	return stub
}

// The duplicate check used to be anchored to the PORT, so `--port <any free
// port>` had no occupant to recognise and started a SECOND server on the same
// state file. It then rewrote run/instance.json to point at itself — so every
// client command followed the newcomer — and on exit removed the record, leaving
// `aboard status` reporting no board while the first one went on serving.
//
// The question is about the project, not about a port: does (this root, this
// name) already have a board that answers /health?
func TestAFreeExplicitPortIsStillRefusedWhenThisProjectHasALiveBoard(t *testing.T) {
	root := Root(t.TempDir())
	live := boardStub(t, root, "", 4242)
	writeInstanceRecord(t, root, "", serverPort(t, live.URL), 4242)

	// A port nothing is listening on: the old check would have found it free and
	// bound it happily.
	freePort := aFreePort(t)

	srv := &server{opts: Options{Logger: log.New(io.Discard, "", 0)}, root: root}
	ln, _, err := srv.listen(context.Background(), freePort, root, "")
	if err == nil {
		_ = ln.Close()
		t.Fatal("a second server started on a free --port while this project's board was live")
	}
	if !strings.Contains(err.Error(), "already running") || !strings.Contains(err.Error(), "4242") {
		t.Errorf("the refusal must name the live board and its pid: %v", err)
	}

	// The named board of the same project is a different board, and must not be
	// refused by the default board's record.
	ln2, _, err := srv.listen(context.Background(), freePort, root, "review")
	if err != nil {
		t.Fatalf("a named board was refused by the default board's record: %v", err)
	}
	_ = ln2.Close()
}

// A record whose process is gone is the commonest real case — a board that was
// killed — and refusing to start because of the corpse of the last one would be
// the worst possible reading of it. The record is proceeded past and overwritten.
func TestAStaleRecordDoesNotStopANewBoard(t *testing.T) {
	root := Root(t.TempDir())

	// A port with nothing on it: allocated, then released.
	writeInstanceRecord(t, root, "", aFreePort(t), 999999)

	srv := &server{opts: Options{Logger: log.New(io.Discard, "", 0)}, root: root}
	ln, got, err := srv.listen(context.Background(), 0, root, "")
	if err != nil {
		t.Fatalf("a stale record stopped a new board: %v", err)
	}
	defer func() { _ = ln.Close() }()
	srv.port = got
	if err := srv.writeInstance(root, ""); err != nil {
		t.Fatal(err)
	}

	rec, err := RunningInstance(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Port != got || rec.PID != os.Getpid() {
		t.Errorf("the stale record was not overwritten: %+v", rec)
	}
}
