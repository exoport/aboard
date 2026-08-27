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
	covers(t, "markup", "Ctrl/Cmd+wheel over an image to zoom it; drag with Move to pan a zoomed one")

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

// The crop tool selects; it does not mark. The distinction matters because
// everything else you can drag on this image is stored, and a rectangle that
// looked like a mark but vanished on reload would be the worst of both.
func TestTheCropToolSelectsWithoutMarking(t *testing.T) {
	covers(t, "markup", "drag with the crop tool to select a rectangle, then copy it to the clipboard with or without its marks")

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
	if err := region.Click(); err != nil {
		t.Fatalf("clicking Copy region: %v", err)
	}
	status := view.Locator(".markup-copy-status")
	if err := expect.Locator(status).ToContainText("copied"); err != nil {
		got, _ := status.TextContent()
		t.Fatalf("the copy did not reach the clipboard (status: %q): %v", got, err)
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
}

// An image uses the width it is given. `.markup-stage` was capped at 900px, so
// on a wide board or a maximised panel most of the row sat empty while the thing
// you are trying to POINT AT was drawn small — reported 2026-08-27 with a
// screenshot of a 1600px panel half full.
//
// Marks are stored as fractions of the image, so nothing about them depends on
// the scale; the cap bought a smaller picture and nothing else.
func TestAMarkupImageUsesTheWidthItIsGiven(t *testing.T) {
	id := makeScratchTab(t, "Wide image")

	d := readDoc(t)
	d.tab(t, id)["type"] = "markup"
	d.tab(t, id)["state"] = map[string]any{
		"layout": "stacked",
		"images": []any{map[string]any{"id": "only", "src": "assets/mock-screen.svg", "caption": "wide.svg"}},
	}
	apply(t, d)

	s := open(t, "tab="+id)
	var stage, view float64
	s.evalJSON(&stage, `(q) => document.querySelector(q).getBoundingClientRect().width`,
		`[data-tab="`+id+`"] .markup-stage`)
	s.evalJSON(&view, `(q) => document.querySelector(q).getBoundingClientRect().width`,
		`[data-tab="`+id+`"] .markup-images`)

	// The suite runs at 1400px, so a 900px cap is unmissable here. Compared
	// against the row it sits in rather than a constant: the assertion is "it uses
	// what it is given", not "it is at least N pixels".
	if stage < view-2 {
		t.Errorf("the image stage is %.0fpx inside a %.0fpx row — something is still capping it", stage, view)
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
