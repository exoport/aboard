package aboard

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/exoport/aboard/pkg/aboard/web"
)

// tokenNames pulls every custom property a `:root` body declares.
// Not line-anchored: the fallback literal packs several declarations onto one
// line, and a per-line regexp silently reported one token per line for it.
var tokenDecl = regexp.MustCompile(`(?:^|[;{\s])(--[a-z0-9-]+)\s*:`)

func tokenNames(css string) []string {
	out := []string{}
	for _, hit := range tokenDecl.FindAllStringSubmatch(css, -1) {
		out = append(out, hit[1])
	}
	sort.Strings(out)
	return out
}

// The frame's token set is app.css's token set, not a copy of it that was true
// once. The copy this replaced was missing --accent-dim, --drop and all three
// --status-* tokens: a widget naming one got no colour and no warning.
//
// Asserted as SET EQUALITY in both directions on purpose. "every app.css token is
// in the frame" alone would pass a frame that also carried five invented ones.
func TestTheHTMLFramePaletteIsAppCSSsOwn(t *testing.T) {
	srv := testServer(t, htmlTabBoard)

	rec := httptest.NewRecorder()
	srv.serveTabHTML(rec, httptest.NewRequest(http.MethodGet, "http://localhost/tab/bb1/html", http.NoBody), "bb1")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /tab/bb1/html = %d", rec.Code)
	}
	frame := rec.Body.String()

	inner, ok := frameRootBlock(frame)
	if !ok {
		t.Fatalf("the served frame has no :root block:\n%s", frame)
	}

	raw, err := web.FS.ReadFile("app.css")
	if err != nil {
		t.Fatal(err)
	}
	sheet, ok := parseRootBlock(web.FS)
	if !ok {
		t.Fatal("app.css's own :root block does not parse — the frame is silently on the fallback")
	}

	got, want := tokenNames(inner), tokenNames(sheet)
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the frame's tokens are not app.css's:\n frame:   %s\n app.css: %s", got, want)
	}
	if len(want) < 15 {
		t.Errorf("only %d tokens parsed out of app.css (%d bytes) — the parse is finding the wrong block", len(want), len(raw))
	}
	// The tokens that were missing from the hand-copied subset. Named, so a
	// regression reads as "the drift is back" rather than as a count.
	for _, token := range []string{"--accent-dim", "--drop", "--status-todo", "--status-doing", "--status-done"} {
		if !strings.Contains(inner, token+":") {
			t.Errorf("the frame is missing %s, which app.css declares", token)
		}
	}
	// color-scheme comes along with the block; without it native controls in a
	// widget render light on a black ground.
	if !strings.Contains(inner, "color-scheme") {
		t.Error("the frame's :root does not set color-scheme")
	}
}

// Fail CLOSED. A stylesheet that cannot be parsed leaves the frame on the
// literal, because a widget with no ground and no ink at all is worse than one
// on a palette that has stopped being current.
func TestAnUnparseableStylesheetLeavesTheFrameOnTheBuiltInPalette(t *testing.T) {
	cases := map[string]fstest.MapFS{
		"no app.css at all":     {},
		"no :root block":        {"app.css": &fstest.MapFile{Data: []byte("body { color: red; }")}},
		"unterminated block":    {"app.css": &fstest.MapFile{Data: []byte(":root { --bg: #000; --text: #fff;")}},
		"a nested brace":        {"app.css": &fstest.MapFile{Data: []byte(":root { --bg:#000; --text:#fff; @media x { } }")}},
		"no ground and no ink":  {"app.css": &fstest.MapFile{Data: []byte(":root { --accent: #a4bd00; }")}},
		"only a commented root": {"app.css": &fstest.MapFile{Data: []byte("/* :root { --bg:#000; --text:#fff; } */ body{}")}},
	}
	for name, assets := range cases {
		t.Run(name, func(t *testing.T) {
			if block, ok := parseRootBlock(assets); ok {
				t.Fatalf("%s parsed as a palette: %q", name, block)
			}
			srv := testServer(t, htmlTabBoard)
			srv.assets = assets

			rec := httptest.NewRecorder()
			srv.serveTabHTML(rec, httptest.NewRequest(http.MethodGet, "http://localhost/tab/bb1/html", http.NoBody), "bb1")
			if rec.Code != http.StatusOK {
				t.Fatalf("GET /tab/bb1/html = %d", rec.Code)
			}
			inner, ok := frameRootBlock(rec.Body.String())
			if !ok {
				t.Fatal("the served frame has no :root block")
			}
			// The ground and the ink, which is what the fallback exists to keep.
			for _, token := range []string{"--bg:", "--text:", "--accent:"} {
				if !strings.Contains(inner, token) {
					t.Errorf("the fallback palette is missing %s:\n%s", token, inner)
				}
			}
		})
	}
}

// The real stylesheet parses, so nothing in production is quietly on the
// fallback. Separate from the equality test above so the two failures read
// differently: "the parse gave up" and "the parse gave the wrong answer".
func TestAppCSSParsesSoTheFallbackIsNeverInUse(t *testing.T) {
	if _, ok := parseRootBlock(web.FS); !ok {
		t.Fatal("app.css's :root block no longer parses — every html tab is on the stale literal")
	}
}

// frameRootBlock returns what the served document put between the braces of its
// own `:root {`.
func frameRootBlock(frame string) (string, bool) {
	_, after, found := strings.Cut(frame, ":root {")
	if !found {
		return "", false
	}
	inner, _, closed := strings.Cut(after, "}")
	return inner, closed
}
