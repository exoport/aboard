//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"

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

// `notabs` is what an embedder loads. The whole tab STRIP goes — the list and
// the `+` with it — and everything a human working inside that embedder still
// needs stays.
//
// The `+` used to stay, on the reasoning that hiding it would either strand the
// human or make every embedder reimplement the board's new-tab dialog. That was
// half right and it cost a whole row of vertical space: with the list gone the
// button sat alone on a line of its own, in the one mode where the host has a
// toolbar of its own to put it in. Reported on 2026-08-27 from the VS Code
// panel. The other half of the old reasoning still holds and is now paid for
// properly — the host does NOT reimplement anything, it posts
// `{__aboard:'newtab'}` and the board opens its own sheet (see the test below).
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
	// The `+` goes with the list it belonged to, and for the same reason: this
	// is the mode where the host owns the tab strip.
	if err := expect.Locator(s.page.Locator("#add-tab")).ToBeHidden(); err != nil {
		t.Errorf("the + button is still taking a row under chrome=notabs: %v", err)
	}
	if n, err := s.page.Locator("#add-tab").Count(); err != nil || n != 1 {
		t.Errorf("the + button stopped being BUILT (count %d, err %v) — the newtab message opens the sheet it owns, so it has to exist", n, err)
	}

	for _, sel := range []string{".topbar", "#poke", "#tab-note"} {
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

	// Ready on `.topbar` rather than `#add-tab`: under notabs the + is hidden
	// with the strip, and a hidden element is not something to wait for.
	s := openReady(t, dev.url, "chrome=notabs#tab=bb14", ".topbar")
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

/* ---------- {__aboard:'newtab'} ---------- */

// A host that hides the strip with `?chrome=notabs` hides the `+` with it, so it
// needs a way back to the one gesture that went missing. This is that way, and
// the shape of it is the point: the host asks, the BOARD opens its own sheet.
//
// The alternative — the host building its own new-tab flow — needs the list of
// types and an empty state for each, which is the board's schema living in a
// second place with nothing to notice when it goes stale. The extension is a
// viewer; this keeps it one.
func TestAnEmbedderCanOpenTheBoardsNewTabSheet(t *testing.T) {
	s := openWrapper(t)
	frame := s.page.FrameLocator("#frame")

	// The premise. If the + were reachable there would be nothing to fix.
	if err := expect.Locator(frame.Locator("#add-tab")).ToBeHidden(); err != nil {
		t.Fatalf("the + is visible under chrome=notabs, so this test is testing nothing: %v", err)
	}
	before := s.tabCount()

	if _, err := s.page.Evaluate(
		`() => document.getElementById('frame').contentWindow.postMessage({ __aboard: 'newtab' }, '*')`, nil); err != nil {
		t.Fatalf("posting newtab into the frame: %v", err)
	}

	if err := expect.Locator(frame.Locator("#new-tab-dialog")).ToBeVisible(); err != nil {
		t.Fatalf("the newtab message did not open the board's own sheet: %v", err)
	}
	// The board's sheet, with the board's own type list in it — not an empty
	// shell the host would have to fill.
	if n, err := frame.Locator("#new-tab-type option").Count(); err != nil || n == 0 {
		t.Errorf("the sheet opened with no types in it (count %d, err %v)", n, err)
	}

	// It OPENS the sheet and stops. An embedder that could create a tab outright
	// would be a host writing to the board with nobody having named anything.
	if got := s.tabCount(); got != before {
		t.Errorf("the newtab message created a tab on its own: %d tabs, was %d", got, before)
	}
	if err := frame.Locator("#new-tab-cancel").Click(); err != nil {
		t.Fatalf("cancelling the sheet: %v", err)
	}
	if got := s.tabCount(); got != before {
		t.Errorf("cancelling the sheet still left a tab behind: %d tabs, was %d", got, before)
	}
}

// Same rule as the theme message, and it matters more here: this one draws a
// MODAL. An `html` tab is a sandboxed frame that can reach `window.top`, so a
// widget could otherwise pop the new-tab sheet over the board whenever it liked.
func TestANewTabRequestFromSomewhereOtherThanTheParentIsIgnored(t *testing.T) {
	s := openWrapper(t)
	frame := s.page.FrameLocator("#frame")

	// Posted from inside the board, so `e.source` is the board's own window and
	// not its parent — the closest a test gets to a sibling frame or a pasted
	// console line. It has to run in the FRAME's realm; posting from the wrapper
	// would make the wrapper the source, which IS the parent.
	board := s.boardFrame()
	if _, err := board.Evaluate(`() => window.postMessage({ __aboard: 'newtab' }, '*')`); err != nil {
		t.Fatalf("posting from inside the board: %v", err)
	}
	time.Sleep(settle)

	if err := expect.Locator(frame.Locator("#new-tab-dialog")).ToBeHidden(); err != nil {
		t.Errorf("a newtab request from a non-parent source opened the sheet: %v", err)
	}
}

/* ---------- {__aboard:'clipboard-image'} ---------- */

// The board asks its HOST to put an image on the clipboard when it cannot do it
// itself, and takes the host's answer.
//
// This exists because a VS Code webview holds a permissions policy that blocks
// the Clipboard API and VS Code offers no way to lift it — but the extension
// host is a Node process and can run xclip. The board does not know or care that
// it is VS Code: it asks whoever framed it, and a host that does not implement
// this never answers, which is the same as a refusal.
func TestTheBoardAsksItsHostToCopyAnImage(t *testing.T) {
	s := openWrapperWithClipboard(t)
	frame := s.page.FrameLocator("#frame")

	// The browser's own clipboard refuses, which is what a panel does.
	board := s.boardFrame()
	if _, err := board.Evaluate(`() => {
		Object.defineProperty(navigator, 'clipboard', {
			configurable: true,
			value: { write: () => Promise.reject(new Error('blocked by a permissions policy')) },
		});
	}`); err != nil {
		t.Fatalf("blocking the clipboard inside the frame: %v", err)
	}

	cropInFrame(t, s)

	// The wrapper stands in for the extension: it answers yes.
	var asks []map[string]any
	eventually(t, "the board to ask its host", func() bool {
		s.evalJSON(&asks, `() => window.__clip`)
		return len(asks) > 0
	})
	ask := asks[len(asks)-1]
	if url, _ := ask["dataUrl"].(string); !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Errorf("the board sent %q, want a PNG data URL", firstChars(url, 32))
	}
	if _, ok := ask["id"].(float64); !ok {
		t.Errorf("the request carries no id, so an answer could not be matched to it: %v", ask)
	}

	// Answered yes, so the board reports a copy and does NOT fall back to the
	// picture — the dialog is the thing a working host makes unnecessary.
	if err := expect.Locator(frame.Locator(".markup-copy-status")).ToContainText("copied"); err != nil {
		got, _ := frame.Locator(".markup-copy-status").TextContent()
		t.Fatalf("the host said yes and the board did not report a copy (status %q): %v", got, err)
	}
	if err := expect.Locator(frame.Locator(".markup-image-dialog[open]")).ToHaveCount(0); err != nil {
		t.Errorf("the host copied it and the board offered the fallback anyway: %v", err)
	}
	// And the selection is spent, exactly as it is after a browser copy.
	if err := expect.Locator(frame.Locator(".markup-crop")).ToHaveCount(0); err != nil {
		t.Errorf("the crop rectangle survived a host copy: %v", err)
	}
}

