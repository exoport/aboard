//go:build e2e

package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// Markup: nineteen declared controls and six pointer-capture tools. It is the
// renderer with the largest gap between "it mounted" and "it works", because
// every mark it stores comes from a drag on an <svg> overlay.
//
// The fixture gives `bb22` two images and three marks — the example board ships
// it EMPTY, because an image with somebody else's annotations on it is not what
// a new project should start with. See test/e2e/testdata/fixture.json.

const markupTab = "bb22"

// Drag with the region tool to draw a rectangle. This is also the negative case
// for the tool switch: move and resize deliberately do nothing on empty canvas,
// so drawing has to be preceded by choosing a drawing tool.
func TestDrawingARegionOnAMarkupImage(t *testing.T) {
	covers(t, "markup", "each mark is badged on the image with its id, so the image, the list and a sentence all use one identifier")

	s := open(t, "tab="+markupTab)
	view := s.view(markupTab)

	before := markCount(t, "img1")
	if err := s.control(markupTab, "region").Click(); err != nil {
		t.Fatalf("choosing the region tool: %v", err)
	}

	svg := view.Locator(`.markup-figure[data-image-id="img1"] .markup-svg`)
	s.dragPointer(s.at(svg, 0.55, 0.1), s.at(svg, 0.85, 0.3))

	eventually(t, "the new mark to reach the server", func() bool { return markCount(t, "img1") == before+1 })

	// Every mark is badged with its OWN id on the image, in the list and in a
	// sentence — one identifier, because per-image counters restarted at 1 on
	// every image and none of them agreed with the list.
	added := lastRegion(t, "img1")
	id, _ := added["id"].(string)
	if id == "" {
		t.Fatal("the new mark has no id")
	}
	if err := expect.Locator(view.Locator(`.markup-list [data-mark-key="img1::` + id + `"]`)).ToBeVisible(); err != nil {
		t.Errorf("the marks list has no row for %s: %v", id, err)
	}
	if err := expect.Locator(view.Locator(`[data-mark-key="img1::` + id + `"]`).First()).ToBeAttached(); err != nil {
		t.Errorf("the mark is not on the image: %v", err)
	}
}

// The resize tool: select a rectangle, drag a corner handle, and the mark's box
// changes. Handles are plain HTML divs outside the <svg> — the svg uses
// viewBox="0 0 1 1" with preserveAspectRatio="none", so anything drawn inside it
// stretches, and a handle has to stay a true square.
func TestResizingAMarkupRegion(t *testing.T) {
	s := open(t, "tab="+markupTab)
	view := s.view(markupTab)

	if err := s.control(markupTab, "resize").Click(); err != nil {
		t.Fatalf("choosing the resize tool: %v", err)
	}
	// A mark is selected by pointing at it, which is the same gesture whichever
	// tool is active.
	shape := view.Locator(`[data-mark-key="img1::r1"]`).First()
	if err := shape.Click(); err != nil {
		t.Fatalf("selecting the mark: %v", err)
	}

	handle := view.Locator(`.markup-figure[data-image-id="img1"] .markup-handle[data-handle="se"]`)
	if err := expect.Locator(handle).ToBeVisible(); err != nil {
		t.Fatalf("the resize tool drew no handles on the selected rectangle: %v", err)
	}

	before := regionBox(t, "img1", "r1")
	svg := view.Locator(`.markup-figure[data-image-id="img1"] .markup-svg`)
	s.dragPointer(s.centre(handle), s.at(svg, 0.62, 0.55))

	eventually(t, "the resized mark to reach the server", func() bool {
		return regionBox(t, "img1", "r1") != before
	})

	// A pen stroke has no handles, and the renderer says why rather than leaving
	// the human wondering which marks resize.
	//
	// Selected from the LIST rather than from the image, and that is not a
	// shortcut: a pen mark is a <polyline fill="none">, so the middle of its
	// bounding box is empty space and a click there lands on the svg underneath.
	// Playwright reports it exactly ("svg intercepts pointer events") and it is
	// the truth about the shape, not a driver limitation — a human aiming at a
	// pen stroke has to hit the line too.
	if err := view.Locator(`.markup-list [data-mark-key="img1::s1"] .markup-chip`).Click(); err != nil {
		t.Fatalf("selecting the pen stroke from the marks list: %v", err)
	}
	if err := expect.Locator(view.Locator(".markup-resize-hint")).ToBeVisible(); err != nil {
		t.Errorf("nothing explains why a pen stroke has no handles: %v", err)
	}
}

// Clear marks goes through a modal that says how many are about to go and cannot
// be undone — a destructive action in a renderer, kept inside the page.
//
// It draws its own mark on the SECOND image first, and that is deliberate: an
// earlier draft cleared img1, which the fixture stocks, and every later test in
// this file then depended on running before it. A test whose correctness rests
// on its position in the file is a test that breaks when somebody adds one above
// it, silently and somewhere else.
func TestClearingMarksAsksFirst(t *testing.T) {
	s := open(t, "tab="+markupTab)
	view := s.view(markupTab)

	if err := s.control(markupTab, "ellipse").Click(); err != nil {
		t.Fatalf("choosing the ellipse tool: %v", err)
	}
	svg := view.Locator(`.markup-figure[data-image-id="img2"] .markup-svg`)
	s.dragPointer(s.at(svg, 0.2, 0.2), s.at(svg, 0.45, 0.5))
	eventually(t, "a mark to clear", func() bool { return markCount(t, "img2") > 0 })
	before := markCount(t, "img2")

	figure := view.Locator(`.markup-figure[data-image-id="img2"]`)
	if err := figure.Locator(`[data-gesture="clear-marks"]`).Click(); err != nil {
		t.Fatalf("pressing Clear marks: %v", err)
	}
	dialog := view.Locator("dialog.markup-dialog")
	if err := expect.Locator(dialog).ToBeVisible(); err != nil {
		t.Fatalf("the confirmation did not open: %v", err)
	}
	if err := expect.Locator(dialog).ToContainText("cannot be undone"); err != nil {
		t.Errorf("the confirmation does not say the marks are unrecoverable: %v", err)
	}

	// Cancel changes nothing.
	if err := dialog.GetByText("Cancel").Click(); err != nil {
		t.Fatalf("cancelling: %v", err)
	}
	if got := markCount(t, "img2"); got != before {
		t.Fatalf("Cancel deleted %d marks anyway", before-got)
	}

	if err := figure.Locator(`[data-gesture="clear-marks"]`).Click(); err != nil {
		t.Fatalf("pressing Clear marks again: %v", err)
	}
	if err := dialog.GetByText("Delete marks").Click(); err != nil {
		t.Fatalf("confirming: %v", err)
	}
	eventually(t, "the marks to be cleared", func() bool { return markCount(t, "img2") == 0 })

	// A mark is a position ON an image, so clearing one image must not reach the
	// other — which is the assertion that would have caught a clear written
	// against the whole marks list instead of one image's.
	if markCount(t, "img1") == 0 {
		t.Error("clearing img2 emptied img1 as well")
	}
}

// Hiding marks is per-viewer: it toggles a class and writes nothing at all. The
// board is a shared document, and one viewer turning the overlay off must not
// turn it off for the agent reading the same tab.
func TestHidingMarksIsPerViewer(t *testing.T) {
	s := open(t, "tab="+markupTab)
	view := s.view(markupTab)

	revBefore := readDoc(t)["rev"]
	figure := view.Locator(`.markup-figure[data-image-id="img2"]`)
	if err := figure.Locator(`[data-gesture="hide-marks"]`).Click(); err != nil {
		t.Fatalf("hiding the marks: %v", err)
	}
	if err := expect.Locator(figure.Locator(".markup-svg")).ToHaveClass("markup-svg is-marks-hidden"); err != nil {
		t.Errorf("the overlay is not hidden: %v", err)
	}
	if got := readDoc(t)["rev"]; got != revBefore {
		t.Errorf("hiding the overlay wrote to the board (rev %v -> %v)", revBefore, got)
	}
}

