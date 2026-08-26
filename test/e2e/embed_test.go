//go:build e2e

package e2e

import (
	"strings"
	"testing"

	"github.com/mxschmitt/playwright-go"

	"github.com/exoport/aboard/pkg/aboard"
)

// Embedding: the three things a host that frames the board needs from it, and
// none of which it can take for itself. The frame is cross-origin by design, so
// the embedder can neither inject CSS to hide the board's own tab strip nor read
// the DOM to learn which tab is on screen — it can only ask in the URL, and be
// told by a message. The third is not a feature at all: a third-party frame is
// where `localStorage` is refused outright, and a refusal used to take tab
// switching down with it.

/* ---------- ?chrome= ---------- */

// `notabs` is what an embedder loads. The button list goes; everything a human
// working inside that embedder still needs stays, because the alternative is
// either stranding them or making every embedder reimplement the board's own
// new-tab dialog.
func TestChromeNotabsHidesTheStripAndKeepsEverythingElse(t *testing.T) {
	s := openChrome(t, "chrome=notabs&tab=bb13")

	if got := s.chrome(); got != "notabs" {
		t.Fatalf("body[data-chrome] is %q, want \"notabs\"", got)
	}
	// Visible, not attached: the rule is CSS keyed off the data attribute, so
	// the shell still BUILDS the strip — which is what keeps `chrome` a viewer's
	// choice about pixels rather than a second code path through the shell.
	if err := expect.Locator(s.page.Locator("#tabs .tab:visible")).ToHaveCount(0); err != nil {
		t.Errorf("the tab strip is still on screen under chrome=notabs: %v", err)
	}
	if n, err := s.page.Locator("#tabs .tab").Count(); err != nil || n == 0 {
		t.Errorf("the shell stopped building the strip entirely (count %d, err %v) — chrome= is meant to hide it, not to fork the shell", n, err)
	}

	for _, sel := range []string{"#add-tab", ".topbar", "#poke", "#tab-note"} {
		if err := expect.Locator(s.page.Locator(sel)).ToBeVisible(); err != nil {
			t.Errorf("%s did not survive chrome=notabs: %v", sel, err)
		}
	}
	// And the deep link still landed: ?chrome= composes with tab addressing
	// rather than replacing it.
	if err := expect.Locator(s.view("bb13")).ToBeVisible(); err != nil {
		t.Errorf("?chrome=notabs&tab=bb13 did not activate the tab: %v", err)
	}
}

// `none` is the screenshot and bare-embedding case: the view, and nothing around
// it.
func TestChromeNoneHidesTheWholeHead(t *testing.T) {
	s := openChrome(t, "chrome=none&tab=bb13")

	if got := s.chrome(); got != "none" {
		t.Fatalf("body[data-chrome] is %q, want \"none\"", got)
	}
	if err := expect.Locator(s.page.Locator(".board-head")).ToBeHidden(); err != nil {
		t.Errorf("the board head survived chrome=none: %v", err)
	}
	if err := expect.Locator(s.view("bb13")).ToBeVisible(); err != nil {
		t.Errorf("chrome=none took the view with it: %v", err)
	}
}

// A typo must do nothing, not blank the UI. `?chrome=notab` (one letter short of
// the real value) is the realistic one, and it has to leave a fully chromed
// board rather than an unrecognised state.
func TestAnUnknownChromeValueFallsBackToFull(t *testing.T) {
	// `constructor` is in the list because the check was once an object literal
	// keyed by value, where every inherited property answers truthy — so that
	// word would have been stamped onto <body> as though the shell had chosen it.
	for _, query := range []string{"chrome=notab", "chrome=", "chrome=FULL", "chrome=constructor"} {
		t.Run(query, func(t *testing.T) {
			s := open(t, query)
			if got := s.chrome(); got != "full" {
				t.Errorf("?%s stamped %q, want \"full\"", query, got)
			}
			if err := expect.Locator(s.page.Locator("#tabs .tab").First()).ToBeVisible(); err != nil {
				t.Errorf("?%s hid the tab strip: %v", query, err)
			}
		})
	}
}