// A host that says no gets the picture, and the host's REASON — "xclip is not
// installed" is something the human can act on, where the browser's
// "permissions policy" is not.
func TestAHostThatRefusesGivesItsOwnReason(t *testing.T) {
	s := openWrapperWithClipboard(t)
	frame := s.page.FrameLocator("#frame")

	if _, err := s.page.Evaluate(`() => { window.__clipAnswer = { ok: false, error: 'xclip is not installed' }; }`, nil); err != nil {
		t.Fatalf("arming the refusal: %v", err)
	}
	board := s.boardFrame()
	if _, err := board.Evaluate(`() => {
		Object.defineProperty(navigator, 'clipboard', {
			configurable: true,
			value: { write: () => Promise.reject(new Error('blocked by a permissions policy')) },
		});
	}`); err != nil {
		t.Fatalf("blocking the clipboard inside the frame: %v", err)
	}

	cropInFrame(t, s)

	dialog := frame.Locator(".markup-image-dialog[open]")
	if err := expect.Locator(dialog).ToBeVisible(); err != nil {
		t.Fatalf("a refusing host offered nothing: %v", err)
	}
	if err := expect.Locator(dialog).ToContainText("xclip is not installed"); err != nil {
		t.Errorf("the dialog shows the browser's reason instead of the host's actionable one: %v", err)
	}
}