// The swatch row is built FROM the declared palette (`colors` in
// markup.spec.json), which is what makes an unknown colour name a warning rather
// than a mark that renders in no colour at all. Five swatches, and choosing one
// changes what the NEXT mark is drawn in — a per-viewer preference, never saved.
// The paste bar and the tools stay on screen while you scroll through the
// images. With one image they were always visible; with six they were a scroll
// away from whichever picture you were working on, so picking a colour meant
// scrolling up and then finding your place again. Reported 2026-08-27.
//
// The offset is the interesting part: the shell's own head is sticky above this,
// and its height changes when the tab strip wraps or a banner appears — so
// aboard.html measures it and publishes `--head-h`, and this asserts the tools
// land BELOW the head rather than under it.
func TestTheMarkupToolsStayOnScreenWhileScrolling(t *testing.T) {
	id := makeScratchTab(t, "Long markup")

	const count = 5
	images := make([]any, 0, count)
	for i := range count {
		images = append(images, map[string]any{
			"id": fmt.Sprintf("im%d", i), "src": "assets/mock-screen.svg",
			"caption": fmt.Sprintf("shot-%d.svg", i),
		})
	}
	d := readDoc(t)
	d.tab(t, id)["type"] = "markup"
	d.tab(t, id)["state"] = map[string]any{"layout": "stacked", "images": images}
	apply(t, d)

	s := open(t, "tab="+id)

	top := func(sel string) float64 {
		var y float64
		s.evalJSON(&y, `(q) => document.querySelector(q).getBoundingClientRect().top`, sel)
		return y
	}
	tools := `[data-tab="` + id + `"] .markup-sticky`

	// Measured at TWO depths, not against the resting position. A sticky element
	// does travel with the page until it reaches its pin — comparing "before
	// scrolling" with "after" fails on correct behaviour, which is how this
	// assertion was written the first time.
	scrollTo := func(y int) float64 {
		if _, err := s.page.Evaluate(fmt.Sprintf(`() => window.scrollTo(0, %d)`, y), nil); err != nil {
			t.Fatalf("scrolling: %v", err)
		}
		var at float64
		s.evalJSON(&at, `() => window.scrollY`)
		return at
	}
	if at := scrollTo(2500); at < 1000 {
		t.Fatalf("the page only scrolled %.0fpx — this tab is not long enough to test stickiness", at)
	}
	pinned := top(tools)
	deeper := scrollTo(4500)
	after := top(tools)

	if after < -1 {
		t.Errorf("the tools scrolled off the top (%.0f) — they are not sticky", after)
	}
	if diff := after - pinned; diff > 2 || diff < -2 {
		t.Errorf("the tools moved %.0fpx between scroll %.0f and %.0f (%.0f then %.0f) — they are still travelling",
			diff, 2500.0, deeper, pinned, after)
	}

	// And below the shell's own sticky head, not underneath it. This is what
	// --head-h buys; with `top: 0` the tools would slide under the tab strip.
	head := top(".board-head")
	var headH float64
	s.evalJSON(&headH, `() => document.querySelector('.board-head').getBoundingClientRect().height`)
	if after < head+headH-2 {
		t.Errorf("the tools sit at %.0f, inside the head which ends at %.0f — they are sticking to the wrong thing",
			after, head+headH)
	}
}

// A slice per image: caption and buttons, the picture, then THAT picture's own
// marks. Reported 2026-08-27 — with several screenshots on a tab, one table at
// the bottom meant scrolling past every other image to read a note and back up
// to see what it was about, and a row said which image it came from only through
// a caption column.
//
// The numbering stays global across the tab, and that is asserted here because
// it is the thing a reasonable person would "fix" while splitting the table:
// restarting at 1 per image made "mark 1" name two different things, which is
// the one thing this view exists to prevent.
func TestEachImageGetsItsOwnMarksTable(t *testing.T) {
	covers(t, "markup", "each image is its own slice — caption, buttons, the picture, then that image's own marks")

	id := makeScratchTab(t, "Two images")
	d := readDoc(t)
	d.tab(t, id)["type"] = "markup"
	d.tab(t, id)["state"] = map[string]any{
		"layout": "stacked",
		"images": []any{
			map[string]any{"id": "one", "src": "assets/mock-screen.svg", "caption": "first.svg", "regions": []any{
				map[string]any{"id": "bb301", "x": 0.1, "y": 0.1, "w": 0.2, "h": 0.2},
				map[string]any{"id": "bb302", "x": 0.4, "y": 0.1, "w": 0.2, "h": 0.2},
			}},
			map[string]any{"id": "two", "src": "assets/mock-screen-after.svg", "caption": "second.svg", "regions": []any{
				map[string]any{"id": "bb303", "x": 0.1, "y": 0.5, "w": 0.2, "h": 0.2},
			}},
		},
	}
	apply(t, d)

	s := open(t, "tab="+id)
	view := s.view(id)

	// Two figures, two tables — not one table for the tab.
	if err := expect.Locator(view.Locator(".markup-figure")).ToHaveCount(2); err != nil {
		t.Fatalf("want a slice per image: %v", err)
	}
	if err := expect.Locator(view.Locator(".markup-list")).ToHaveCount(2); err != nil {
		t.Fatalf("want a marks table per image: %v", err)
	}

	// Each table holds only its own image's marks, and the table is INSIDE the
	// slice — which is what makes "scroll to the note, scroll back to the
	// picture" impossible by construction rather than by luck.
	first := view.Locator(`.markup-figure[data-image-id="one"]`)
	second := view.Locator(`.markup-figure[data-image-id="two"]`)
	if err := expect.Locator(first.Locator(".markup-row:not(.markup-row-head)")).ToHaveCount(2); err != nil {
		t.Errorf("the first image's table does not hold its two marks: %v", err)
	}
	if err := expect.Locator(second.Locator(".markup-row:not(.markup-row-head)")).ToHaveCount(1); err != nil {
		t.Errorf("the second image's table does not hold its one mark: %v", err)
	}
	if err := expect.Locator(first.Locator(".markup-chip").Filter(playwright.LocatorFilterOptions{HasText: "bb303"})).ToHaveCount(0); err != nil {
		t.Errorf("the second image's mark is listed under the first image: %v", err)
	}

	// Numbering runs 1,2,3 ACROSS the tab. A per-image restart would make this
	// 1,2 then 1.
	var nums []string
	s.evalJSON(&nums, `(q) => [...document.querySelectorAll(q)].map((el) => el.textContent.trim())`,
		`[data-tab="`+id+`"] .markup-row:not(.markup-row-head) .markup-index`)
	if strings.Join(nums, ",") != "1,2,3" {
		t.Errorf("mark numbers are %v — they are numbered across the tab, not restarted per image", nums)
	}
}

// Zoom, and the two things about it that are not obvious: it is per IMAGE, and
// it is per VIEWER. A zoom that reached the document would move somebody else's
// view of the same board, which is the rule that already keeps scroll, theme and
// the selection out of it.
func TestZoomingAMarkupImageIsPerImageAndNeverStored(t *testing.T) {
	covers(t, "markup", "Ctrl/Cmd+wheel over an image to zoom it (the readout is its size against the original, not the zoom factor); pan a zoomed one by dragging with Move, or with the middle button and any tool")

	id := makeScratchTab(t, "Zoom me")
	d := readDoc(t)
	d.tab(t, id)["type"] = "markup"
	d.tab(t, id)["state"] = map[string]any{
		"layout": "stacked",
		"images": []any{
			map[string]any{"id": "one", "src": "assets/mock-screen.svg", "caption": "a.svg"},
			map[string]any{"id": "two", "src": "assets/mock-screen-after.svg", "caption": "b.svg"},
		},
	}
	apply(t, d)
	before := readDoc(t).state(t, id)

	s := open(t, "tab="+id)
	view := s.view(id)
	first := view.Locator(`.markup-figure[data-image-id="one"]`)

	if err := first.Locator(`[data-gesture="zoom-in"]`).Click(); err != nil {
		t.Fatalf("clicking zoom in: %v", err)
	}
	if err := first.Locator(`[data-gesture="zoom-in"]`).Click(); err != nil {
		t.Fatalf("clicking zoom in again: %v", err)
	}

	transform := func(imageID string) string {
		var out string
		s.evalJSON(&out, `(q) => getComputedStyle(document.querySelector(q)).transform`,
			`[data-tab="`+id+`"] .markup-figure[data-image-id="`+imageID+`"] .markup-zoom`)
		return out
	}
	if transform("one") == "none" {
		t.Error("zooming in left the first image untransformed")
	}
	// The OTHER image is untouched. One zoom for the tab would mean zooming a
	// screenshot to read it and losing your place in every other one.
	if got := transform("two"); got != "none" && got != "matrix(1, 0, 0, 1, 0, 0)" {
		t.Errorf("zooming one image also zoomed the other (transform %q)", got)
	}

	if err := expect.Locator(first.Locator(".markup-zoom-label")).ToContainText("%"); err != nil {
		t.Errorf("no zoom readout: %v", err)
	}

	// Ctrl+wheel, which is the gesture people will actually reach for. Plain
	// wheel deliberately still scrolls the page: a tall markup tab that trapped
	// the wheel would be unscrollable exactly where it is longest.
	stage := first.Locator(".markup-stage")
	sbox, err := stage.BoundingBox()
	if err != nil || sbox == nil {
		t.Fatalf("no stage to point at: %v", err)
	}
	if err := s.page.Mouse().Move(sbox.X+sbox.Width/2, sbox.Y+sbox.Height/2); err != nil {
		t.Fatalf("moving onto the image: %v", err)
	}
	beforeWheel, _ := first.Locator(".markup-zoom-label").TextContent()
	if err := s.page.Mouse().Wheel(0, 120); err != nil {
		t.Fatalf("plain wheel: %v", err)
	}
	if after, _ := first.Locator(".markup-zoom-label").TextContent(); after != beforeWheel {
		t.Errorf("a plain wheel zoomed the image (%s -> %s) — it has to scroll the page", beforeWheel, after)
	}
	if err := s.page.Keyboard().Down("Control"); err != nil {
		t.Fatalf("holding Control: %v", err)
	}
	if err := s.page.Mouse().Wheel(0, -120); err != nil {
		t.Fatalf("ctrl+wheel: %v", err)
	}
	if err := s.page.Keyboard().Up("Control"); err != nil {
		t.Fatalf("releasing Control: %v", err)
	}
	if after, _ := first.Locator(".markup-zoom-label").TextContent(); after == beforeWheel {
		t.Errorf("Ctrl+wheel did not zoom (still %s)", after)
	}

	// Fit puts it back.
	if err := first.Locator(`[data-gesture="zoom-fit"]`).Click(); err != nil {
		t.Fatalf("clicking fit: %v", err)
	}
	if err := expect.Locator(first.Locator(".markup-zoom-label")).ToHaveText("100%"); err != nil {
		t.Errorf("Fit did not return to 100%%: %v", err)
	}

	// And none of it reached the server. Compared as JSON against the state we
	// seeded, so a zoom field appearing anywhere in it fails.
	time.Sleep(settle)
	after := readDoc(t).state(t, id)
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Errorf("zooming changed the document:\n before %v\n after  %v", before, after)
	}
}