// The board reloads itself when its own code changes, and a reload that dropped
// the query would put the strip back inside an embedder that had asked for it to
// go — silently, and only after a rebuild. The fragment has to survive with it,
// or the panel comes back on a different tab than the one the human was reading.
func TestChromeAndTheDeepLinkSurviveASelfReload(t *testing.T) {
	dev := startDevBoard(t)

	s := openReady(t, dev.url, "chrome=notabs#tab=bb14", "#add-tab")
	if err := expect.Locator(s.view("bb14")).ToBeVisible(); err != nil {
		t.Fatalf("the deep link did not land: %v", err)
	}
	s.markPage()

	// A change to the html is not survivable in place, so the page reloads
	// itself — the same path a `--force` restart takes.
	appendToFile(t, aboard.DevWebFile(dev.webDir, "aboard.html"), "\n<!-- touched by the browser suite -->\n")
	eventually(t, "the page to reload itself", s.pageReloaded)

	// The board is back on screen FIRST, and only then is it asked what it is
	// showing. A reloading page has an empty tab strip for a moment, and every
	// assertion below is one an empty page would sail through.
	s.stripBuilt()
	if err := expect.Locator(s.view("bb14")).ToBeVisible(); err != nil {
		t.Errorf("the self-reload landed on a different tab than the human was reading: %v", err)
	}
	if err := expect.Locator(s.page.Locator("#tabs .tab:visible")).ToHaveCount(0); err != nil {
		t.Errorf("the tab strip came back after the self-reload: %v", err)
	}
	if got := s.chrome(); got != "notabs" {
		t.Errorf("body[data-chrome] is %q after the self-reload, want \"notabs\"", got)
	}
}

/* ---------- {__aboard:'active'} ---------- */

// wrapperHTML is a page that frames the board and records what it is told. It is
// SAME-ORIGIN with the board, served by intercepting a path the board would
// otherwise refuse — which is enough, because the only thing the real embedder
// does differently is be cross-origin, and postMessage with a '*' target and a
// source check behaves identically either way.
//
// The frame carries `?probe=1` so a test can make the shell repaint without
// changing anything, which is what a foreign write looks like from in here.
const wrapperHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>embedder</title></head>
<body style="margin:0">
<iframe id="frame" src="/?chrome=notabs&amp;probe=1" style="width:100%;height:900px;border:0"></iframe>
<script>
  // Everything the frame posts, not only what was expected: a channel is as
  // narrow as what it is checked against.
  window.__msgs = [];
  addEventListener('message', (e) => {
    // Authenticate by SOURCE WINDOW, never by origin — exactly what the real
    // embedder does, since a webview's origin is not knowable in advance.
    if (e.source !== document.getElementById('frame').contentWindow) return;
    window.__msgs.push(e.data);
  });
  window.__active = () => window.__msgs
    .filter((m) => m && m.__aboard === 'active')
    .map((m) => m.tab);
