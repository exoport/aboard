//go:build e2e

package e2e

import (
	"context"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"

	"github.com/exoport/aboard/pkg/aboard"
)

// The theme is the one thing on this board that is entirely per-viewer and
// entirely visual, which is an awkward pair to test: a screenshot proves it and
// cannot be asserted on, and a DOM assertion is easy and proves almost nothing.
//
// So everything here asserts on COMPUTED STYLE — what the browser resolved,
// after the cascade, in the document that is actually on screen — rather than on
// the attribute that caused it. `data-theme="light"` with app.css failing to
// load would satisfy the attribute check and leave a black board.

// computed returns one resolved CSS custom property from the page's root.
func (s *session) rootToken(name string) string {
	s.t.Helper()
	var got string
	s.evalJSON(&got, `(n) => getComputedStyle(document.documentElement).getPropertyValue(n).trim()`, name)
	return got
}

// widgetFrame is an html tab's sandboxed frame as a Frame handle.
//
// It cannot be reached through the DOM: the frame is `sandbox="allow-scripts"`
// with no `allow-same-origin`, so its origin is opaque and `contentDocument` is
// null from the parent — which is the containment working, and is asserted
// elsewhere. Playwright crosses that boundary because it drives the frame's own
// realm rather than the parent's, and that is the single largest reason this
// suite is Playwright rather than a hand-rolled CDP client.
func (s *session) widgetFrame(tabID string) playwright.Frame {
	s.t.Helper()
	// Two spellings, because a BLOCK's id is compound (`ab32/ab197`) and
	// views/html.js encodeURIComponent()s it into the path — so the frame's URL
	// carries `%2F` where the id has a slash, and whether the browser hands that
	// back encoded or decoded is not ours to depend on.
	want := []string{
		"/tab/" + tabID + "/html",
		"/tab/" + strings.ReplaceAll(tabID, "/", "%2F") + "/html",
	}
	var found playwright.Frame
	eventually(s.t, "the widget frame for "+tabID+" to attach", func() bool {
		for _, f := range s.page.Frames() {
			for _, w := range want {
				if strings.Contains(f.URL(), w) {
					found = f
					return true
				}
			}
		}
		return false
	})
	return found
}

// frameToken reads one resolved custom property from inside a widget frame.
func (s *session) frameToken(tabID, name string) string {
	s.t.Helper()
	got, err := s.widgetFrame(tabID).Evaluate(
		`(n) => getComputedStyle(document.documentElement).getPropertyValue(n).trim()`, name)
	if err != nil {
		return ""
	}
	text, _ := got.(string)
	return text
}

func (s *session) themeAttr() string {
	s.t.Helper()
	var got string
	s.evalJSON(&got, `() => document.documentElement.getAttribute('data-theme') || ''`)
	return got
}

// Pressing the switch changes the colours, and the change is THIS viewer's. A
// board is a shared document and a theme is not part of it — two people can look
// at one board in the same second and must disagree about this.
func TestTheThemeSwitchIsPerViewerAndSurvivesAReload(t *testing.T) {
	s := open(t, "tab=ab133")

	if got := s.themeAttr(); got != "dark" {
		t.Fatalf("a fresh viewer booted %q, not dark", got)
	}
	darkBg := s.rootToken("--bg")

	if err := s.page.Locator("#theme").Click(); err != nil {
		t.Fatalf("pressing the theme switch: %v", err)
	}
	if got := s.themeAttr(); got != "light" {
		t.Fatalf("after the switch, data-theme is %q", got)
	}
	lightBg := s.rootToken("--bg")
	if lightBg == darkBg {
		t.Fatalf("--bg is %q in both themes — the attribute moved and the cascade did not", lightBg)
	}
	// The label says which theme is on. A toggle whose label does not move is one
	// nobody can read the state of.
	if err := expect.Locator(s.page.Locator("#theme")).ToContainText("light"); err != nil {
		t.Errorf("the switch does not say which theme is on: %v", err)
	}

	// It survives the page throwing everything away, because the choice is in
	// localStorage and stamped before the first paint. This is the case the
	// self-reload makes real: a code change reloads an open page, and losing the
	// theme every time somebody rebuilt would be worse than not having one.
	if _, err := s.page.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if err := s.page.Locator("#tabs .tab").First().WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	}); err != nil {
		t.Fatalf("the board never came back: %v", err)
	}
	if got := s.themeAttr(); got != "light" {
		t.Errorf("the theme did not survive a reload: %q", got)
	}
	if got := s.rootToken("--bg"); got != lightBg {
		t.Errorf("--bg after the reload is %q, was %q", got, lightBg)
	}

	// A second viewer is untouched. Its own context means its own storage, which
	// is exactly the isolation a per-viewer preference has to have.
	other := open(t, "tab=ab133")
	if got := other.themeAttr(); got != "dark" {
		t.Errorf("a second viewer inherited the first one's theme: %q", got)
	}
}