// When the clipboard is refused, the picture is offered instead — which is not
// a nicety, it is the ONLY route in the place this board most wants to be.
//
// A VS Code webview applies a permissions policy the board sits inside of, so
// `clipboard-write` has to be delegated by the host; when it is not, Chromium
// refuses with "The Clipboard API has been blocked because of a permissions
// policy applied to the current document", which is exactly what was reported on
// 2026-08-27 from the panel. Nothing the page can do fixes that from the inside.
//
// So the refusal path puts the cropped picture in a dialog and the human uses
// their own context menu on it. Driven here by denying the permission, which is
// the same refusal arriving by a different road.
func TestARefusedCopyOffersThePictureInstead(t *testing.T) {
	id := makeScratchTab(t, "Refused copy")
	d := readDoc(t)
	d.tab(t, id)["type"] = "markup"
	d.tab(t, id)["state"] = map[string]any{
		"layout": "stacked",
		"images": []any{map[string]any{"id": "one", "src": "assets/mock-screen.svg", "caption": "a.svg"}},
	}
	apply(t, d)

	s := open(t, "tab="+id)
	view := s.view(id)

	// The board's own copy path, with the clipboard replaced by one that refuses
	// the way a permissions policy does. Overriding the API rather than revoking
	// the permission because a revoked permission and a policy-blocked one reject
	// at different layers, and the string in the message is what the human reads.
	if _, err := s.page.Evaluate(`() => {
		Object.defineProperty(navigator, 'clipboard', {
			configurable: true,
			value: { write: () => Promise.reject(new Error(
				"Failed to execute 'write' on 'Clipboard': The Clipboard API has been blocked because of a permissions policy applied to the current document.")) },
		});
	}`, nil); err != nil {
		t.Fatalf("replacing the clipboard: %v", err)
	}

	if err := view.Locator(`[data-gesture="crop"]`).Click(); err != nil {
		t.Fatalf("choosing the crop tool: %v", err)
	}
	svg := view.Locator(".markup-svg").First()
	box, err := svg.BoundingBox()
	if err != nil || box == nil {
		t.Fatalf("no svg to drag on: %v", err)
	}
	s.dragPointer(
		point{box.X + box.Width*0.2, box.Y + box.Height*0.2},
		point{box.X + box.Width*0.6, box.Y + box.Height*0.6},
	)
	if err := view.Locator(`[data-gesture="copy-region"]`).Click(); err != nil {
		t.Fatalf("clicking Copy region: %v", err)
	}

	// It says the clipboard failed...
	status := view.Locator(".markup-copy-status")
	if err := expect.Locator(status).ToContainText("not available"); err != nil {
		got, _ := status.TextContent()
		t.Fatalf("a refused copy did not say so (status %q): %v", got, err)
	}
	// ...and hands over the picture, with the reason on it.
	dialog := view.Locator(".markup-image-dialog[open]")
	if err := expect.Locator(dialog).ToBeVisible(); err != nil {
		t.Fatalf("a refused copy offered nothing: %v", err)
	}
	// A BUTTON that works everywhere, not an instruction that does not. The
	// heading said "Right-click the picture" until 2026-08-28, and in a VS Code
	// webview the host owns the context menu and offers no "Copy image" — so the
	// dialog raised to explain one thing that appeared to do nothing was itself a
	// second thing that appeared to do nothing.
	if err := expect.Locator(dialog.GetByText("Add this picture to the tab")).ToBeVisible(); err != nil {
		t.Errorf("the dialog offers no action that works here: %v", err)
	}
	if err := expect.Locator(dialog.Locator(".panel-head")).Not().ToContainText("Right-click"); err != nil {
		t.Errorf("the dialog still leads with right-click, which a webview cannot do: %v", err)
	}
	if err := expect.Locator(dialog).ToContainText("permissions policy"); err != nil {
		t.Errorf("the dialog does not carry the refusal it is standing in for: %v", err)
	}

	// A real picture, not an empty box: the crop is drawn before the clipboard is
	// ever asked, so a refusal costs the human nothing.
	var img struct {
		Src      string
		W, H     float64
		Complete bool
	}
	s.evalJSON(&img, `(q) => {
		const el = document.querySelector(q);
		return { Src: el.getAttribute('src').slice(0, 22), W: el.naturalWidth, H: el.naturalHeight, Complete: el.complete };
	}`, `[data-tab="`+id+`"] .markup-offer-img`)
	if img.Src != "data:image/png;base64," {
		t.Errorf("the offered picture is not a PNG data URL (src starts %q)", img.Src)
	}
	if img.W < 10 || img.H < 10 {
		t.Errorf("the offered picture is %.0f×%.0f — the crop was not drawn", img.W, img.H)
	}
}

// The readout says how big a pixel of the ORIGINAL is, not what the zoom factor
// happens to be. A screenshot wider than the row is shrunk to fit it, and the
// label said 100% while the picture was at something nearer 60% — so "100%"
// meant two different things depending on how wide the board was. Reported
// 2026-08-27 with a picture of it.
func TestTheZoomReadoutIsTheEffectiveScale(t *testing.T) {
	id := makeScratchTab(t, "Shrunk to fit")
	d := readDoc(t)
	d.tab(t, id)["type"] = "markup"
	d.tab(t, id)["state"] = map[string]any{
		"layout": "stacked",
		"images": []any{map[string]any{"id": "one", "src": "assets/mock-screen.svg", "caption": "900px.svg"}},
	}
	apply(t, d)

	s := open(t, "tab="+id)
	label := s.view(id).Locator(".markup-zoom-label")

	// Wide enough that the 900px picture is NOT shrunk: it is at 1:1, so the
	// readout is 100% and the two numbers agree. This is the control.
	if err := expect.Locator(label).ToHaveText("100%"); err != nil {
		t.Fatalf("at full size the readout is not 100%%: %v", err)
	}

	// Now narrow the window until the picture has to shrink. The zoom factor has
	// not changed — nobody pressed anything — but what is on screen has.
	if err := s.page.SetViewportSize(600, 900); err != nil {
		t.Fatalf("narrowing the viewport: %v", err)
	}
	var got string
	var shown, natural float64
	eventually(t, "the readout to follow the picture down", func() bool {
		s.evalJSON(&got, `(q) => document.querySelector(q).textContent.trim()`,
			`[data-tab="`+id+`"] .markup-zoom-label`)
		return got != "100%"
	})
	s.evalJSON(&shown, `(q) => document.querySelector(q).clientWidth`, `[data-tab="`+id+`"] .markup-img`)
	s.evalJSON(&natural, `(q) => document.querySelector(q).naturalWidth`, `[data-tab="`+id+`"] .markup-img`)

	want := int((shown/natural)*100 + 0.5)
	if got != fmt.Sprintf("%d%%", want) {
		t.Errorf("the readout says %s for a %.0fpx picture shown at %.0fpx — want %d%%", got, natural, shown, want)
	}
}

