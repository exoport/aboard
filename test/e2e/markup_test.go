//go:build e2e

package e2e

import (
	"strings"
	"testing"

	"github.com/mxschmitt/playwright-go"
)

// Markup: twelve declared controls and five pointer-capture tools. It is the
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

// The image column in the marks list is capped and ellipsised, so the full name
// has to be reachable — three screenshots all called "image.png" are otherwise
// indistinguishable in the list that names them.
func TestATruncatedImageNameIsReachableOnHover(t *testing.T) {
	covers(t, "markup", "hover a truncated image name in the marks list to see it in full")

	s := open(t, "tab="+markupTab)
	view := s.view(markupTab)

	cell := view.Locator(".markup-list .markup-row-image").First()
	title, err := cell.GetAttribute("title")
	if err != nil {
		t.Fatalf("reading the title: %v", err)
	}
	if strings.TrimSpace(title) == "" {
		t.Error("the image column carries no title, so a truncated name is unreadable")
	}
}

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
