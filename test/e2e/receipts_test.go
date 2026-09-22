//go:build e2e

package e2e

import (
	"slices"
	"strings"
	"testing"

	"github.com/exoport/aboard/pkg/aboard"
	"github.com/mxschmitt/playwright-go"
)

// Neither test registers gesture coverage. Both drive CONTROLS — a declared
// button id — and the coverage gate is about the `gestures` list, which is the
// half of the manifest with no other consumer. dag and notes are both already
// covered by tests that drive their real gestures; claiming a second, invented
// sentence here would fail the gate for saying something no spec declares, which
// is the gate working.

// Mount receipts, end to end: the browser draws a tab and the agent finds out
// what it drew.
//
// This is the loop `aboard apply` could not close on its own. "applied", exit 0,
// is evidence a write was ACCEPTED — an unknown `ui` component draws a marker and
// an unknown PROP draws nothing at all — so the only reader who ever saw the
// result was the human, which is backwards: the agent is the one still holding
// the context to fix it.
//
// Asserted against a real Chromium mounting the real shell, because that is the
// only place the claim is made. A unit test of the sweep would assert that a
// function reads attributes, which is not the thing anybody doubts.
func TestTheBrowserReportsWhatItRendered(t *testing.T) {
	s := open(t, "")

	// The gallery first, for the plain case: a tab was mounted and said so.
	s.tab("ab133")
	eventually(t, "the ui gallery's receipt to arrive", func() bool {
		return receiptFor(t, "ab133").Mounts >= 1
	})
	if got := receiptFor(t, "ab133"); got.Type != "ui" {
		t.Errorf("unexpected receipt for the gallery: %+v", got)
	}

	// A tab with declared controls: the ids reported are the ones
	// views/dag.spec.json declares, which is what makes this not a DOM sweep.
	s.tab("ab1")
	eventually(t, "the dag's receipt to arrive", func() bool {
		return len(receiptFor(t, "ab1").Controls) > 0
	})
	dag := receiptFor(t, "ab1")
	if !containsID(dag.Controls, "relayout") {
		t.Errorf("the dag's declared controls were not reported: %+v", dag.Controls)
	}
	if len(dag.Undeclared) != 0 {
		t.Errorf("the dag drew an undeclared control: %v", dag.Undeclared)
	}

	// And a press. A recorded press proves the control was REACHED and nothing
	// more, which is one of the two limits the command prints about itself.
	if err := s.control("ab1", "relayout").Click(); err != nil {
		t.Fatalf("pressing the dag's relayout control: %v", err)
	}
	eventually(t, "the press to be reported", func() bool {
		return receiptFor(t, "ab1").Fired["relayout"] >= 1
	})
}

// The gallery's "Unknown" panel holds a `sparkline` on purpose, to demonstrate
// the unknown-component marker. It is a TRUE POSITIVE and must keep being
// reported — "fixing" it by deleting the demonstration is the temptation this
// pins against.
//
// It is also the reason a receipt is swept on ACTIVATION and not only on mount:
// `ui`'s `tabs` component builds only the open panel, so until the human reveals
// the fifth one the component is genuinely not drawn, and a mount-only sweep
// would report the board as clean forever. Reveal it, come back to the tab, and
// the marker reaches the agent.
func TestAnUnknownComponentTheHumanRevealedReachesTheAgent(t *testing.T) {
	s := open(t, "")
	view := s.tab("ab133")

	if err := view.Locator(`.uic-tab:has-text("Unknown")`).Click(); err != nil {
		t.Fatalf("opening the gallery's Unknown panel: %v", err)
	}
	if err := expect.Locator(view.Locator(".uic-unknown").First()).ToBeVisible(); err != nil {
		t.Fatalf("the unknown-component marker is not on screen: %v", err)
	}

	// Away and back: the re-sweep happens when a tab becomes active.
	s.tab("ab1")
	s.tab("ab133")

	eventually(t, "the unknown-component marker to be reported", func() bool {
		return containsID(receiptFor(t, "ab133").Unknown, "sparkline")
	})
}

// receiptFor reads the sidecar the same way `aboard rendered` does. Straight off
// disk: the file is written by the server, and reading it needs no server, which
// is the property that lets a session ask after the board has stopped.
func receiptFor(t *testing.T, tab string) aboard.Receipt {
	t.Helper()
	list, err := aboard.Rendered(t.Context(), board, "", tab)
	if err != nil {
		t.Fatalf("reading receipts: %v", err)
	}
	if len(list) == 0 {
		return aboard.Receipt{}
	}
	return list[0]
}

func containsID(list []string, want string) bool { return slices.Contains(list, want) }