// Copy view captures what the stage is SHOWING — not a fragment of it.
//
// Reported 2026-08-27 from a panel at 244%: the copy came back 55x60, a
// thumbnail of a corner. The refusal dialog is what made it visible, since it
// prints the size of what it is holding.
func TestCopyViewCapturesTheWholeVisibleArea(t *testing.T) {
	id := makeScratchTab(t, "Copy the view")
	d := readDoc(t)
	d.tab(t, id)["type"] = "markup"
	d.tab(t, id)["state"] = map[string]any{
		"layout": "stacked",
		"images": []any{map[string]any{"id": "one", "src": "assets/mock-screen.svg", "caption": "a.svg"}},
	}
	apply(t, d)

	s := open(t, "tab="+id)
	view := s.view(id)
	first := view.Locator(`.markup-figure[data-image-id="one"]`)

	// The clipboard refuses, so every copy lands in the dialog — which is the one
	// place the SIZE of the capture is stated.
	if _, err := s.page.Evaluate(`() => {
		Object.defineProperty(navigator, 'clipboard', {
			configurable: true,
			value: { write: () => Promise.reject(new Error('blocked for this test')) },
		});
	}`, nil); err != nil {
		t.Fatalf("replacing the clipboard: %v", err)
	}

	capture := func() (w, h float64) {
		// The previous capture is still in the <img>, so waiting for "a picture
		// exists" returns the OLD one instantly. Waited on the src CHANGING
		// instead — the first version of this test measured the same 900px twice
		// and reported the code as broken in the opposite direction to the bug.
		var was string
		s.evalJSON(&was, `(q) => { const el = document.querySelector(q); return el ? el.getAttribute('src') || '' : ''; }`,
			`[data-tab="`+id+`"] .markup-offer-img`)
		if err := first.Locator(`[data-gesture="copy-view"]`).Click(); err != nil {
			t.Fatalf("clicking Copy view: %v", err)
		}
		var out struct {
			W, H float64
			Src  string
		}
		eventually(t, "a NEW offered picture to be drawn", func() bool {
			s.evalJSON(&out, `(q) => {
				const el = document.querySelector(q);
				return { W: el ? el.naturalWidth : 0, H: el ? el.naturalHeight : 0, Src: el ? (el.getAttribute('src') || '') : '' };
			}`, `[data-tab="`+id+`"] .markup-offer-img`)
			return out.W > 0 && out.Src != was
		})
		if err := view.Locator(".markup-image-dialog").GetByText("Close").Click(); err != nil {
			t.Fatalf("closing the dialog: %v", err)
		}
		return out.W, out.H
	}

	var natural, shown float64
	s.evalJSON(&natural, `(q) => document.querySelector(q).naturalWidth`, `[data-tab="`+id+`"] .markup-img`)
	s.evalJSON(&shown, `(q) => document.querySelector(q).clientWidth`, `[data-tab="`+id+`"] .markup-img`)

	// Unzoomed, the whole picture is visible, so the capture is the whole picture
	// at the size it is DISPLAYED (times the device pixel ratio).
	var dpr float64
	s.evalJSON(&dpr, `() => Math.min(3, Math.max(1, window.devicePixelRatio || 1))`)
	w, h := capture()
	t.Logf("at rest: natural=%.0f shown=%.0f dpr=%.1f captured=%.0fx%.0f", natural, shown, dpr, w, h)
	if want := shown * dpr; w < want*0.95 || w > want*1.05 {
		t.Errorf("unzoomed, Copy view captured %.0fpx; the picture is shown at %.0fpx (dpr %.1f)", w, shown, dpr)
	}

	// Zoomed in twice: a quarter-ish of the picture is visible, so the capture is
	// that much of it — and still hundreds of pixels, not tens.
	for range 2 {
		if err := first.Locator(`[data-gesture="zoom-in"]`).Click(); err != nil {
			t.Fatalf("zooming: %v", err)
		}
	}
	var z float64
	s.evalJSON(&z, `(q) => {
		const m = new DOMMatrixReadOnly(getComputedStyle(document.querySelector(q)).transform);
		return m.a;
	}`, `[data-tab="`+id+`"] .markup-zoom`)
	w2, h2 := capture()
	t.Logf("zoomed: z=%.3f captured=%.0fx%.0f", z, w2, h2)
	// The KEY property, and the one that was inverted: zooming IN must not make
	// the copy smaller. The stage is the same size whatever the zoom, so what is
	// produced is the same size too — you just get less of the picture, larger.
	if want := shown * dpr; w2 < want*0.95 || w2 > want*1.05 {
		t.Errorf("at z=%.2f Copy view captured %.0fpx wide; the stage still shows %.0fpx (dpr %.1f) — zooming in shrank the copy",
			z, w2, shown, dpr)
	}
	if h2 < 20 {
		t.Errorf("Copy view captured %.0fpx tall — that is a thumbnail, not the view", h2)
	}
}

// The crop, added straight to the tab as a new image — no clipboard involved.
//
// This is what the clipboard was for here: "copy a rectangle so it could then be
// pasted as a new image, like a closeup". In a VS Code panel the clipboard is
// blocked by a permissions policy the board cannot change, so the round trip
// through it was the one part that could not be made to work. This route needs
// no permission at all.
func TestACropCanBeAddedToTheTabAsANewImage(t *testing.T) {
	covers(t, "markup", "drag with the crop tool to select a rectangle, then copy it to the clipboard with or without its marks, or add it to this tab as a new image to draw on")

	id := makeScratchTab(t, "Closeup")
	d := readDoc(t)
	d.tab(t, id)["type"] = "markup"
	d.tab(t, id)["state"] = map[string]any{
		"layout": "stacked",
		"images": []any{map[string]any{
			"id": "one", "src": "assets/mock-screen.svg", "caption": "source.svg",
			"regions": []any{map[string]any{"id": "bb501", "x": 0.1, "y": 0.1, "w": 0.2, "h": 0.2, "color": "danger"}},
		}},
	}
	apply(t, d)

	s := open(t, "tab="+id)
	view := s.view(id)

	if err := view.Locator(`[data-gesture="crop"]`).Click(); err != nil {
		t.Fatalf("choosing the crop tool: %v", err)
	}
	svg := view.Locator(".markup-svg").First()
	box, err := svg.BoundingBox()
	if err != nil || box == nil {
		t.Fatalf("no svg to drag on: %v", err)
	}
	s.dragPointer(
		point{box.X + box.Width*0.4, box.Y + box.Height*0.4},
		point{box.X + box.Width*0.8, box.Y + box.Height*0.8},
	)
	if err := view.Locator(`[data-gesture="crop-to-image"]`).Click(); err != nil {
		t.Fatalf("clicking Add as image: %v", err)
	}

	// A second image on the tab, uploaded and stored like any pasted one.
	eventually(t, "the closeup to reach the server", func() bool {
		st := readDoc(t).state(t, id)
		images, _ := st["images"].([]any)
		return len(images) == 2
	})
	images, _ := readDoc(t).state(t, id)["images"].([]any)
	added, _ := images[1].(map[string]any)
	src, _ := added["src"].(string)
	if !strings.HasPrefix(src, "uploads/") {
		t.Errorf("the closeup was stored at %q, want it under uploads/", src)
	}
	if caption, _ := added["caption"].(string); !strings.Contains(caption, "closeup") {
		t.Errorf("the closeup is captioned %q — it should say where it came from", caption)
	}
	// Its own slice, its own (empty) table: it is a first-class image, not an
	// attachment of the one it was cut from.
	if err := expect.Locator(view.Locator(".markup-figure")).ToHaveCount(2); err != nil {
		t.Errorf("the closeup did not get a slice of its own: %v", err)
	}

	// CLEAN: the source's mark is not baked into it. A closeup exists to be drawn
	// on, and pixels of an old mark cannot be selected, recoloured or deleted.
	if regions, _ := added["regions"].([]any); len(regions) != 0 {
		t.Errorf("the closeup arrived with marks on it: %v", regions)
	}

	// And it is a real image the server will serve.
	res, err := s.page.Request().Get(boardURL + "/" + src)
	if err != nil || !res.Ok() {
		t.Fatalf("the uploaded closeup does not serve: %v", err)
	}

	// The selection is spent, like it is after a copy.
	if err := expect.Locator(view.Locator(".markup-crop")).ToHaveCount(0); err != nil {
		t.Errorf("the crop rectangle survived being turned into an image: %v", err)
	}
}

// Zooming does not move the buttons.
//
// The "how to pan" hint started life as a span in the image's head row, so it
// pushed every button beside it sideways when it appeared and back again when it
// went — the toolbar jumped about as you zoomed, which is worse than the
// discoverability problem it was added to solve. Moving it onto the picture
// stopped the shuffling and was still clutter, so it is gone. Both were reported
// on 2026-08-27, an hour apart.
//
// What survives is the measurement, which outlasted three versions of the thing
// it was measuring: zooming does not move the buttons.
func TestZoomingDoesNotMoveTheImageButtons(t *testing.T) {
	id := makeScratchTab(t, "Steady buttons")
	d := readDoc(t)
	d.tab(t, id)["type"] = "markup"
	d.tab(t, id)["state"] = map[string]any{
		"layout": "stacked",
		"images": []any{map[string]any{"id": "one", "src": "assets/mock-screen.svg", "caption": "a.svg"}},
	}
	apply(t, d)

	s := open(t, "tab="+id)
	first := s.view(id).Locator(`.markup-figure[data-image-id="one"]`)

	// Every button on the head row, by left edge.
	positions := func() []float64 {
		var out []float64
		s.evalJSON(&out, `(q) => [...document.querySelectorAll(q + ' .markup-figure-head button')]
			.map((b) => Math.round(b.getBoundingClientRect().left))`, `[data-tab="`+id+`"]`)
		return out
	}
	before := positions()
	if len(before) < 4 {
		t.Fatalf("only %d buttons on the head row — this is not measuring what it thinks", len(before))
	}

	for range 3 {
		if err := first.Locator(`[data-gesture="zoom-in"]`).Click(); err != nil {
			t.Fatalf("zooming in: %v", err)
		}
	}
	after := positions()
	if len(after) != len(before) {
		t.Fatalf("zooming changed the number of buttons from %d to %d", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("zooming moved the head-row buttons: %v became %v", before, after)
			break
		}
	}

	// And nothing was ADDED to the picture either. The hint went through a head-row
	// span and then a fading overlay before being removed outright: a permanent
	// instruction is clutter for everyone who has read it once, and the gesture is
	// declared in views/markup.spec.json, so the help panel carries it for anybody
	// looking. The readout's tooltip says it too, at no cost in layout.
	if err := expect.Locator(first.Locator(".markup-pan-hint")).ToHaveCount(0); err != nil {
		t.Errorf("the pan hint is back on screen: %v", err)
	}
	title, err := first.Locator(".markup-zoom-label").GetAttribute("title")
	if err != nil || !strings.Contains(title, "pan") {
		t.Errorf("the zoom readout's tooltip does not mention panning (title %q, err %v)", title, err)
	}

	// Back to Fit, and the buttons are still where they were.
	if err := first.Locator(`[data-gesture="zoom-fit"]`).Click(); err != nil {
		t.Fatalf("clicking fit: %v", err)
	}
	fitted := positions()
	for i := range before {
		if before[i] != fitted[i] {
			t.Errorf("returning to Fit moved the head-row buttons: %v became %v", before, fitted)
			break
		}
	}
}