// The html tab's frame is a SEPARATE DOCUMENT with its own :root, so the switch
// cannot reach it through the cascade. It is told, and it must be told fast
// enough that the two halves of the screen are never visibly disagreeing.
func TestAnHTMLFrameFollowsTheThemeSwitch(t *testing.T) {
	s := open(t, "tab=ab72")

	frameBg := func() string { return s.frameToken("ab72", "--bg") }
	// An html BLOCK inside a stack is the same document under a compound id,
	// mounted by a renderer the shell never speaks to directly — so it is mounted
	// HERE, before the switch, or the test would only prove that a frame loading
	// after a switch gets the right `?theme=`, which is the other half.
	blockBg := func() string { return s.frameToken("ab32/ab197", "--bg") }
	s.tab("ab32")
	eventually(t, "the stacked widget frame to come up", func() bool { return blockBg() != "" })
	s.tab("ab72")

	eventually(t, "the widget frame to come up", func() bool { return frameBg() != "" })
	before, blockBefore := frameBg(), blockBg()

	if err := s.page.Locator("#theme").Click(); err != nil {
		t.Fatalf("pressing the theme switch: %v", err)
	}
	eventually(t, "the widget frame to follow the theme", func() bool { return frameBg() != before })
	eventually(t, "the stacked widget frame to follow the theme", func() bool { return blockBg() != blockBefore })

	// And it followed by being TOLD, not by being thrown away and rebuilt: a
	// reload would lose whatever the widget was holding — a half-drawn stroke, a
	// scroll position, a simulation mid-run.
	if err := expect.Locator(s.widget("ab72").Locator("canvas")).ToBeVisible(); err != nil {
		t.Errorf("the widget's canvas is gone after the switch — the frame was reloaded, not told: %v", err)
	}

	if got, want := blockBg(), frameBg(); got != want {
		t.Errorf("the html block inside the stack is on %q while the html tab is on %q", got, want)
	}
}

// A rendered mermaid diagram has the token VALUES baked into its SVG — mermaid
// reads them once and writes literal colours. So it is the one thing a custom
// property cannot fix, and the only proof is that the markup itself changed.
func TestAMermaidDiagramIsReRenderedOnAThemeSwitch(t *testing.T) {
	s := open(t, "tab=ab14")

	fill := func() string {
		var got string
		s.evalJSON(&got, `() => {
			const node = document.querySelector('[data-tab="ab14"][data-active="yes"] .diagram-render svg .node rect, '
				+ '[data-tab="ab14"][data-active="yes"] .diagram-render svg rect');
			return node ? getComputedStyle(node).fill : '';
		}`)
		return got
	}
	eventually(t, "the diagram to render", func() bool { return fill() != "" })
	before := fill()

	if err := s.page.Locator("#theme").Click(); err != nil {
		t.Fatalf("pressing the theme switch: %v", err)
	}
	eventually(t, "the diagram to be re-rendered in the new theme", func() bool { return fill() != before })
}