// The change banner answers half a question — "somebody changed this" — and the
// journal has held the other half all along with nothing in the UI able to reach
// it. The link is READ-ONLY on purpose: it shows the previous state and prints
// the command that puts it back, because restoring from a button would be a
// write the human made without seeing the document it produces, on a board where
// a bad write is the thing history exists to recover from.
func TestTheChangeBannerLinksToWhatTheTabSaidBefore(t *testing.T) {
	id := makeScratchTab(t, "History probe")

	// A second agent write, so the journal holds a `before` for this tab: the
	// write that CREATES a tab has no previous state, and offering that as a
	// version would offer a restore that blanks it.
	d := readDoc(t)
	d.state(t, id)["text"] = "the second thing it said\n"
	apply(t, d)

	// After the writes, never before: a write issued once the page is up is a
	// foreign change racing the thing under test.
	s := open(t, "")
	s.tab(id)

	banner := s.view(id).Locator(".banner").First()
	if err := expect.Locator(banner).ToContainText("changed this tab"); err != nil {
		t.Fatalf("no change banner on a tab an agent just wrote to: %v", err)
	}

	link := s.view(id).Locator(`button:has-text("What it said before")`)
	if err := link.Click(); err != nil {
		t.Fatalf("pressing the history link: %v", err)
	}
	prev := s.view(id).Locator(".history-prev")
	if err := expect.Locator(prev).ToBeVisible(); err != nil {
		t.Fatalf("the previous state never appeared: %v", err)
	}
	if err := expect.Locator(prev).ToContainText("scratch"); err != nil {
		t.Errorf("the panel does not show what the tab said before: %v", err)
	}
	// The one command that puts it back, in the panel, because the id alone is
	// not enough going TO the human.
	if err := expect.Locator(prev).ToContainText("aboard history " + id + " --at 1"); err != nil {
		t.Errorf("the panel does not say how to restore it: %v", err)
	}

	text, err := prev.TextContent()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "the second thing it said") {
		t.Error("the panel is showing the CURRENT state, not the previous one")
	}

	// A second press hides it again: it is a toggle on a notice, not a panel the
	// human has to dismiss some other way.
	if err := link.Click(); err != nil {
		t.Fatal(err)
	}
	if err := expect.Locator(prev).ToBeHidden(); err != nil {
		t.Errorf("the panel did not toggle shut: %v", err)
	}
}

// A page `aboard shot` loads (`?shot=1`) posts no receipt, and paints a
// deep-linked node at scroll 0.
//
// The receipt half: `wait --for "rendered <id>"` is a session waiting for a
// PERSON to have the tab open, and an agent photographing the tab must not
// release it, nor count as a mount in `aboard rendered`. Made non-vacuous by
// opening an ordinary page afterwards: its receipt arrives, and the count it
// arrives at says whether the shot's page posted one first.
//
// The scroll half: chromium's --screenshot draws a scrolled document wrongly (a
// node 1600px down came out as a black frame), so a shot page hands the offset
// to the views as a transform and stays at scroll 0.
func TestAShotPagePostsNoReceiptAndPaintsAtScrollZero(t *testing.T) {
	id := makeScratchTabOfType(t, "Shot probe", "ui", map[string]any{
		"data": map[string]any{},
		"root": map[string]any{"type": "col", "children": []any{
			map[string]any{"type": "title", "value": "Top of the tab"},
			map[string]any{"type": "spacer", "size": "1600px"},
			map[string]any{"type": "notice", "id": "far", "value": "down here"},
			map[string]any{"type": "spacer", "size": "600px"},
		}},
	})

	shot := open(t, "tab="+id+"&shot=1&nosse=1&node=far")
	far := shot.view(id).Locator(`[data-ui-id="far"]`)
	if err := expect.Locator(far).ToBeVisible(); err != nil {
		t.Fatalf("the deep-linked node never drew: %v", err)
	}
	got, err := shot.page.Evaluate(`() => ({ y: scrollY, shift: document.getElementById('views').style.transform,
		top: document.querySelector('[data-ui-id="far"]').getBoundingClientRect().top })`)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := got.(map[string]any)
	if y, _ := m["y"].(float64); y != 0 {
		t.Errorf("a shot page is scrolled to %v; chromium's --screenshot draws that wrongly", y)
	}
	if shift, _ := m["shift"].(string); !strings.HasPrefix(shift, "translateY(-") {
		t.Errorf("the deep link's offset was not handed to the views: transform %q", shift)
	}
	if top, _ := m["top"].(float64); top < 0 || top > 900 {
		t.Errorf("the node sits at %vpx, outside the window a picture would show", top)
	}

	s := open(t, "tab="+id)
	if err := expect.Locator(s.view(id)).ToBeVisible(); err != nil {
		t.Fatalf("the tab never mounted in an ordinary page: %v", err)
	}
	eventually(t, "the ordinary page's receipt", func() bool { return receiptFor(t, id).Mounts >= 1 })
	if n := receiptFor(t, id).Mounts; n != 1 {
		t.Errorf("the tab has %d mounts after one ordinary page and one shot page; the shot posted a receipt", n)
	}
}