// side-by-side means a PAIR, and several images make several pairs.
//
// It set one grid column per image, so six screenshots became six columns —
// and a slice now carries a marks table as well as a picture, so a third column
// is unreadable before it is even narrow.
func TestSideBySideArrangesImagesInPairs(t *testing.T) {
	id := makeScratchTab(t, "Pairs")

	const count = 6
	images := make([]any, 0, count)
	for i := range count {
		src := "assets/mock-screen.svg"
		if i%2 == 1 {
			src = "assets/mock-screen-after.svg"
		}
		images = append(images, map[string]any{
			"id": fmt.Sprintf("im%d", i), "src": src,
			"caption": fmt.Sprintf("pair-%d-%d.svg", i/2+1, i%2+1),
		})
	}
	d := readDoc(t)
	d.tab(t, id)["type"] = "markup"
	d.tab(t, id)["state"] = map[string]any{"layout": "side-by-side", "images": images}
	apply(t, d)

	s := open(t, "tab="+id)
	view := s.view(id)
	if err := expect.Locator(view.Locator(".markup-figure")).ToHaveCount(count); err != nil {
		t.Fatalf("want %d slices: %v", count, err)
	}

	// Two per row, three rows: read off the left edges, which is what a column
	// actually is. A count of distinct edges IS the column count.
	var lefts []float64
	s.evalJSON(&lefts, `(q) => [...document.querySelectorAll(q + ' .markup-figure')]
		.map((f) => Math.round(f.getBoundingClientRect().left))`, `[data-tab="`+id+`"]`)
	if len(lefts) != count {
		t.Fatalf("measured %d slices, want %d", len(lefts), count)
	}
	columns := map[float64]bool{}
	for _, x := range lefts {
		columns[x] = true
	}
	if len(columns) != 2 {
		t.Errorf("the six slices sit in %d columns (left edges %v) — side by side means two", len(columns), lefts)
	}

	// And they pair in order: 0 and 1 share a row, 2 and 3 the next.
	var tops []float64
	s.evalJSON(&tops, `(q) => [...document.querySelectorAll(q + ' .markup-figure')]
		.map((f) => Math.round(f.getBoundingClientRect().top))`, `[data-tab="`+id+`"]`)
	for i := 0; i < count; i += 2 {
		if tops[i] != tops[i+1] {
			t.Errorf("slices %d and %d are not on the same row (tops %v)", i, i+1, tops)
			break
		}
	}
	if tops[0] == tops[2] {
		t.Errorf("all six slices are on one row (tops %v) — the pairs did not wrap", tops)
	}
}

// The dialog adds the picture it is SHOWING, not one it re-derives.
//
// "Copy as seen" showed a region with its marks drawn on and then added the same
// region WITHOUT them, because the button re-rendered a clean crop from the
// selection. "Copy view" had no selection at all, so the button was hidden
// entirely. Both reported 2026-08-28.
//
// The picture is compared by its own bytes: the offered <img> and the image that
// lands on the tab have to be the same PNG, which is the only statement of "what
// you saw is what you got" that cannot be satisfied by accident.
func TestTheDialogAddsThePictureItIsShowing(t *testing.T) {
	id := makeScratchTab(t, "As seen")
	d := readDoc(t)
	d.tab(t, id)["type"] = "markup"
	d.tab(t, id)["state"] = map[string]any{
		"layout": "stacked",
		"images": []any{map[string]any{
			"id": "one", "src": "assets/mock-screen.svg", "caption": "source.svg",
			"regions": []any{map[string]any{"id": "bb601", "x": 0.45, "y": 0.45, "w": 0.3, "h": 0.3, "color": "danger"}},
		}},
	}
	apply(t, d)

	s := open(t, "tab="+id)
	view := s.view(id)
	if _, err := s.page.Evaluate(`() => {
		Object.defineProperty(navigator, 'clipboard', {
			configurable: true,
			value: { write: () => Promise.reject(new Error('blocked for this test')) },
		});
	}`, nil); err != nil {
		t.Fatalf("blocking the clipboard: %v", err)
	}

	// A crop that OVERLAPS the mark, so "as seen" and "region" genuinely differ.
	if err := view.Locator(`[data-gesture="crop"]`).Click(); err != nil {
		t.Fatalf("choosing the crop tool: %v", err)
	}
	svg := view.Locator(".markup-svg").First()
	box, err := svg.BoundingBox()
	if err != nil || box == nil {
		t.Fatalf("no svg to drag on: %v", err)
	}
	s.dragPointer(
		point{box.X + box.Width*0.4, box.Y + box.Height*0.4},
		point{box.X + box.Width*0.85, box.Y + box.Height*0.85},
	)
	if err := view.Locator(`[data-gesture="copy-seen"]`).Click(); err != nil {
		t.Fatalf("clicking Copy as seen: %v", err)
	}

	dialog := view.Locator(".markup-image-dialog[open]")
	if err := expect.Locator(dialog).ToBeVisible(); err != nil {
		t.Fatalf("no dialog after a refused Copy as seen: %v", err)
	}
	var shown string
	s.evalJSON(&shown, `(q) => document.querySelector(q).getAttribute('src')`,
		`[data-tab="`+id+`"] .markup-offer-img`)

	if err := dialog.GetByText("Add this picture to the tab").Click(); err != nil {
		t.Fatalf("clicking the dialog's add button: %v", err)
	}
	eventually(t, "the picture to reach the server", func() bool {
		images, _ := readDoc(t).state(t, id)["images"].([]any)
		return len(images) == 2
	})
	images, _ := readDoc(t).state(t, id)["images"].([]any)
	added, _ := images[1].(map[string]any)
	src, _ := added["src"].(string)

	// The uploaded bytes, as a data URL, against the ones the dialog displayed.
	var uploaded string
	s.evalJSON(&uploaded, `async (u) => {
		const res = await fetch(u);
		const blob = await res.blob();
		return await new Promise((resolve) => {
			const r = new FileReader();
			r.onload = () => resolve(String(r.result));
			r.readAsDataURL(blob);
		});
	}`, boardURL+"/"+src)
	if uploaded != shown {
		t.Errorf("the tab got a different picture from the one the dialog showed (%d bytes vs %d)",
			len(uploaded), len(shown))
	}
}

// Copy view offers the same button. It has no crop selection, and the button was
// keyed off the selection existing, so it was hidden for exactly the case where
// the clipboard is blocked and this is the only way out.
func TestTheDialogOffersToAddAViewToo(t *testing.T) {
	id := makeScratchTab(t, "View add")
	d := readDoc(t)
	d.tab(t, id)["type"] = "markup"
	d.tab(t, id)["state"] = map[string]any{
		"layout": "stacked",
		"images": []any{map[string]any{"id": "one", "src": "assets/mock-screen.svg", "caption": "source.svg"}},
	}
	apply(t, d)

	s := open(t, "tab="+id)
	view := s.view(id)
	if _, err := s.page.Evaluate(`() => {
		Object.defineProperty(navigator, 'clipboard', {
			configurable: true,
			value: { write: () => Promise.reject(new Error('blocked for this test')) },
		});
	}`, nil); err != nil {
		t.Fatalf("blocking the clipboard: %v", err)
	}

	if err := view.Locator(`.markup-figure[data-image-id="one"] [data-gesture="copy-view"]`).Click(); err != nil {
		t.Fatalf("clicking Copy view: %v", err)
	}
	dialog := view.Locator(".markup-image-dialog[open]")
	add := dialog.GetByText("Add this picture to the tab")
	if err := expect.Locator(add).ToBeVisible(); err != nil {
		t.Fatalf("Copy view's dialog offers no way to add it: %v", err)
	}
	if err := add.Click(); err != nil {
		t.Fatalf("adding the view: %v", err)
	}
	eventually(t, "the view to reach the server", func() bool {
		images, _ := readDoc(t).state(t, id)["images"].([]any)
		return len(images) == 2
	})
	images, _ := readDoc(t).state(t, id)["images"].([]any)
	added, _ := images[1].(map[string]any)
	if caption, _ := added["caption"].(string); !strings.Contains(caption, "view") {
		t.Errorf("the added picture is captioned %q — it should say it was the view", caption)
	}
}

// The image caption names the image's ID on hover. It is what you say to an
// agent — "the image bb214" — and the caption itself is a label the human is
// free to change, so the two are not interchangeable.
func TestHoveringAnImageNameShowsItsId(t *testing.T) {
	id := makeScratchTab(t, "Named image")
	d := readDoc(t)
	d.tab(t, id)["type"] = "markup"
	d.tab(t, id)["state"] = map[string]any{
		"layout": "stacked",
		"images": []any{map[string]any{"id": "bb777", "src": "assets/mock-screen.svg", "caption": "a.svg"}},
	}
	apply(t, d)

	s := open(t, "tab="+id)
	title, err := s.view(id).Locator(".markup-figure-caption").First().GetAttribute("title")
	if err != nil {
		t.Fatalf("reading the caption's title: %v", err)
	}
	if !strings.Contains(title, "bb777") {
		t.Errorf("hovering the image name shows %q, which does not name the image", title)
	}
	// The rename affordance survives: the title is the only thing saying the
	// caption is clickable at all.
	if !strings.Contains(strings.ToLower(title), "rename") {
		t.Errorf("the title %q stopped saying the name can be changed", title)
	}
}