/* ---------- .aboard/theme.json ---------- */

// A board with a house style, on its own server: the theme file changes what
// every viewer sees by default, so it cannot be applied to the shared board
// without changing the colours under forty other tests.
func startThemedBoard(t *testing.T, themeJSON string) string {
	t.Helper()
	return startThemedBoardIn(t, t.TempDir(), themeJSON)
}

// startThemedBoardIn is the same with the directory named, for the one test that
// has to edit the theme file after the board is up.
func startThemedBoardIn(t *testing.T, dir, themeJSON string) string {
	t.Helper()

	if _, err := aboard.Init(aboard.InitConfig{Dir: dir, Example: true}); err != nil {
		t.Fatalf("seeding the themed board: %v", err)
	}
	root := aboard.Root(dir)
	if err := os.WriteFile(root.ThemeFile(), []byte(themeJSON), 0o644); err != nil {
		t.Fatalf("writing theme.json: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	port, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	url := "http://127.0.0.1:" + strconv.Itoa(port)

	served := make(chan error, 1)
	go func() {
		served <- aboard.Serve(ctx, aboard.Options{
			Host: aboard.HostStandalone, Argv0: "aboard", Logger: log.New(io.Discard, "", 0),
		}, aboard.ServeConfig{Root: root, Port: port})
	}()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-served:
			t.Fatalf("the themed board stopped: %v", err)
		default:
		}
		if aboard.ProbeBoard(ctx, port, "") != nil {
			return url
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the themed board never answered /health")
	return ""
}

// `default: light` decides for a viewer who has never chosen — and stops
// deciding the moment they do. Both halves matter: a project default that
// overrode a human's own choice would be a preference that cannot be kept.
func TestAProjectDefaultDecidesUntilTheViewerDoes(t *testing.T) {
	url := startThemedBoard(t, `{"version":1,"default":"light","light":{"--accent":"#123456"}}`)

	fresh := openAt(t, url, "tab=ab133")
	if got := fresh.themeAttr(); got != "light" {
		t.Fatalf("a fresh viewer of a light-default board booted %q", got)
	}
	// The override reached the cascade, not merely the JSON. Browsers report a
	// resolved colour, so compare on what it resolves TO.
	if got := fresh.rootToken("--accent"); !strings.Contains(got, "#123456") && !strings.Contains(got, "18, 52, 86") {
		t.Errorf("--accent is %q, not the project's #123456", got)
	}
	// It reaches an html tab's frame too — a house style that stopped at the
	// widget boundary would be a house style with a hole in it.
	fresh.tab("ab72")
	eventually(t, "the widget frame to inherit the project's accent", func() bool {
		return strings.Contains(fresh.frameToken("ab72", "--accent"), "#123456")
	})

	// A stored choice beats the project's default.
	chose := openAt(t, url, "tab=ab133")
	if err := chose.page.Locator("#theme").Click(); err != nil {
		t.Fatalf("pressing the theme switch: %v", err)
	}
	if _, err := chose.page.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if err := chose.page.Locator("#tabs .tab").First().WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	}); err != nil {
		t.Fatalf("the board never came back: %v", err)
	}
	if got := chose.themeAttr(); got != "dark" {
		t.Errorf("the project default overrode the viewer's own choice: %q", got)
	}
}

