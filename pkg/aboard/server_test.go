package aboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
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