// Panning a zoomed image with the Move tool, which is the only way to reach the
// parts of a magnified screenshot that are off the stage.
func TestPanningAZoomedMarkupImage(t *testing.T) {
	id := makeScratchTab(t, "Pan me")
	d := readDoc(t)
	d.tab(t, id)["type"] = "markup"
	d.tab(t, id)["state"] = map[string]any{
		"layout": "stacked",
		"images": []any{map[string]any{"id": "one", "src": "assets/mock-screen.svg", "caption": "a.svg"}},
	}
	apply(t, d)

	s := open(t, "tab="+id)
	view := s.view(id)
	first := view.Locator(`.markup-figure[data-image-id="one"]`)

	for range 3 {
		if err := first.Locator(`[data-gesture="zoom-in"]`).Click(); err != nil {
			t.Fatalf("zooming in: %v", err)
		}
	}
	if err := view.Locator(`[data-gesture="move"]`).Click(); err != nil {
		t.Fatalf("choosing the move tool: %v", err)
	}

	transform := func() string {
		var out string
		s.evalJSON(&out, `(q) => getComputedStyle(document.querySelector(q)).transform`,
			`[data-tab="`+id+`"] .markup-zoom`)
		return out
	}
	before := transform()

	stage := first.Locator(".markup-stage")
	box, err := stage.BoundingBox()
	if err != nil || box == nil {
		t.Fatalf("no stage to drag on: %v", err)
	}
	// From the middle, towards the top-left, well inside the stage.
	s.dragPointer(
		point{box.X + box.Width*0.6, box.Y + box.Height*0.6},
		point{box.X + box.Width*0.3, box.Y + box.Height*0.3},
	)

	after := transform()
	if after == before {
		t.Errorf("dragging with Move did not pan the zoomed image (transform stayed %s)", before)
	}
}

// Choosing a tool does not move the page. It called render(), which re-appends
// every figure and every row to keep DOM order in sync — and moving a node is a
// remove and an insert, so on a tab with several tall images the document
// briefly lost its height and the browser clamped the scroll. Picking a tool
// threw you back to the top. Reported 2026-08-27.
func TestChoosingAToolDoesNotScrollThePage(t *testing.T) {
	id := makeScratchTab(t, "Tall markup")

	const count = 4
	images := make([]any, 0, count)
	for i := range count {
		images = append(images, map[string]any{
			"id": fmt.Sprintf("im%d", i), "src": "assets/mock-screen.svg",
			"caption": fmt.Sprintf("shot-%d.svg", i),
			"regions": []any{map[string]any{"id": fmt.Sprintf("bb%d", 400+i), "x": 0.1, "y": 0.1, "w": 0.2, "h": 0.2}},
		})
	}
	d := readDoc(t)
	d.tab(t, id)["type"] = "markup"
	d.tab(t, id)["state"] = map[string]any{"layout": "stacked", "images": images}
	apply(t, d)

	s := open(t, "tab="+id)

	if _, err := s.page.Evaluate(`() => window.scrollTo(0, 2000)`, nil); err != nil {
		t.Fatalf("scrolling: %v", err)
	}
	var before float64
	s.evalJSON(&before, `() => window.scrollY`)
	if before < 500 {
		t.Fatalf("the page only scrolled %.0fpx — not long enough to catch a jump", before)
	}

	// Every tool, because the one that re-renders most is not obvious from here.
	for _, tool := range []string{"ellipse", "pen", "move", "resize", "crop", "region"} {
		if err := s.view(id).Locator(`[data-gesture="` + tool + `"]`).Click(); err != nil {
			t.Fatalf("choosing %s: %v", tool, err)
		}
		var after float64
		s.evalJSON(&after, `() => window.scrollY`)
		if diff := after - before; diff > 2 || diff < -2 {
			t.Fatalf("choosing %s moved the page %.0fpx (from %.0f to %.0f)", tool, diff, before, after)
		}
	}
}

// The crop tool selects; it does not mark. The distinction matters because
// everything else you can drag on this image is stored, and a rectangle that
// looked like a mark but vanished on reload would be the worst of both.
func TestTheCropToolSelectsWithoutMarking(t *testing.T) {
	covers(t, "markup", "drag with the crop tool to select a rectangle, then copy it to the clipboard with or without its marks, or add it to this tab as a new image to draw on")

	id := makeScratchTab(t, "Crop me")
	d := readDoc(t)
	d.tab(t, id)["type"] = "markup"
	d.tab(t, id)["state"] = map[string]any{
		"layout": "stacked",
		"images": []any{map[string]any{"id": "one", "src": "assets/mock-screen.svg", "caption": "a.svg"}},
	}
	apply(t, d)

	s := open(t, "tab="+id)
	view := s.view(id)

	// Granted HERE, in setup, and not beside the click it enables. It WAS beside
	// the click and this test failed roughly one run in four with an empty
	// status: granting is a round trip to the browser and the press beat it, so
	// the handler ran against a clipboard that was not permitted yet. A
	// permission is a fact about the context, so it belongs where the context is.
	//
	// Not a cheat either: a real browser asks the human once and remembers, and
	// what is under test is the canvas path, not the prompt.
	if err := s.ctx.GrantPermissions([]string{"clipboard-read", "clipboard-write"}); err != nil {
		t.Fatalf("granting clipboard permission: %v", err)
	}

	// Both copy buttons are dead until there is something to copy, and say why.
	region := view.Locator(`[data-gesture="copy-region"]`)
	if err := expect.Locator(region).ToBeDisabled(); err != nil {
		t.Errorf("Copy region is live with nothing selected: %v", err)
	}

	if err := view.Locator(`[data-gesture="crop"]`).Click(); err != nil {
		t.Fatalf("choosing the crop tool: %v", err)
	}
	svg := view.Locator(".markup-svg").First()
	box, err := svg.BoundingBox()
	if err != nil || box == nil {
		t.Fatalf("no svg to drag on: %v", err)
	}
	s.dragPointer(
		point{box.X + box.Width*0.2, box.Y + box.Height*0.2},
		point{box.X + box.Width*0.6, box.Y + box.Height*0.6},
	)

	if err := expect.Locator(view.Locator(".markup-crop")).ToHaveCount(1); err != nil {
		t.Fatalf("the crop tool drew no selection: %v", err)
	}
	if err := expect.Locator(region).ToBeEnabled(); err != nil {
		t.Errorf("Copy region is still disabled with a selection drawn: %v", err)
	}

	// It is NOT a mark: no row, and nothing in the document.
	if err := expect.Locator(view.Locator(".markup-row:not(.markup-row-head)")).ToHaveCount(0); err != nil {
		t.Errorf("the crop rectangle was listed as a mark: %v", err)
	}
	time.Sleep(settle)
	st := readDoc(t).state(t, id)
	images, _ := st["images"].([]any)
	im, _ := images[0].(map[string]any)
	if regions, _ := im["regions"].([]any); len(regions) != 0 {
		t.Errorf("the crop rectangle was stored as a region: %v", regions)
	}

	// And the copy actually reaches the clipboard. This is the half that could
	// only be reasoned about otherwise, and the reasoning has one load-bearing
	// step: the image is served from the board's OWN origin, so the canvas it is
	// drawn into is not tainted and toBlob() returns. A cross-origin image would
	// fail here with a SecurityError and no amount of care in this code would
	// help.
	// Focus first. Chromium will not complete a clipboard write for a document
	// that does not have focus, and the suite drives several contexts through one
	// browser — so which page holds focus at this instant is not this test's to
	// assume. Without it the copy hung about one run in six, and the board's own
	// timeout (see copyRect) turned that into a visible failure rather than a
	// hang, which is right for a human and still a failed test here.
	if err := s.page.BringToFront(); err != nil {
		t.Fatalf("focusing the page: %v", err)
	}
	if err := region.Click(); err != nil {
		t.Fatalf("clicking Copy region: %v", err)
	}
	status := view.Locator(".markup-copy-status")
	// The press ANSWERS, and it answers at once. Asserted separately from the
	// result because the two can fail for entirely different reasons: this one
	// says the button is wired, the one below says the clipboard worked.
	if err := expect.Locator(status).Not().ToHaveText(""); err != nil {
		t.Fatalf("pressing Copy region said nothing at all — the press was swallowed: %v", err)
	}
	if err := expect.Locator(status).ToContainText("copied"); err != nil {
		var diag map[string]any
		s.evalJSON(&diag, `(q) => {
			const btns = [...document.querySelectorAll(q + ' [data-gesture="copy-region"]')];
			const stats = [...document.querySelectorAll(q + ' .markup-copy-status')];
			const views = [...document.querySelectorAll('[data-view="markup"]')];
			return {
				buttons: btns.length,
				disabled: btns.map((b) => b.disabled),
				statuses: stats.length,
				statusText: stats.map((e) => e.textContent),
				markupViews: views.length,
				crops: document.querySelectorAll(q + ' .markup-crop').length,
			};
		}`, `[data-tab="`+id+`"]`)
		got, _ := status.TextContent()
		t.Fatalf("the copy did not reach the clipboard (status: %q) diag=%+v: %v", got, diag, err)
	}
	// It says WHAT it copied, because "copied" alone cannot be checked by the
	// person who pressed it until they paste somewhere else.
	if err := expect.Locator(status).ToContainText("×"); err != nil {
		t.Errorf("the confirmation does not give the size of what it copied: %v", err)
	}
	var bad bool
	s.evalJSON(&bad, `(q) => document.querySelector(q).classList.contains('is-bad')`,
		`[data-tab="`+id+`"] .markup-copy-status`)
	if bad {
		got, _ := status.TextContent()
		t.Errorf("the copy reported a failure: %q", got)
	}

	// And the selection is gone the moment it has done its job. A dashed box that
	// outlives the copy sits on the picture looking like a mark you cannot
	// select, note or delete — which is how it was reported.
	if err := expect.Locator(view.Locator(".markup-crop")).ToHaveCount(0); err != nil {
		t.Errorf("the crop rectangle is still on the image after being copied: %v", err)
	}
	if err := expect.Locator(region).ToBeDisabled(); err != nil {
		t.Errorf("Copy region is still live with nothing selected: %v", err)
	}
	// Still not a mark, after all that.
	if err := expect.Locator(view.Locator(".markup-row:not(.markup-row-head)")).ToHaveCount(0); err != nil {
		t.Errorf("copying turned the crop rectangle into a mark: %v", err)
	}
}