</script>
</body></html>`

// The board switches tabs on its own — [ and ], 1-9, and whichever tab it picks
// at load. A sidebar that can only ever SEND navigation drifts out of sync with
// what the human is looking at within seconds, and a highlight that lies is
// worse than no highlight.
func TestTheBoardTellsAnEmbedderWhichTabIsActive(t *testing.T) {
	s := openWrapper(t)

	// At load, too: the board picks its own initial tab, so an embedder that was
	// only told about ITS OWN navigation would not know what it was showing until
	// the human's first click.
	var atLoad []string
	eventually(t, "the board to announce the tab it opened on", func() bool {
		s.evalJSON(&atLoad, `() => window.__active()`)
		return len(atLoad) > 0
	})
	first := atLoad[len(atLoad)-1]

	// `]` inside the frame — navigation the embedder did not ask for and cannot
	// otherwise observe.
	s.pressInFrame(t, "]")

	var after []string
	eventually(t, "an active message naming a different tab", func() bool {
		s.evalJSON(&after, `() => window.__active()`)
		return len(after) > 0 && after[len(after)-1] != first
	})
	moved := after[len(after)-1]
	if !strings.HasPrefix(moved, "bb") {
		t.Errorf("the active message named %q, which is not a board id", moved)
	}
	// It is the tab actually on screen, not merely a different string.
	if err := expect.Locator(s.page.FrameLocator("#frame").
		Locator(`[data-tab="` + moved + `"][data-active="yes"]`)).ToBeVisible(); err != nil {
		t.Errorf("the board announced %s but is showing something else: %v", moved, err)
	}
}

// Nothing else travels this way, and that is a deliberate constraint rather than
// an accident of what has been written so far: the tab list, the document and
// the notices are read from /aboard.json and /events like every other client. A
// second, weaker channel carrying the same data is a bug factory, and this is
// the assertion that keeps one from growing.
func TestTheBoardPostsNothingButTheActiveTab(t *testing.T) {
	s := openWrapper(t)

	// Drive real traffic first, so "no bad shapes" is not "no shapes".
	for range 3 {
		s.pressInFrame(t, "]")
	}
	var seen []string
	eventually(t, "the board to post something", func() bool {
		s.evalJSON(&seen, `() => window.__msgs.map((m) =>
		  (m && typeof m === 'object') ? Object.keys(m).sort().join('+') : typeof m)`)
		return len(seen) >= 3
	})

	for _, shape := range seen {
		if shape != "__aboard+tab" {
			t.Errorf("the board posted a message shaped {%s} to its embedder; the only envelope that may travel this way is {__aboard, tab}", shape)
		}
	}
	var kinds []string
	s.evalJSON(&kinds, `() => [...new Set(window.__msgs.map((m) => m && m.__aboard))]`)
	if len(kinds) != 1 || kinds[0] != "active" {
		t.Errorf("the board posted the kinds %v, want only [active]", kinds)
	}
}

// A repaint is not a tab change, and the embedder must not be told it was one.
//
// repaint() re-activates the current tab at the end of every pass, and a pass
// runs on every write that arrives over SSE — so an announce made
// unconditionally from inside activate() posts the same id again each time an
// AGENT touches the board. The receiver this exists for answers every message by
// revealing the row in its tree, so the human's sidebar would be dragged back
// under their cursor at the rate somebody else is working. What the embedder
// subscribes to is a change.
func TestARepaintIsNotATabChange(t *testing.T) {
	s := openWrapper(t)

	var atLoad []string
	eventually(t, "the board to announce the tab it opened on", func() bool {
		s.evalJSON(&atLoad, `() => window.__active()`)
		return len(atLoad) > 0
	})

	// The same thing an SSE frame from another actor's write does, minus the
	// write: a full repaint, ending in activate(activeId) with the id unchanged.
	if _, err := s.page.FrameLocator("#frame").Locator("body").
		Evaluate(`() => window.__aboardProbe.repaint()`, nil); err != nil {
		t.Fatalf("repainting the board inside the frame: %v", err)
	}

	// Then a real change, which pins the assertion below without a sleep in it:
	// whatever the repaint posted has been delivered by the time the NEXT message
	// arrives, so "no two in a row name the same tab" is a decidable question
	// rather than a race against a timer.
	s.pressInFrame(t, "]")
	var seen []string
	eventually(t, "an active message naming a different tab", func() bool {
		s.evalJSON(&seen, `() => window.__active()`)
		return len(seen) > 0 && seen[len(seen)-1] != atLoad[len(atLoad)-1]
	})

	for i := 1; i < len(seen); i++ {
		if seen[i] == seen[i-1] {
			t.Errorf("the board announced %s twice in a row (%v) — a repaint was reported as a tab change", seen[i], seen)
			break
		}
	}
}

/* ---------- storage refused ---------- */

// Inside a webview the board is a third-party frame, where storage is
// partitioned and in some configurations refused outright. A refusal is a thrown
// SecurityError, not a null — and both of the UI's only two call sites sat on
// the path everything else depends on: the read is in load(), so the board came
// up EMPTY, and the write is in activate(), so tab switching died with it.
func TestTheBoardWorksWhereStorageIsRefused(t *testing.T) {
	s := open(t, "tab=bb13")

	// Applied to every document created from here on, and then the page is
	// reloaded so the refusal is in place before the shell's first line runs.
	deny := `Object.defineProperty(window, 'localStorage', {
	  configurable: true,
	  get() { throw new DOMException('access denied', 'SecurityError'); },
	});`
	if err := s.page.AddInitScript(playwright.Script{Content: &deny}); err != nil {
		t.Fatalf("installing the storage refusal: %v", err)
	}
	if _, err := s.page.Reload(); err != nil {
		t.Fatalf("reloading with storage refused: %v", err)
	}

	if !s.evalBool(`() => { try { localStorage; return false; } catch { return true; } }`) {
		t.Fatal("the refusal did not take — this test would pass without testing anything")
	}
	// The board came up at all: the read at load time is the first casualty.
	if err := expect.Locator(s.page.Locator("#tabs .tab").First()).ToBeVisible(); err != nil {
		t.Fatalf("the board never rendered with storage refused: %v", err)
	}
	// And the one gesture an embedder depends on still works.
	if err := s.page.Locator(`#tabs .tab[data-id="bb14"]`).Click(); err != nil {
		t.Fatalf("clicking a tab: %v", err)
	}
	if err := expect.Locator(s.view("bb14")).ToBeVisible(); err != nil {
		t.Errorf("tab switching died with storage: %v", err)
	}
	if err := s.page.Locator(`#tabs .tab[data-id="bb111"]`).Click(); err != nil {
		t.Fatalf("clicking a second tab: %v", err)
	}
	if err := expect.Locator(s.view("bb111")).ToBeVisible(); err != nil {
		t.Errorf("the second switch failed: %v", err)
	}
}