// A host that never announced is still ASKED, and only then named.
//
// The announcement exists to make a failure legible, not to gate the attempt.
// Gating it was a regression the day it shipped: a panel one build older
// announces nothing and copies perfectly well, so the board refused to ask a
// host that would have said yes, then reported the silence it had caused.
//
// This wrapper announces nothing AND answers nothing, which is the only case
// where the "never said what it can do" sentence is the true one.
func TestAHostThatNeverAnnouncedIsAskedAnyway(t *testing.T) {
	s := openWrapper(t) // deliberately NOT the announcing wrapper
	frame := s.page.FrameLocator("#frame")

	if _, err := s.page.Evaluate(`(tab) => {
		const f = document.getElementById('frame');
		f.src = f.getAttribute('src').split('#')[0] + '#tab=' + tab;
	}`, markupTab); err != nil {
		t.Fatalf("pointing the frame at the markup tab: %v", err)
	}
	if err := frame.Locator(".markup-svg").First().
		WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible}); err != nil {
		t.Fatalf("the markup tab never came up: %v", err)
	}

	// Record what reaches the parent, so the ask itself can be asserted rather
	// than inferred from the message the human ends up reading.
	if _, err := s.page.Evaluate(`() => {
		window.__asks = [];
		window.addEventListener('message', (e) => {
			if (e.data && e.data.__aboard === 'clipboard-image') window.__asks.push(e.data.id);
		});
	}`); err != nil {
		t.Fatalf("watching for the ask: %v", err)
	}

	board := s.boardFrame()
	if _, err := board.Evaluate(`() => {
		Object.defineProperty(navigator, 'clipboard', {
			configurable: true,
			value: { write: () => Promise.reject(new Error('blocked by a permissions policy')) },
		});
	}`); err != nil {
		t.Fatalf("blocking the clipboard: %v", err)
	}
	cropInFrame(t, s)

	// The ask goes out even though nothing announced itself. This is the
	// assertion the regression would have failed. Waited for rather than read
	// once: the ask is two async hops behind the click — the clipboard write has
	// to be refused first, and the PNG has to be read as a data URL.
	if _, err := s.page.WaitForFunction(`() => window.__asks.length > 0`, playwright.PageWaitForFunctionOptions{
		Timeout: playwright.Float(5000),
	}); err != nil {
		t.Fatalf("the board did not ask a host that had not announced: %v", err)
	}

	// And then, having waited and heard nothing, it says both halves: the host
	// never announced, and it did not answer either.
	dialog := frame.Locator(".markup-image-dialog[open]")
	if err := expect.Locator(dialog).ToBeVisible(playwright.LocatorAssertionsToBeVisibleOptions{
		Timeout: playwright.Float(12000), // the ask waits six seconds first
	}); err != nil {
		t.Fatalf("no dialog: %v", err)
	}
	if err := expect.Locator(dialog).ToContainText("never said what it can do"); err != nil {
		t.Errorf("the dialog does not name the silent host: %v", err)
	}
	if err := expect.Locator(dialog).ToContainText("did not answer when asked"); err != nil {
		t.Errorf("the dialog does not say it tried anyway: %v", err)
	}
	if err := expect.Locator(dialog).ToContainText(".vsix"); err != nil {
		t.Errorf("the dialog does not say what to do about it: %v", err)
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
// readiness check is `.topbar` — the part of the head that every chrome mode but
// `none` keeps.
//
// It was `#add-tab` until 2026-08-27, when `notabs` started hiding the whole
// `.tabstrip` rather than just the list inside it. The `+` went with the strip,
// so the readiness wait became a wait for something that will never be visible,
// and every notabs test failed as "the board never came up".
//
// Then it waits for the strip to be BUILT, and that second wait is the one that
// earns its place. `.topbar` is in the static markup, so it is on screen before
// the first fetch has even returned — and "no VISIBLE .tab" is trivially true of
// a board that has not drawn its tabs yet. Without this, the notabs assertion
// could pass against a shell with the CSS rule deleted, which is the one thing
// it exists to catch.
func openChrome(t *testing.T, query string) *session {
	t.Helper()
	ready := ".topbar"
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
	// `.topbar h1`, not `#add-tab`: the frame is `?chrome=notabs`, where the +
	// is hidden along with the strip it belongs to, and waiting for a hidden
	// element to become visible fails as "the board never came up".
	if err := s.page.FrameLocator("#frame").Locator(".topbar h1").
		WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible}); err != nil {
		t.Fatalf("the board never came up inside the embedder: %v", err)
	}
	return s
}