// An image uses the width it is given, and NEVER more than its own.
//
// Two reports a day apart, and the fix for the first overshot the second.
// `.markup-stage` was capped at 900px, so on a wide board most of the row sat
// empty while the thing you are pointing AT was drawn small. Removing the cap
// left the image at `width: 100%`, which stretched a 200px screenshot across
// 1300px of blur — "a small image is shown huge".
//
// So the contract is a CEILING, not a target: as wide as the row allows, as wide
// as the picture actually is, whichever is smaller. Zoom is how you deliberately
// go past it. Asserted at two viewport widths, because either one alone shows
// only half of the rule.
func TestAMarkupImageFillsTheRowWithoutBeingUpscaled(t *testing.T) {
	id := makeScratchTab(t, "Sized image")

	d := readDoc(t)
	d.tab(t, id)["type"] = "markup"
	d.tab(t, id)["state"] = map[string]any{
		"layout": "stacked",
		"images": []any{map[string]any{"id": "only", "src": "assets/mock-screen.svg", "caption": "900px.svg"}},
	}
	apply(t, d)

	s := open(t, "tab="+id)
	measure := func() (w, natural, row float64) {
		var out struct{ W, Natural, Row float64 }
		s.evalJSON(&out, `(q) => {
			const img = document.querySelector(q + ' .markup-img');
			const row = document.querySelector(q + ' .markup-images');
			return {
				W: img.getBoundingClientRect().width,
				Natural: img.naturalWidth,
				Row: row.getBoundingClientRect().width,
			};
		}`, `[data-tab="`+id+`"]`)
		return out.W, out.Natural, out.Row
	}

	// Wide: the row is bigger than the picture, so the picture stays its own
	// size. This is the half that regressed.
	w, natural, row := measure()
	if row <= natural {
		t.Fatalf("the row (%.0f) is not wider than the image (%.0f), so this cannot test upscaling", row, natural)
	}
	if w > natural+1 {
		t.Errorf("a %.0fpx image was blown up to %.0fpx to fill a %.0fpx row", natural, w, row)
	}

	// Narrow: the picture is bigger than the row, so it comes down to fit. This
	// is the half the 900px cap used to break.
	if err := s.page.SetViewportSize(700, 900); err != nil {
		t.Fatalf("narrowing the viewport: %v", err)
	}
	w, natural, row = measure()
	if row >= natural {
		t.Fatalf("at 700px the row (%.0f) is still wider than the image (%.0f)", row, natural)
	}
	if w > row+1 {
		t.Errorf("a %.0fpx image overflows a %.0fpx row, at %.0fpx wide", natural, row, w)
	}
	// The stage draws a 1px border on each side, so a picture that fills the row
	// is `row - 2` wide and not `row`. Stated rather than absorbed into a fuzzy
	// tolerance: a test that allows 4px of slack would also pass a picture that
	// was 4px short for a reason nobody intended.
	const stageBorders = 2
	if w < row-stageBorders-1 {
		t.Errorf("the image is %.0fpx inside a %.0fpx row (less than %.0f) — it is not using the width it has",
			w, row, row-stageBorders)
	}
}

// The marks list is a TABLE, so its header has to sit over its columns — with
// one image on the tab, which is the case the fixture above does not cover.
//
// Reported 2026-08-27 from a board with a single pasted screenshot, as "the
// marks table columns are odd", and it was two faults at once.
//
// The first is why the suite never saw it. A row emptied its image cell with
// `hidden`, and `hidden` is `display: none` — a display:none grid item is not
// placed in the grid AT ALL, so every remaining cell in that row slid one
// column left while the header's own image cell stayed put. The id landed in
// the 22px mark-number track and rendered as "bb"; the delete button landed in
// the note track and became a full-width box with an ✕ adrift in it. With TWO
// images nothing is hidden and nothing shifts, and two images is what
// fixture.json gives `bb22`.
//
// The second was there at any image count: each row declared the shared column
// template itself, and grid aligns tracks within a container. A row was its own
// container, so `max-content` on the colour track meant the bulk-recolour button
// in the header and five 18px swatches in a row, and the fr tracks then divided
// two different remainders. Fixed with one grid on the list and `subgrid` on the
// rows.
func TestTheMarksTableHeaderSitsOverItsColumns(t *testing.T) {
	id := makeScratchTab(t, "One image")

	d := readDoc(t)
	d.tab(t, id)["type"] = "markup"
	d.tab(t, id)["state"] = map[string]any{
		"layout": "stacked",
		"images": []any{map[string]any{
			"id": "only", "src": "assets/mock-screen.svg", "caption": "one.svg",
			"regions": []any{
				map[string]any{"id": "bb206", "x": 0.04, "y": 0.01, "w": 0.15, "h": 0.93, "color": "danger"},
				map[string]any{"id": "bb207", "shape": "ellipse", "x": 0.23, "y": 0.77, "w": 0.12, "h": 0.14, "color": "accent"},
			},
			"strokes": []any{map[string]any{"id": "bb208", "points": []any{[]any{0.2, 0.7}, []any{0.3, 0.74}}, "color": "focus"}},
		}},
	}
	apply(t, d)

	s := open(t, "tab="+id)
	view := s.view(id)
	if err := expect.Locator(view.Locator(".markup-row:not(.markup-row-head)")).ToHaveCount(3); err != nil {
		t.Fatalf("the three seeded marks are not listed: %v", err)
	}

	// Every cell of the header and of the first data row, as {left, width}. The
	// seven cells are in the same order in both, and the pairs must land on the
	// same track.
	type cell struct {
		X, W float64
		Text string
	}
	var head, body []cell
	const probe = `(sel) => [...document.querySelector(sel).children].map((el) => {
		const r = el.getBoundingClientRect();
		return { X: r.left, W: r.width, Text: (el.textContent || '').trim() };
	})`
	s.evalJSON(&head, probe, `[data-tab="`+id+`"] .markup-row-head`)
	s.evalJSON(&body, probe, `[data-tab="`+id+`"] .markup-row:not(.markup-row-head)`)

	// Same number of cells, which is the `hidden` fault stated directly: a row
	// with six children against a header with seven cannot line up whatever the
	// tracks do.
	if len(head) != len(body) {
		t.Fatalf("the header has %d cells and a row has %d — a cell is being taken out of the grid flow, not emptied", len(head), len(body))
	}
	if len(head) != 6 {
		t.Fatalf("the header has %d cells, want 6", len(head))
	}

	// Six, not seven: the image-name column went with the restructure. Every row
	// in this table belongs to the image directly above it, so a caption repeated
	// down a column was noise — and it was the cell whose `hidden` caused the
	// shift this test was written for.
	names := []string{"index", "id", "summary", "colour", "note", "delete"}

	// LEFT EDGES, not widths. A cell's width is the element's, and two elements
	// can share a column honestly while differing in size — the header's delete
	// slot is a 24px spacer and the row's is a 16px button, both correct, both in
	// the same 24px track. Where a cell STARTS is the column it is in, and "the
	// header label sits over the column it names" is the whole of what was
	// reported.
	for i := range head {
		if diff := head[i].X - body[i].X; diff > 1 || diff < -1 {
			t.Errorf("the %s column: header starts at %.1f, the row starts at %.1f — they are not the same column",
				names[i], head[i].X, body[i].X)
		}
	}

	// And nothing spills out of its own column into the next one, which is the
	// other half of what a shifted grid looked like: the delete button had landed
	// in the note track and drawn a full-width box.
	for _, r := range []struct {
		what  string
		cells []cell
	}{{"the header", head}, {"the row", body}} {
		// Zero-width cells are skipped on both sides. The image column is 0px on
		// a single-image tab and its cell is deliberately empty — it holds a slot
		// and occupies nothing, so it can neither overflow nor be overflowed
		// into, and its neighbour's left edge is not meaningfully "after" it.
		for i := range r.cells {
			if r.cells[i].W == 0 {
				continue
			}
			for j := i + 1; j < len(r.cells); j++ {
				if r.cells[j].W == 0 {
					continue
				}
				if r.cells[i].X+r.cells[i].W > r.cells[j].X+1 {
					t.Errorf("%s: the %s cell runs from %.1f for %.1fpx, into the %s column at %.1f",
						r.what, names[i], r.cells[i].X, r.cells[i].W, names[j], r.cells[j].X)
				}
				break
			}
		}
	}

	// And the id is READABLE, which is the symptom a human actually reports. It
	// is the whole point of the column: the id is how a mark is named to an agent.
	var chip struct {
		Text           string
		Scroll, Client float64
	}
	s.evalJSON(&chip, `(sel) => {
		const el = document.querySelector(sel);
		return { Text: el.textContent.trim(), Scroll: el.scrollWidth, Client: el.clientWidth };
	}`, `[data-tab="`+id+`"] .markup-row:not(.markup-row-head) .markup-chip`)
	if chip.Text != "bb206" {
		t.Errorf("the id cell reads %q, want the mark's own id", chip.Text)
	}
	if chip.Scroll > chip.Client+1 {
		t.Errorf("the id %q is clipped: it needs %.0fpx and has %.0f", chip.Text, chip.Scroll, chip.Client)
	}
}

