package aboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

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

// The first SSE frame carries the UI signature, and it is what makes a restart
// self-healing: the stream drops, the browser reconnects on its own, the
// signature does not match what the page loaded, the page reloads. If it stops
// arriving FIRST, every open page silently keeps running stale JavaScript again
// — and nothing else on the stream would look any different.
//
// Carried over from test/smoke.sh, where it was `curl -sN /events | grep -m1`.
func TestTheFirstSSEFrameCarriesTheUISignature(t *testing.T) {
	srv := testServer(t, twoTabs)

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "http://localhost/events", http.NoBody).WithContext(ctx)
	rec := httptest.NewRecorder()
	srv.route(rec, req)

	// The very first line is `retry:`, which tells the browser how long to wait
	// before reconnecting — the frame under test is the first DATA line after it.
	body := rec.Body.String()
	payload := ""
	for line := range strings.SplitSeq(body, "\n") {
		if rest, ok := strings.CutPrefix(line, "data: "); ok {
			payload = rest
			break
		}
	}
	if payload == "" {
		t.Fatalf("the stream produced no data frame at all: %q", body)
	}

	var frame struct {
		UI *struct {
			HTML string `json:"html"`
			CSS  string `json:"css"`
			JS   string `json:"js"`
		} `json:"ui"`
	}
	if err := json.Unmarshal([]byte(payload), &frame); err != nil {
		t.Fatalf("unreadable first frame %q: %v", payload, err)
	}
	if frame.UI == nil {
		t.Fatalf("the first frame is not the UI signature: %q", payload)
	}
	for name, got := range map[string]string{"html": frame.UI.HTML, "css": frame.UI.CSS, "js": frame.UI.JS} {
		if len(got) < 8 {
			t.Errorf("the %s hash is %q — too short to distinguish two builds", name, got)
		}
	}
	// Three hashes, not one, because a stylesheet change costs a re-link and not
	// a reload. If they were the same value the page could not tell them apart.
	if frame.UI.HTML == frame.UI.CSS {
		t.Error("the html and css hashes are identical, so a CSS-only change is indistinguishable from a code change")
	}
}

// A predicate the board does not understand is refused UP FRONT, not awaited.
// The distinction is the whole point: a valid predicate that never fires times
// out, and a typo that would never have fired must not look the same.
func TestAnUnknownWaitPredicateIsRefusedRatherThanAwaited(t *testing.T) {
	srv := testServer(t, twoTabs)

	for _, bad := range []string{"form 15 answered", "node bb58"} {
		req := httptest.NewRequest(http.MethodGet,
			"http://localhost/wait?for="+url.QueryEscape(bad)+"&timeout=2", http.NoBody)
		rec := httptest.NewRecorder()
		srv.route(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("/wait?for=%q = %d, want 400", bad, rec.Code)
		}
	}

	// And a valid one is accepted and times out cleanly, which is a different
	// answer and has to stay one.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "http://localhost/wait?for=change&timeout=1", http.NoBody).WithContext(ctx)
	rec := httptest.NewRecorder()
	srv.route(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("a valid predicate answered %d", rec.Code)
	}
	var ev struct {
		Event string `json:"event"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &ev); err != nil {
		t.Fatalf("unreadable answer: %v", err)
	}
	if ev.Event != "timeout" {
		t.Errorf("a valid predicate that never fired answered %q, want \"timeout\"", ev.Event)
	}
}

// Poking a board nobody is listening to releases nobody, and says so.
//
// It reads like a degenerate case and it is the one the human actually meets:
// the notify button is disabled precisely BECAUSE `/waiters` says zero, so this
// pair is what the button's whole claim rests on. The browser half is
// `TestTheNotifyButtonIsDisabledWithNobodyWaiting` in test/e2e, which can only
// assert that the button looks dead — it cannot press a disabled button, so the
// COUNT has to be asserted here. test/smoke.sh checked it with curl, on a live
// board, where "nobody is waiting" was an assumption about somebody else's
// session rather than a fact; on a server built for the test it is a fact.
func TestPokingWithNobodyWaitingReleasesNobody(t *testing.T) {
	srv := testServer(t, twoTabs)

	req := httptest.NewRequest(http.MethodPost, "http://localhost/poke", strings.NewReader(`{"by":"agent-test"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.route(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /poke = %d: %s", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	var poked struct {
		OK       bool   `json:"ok"`
		Released int    `json:"released"`
		By       string `json:"by"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &poked); err != nil {
		t.Fatalf("unreadable reply: %v", err)
	}
	if !poked.OK || poked.Released != 0 {
		t.Errorf("poking an idle board answered ok=%v released=%d, want ok=true released=0", poked.OK, poked.Released)
	}
	if poked.By != "agent-test" {
		t.Errorf("the poke is attributed to %q, not to whoever sent it", poked.By)
	}

	// And the count the button reads: zero waiters, an empty list — not a null
	// the shell would have to special-case.
	wRec := httptest.NewRecorder()
	srv.route(wRec, httptest.NewRequest(http.MethodGet, "http://localhost/waiters", http.NoBody))
	if wRec.Code != http.StatusOK {
		t.Fatalf("GET /waiters = %d", wRec.Code)
	}
	var waiters struct {
		Waiting int   `json:"waiting"`
		Waiters []any `json:"waiters"`
	}
	if err := json.Unmarshal(wRec.Body.Bytes(), &waiters); err != nil {
		t.Fatalf("unreadable /waiters: %v", err)
	}
	if waiters.Waiting != 0 || len(waiters.Waiters) != 0 {
		t.Errorf("an idle board reports %d waiting: %v", waiters.Waiting, waiters.Waiters)
	}
}
