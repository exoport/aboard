package aboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/exoport/aboard/pkg/aboard/web"
)

// A board served under --base-path answers at <base>/health and nowhere else, so
// a probe that always asked for the bare "/health" reported a live board as a
// stale record — and "stale record: … is not answering" is the one sentence that
// sends a session off to restart a server that was fine.
func TestProbeBoardHonoursBasePath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/x/health" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(Instance{App: HostStandalone, Port: 1, Base: "/x"})
	}))
	defer srv.Close()

	port := serverPort(t, srv.URL)

	if got := ProbeBoard(t.Context(), port, "/x"); got == nil {
		t.Error("a board under /x was not recognised when probed with its base")
	}
	if got := ProbeBoard(t.Context(), port, "x"); got == nil {
		t.Error("the base is normalised, so an unslashed one must work too")
	}
	if got := ProbeBoard(t.Context(), port, ""); got != nil {
		t.Error("probing the bare root found something; the fixture only answers under /x")
	}
}

// The identity check is what makes the probe a BOARD probe rather than a
// liveness check: any other server on the port must not be mistaken for one.
func TestProbeBoardAcceptsEitherIdentity(t *testing.T) {
	for _, tc := range []struct {
		app  string
		want bool
	}{
		{HostStandalone, true},
		{HostApe, true},
		{"something-else", false},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(Instance{App: tc.app})
		}))
		got := ProbeBoard(t.Context(), serverPort(t, srv.URL), "") != nil
		srv.Close()
		if got != tc.want {
			t.Errorf("app %q: recognised=%v, want %v", tc.app, got, tc.want)
		}
	}
}

func serverPort(t *testing.T, raw string) int {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %s: %v", raw, err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("port of %s: %v", raw, err)
	}
	return port
}

// The reference says "anything not matched, and any method not listed for a
// matched path, is 404", and that was false for the shell: `/` and
// `/aboard.html` had no method check, so `POST /` returned the whole page with a
// 200. Harmless in itself — nothing in the browser executes anything — but a
// documented rule with one silent exception is one nobody trusts the rest of.
func TestTheShellIsGETOnly(t *testing.T) {
	srv := testServer(t, `{"version":3,"rev":1,"nextId":1,"tabs":[]}`)
	srv.assets = web.FS

	for _, path := range []string{"/", "/aboard.html"} {
		get := httptest.NewRecorder()
		srv.route(get, httptest.NewRequest(http.MethodGet, "http://localhost"+path, http.NoBody))
		if get.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, get.Code)
		}

		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodHead} {
			rec := httptest.NewRecorder()
			// No Origin and no Sec-Fetch-Site, so this is not refused as
			// cross-site: what is being asserted is the METHOD rule, not the
			// origin rule that would otherwise mask it.
			srv.route(rec, httptest.NewRequest(method, "http://localhost"+path, http.NoBody))
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s %s = %d, want 404", method, path, rec.Code)
			}
		}
	}
}

// And the other half of the same sentence: the refusals run BEFORE the path is
// matched, so they answer 403 for a path that does not exist at all. That is the
// part the reference used to leave out, which made a reader who tried it think
// the table was wrong about everything else too.
func TestTheRefusalsRunBeforeThePathIsMatched(t *testing.T) {
	srv := testServer(t, `{"version":3,"rev":1,"nextId":1,"tabs":[]}`)
	srv.assets = web.FS

	badHost := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://localhost/no/such/path", http.NoBody)
	req.Host = "board.attacker.example"
	srv.route(badHost, req)
	if badHost.Code != http.StatusForbidden {
		t.Errorf("a disallowed Host on an unmatched path = %d, want 403", badHost.Code)
	}

	foreign := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "http://localhost/no/such/path", http.NoBody)
	req2.Header.Set("Origin", "http://elsewhere.example")
	srv.route(foreign, req2)
	if foreign.Code != http.StatusForbidden {
		t.Errorf("a foreign Origin on an unmatched path = %d, want 403", foreign.Code)
	}

	// A local unmatched path with nothing suspicious about it is still 404.
	plain := httptest.NewRecorder()
	srv.route(plain, httptest.NewRequest(http.MethodPost, "http://localhost/no/such/path", http.NoBody))
	if plain.Code != http.StatusNotFound {
		t.Errorf("an unmatched local POST = %d, want 404", plain.Code)
	}
}

// The other half of the reference's refusal table, and the half its "anything
// unmatched is 404" sentence was still wrong about after the shell was made
// GET-only: the LAST route serves the embedded tree through an allow-list, and a
// name outside that list is refused rather than looked up. So `/nope.js` is 403
// and `/views/nope.js` is 404 — a distinction the reference now states, pinned
// here so the next edit to either has to move both.
func TestAnUnmatchedGETIsRefusedOrNotFoundAccordingToTheAllowList(t *testing.T) {
	srv := testServer(t, `{"version":3,"rev":1,"nextId":1,"tabs":[]}`)
	srv.assets = web.FS

	for path, want := range map[string]int{
		"/nope.js":           http.StatusForbidden, // outside the allow-list
		"/no/such/path":      http.StatusForbidden,
		"/aboard.css":        http.StatusForbidden, // the stylesheet is app.css
		"/views/nosuch.js":   http.StatusNotFound,  // inside it, and absent
		"/assets/nosuch.svg": http.StatusNotFound,
		"/app.css":           http.StatusOK,
	} {
		rec := httptest.NewRecorder()
		srv.route(rec, httptest.NewRequest(http.MethodGet, "http://localhost"+path, http.NoBody))
		if rec.Code != want {
			t.Errorf("GET %s = %d, want %d", path, rec.Code, want)
		}
	}
}