// A house style is something a person ITERATES on: change a hex value, alt-tab,
// and the board is either right or wrong in front of them. So an edit to
// theme.json reaches an open page on its own, over the same stream a state
// change rides — and without reloading, because a reload would throw away
// whatever they were in the middle of.
func TestEditingTheProjectThemeReachesAnOpenPage(t *testing.T) {
	dir := t.TempDir()
	url := startThemedBoardIn(t, dir, `{"version":1,"dark":{"--accent":"#112233"}}`)

	// On the html tab, because the FRAME is the half of this that a cascade
	// cannot reach: both variants were spliced into that document when it loaded,
	// so an edit to theme.json changes values the frame is not looking at. A
	// house style that stopped at the widget boundary the moment somebody
	// iterated on it would stop exactly when they were watching.
	s := openAt(t, url, "tab=ab72")
	s.markPage()
	is := func(got, hex, rgb string) bool {
		return strings.Contains(got, hex) || strings.Contains(got, rgb)
	}
	eventually(t, "the first accent to apply", func() bool {
		return is(s.rootToken("--accent"), "#112233", "17, 34, 51")
	})
	eventually(t, "the widget frame to take the first accent", func() bool {
		return is(s.frameToken("ab72", "--accent"), "#112233", "17, 34, 51")
	})

	if err := os.WriteFile(aboard.Root(dir).ThemeFile(),
		[]byte(`{"version":1,"dark":{"--accent":"#445566"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	eventually(t, "the edited theme to reach the open page", func() bool {
		return is(s.rootToken("--accent"), "#445566", "68, 85, 102")
	})
	eventually(t, "the edited theme to reach the widget frame", func() bool {
		return is(s.frameToken("ab72", "--accent"), "#445566", "68, 85, 102")
	})
	if s.pageReloaded() {
		t.Error("the page reloaded to pick up a colour — scroll, selection and anything half-typed are gone")
	}
	// And the frame was TOLD, not rebuilt: the canvas the human drew on is still
	// the one that was there.
	if err := expect.Locator(s.widget("ab72").Locator("canvas")).ToBeVisible(); err != nil {
		t.Errorf("the widget's canvas is gone after a theme edit — the frame was reloaded: %v", err)
	}
}

/* ---------- a theme handed over by an embedder ---------- */

// boardFrame is the wrapper's <iframe> as a Frame — a handle that can evaluate
// IN the board's own realm, which a FrameLocator cannot.
func (s *session) boardFrame() playwright.Frame {
	s.t.Helper()
	for _, f := range s.page.Frames() {
		if strings.Contains(f.URL(), "chrome=notabs") {
			return f
		}
	}
	s.t.Fatal("the board frame is not in the page")
	return nil
}

// A host that frames the board hands it a palette — the VS Code panel derives
// one from the editor's theme, so the board belongs in the window instead of
// being a dark rectangle inside a light IDE.
//
// Authenticated by SOURCE, never by origin, which is the same rule the `active`
// message obeys going the other way and the same rule an html tab's bridge obeys.
func TestAnEmbedderCanHandTheBoardATheme(t *testing.T) {
	s := openWrapper(t)

	framedToken := func(name string) string {
		var got string
		s.evalJSON(&got, `(n) => {
			const f = document.getElementById('frame');
			return getComputedStyle(f.contentDocument.documentElement).getPropertyValue(n).trim();
		}`, name)
		return got
	}
	before := framedToken("--bg")

	s.evalJSON(new(any), `() => {
		document.getElementById('frame').contentWindow.postMessage(
			{ __aboard: 'theme', kind: 'light', tokens: { '--bg': '#fff8e7', '--nonsense': '#000' } }, '*');
		return null;
	}`)

	eventually(t, "the board to take the embedder's ground", func() bool {
		return strings.Contains(framedToken("--bg"), "255, 248, 231") ||
			strings.Contains(framedToken("--bg"), "#fff8e7")
	})
	if got := framedToken("--bg"); got == before {
		t.Errorf("--bg is still %q", got)
	}

	// The variant came across with the tokens, so a host's dark/light state is
	// not something the board has to infer from the colours it was handed.
	var attr string
	s.evalJSON(&attr, `() => document.getElementById('frame').contentDocument.documentElement.getAttribute('data-theme')`)
	if attr != "light" {
		t.Errorf("the embedder's kind did not reach the board: %q", attr)
	}

	// And it is written NOWHERE: not the state file, not storage. A host's
	// opinion is not the human's choice.
	var stored string
	s.evalJSON(&stored, `() => {
		try { return document.getElementById('frame').contentWindow.localStorage.getItem('aboard.theme') || ''; }
		catch (e) { return 'unreadable'; }
	}`)
	if stored != "" && stored != "unreadable" {
		t.Errorf("the embedder's theme was stored as the viewer's own choice: %q", stored)
	}
}

// A message that did not come from the parent is ignored. The board has no
// authentication, so every channel into it is a channel somebody else can shout
// down — this one is closed by comparing the source WINDOW, which nothing but
// the real parent can forge.
func TestAThemeFromSomewhereOtherThanTheParentIsIgnored(t *testing.T) {
	s := openWrapper(t)

	framedBg := func() string {
		var got string
		s.evalJSON(&got, `() => getComputedStyle(
			document.getElementById('frame').contentDocument.documentElement).getPropertyValue('--bg').trim()`)
		return got
	}
	before := framedBg()

	// Posted FROM INSIDE the board, so `e.source` is the board's own window rather
	// than its parent. That is the closest a test can get to a stranger on this
	// origin — a sibling frame, an opener, a script somebody pasted into the
	// console — and it is exactly the case the source check exists for.
	//
	// It has to run in the FRAME's own realm: posting from the wrapper, however it
	// is spelt, makes the wrapper the source, which IS the parent and would be
	// accepted correctly. That distinction is the entire test, so it is worth
	// spelling out rather than trusting a one-liner to have got it right.
	board := s.boardFrame()
	if _, err := board.Evaluate(
		`() => window.postMessage({ __aboard: 'theme', kind: 'light', tokens: { '--bg': '#ff00ff' } }, '*')`); err != nil {
		t.Fatalf("posting from inside the board: %v", err)
	}
	time.Sleep(settle)

	if got := framedBg(); got != before {
		t.Errorf("a theme from a non-parent source was applied: --bg went from %q to %q", before, got)
	}
}

// The **Add colours** button writes literal hex values into the diagram source —
// that is its job, since a `classDef` is text in the document and cannot
// reference a custom property. Which means it is the one control on the board
// that can bake a colour that is wrong for the theme it was pressed in.
//
// It did. The ink on each solid fill was a hand-picked near-black, one per hue,
// chosen when the board had one theme: on a light board every hue darkens and
// the four inks did not, so `accentFill` came out as #151515 on #454f00 — 1.5:1,
// a black label on a dark olive box, produced by the board's own button. The ink
// is `--accent-ink` now, whose entire job is "text ON a saturated ground of this
// theme".
//
// On its own board, because pressing the button WRITES to the document and the
// shared board is read by everything else in this file.
func TestTheDiagramPaletteButtonInksForTheThemeItIsPressedIn(t *testing.T) {
	url := startThemedBoard(t, `{"version":1,"default":"light"}`)
	s := openAt(t, url, "tab=ab14")

	if got := s.themeAttr(); got != "light" {
		t.Fatalf("the themed board booted %q", got)
	}
	ink := s.rootToken("--accent-ink")
	if ink == "" {
		t.Fatal("--accent-ink does not resolve, so this test cannot measure anything")
	}

	if err := s.control("ab14", "palette").Click(); err != nil {
		t.Fatalf("pressing Add colours: %v", err)
	}

	var source string
	s.evalJSON(&source, `() => document.querySelector(
		'[data-tab="ab14"] [data-role="source"]').value`)
	if !strings.Contains(source, "classDef accentFill") {
		t.Fatalf("the palette block is not in the source:\n%s", source)
	}
	if !strings.Contains(source, "color:"+ink) {
		t.Errorf("a solid fill is not inked with --accent-ink (%s):\n%s", ink, source)
	}
	// The four literals that used to be here. Named rather than inferred: this is
	// the regression, and a test that only checked the good value would pass on a
	// palette that emitted both.
	for _, dead := range []string{"#151515", "#08141a", "#1a0f00", "#12142b"} {
		if strings.Contains(source, dead) {
			t.Errorf("the palette block still carries the hard-coded dark ink %s:\n%s", dead, source)
		}
	}
}
