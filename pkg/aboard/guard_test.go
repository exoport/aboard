package aboard

import (
	"context"
	"encoding/json"
	"io"
	"log"
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
