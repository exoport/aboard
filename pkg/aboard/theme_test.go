package aboard

import (
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/exoport/aboard/pkg/aboard/web"
)

// The drift this whole file exists to stop.
//
// A custom property that is not redeclared under `:root[data-theme="light"]`
// simply INHERITS the dark value, so a token forgotten in the light block is not
// an error anywhere: the board comes up light with one black-on-black label in
// it, and the only way anybody finds out is by looking. It is the same failure
// the html frame's hand-copied palette had — five missing tokens, no colour, no
// warning — which is why the fix there was "parse the stylesheet, do not copy
// it" and the fix here is "count them".
func TestBothThemesDeclareTheSameTokens(t *testing.T) {
	dark, light, ok := parseThemeVariants(web.FS)
	if !ok {
		t.Fatal("app.css's :root block no longer parses — every html tab is on the stale literal")
	}
	if len(dark.order) < 15 {
		t.Fatalf("only %d tokens parsed out of the dark theme — the parse is finding the wrong block", len(dark.order))
	}
	if len(light.order) == 0 {
		t.Fatal("app.css declares no light variant; the switch has nothing to switch to")
	}
	missing, extra := setDifference(dark.tokens, light.tokens), setDifference(light.tokens, dark.tokens)
	if len(missing) > 0 {
		t.Errorf("the light theme does not declare %s — those tokens inherit the DARK value on a light board, "+
			"which renders as no error at all", strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		t.Errorf("the light theme declares %s, which the dark one does not — a token that exists in one "+
			"theme is a token half the board cannot use", strings.Join(extra, ", "))
	}
	// The two variants must actually DIFFER, or the switch is a no-op with a
	// button on it. Checked on the ground and the ink, which are the two nothing
	// can be readable without.
	for _, token := range []string{"--bg", "--text"} {
		if dark.tokens[token] == light.tokens[token] {
			t.Errorf("%s is %q in both themes", token, dark.tokens[token])
		}
	}
	if !strings.Contains(light.body, "color-scheme") {
		t.Error("the light variant does not set color-scheme, so native controls stay dark on it")
	}
}

func setDifference(a, b map[string]string) []string {
	var out []string
	for name := range a {
		if _, ok := b[name]; !ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// The colour NAMES `ui` and `markup` accept are token names with the `--`
// stripped. Declared in two spec files and in a stylesheet, which is three
// places — so rather than a fourth list, this is the link between them: every
// name a renderer advertises must be a token the board actually has.
//
// This is what "reuse the declared palette, do not duplicate it" means in
// practice. The --claude to --agent rename is the case: the specs listed a name
// that resolved to nothing, and only a human noticing a missing colour said so.
func TestEveryDeclaredColourNameIsAToken(t *testing.T) {
	m, err := buildManifest(web.FS)
	if err != nil {
		t.Fatal(err)
	}
	has := map[string]bool{}
	for _, token := range m.Theme.Tokens {
		has[token] = true
	}
	if len(has) == 0 {
		t.Fatal("the manifest reports no theme tokens at all")
	}
	seen := 0
	for _, spec := range m.Types {
		for _, group := range [][]string{spec.Tones, spec.Colors} {
			for _, name := range group {
				seen++
				if !has["--"+name] {
					t.Errorf("%s advertises the colour %q and this board has no --%s token", spec.Type, name, name)
				}
			}
		}
	}
	if seen == 0 {
		t.Error("no renderer declares a colour palette; this check is covering nothing")
	}
}

/* ---------- .aboard/theme.json ---------- */

// writeTheme puts a theme file in a fresh project and loads it.
func writeTheme(t *testing.T, body string) *Theme {
	t.Helper()
	root := Root(t.TempDir())
	if err := os.MkdirAll(root.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root.ThemeFile(), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return LoadTheme(root, web.FS)
}

func TestNoThemeFileIsNotAnError(t *testing.T) {
	root := Root(t.TempDir())
	if got := LoadTheme(root, web.FS); got != nil {
		t.Fatalf("a project with no theme file loaded %+v", got)
	}
	if (*Theme)(nil).DefaultKind() != ThemeDark {
		t.Error("a board with no theme file must boot dark")
	}
}

func TestAThemeFileIsAPatchAndNotAReplacement(t *testing.T) {
	theme := writeTheme(t, `{"version":1,"default":"light",
		"dark":{"--accent":"#ff00ff"},"light":{"--bg":"#fefefe"}}`)
	if theme == nil {
		t.Fatal("the theme file did not load")
	}
	if len(theme.Warnings) > 0 {
		t.Fatalf("a valid theme file warned: %v", theme.Warnings)
	}
	if theme.DefaultKind() != ThemeLight {
		t.Errorf("default = %q, want light", theme.DefaultKind())
	}
	if theme.Dark["--accent"] != "#ff00ff" || len(theme.Dark) != 1 {
		t.Errorf("dark overrides = %v", theme.Dark)
	}

	// The patch is APPENDED to the built-in block, so everything the file did not
	// mention still has its value — which is the whole difference between a patch
	// and a replacement, and the reason a theme file written today does not lose
	// a token added tomorrow.
	body := rootDeclarations(web.FS, theme)
	if !strings.Contains(body, "--accent: #ff00ff;") {
		t.Errorf("the override is not in the frame's palette:\n%s", body)
	}
	for _, token := range []string{"--bg:", "--text:", "--status-todo:"} {
		if !strings.Contains(body, token) {
			t.Errorf("the patch dropped %s, which the file never mentioned", token)
		}
	}
	// A later declaration of the same property wins inside one block, so the
	// override must come after the built-in value rather than before it.
	if strings.LastIndex(body, "--accent: #ff00ff") < strings.Index(body, "--accent: #a4bd00") {
		t.Errorf("the override is declared BEFORE the built-in value, so the built-in one wins:\n%s", body)
	}
}

func TestAThemeFileNamingSomethingTheBoardDoesNotHaveWarnsAndCarriesOn(t *testing.T) {
	theme := writeTheme(t, `{"version":1,"default":"neon",
		"dark":{"--claude":"#a7adf4","--accent":"javascript:alert(1)","--bg":"#111"}}`)
	if theme == nil {
		t.Fatal("the theme file did not load")
	}
	joined := strings.Join(theme.Warnings, "\n")
	for _, want := range []string{"--claude", "--accent", `default = "neon"`} {
		if !strings.Contains(joined, want) {
			t.Errorf("nothing warned about %s:\n%s", want, joined)
		}
	}
	// Naming the available set, in the same voice `apply` uses for an unknown
	// colour name — the warning has to be actionable without a second lookup.
	if !strings.Contains(joined, "Available: --accent") {
		t.Errorf("the unknown-token warning does not list what is available:\n%s", joined)
	}
	// What survived: the one good override, and dark as the default the bad one
	// fell back to. A file with a mistake in it must not lose the rest of itself.
	if theme.Dark["--bg"] != "#111" {
		t.Errorf("the valid override did not survive: %v", theme.Dark)
	}
	if _, still := theme.Dark["--accent"]; still {
		t.Error("an unusable colour value was kept")
	}
	if theme.DefaultKind() != ThemeDark {
		t.Errorf("an unknown default did not fall back to dark: %q", theme.DefaultKind())
	}
}

func TestAnUnparseableThemeFileLeavesTheBuiltInPaletteAndSaysSo(t *testing.T) {
	theme := writeTheme(t, `{"dark":{"--bg":"#000",}}`)
	if theme == nil {
		t.Fatal("an unparseable theme file must still report itself")
	}
	if len(theme.Warnings) != 1 || !strings.Contains(theme.Warnings[0], "not valid JSON") {
		t.Fatalf("warnings = %v", theme.Warnings)
	}
	if len(theme.Dark)+len(theme.Light) != 0 {
		t.Error("an unparseable file contributed overrides")
	}
	// The board still has colours. This is the whole reason the loader cannot
	// fail: a trailing comma in a config file must never blank a board.
	if body := rootDeclarations(web.FS, theme); !strings.Contains(body, "--bg:") {
		t.Errorf("the built-in palette did not survive:\n%s", body)
	}
}

// A value is validated rather than sanitised, because it is SPLICED into three
// documents. Nothing that reaches a stylesheet or a <script> may close a tag.
func TestThemeValuesAreValidatedNotSanitised(t *testing.T) {
	good := []string{"#000", "#0a0a0aff", "#a4bd00", "rebeccapurple", "rgb(10 10 10 / 0.6)", "oklch(0.7 0.1 250)"}
	bad := []string{
		"", "#12345", "red;}", `#000" onload="x`, "</style><script>x</script>",
		"url(http://elsewhere/x.png)", "var(--bg)\\", strings.Repeat("#a", 80),
	}
	for _, v := range good {
		if !validThemeValue(v) {
			t.Errorf("%q was refused and is a perfectly good colour", v)
		}
	}
	for _, v := range bad {
		if validThemeValue(v) {
			t.Errorf("%q was accepted", v)
		}
	}
}

/* ---------- the route and the shell ---------- */

func TestTheThemeRouteServesTheValidatedFileAndRevalidates(t *testing.T) {
	srv := testServer(t, htmlTabBoard)

	rec := httptest.NewRecorder()
	srv.route(rec, httptest.NewRequest(http.MethodGet, "http://localhost"+routeTheme, http.NoBody))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a project with no theme file answered %d", rec.Code)
	}

	if err := os.WriteFile(srv.root.ThemeFile(), []byte(`{"version":1,"light":{"--accent":"#123456"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	srv.reloadTheme()

	rec = httptest.NewRecorder()
	srv.route(rec, httptest.NewRequest(http.MethodGet, "http://localhost"+routeTheme, http.NoBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d", routeTheme, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"--accent":"#123456"`) {
		t.Errorf("the served theme is not the file:\n%s", rec.Body.String())
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag: the page revalidates this on every theme ping")
	}

	again := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://localhost"+routeTheme, http.NoBody)
	req.Header.Set("If-None-Match", etag)
	srv.route(again, req)
	if again.Code != http.StatusNotModified {
		t.Errorf("an unchanged theme answered %d, not 304", again.Code)
	}
}

// The shell is handed the theme BEFORE it paints, which is the only way there is
// no flash of the built-in palette. A fetch cannot do it; a splice can.
func TestTheShellCarriesTheProjectThemeBeforeItPaints(t *testing.T) {
	srv := testServer(t, htmlTabBoard)

	rec := httptest.NewRecorder()
	srv.route(rec, httptest.NewRequest(http.MethodGet, "http://localhost/", http.NoBody))
	if !strings.Contains(rec.Body.String(), themePlaceholder) {
		t.Fatalf("a board with no theme file did not serve the shell's own placeholder")
	}

	if err := os.WriteFile(srv.root.ThemeFile(),
		[]byte(`{"version":1,"default":"light","dark":{"--bg":"#010101"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	srv.reloadTheme()

	rec = httptest.NewRecorder()
	srv.route(rec, httptest.NewRequest(http.MethodGet, "http://localhost/", http.NoBody))
	shell := rec.Body.String()
	if strings.Contains(shell, themePlaceholder) {
		t.Fatal("the theme was not spliced — the page will paint the built-in palette and then correct itself")
	}
	if !strings.Contains(shell, `window.ABOARD_THEME = {`) || !strings.Contains(shell, `"--bg":"#010101"`) {
		t.Errorf("the spliced theme is not the file:\n%s", shellHead(shell))
	}
}

// Whatever a theme file says ends up inside a <script>, and a token NAME is
// quoted back verbatim in a warning. So the splice has to escape, and the test
// that says so has to use the shape that would have broken it.
func TestASplicedThemeCannotCloseTheScriptItLandsIn(t *testing.T) {
	srv := testServer(t, htmlTabBoard)
	if err := os.WriteFile(srv.root.ThemeFile(),
		[]byte(`{"version":1,"dark":{"--x</script><script>alert(1)</script>":"#000"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	srv.reloadTheme()

	rec := httptest.NewRecorder()
	srv.route(rec, httptest.NewRequest(http.MethodGet, "http://localhost/", http.NoBody))
	shell := rec.Body.String()
	if strings.Contains(shell, "<script>alert(1)</script>") {
		t.Fatalf("the theme file closed the script it was spliced into:\n%s", shellHead(shell))
	}
	if !strings.Contains(shell, `alert(1)\u003c/script\u003e`) {
		t.Errorf("the escaped form is not there either, so this test is not measuring the splice:\n%s", shellHead(shell))
	}
}

func shellHead(shell string) string {
	if len(shell) > 3000 {
		return shell[:3000]
	}
	return shell
}

/* ---------- the html frame ---------- */

// The frame is a separate document, so the switch has to REACH it. Both variants
// are spliced and the URL says which one to paint first — the alternative,
// serving one variant and reloading on a switch, throws away whatever the widget
// was holding in memory.
func TestTheHTMLFrameCarriesBothThemesAndPaintsTheOneAskedFor(t *testing.T) {
	srv := testServer(t, htmlTabBoard)

	for _, tc := range []struct{ query, want string }{
		{"", `<html lang="en" data-theme="dark">`},
		{"?theme=light", `<html lang="en" data-theme="light">`},
		{"?theme=dark", `<html lang="en" data-theme="dark">`},
		{"?theme=chartreuse", `<html lang="en" data-theme="dark">`},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://localhost/tab/ab1/html"+tc.query, http.NoBody)
		srv.serveTabHTML(rec, req, "ab1")
		frame := rec.Body.String()
		if !strings.Contains(frame, tc.want) {
			t.Errorf("/tab/ab1/html%s did not stamp %s", tc.query, tc.want)
		}
		if !strings.Contains(frame, lightSelector+" {") {
			t.Errorf("/tab/ab1/html%s carries no light variant, so a switch cannot reach it", tc.query)
		}
	}
}

// A project's overrides reach the frame too. They used to be app.css and nothing
// else, which would have meant a house style that stopped at the widget boundary.
func TestTheHTMLFrameInheritsTheProjectsOverrides(t *testing.T) {
	srv := testServer(t, htmlTabBoard)
	if err := os.WriteFile(srv.root.ThemeFile(),
		[]byte(`{"version":1,"dark":{"--bg":"#020202"},"light":{"--bg":"#fdfdfd"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	srv.reloadTheme()

	rec := httptest.NewRecorder()
	srv.serveTabHTML(rec, httptest.NewRequest(http.MethodGet, "http://localhost/tab/ab1/html", http.NoBody), "ab1")
	frame := rec.Body.String()
	for _, want := range []string{"--bg: #020202;", "--bg: #fdfdfd;"} {
		if !strings.Contains(frame, want) {
			t.Errorf("the frame did not inherit %s", want)
		}
	}
}

// A stylesheet with no light variant is the board this project had before the
// switch existed. It must degrade to one theme, never to none.
func TestAStylesheetWithNoLightVariantStillServesAFrame(t *testing.T) {
	assets := fstest.MapFS{"app.css": &fstest.MapFile{
		Data: []byte(":root { color-scheme: dark; --bg: #000; --text: #fff; }"),
	}}
	if block := lightRootBlock(assets, nil); block != "" {
		t.Errorf("a one-theme stylesheet produced a light block: %q", block)
	}
	if body := rootDeclarations(assets, nil); !strings.Contains(body, "--bg: #000") {
		t.Errorf("the dark variant did not survive: %q", body)
	}
}