// What does not fit reaches the agent: a `ui` component whose content is wider
// than its box is in the receipt, named, with the panel it is in, the width the
// browser had and how far it runs over. A panel that fits reports nothing, and
// switching to one that does not sweeps again — the panel switch is not a
// declared control, so without the re-sweep the receipt would describe a panel
// nobody is looking at.
func TestWhatDoesNotFitReachesTheReceipt(t *testing.T) {
	long := "https://example.com/" + strings.Repeat("a", 300)
	id := makeScratchTabOfType(t, "Clip probe", "ui", map[string]any{
		"data": map[string]any{},
		"root": map[string]any{"type": "tabs", "panels": []any{
			map[string]any{"label": "Fits", "children": []any{map[string]any{"type": "text", "value": "short"}}},
			map[string]any{"label": "Wide", "children": []any{
				map[string]any{"type": "card", "title": "Proposed", "children": []any{map[string]any{"type": "text", "value": long}}},
				map[string]any{"type": "code", "value": strings.Repeat("1234567890 ", 60)},
			}},
		}},
	})

	s := open(t, "tab="+id)
	eventually(t, "the mount's receipt", func() bool { return receiptFor(t, id).Mounts >= 1 })
	if got := receiptFor(t, id); len(got.Clipped) != 0 || got.Width != 1400 {
		t.Errorf("a panel that fits reported %+v at width %d", got.Clipped, got.Width)
	}

	panel := s.view(id).Locator(".uic-tabs button").Filter(playwright.LocatorFilterOptions{HasText: "Wide"}).First()
	if err := panel.Click(); err != nil {
		t.Fatalf("opening the Wide panel: %v", err)
	}
	eventually(t, "the re-sweep after the panel switch", func() bool { return len(receiptFor(t, id).Clipped) >= 2 })

	var spill, scroll *aboard.Clip
	clips := receiptFor(t, id).Clipped
	for i := range clips {
		switch {
		case clips[i].Kind == aboard.ClipSpill && strings.HasPrefix(clips[i].Where, `text "https://example.com/`):
			spill = &clips[i]
		case clips[i].Kind == aboard.ClipScroll && strings.HasPrefix(clips[i].Where, "code"):
			scroll = &clips[i]
		}
	}
	if spill == nil || spill.X <= 0 || !strings.HasSuffix(spill.Where, "(panel Wide)") {
		t.Errorf("the unbroken URL was not reported as spilling, in its panel: %+v", clips)
	}
	if scroll == nil || scroll.X <= 0 {
		t.Errorf("the wide code block was not reported as scrolling: %+v", clips)
	}
	// Innermost only: the card the URL spills out of spills too, and saying so
	// would be the same finding once per level of nesting.
	for _, c := range clips {
		if strings.HasPrefix(c.Where, "card") {
			t.Errorf("an ancestor of a spill was reported as well: %+v", c)
		}
	}
}

// An html widget measures itself from inside its frame, which the parent cannot
// read: a fixed stage with overflow hidden whose text runs past its bottom is a
// CUT, and it reaches the receipt once the frame has loaded and reported.
func TestAnHTMLWidgetReportsWhatItCuts(t *testing.T) {
	html := `<style>body{margin:0}.stage{width:600px;height:120px;overflow:hidden}</style>` +
		`<div class="stage" id="slide-1"><p>` + strings.Repeat("A slide whose text runs past its stage. ", 60) + `</p></div>`
	id := makeScratchTabOfType(t, "Cut probe", "html", map[string]any{"html": html, "data": map[string]any{}})

	open(t, "tab="+id)
	eventually(t, "the frame's report to reach the receipt", func() bool {
		for _, c := range receiptFor(t, id).Clipped {
			if c.Where == "widget div#slide-1" && c.Kind == aboard.ClipCut && c.Y > 0 {
				return true
			}
		}
		return false
	})
}

// A page `aboard shot` loads writes the same sweep into itself, where the
// command reads it back out of --dump-dom, instead of posting a receipt.
func TestAShotPageWritesItsReportIntoThePage(t *testing.T) {
	id := makeScratchTabOfType(t, "Shot report probe", "ui", map[string]any{
		"data": map[string]any{},
		"root": map[string]any{"type": "code", "value": strings.Repeat("1234567890 ", 60)},
	})
	s := open(t, "tab="+id+"&shot=1&nosse=1")
	report := s.page.Locator("script#aboard-shot-report")
	if err := expect.Locator(report).ToBeAttached(); err != nil {
		t.Fatalf("a shot page wrote no report: %v", err)
	}
	body, err := report.TextContent()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `"kind":"scroll"`) || !strings.Contains(body, `"width":1400`) {
		t.Errorf("the report does not carry the measurement: %s", body)
	}
	if n := receiptFor(t, id).Mounts; n != 0 {
		t.Errorf("a shot page posted a receipt as well (%d mounts)", n)
	}
}
