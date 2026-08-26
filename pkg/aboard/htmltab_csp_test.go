package aboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const htmlTabBoard = `{"version":3,"rev":1,"nextId":10,"tabs":[
	{"id":"bb1","name":"Sketch","type":"html","state":{"html":"<p>hi</p>","data":{}}},
	{"id":"bb2","name":"Review","type":"stack","state":{"blocks":[
		{"id":"bb3","type":"html","title":"Widget","state":{"html":"<p>block</p>","data":{}}}
	]}}
]}`

// The CSP is the containment, so it is asserted in Go and not only in the local
// shell suite — the shell suite never runs in CI, and this is the one header
// whose loss is invisible until somebody is already exploiting it.
func TestHTMLTabCSP(t *testing.T) {
	srv := testServer(t, htmlTabBoard)

	rec := httptest.NewRecorder()
	srv.serveTabHTML(rec, httptest.NewRequest(http.MethodGet, "http://localhost/tab/bb1/html", http.NoBody), "bb1")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /tab/bb1/html = %d", rec.Code)
	}
	csp := rec.Header().Get("Content-Security-Policy")

	for _, want := range []string{
		// The containment. Everything else on this list is defence around it.
		"connect-src 'none'",
		// Added 2026-08-26. Framed, the iframe's own sandbox attribute already
		// gave an opaque origin; fetched STANDALONE — how a screenshot is taken
		// and how a human checks a widget — the page ran on the board's real
		// origin. Hardening, not an egress fix: it blocks popups and form
		// submission, not the page navigating itself.
		"sandbox allow-scripts",
		"default-src 'none'",
		"form-action 'none'",
		"base-uri 'none'",
		// Deliberately wider than 'self': frame-ancestors is checked against the
		// WHOLE chain, and the board is normally viewed inside VS Code's webview.
		"frame-ancestors 'self' vscode-webview:",
	} {
		if !strings.Contains(csp, want) {
			t.Errorf("the html tab CSP has lost %q:\n%s", want, csp)
		}
	}
	// allow-same-origin would undo the whole point of the sandbox directive.
	if strings.Contains(csp, "allow-same-origin") {
		t.Errorf("the sandbox directive allows same-origin, which is the thing it exists to deny:\n%s", csp)
	}
}

// A stack block's document is contained EXACTLY like a tab's — byte-identical,
// not merely similar. The blank-block bug was a lookup in serveTabHTML, and the
// fix had to prove it had not quietly relaxed anything on the way through.
func TestAStackBlockIsContainedByteIdenticallyToATab(t *testing.T) {
	srv := testServer(t, htmlTabBoard)

	tabRec := httptest.NewRecorder()
	srv.serveTabHTML(tabRec, httptest.NewRequest(http.MethodGet, "http://localhost/tab/bb1/html", http.NoBody), "bb1")

	blockRec := httptest.NewRecorder()
	srv.serveTabHTML(blockRec, httptest.NewRequest(http.MethodGet, "http://localhost/tab/bb2/bb3/html", http.NoBody), "bb2/bb3")

	if blockRec.Code != http.StatusOK {
		t.Fatalf("GET /tab/bb2/bb3/html = %d: %s", blockRec.Code, blockRec.Body.String())
	}
	for _, header := range []string{"Content-Security-Policy", "X-Content-Type-Options", "Cache-Control", "Content-Type"} {
		if got, want := blockRec.Header().Get(header), tabRec.Header().Get(header); got != want {
			t.Errorf("%s differs between a block and a tab:\n  block: %s\n  tab:   %s", header, got, want)
		}
	}
	// And it served the BLOCK's content, not the parent's — the defect that made
	// the frame blank in the first place.
	if !strings.Contains(blockRec.Body.String(), "block") {
		t.Error("the block's document does not carry the block's own html")
	}
}

// Every wrong path names what was wrong. A 404 with no body is what made the
// blank frame take a day to find.
func TestAWrongHTMLPathSaysWhatWasWrong(t *testing.T) {
	srv := testServer(t, htmlTabBoard)
	cases := map[string]string{
		"bb9":       "no such tab",
		"bb1/bb3":   "not a stack",
		"bb2/bb999": "bb999",
	}
	for path, want := range cases {
		rec := httptest.NewRecorder()
		srv.serveTabHTML(rec, httptest.NewRequest(http.MethodGet, "http://localhost/tab/"+path+"/html", http.NoBody), path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s: the refusal does not mention %q: %s", path, want, strings.TrimSpace(rec.Body.String()))
		}
	}
}

// The bridge is a NAME contract, in both directions: the widget calls
// `aboard.set()` as a bare global and the parent matches on the `__aboard`
// envelope property. Rename one half and the frame posts messages the parent
// silently drops — no error anywhere, and a widget that looks like it works.
//
// Checked here rather than only in the shell suite because the shell suite is
// being retired, and because a rename is exactly the kind of change that passes
// every other test in this file.
func TestTheServedFrameCarriesTheBridgeUnderItsAboardNames(t *testing.T) {
	srv := testServer(t, htmlTabBoard)
	rec := httptest.NewRecorder()
	srv.serveTabHTML(rec, httptest.NewRequest(http.MethodGet, "http://localhost/tab/bb1/html", http.NoBody), "bb1")
	body := rec.Body.String()

	for _, want := range []string{"window.aboard", "__ABOARD_DATA__"} {
		if !strings.Contains(body, want) {
			t.Errorf("the served frame does not define %s — the bridge is renamed or gone", want)
		}
	}
	// The pre-rename spellings. `claude` survives in views/chat.js deliberately,
	// because that is an ACTOR name in old transcripts; these are API names and
	// there is nothing to be compatible with.
	for _, gone := range []string{"window.board", "__BOARD_DATA__", "__board"} {
		if strings.Contains(body, gone) {
			t.Errorf("the served frame still carries the pre-rename name %s", gone)
		}
	}
}

// A literal `<\/script>` written into raw HTML swallows the rest of the
// document: the escape is only correct inside a JS string literal, and in markup
// the browser never sees the closing tag at all. The static content still
// renders, nothing runs, and it reads as a styling problem. Cost an hour once.
func TestTheServedFrameTerminatesItsScript(t *testing.T) {
	srv := testServer(t, htmlTabBoard)
	rec := httptest.NewRecorder()
	srv.serveTabHTML(rec, httptest.NewRequest(http.MethodGet, "http://localhost/tab/bb1/html", http.NoBody), "bb1")
	body := rec.Body.String()

	if !strings.Contains(body, "</script>") {
		t.Error("the served frame has no real </script> — everything after the bridge is inside a script that never closes")
	}
	if strings.Contains(body, `<\/script>`) {
		t.Error(`the served frame contains a literal <\/script>, which the browser does not read as a closing tag`)
	}
}

// A block path must serve the BLOCK's document, not the parent's. A stack has no
// state.html of its own, so reading the wrong level yields the empty-tab
// placeholder — which looks like an empty widget rather than a wrong lookup, and
// is precisely how the blank-frame defect presented.
func TestABlockPathDoesNotServeTheEmptyTabPlaceholder(t *testing.T) {
	srv := testServer(t, htmlTabBoard)

	rec := httptest.NewRecorder()
	srv.serveTabHTML(rec, httptest.NewRequest(http.MethodGet, "http://localhost/tab/bb2/bb3/html", http.NoBody), "bb2/bb3")
	if strings.Contains(rec.Body.String(), "An agent sets") {
		t.Error("the block path served the empty-tab placeholder instead of the block's own html")
	}
}