/* ---------- helpers ---------- */

// openChrome opens a page whose tab strip is deliberately not on screen, so the
// readiness check is the `+` button — which every chrome mode but `none` keeps.
//
// Then it waits for the strip to be BUILT, and that second wait is the one that
// earns its place. `#add-tab` is in the static markup, so it is on screen before
// the first fetch has even returned — and "no VISIBLE .tab" is trivially true of
// a board that has not drawn its tabs yet. Without this, the notabs assertion
// could pass against a shell with the CSS rule deleted, which is the one thing
// it exists to catch.
func openChrome(t *testing.T, query string) *session {
	t.Helper()
	ready := "#add-tab"
	if strings.Contains(query, "chrome=none") {
		// `none` hides the head the `+` lives in, so the view is the signal — the
		// ACTIVE view, specifically.
		//
		// It was `#views [data-view]`, and that is a race the shell loses by
		// design: load() paints the remembered-or-first tab before goToTarget
		// applies `?tab=`, so a deep link leaves two sections behind — the boot
		// tab, hidden, first in document order, and the addressed one after it.
		// `.First()` resolves to the hidden one, which never becomes visible: the
		// wait timed out at eight seconds and the failure read as "the board never
		// came up" rather than "this selector means the wrong thing". Confirmed
		// pre-existing on a clean checkout, where it fails alone at HEAD; the
		// extra request the mount-receipt sweep makes on activation is only what
		// tipped it. Every other reach in the suite goes through s.view(), which
		// already scopes on data-active; this one had its own spelling and its own
		// answer.
		ready = `#views [data-view][data-active="yes"]`
	}
	s := openReady(t, boardURL, query, ready)
	s.stripBuilt()
	return s
}

// stripBuilt blocks until the shell has drawn its tab buttons, hidden or not.
// Attached, never visible: under `notabs` and `none` they are in the DOM and off
// the screen, which is the whole feature.
func (s *session) stripBuilt() {
	s.t.Helper()
	if err := s.page.Locator("#tabs .tab").First().WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateAttached,
	}); err != nil {
		s.t.Fatalf("the shell never built its tab strip: %v", err)
	}
}

// openWrapper serves the embedder page from the BOARD's own origin by
// intercepting a path the static allow-list would refuse, then loads it.
func openWrapper(t *testing.T) *session {
	t.Helper()

	s := openReady(t, boardURL, "", "#tabs .tab")
	if err := s.page.Route("**/e2e-embedder.html", func(route playwright.Route) {
		_ = route.Fulfill(playwright.RouteFulfillOptions{
			Status:      new(200),
			ContentType: new("text/html; charset=utf-8"),
			Body:        wrapperHTML,
		})
	}); err != nil {
		t.Fatalf("serving the embedder page: %v", err)
	}
	if _, err := s.page.Goto(boardURL + "/e2e-embedder.html"); err != nil {
		t.Fatalf("loading the embedder page: %v", err)
	}
	if err := s.page.FrameLocator("#frame").Locator("#add-tab").
		WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible}); err != nil {
		t.Fatalf("the board never came up inside the embedder: %v", err)
	}
	return s
}

// pressInFrame sends a key to the BOARD inside the wrapper's iframe.
//
// It clicks first, and that is the whole point of the helper. A keystroke goes
// to the focused frame, and `Locator.Press` on the frame's <body> focuses
// nothing — <body> is not focusable — so the key was delivered to the WRAPPER
// document and the board never saw it. The symptom was an `active` message that
// never arrived, which reads exactly like the feature being broken. The click
// target is the board's own <h1>, which has no handler of its own.
func (s *session) pressInFrame(t *testing.T, key string) {
	t.Helper()
	if err := s.page.FrameLocator("#frame").Locator(".topbar h1").Click(); err != nil {
		t.Fatalf("focusing the board inside the frame: %v", err)
	}
	if err := s.page.Keyboard().Press(key); err != nil {
		t.Fatalf("pressing %s inside the board: %v", key, err)
	}
}

// chrome is what the shell stamped on <body>. Read from the DOM rather than from
// the URL, because the stamp is the thing under test: the URL is only what was
// asked for.
func (s *session) chrome() string {
	s.t.Helper()
	var got string
	s.evalJSON(&got, `() => document.body.dataset.chrome || ''`)
	return got
}