func TestTheSwatchRowComesFromTheDeclaredPalette(t *testing.T) {
	s := open(t, "tab="+markupTab)
	view := s.view(markupTab)

	swatches := view.Locator(`.markup-swatch-group [data-gesture="swatch"]`)
	n, err := swatches.Count()
	if err != nil {
		t.Fatal(err)
	}
	// Two groups: the toolbar's and the bulk-recolour panel's, both built from
	// the same declaration.
	if n < len(markupColors) {
		t.Fatalf("only %d swatches rendered; markup.spec.json declares %d colours", n, len(markupColors))
	}
	for _, token := range markupColors {
		if err := expect.Locator(view.Locator(`[data-gesture="swatch"][data-token="` + token + `"]`).First()).
			ToBeAttached(); err != nil {
			t.Errorf("no swatch for the declared colour %q: %v", token, err)
		}
	}
	if err := expect.Locator(view.Locator(`[data-gesture="swatch"][data-token="claude"]`)).ToHaveCount(0); err != nil {
		t.Errorf("the pre-rename colour name is still offered: %v", err)
	}
}

// markupColors is markup.spec.json's declared palette. Spelled out rather than
// read from the spec on purpose: a test that derives its expectation from the
// same declaration the code renders from asserts the two agree with themselves.
// This is the third opinion — and it is what would have caught `--claude` still
// being offered after the rename.
var markupColors = []string{"mark", "accent", "focus", "agent", "danger"}

// Pasting an image is how a screenshot gets onto the board: Ctrl+V, an upload to
// /upload, and a new entry in `images[]`. The clipboard itself is the one thing
// synthesised here — a real ClipboardEvent carrying a real File, dispatched at
// the document, which is where markup.js listens. Everything after that is the
// product: the fetch, the sniffing server, the state write.
func TestPastingAnImageUploadsItAndAddsIt(t *testing.T) {
	covers(t, "markup", "paste (Ctrl+V) or drop an image to add one")

	s := open(t, "tab="+markupTab)
	view := s.view(markupTab)
	before := len(markupImages(t))

	// A four-pixel PNG, built in the page. The server sniffs the BYTES, so a lie
	// about the type would be refused — which is the behaviour being relied on.
	var pasted bool
	s.evalJSON(&pasted, `async () => {
      const b64 = '`+tinyPNGBase64+`';
      const bin = atob(b64);
      const bytes = new Uint8Array(bin.length);
      for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
      const file = new File([bytes], 'pasted-by-the-suite.png', { type: 'image/png' });
      const dt = new DataTransfer();
      dt.items.add(file);
      return document.dispatchEvent(new ClipboardEvent('paste', {
        clipboardData: dt, bubbles: true, cancelable: true,
      }));
    }`)

	eventually(t, "the pasted image to reach the board", func() bool { return len(markupImages(t)) == before+1 })

	added := markupImages(t)[before]
	src, _ := added["src"].(string)
	if !strings.HasPrefix(src, "uploads/") {
		t.Errorf("the pasted image was stored at %q, want it under uploads/", src)
	}
	if caption, _ := added["caption"].(string); caption != "pasted-by-the-suite.png" {
		t.Errorf("the image lost the name it was pasted with: %q", caption)
	}
	imageID, _ := added["id"].(string)
	if err := expect.Locator(view.Locator(`.markup-figure[data-image-id="` + imageID + `"] img`)).
		ToBeVisible(); err != nil {
		t.Errorf("the pasted image did not render: %v", err)
	}

	// And it really is served as an image, from the URL the board handed back.
	res, err := s.page.Request().Get(boardURL + "/" + src)
	if err != nil {
		t.Fatalf("fetching the upload: %v", err)
	}
	if ct := res.Headers()["content-type"]; !strings.HasPrefix(ct, "image/") {
		t.Errorf("the upload is served as %q", ct)
	}
}

// tinyPNGBase64 is a 4×4 PNG. Inline rather than a testdata file because the
// point is to put BYTES in the page's clipboard, and a path would only become a
// read on the way there.
const tinyPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAQAAAAECAIAAAAmkwkpAAAAFklEQVR4nGN8+" +
	"vQpAzbAhFV0eEkAAP//DIsCtOZ7GAsAAAAASUVORK5CYII="

// A mark's note is what the agent actually reads — the mark itself is only a
// position. It saves as you type, with no button.
func TestNotingAMark(t *testing.T) {
	covers(t, "markup", "note each mark")

	s := open(t, "tab="+markupTab)
	view := s.view(markupTab)

	row := view.Locator(`.markup-list [data-mark-key="img1::r2"]`)
	note := row.Locator(".markup-note")
	written := "The ellipse is what the agent should look at."
	if err := note.Fill(written); err != nil {
		t.Fatalf("typing the note: %v", err)
	}
	if err := note.Blur(); err != nil {
		t.Fatalf("blurring: %v", err)
	}
	eventually(t, "the note to reach the server", func() bool {
		return markNote(t, "img1", "r2") == written
	})
}

// Right-click a mark row: the id, a link to it, and it as markdown. This is the
// only way to get a mark's id out of the screen and into a sentence without
// reading it off and typing it back.
func TestRightClickingAMarkRowOffersItsId(t *testing.T) {
	covers(t, "markup", "right-click a mark row to copy its id, a link to it, or it as markdown")

	s := open(t, "tab="+markupTab)
	view := s.view(markupTab)

	if err := view.Locator(`.markup-list [data-mark-key="img1::r2"]`).Click(playwright.LocatorClickOptions{
		Button: playwright.MouseButtonRight,
	}); err != nil {
		t.Fatalf("right-clicking the row: %v", err)
	}
	menu := s.page.Locator(".ctx-menu")
	if err := expect.Locator(menu).ToContainText("r2"); err != nil {
		t.Errorf("the menu does not name the mark: %v", err)
	}
	if err := expect.Locator(menu).ToContainText("markdown"); err != nil {
		t.Errorf("the menu does not offer the mark as markdown: %v", err)
	}
	if err := s.page.Keyboard().Press("Escape"); err != nil {
		t.Fatal(err)
	}
}

// TestATruncatedImageNameIsReachableOnHover lived here and was DELETED on
// 2026-08-27 with the column it covered. The marks list is per image now — each
// table sits inside the slice for its own picture — so no row can belong to a
// different image from the one above it and there is no caption to truncate.
// The caption is still on screen, once, in the slice head, where it is renamed.

/* ---------- helpers ---------- */

func markupImages(t *testing.T) []map[string]any {
	t.Helper()
	raw, _ := readDoc(t).state(t, markupTab)["images"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, entry := range raw {
		if im, ok := entry.(map[string]any); ok {
			out = append(out, im)
		}
	}
	return out
}

func markNote(t *testing.T, imageID, markID string) string {
	t.Helper()
	im := markupImage(t, imageID)
	for _, key := range []string{"regions", "strokes"} {
		list, _ := im[key].([]any)
		for _, raw := range list {
			if m, ok := raw.(map[string]any); ok && m["id"] == markID {
				note, _ := m["note"].(string)
				return note
			}
		}
	}
	return ""
}

func markupImage(t *testing.T, imageID string) map[string]any {
	t.Helper()
	images, _ := readDoc(t).state(t, markupTab)["images"].([]any)
	for _, raw := range images {
		if im, ok := raw.(map[string]any); ok && im["id"] == imageID {
			return im
		}
	}
	t.Fatalf("no image %q on the markup tab", imageID)
	return nil
}

func markCount(t *testing.T, imageID string) int {
	t.Helper()
	im := markupImage(t, imageID)
	regions, _ := im["regions"].([]any)
	strokes, _ := im["strokes"].([]any)
	return len(regions) + len(strokes)
}

func lastRegion(t *testing.T, imageID string) map[string]any {
	t.Helper()
	regions, _ := markupImage(t, imageID)["regions"].([]any)
	if len(regions) == 0 {
		t.Fatalf("image %q has no regions", imageID)
	}
	out, _ := regions[len(regions)-1].(map[string]any)
	return out
}

// regionBox is a mark's geometry as one comparable string — enough to say "it
// changed", which is what a resize test needs, without pinning pixel arithmetic
// that depends on the viewport.
func regionBox(t *testing.T, imageID, markID string) string {
	t.Helper()
	regions, _ := markupImage(t, imageID)["regions"].([]any)
	for _, raw := range regions {
		r, ok := raw.(map[string]any)
		if !ok || r["id"] != markID {
			continue
		}
		return sprint(r["x"], r["y"], r["w"], r["h"])
	}
	t.Fatalf("no mark %q on image %q", markID, imageID)
	return ""
}