// sandboxedWrapperHTML frames the board the way a host with a restrictive
// sandbox does. `allow-modals` is deliberately ABSENT — that is the whole
// experiment — and the three tokens present are the ones a VS Code webview
// grants. `allow-forms` is there because the brief for this test named it and
// because a real webview has it, NOT because the board needs it: views/dialog.js
// has no <form> in it precisely so that a host granting less than this still
// gets a working dialog.
const sandboxedWrapperHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>webview stand-in</title></head>
<body style="margin:0">
<iframe id="frame" sandbox="allow-scripts allow-same-origin allow-forms"
        src="__SRC__" style="width:100%;height:900px;border:0"></iframe>
</body></html>`

// openSandboxedWrapper serves that page from the board's own origin and loads
// it, with `query` handed to the framed board.
func openSandboxedWrapper(t *testing.T, query string) *session {
	t.Helper()

	src := "/"
	if query != "" {
		src += "?" + strings.ReplaceAll(query, "&", "&amp;")
	}
	body := strings.Replace(sandboxedWrapperHTML, "__SRC__", src, 1)

	s := openReady(t, boardURL, "", "#tabs .tab")
	if err := s.page.Route("**/e2e-sandboxed.html", func(route playwright.Route) {
		_ = route.Fulfill(playwright.RouteFulfillOptions{
			Status:      new(200),
			ContentType: new("text/html; charset=utf-8"),
			Body:        body,
		})
	}); err != nil {
		t.Fatalf("serving the sandboxed wrapper: %v", err)
	}
	if _, err := s.page.Goto(boardURL + "/e2e-sandboxed.html"); err != nil {
		t.Fatalf("loading the sandboxed wrapper: %v", err)
	}
	// `.topbar h1` for the same reason openWrapper uses it: this frame is
	// `chrome=notabs`, where the + is hidden along with the strip.
	if err := s.page.FrameLocator("#frame").Locator(".topbar h1").
		WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible}); err != nil {
		t.Fatalf("the board never came up inside the sandboxed frame: %v", err)
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
// tabCount is how many tabs the BOARD is showing, read from inside the frame.
// The strip is hidden under notabs but it is still built, which is what makes it
// a usable count here — and is itself the thing TestChromeNotabs… pins.
// openWrapperWithClipboard frames the board in a page that answers clipboard
// requests the way the extension does — recording them, and replying with
// whatever `window.__clipAnswer` says (yes, by default).
func openWrapperWithClipboard(t *testing.T) *session {
	t.Helper()
	s := openWrapper(t)
	// Onto a markup tab: the wrapper's frame opens on whatever the board picks,
	// and the clipboard lives in one renderer. A fragment-only change fires
	// hashchange without reloading, which is the same way the real host navigates.
	if _, err := s.page.Evaluate(`(tab) => {
		const frame = document.getElementById('frame');
		frame.src = frame.getAttribute('src').split('#')[0] + '#tab=' + tab;
	}`, markupTab); err != nil {
		t.Fatalf("pointing the frame at the markup tab: %v", err)
	}
	if err := s.page.FrameLocator("#frame").Locator(".markup-svg").First().
		WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible}); err != nil {
		t.Fatalf("the markup tab never came up inside the wrapper: %v", err)
	}
	if _, err := s.page.Evaluate(`() => {
		window.__clip = [];
		window.__clipAnswer = { ok: true, tool: 'xclip' };
		const frame = document.getElementById('frame');
		// What the real host does on every frame load: say what it can do, so the
		// board never has to discover it by timing out.
		frame.contentWindow.postMessage({ __aboard: 'host', name: 'vscode-standin', clipboard: true }, '*');
		addEventListener('message', (e) => {
			if (e.source !== frame.contentWindow) return;
			const m = e.data;
			if (!m || m.__aboard !== 'clipboard-image') return;
			window.__clip.push({ id: m.id, dataUrl: m.dataUrl });
			const answer = window.__clipAnswer;
			frame.contentWindow.postMessage({
				__aboard: 'clipboard-result', id: m.id, ok: answer.ok, error: answer.error, tool: answer.tool,
			}, '*');
		});
	}`, nil); err != nil {
		t.Fatalf("installing the clipboard stand-in: %v", err)
	}
	return s
}

// cropInFrame draws a crop rectangle inside the wrapper's board and presses Copy
// region. The scroll is the part worth naming: the wrapper's iframe is a window
// onto a long tab, and an element the board has laid out below that window has
// page coordinates the mouse cannot reach — the drag lands on nothing and the
// failure reads as "Copy region is disabled" three steps later.
func cropInFrame(t *testing.T, s *session) {
	t.Helper()
	frame := s.page.FrameLocator("#frame")
	if err := frame.Locator(`[data-gesture="crop"]`).Click(); err != nil {
		t.Fatalf("choosing the crop tool: %v", err)
	}
	board := s.boardFrame()
	if _, err := board.Evaluate(`() => document.querySelector('.markup-svg').scrollIntoView({ block: 'center' })`); err != nil {
		t.Fatalf("scrolling the image into the frame's view: %v", err)
	}
	svg := frame.Locator(".markup-svg").First()
	box, err := svg.BoundingBox()
	if err != nil || box == nil {
		t.Fatalf("no svg to drag on: %v", err)
	}
	// Bottom right, which is the one corner of the fixture image with no mark on
	// it. A crop drag that STARTS on a mark never begins: onPointerDown selects
	// the mark and returns, so the failure is "Copy region is disabled" three
	// steps later with nothing to say why.
	s.dragPointer(
		point{box.X + box.Width*0.78, box.Y + box.Height*0.78},
		point{box.X + box.Width*0.95, box.Y + box.Height*0.95},
	)
	if err := expect.Locator(frame.Locator(".markup-crop")).ToHaveCount(1); err != nil {
		t.Fatalf("the drag inside the frame drew no crop rectangle: %v", err)
	}
	if err := frame.Locator(`[data-gesture="copy-region"]`).Click(); err != nil {
		t.Fatalf("clicking Copy region: %v", err)
	}
}

func firstChars(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func (s *session) tabCount() int {
	s.t.Helper()
	n, err := s.page.FrameLocator("#frame").Locator("#tabs .tab").Count()
	if err != nil {
		s.t.Fatalf("counting the tabs inside the frame: %v", err)
	}
	return n
}

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
