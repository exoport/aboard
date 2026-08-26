//go:build e2e

package e2e

import (
	"strconv"
	"strings"
	"testing"

	"github.com/mxschmitt/playwright-go"
)

// The dag is the pointer-capture renderer: every one of its gestures is a
// down/move/up sequence on an <svg>, which is precisely the class of thing
// `chromium --dump-dom` could never reach. Seven declared gestures, and the
// tests below are ordered the way a human meets them.

// Drop one node ON another to reparent. The drop target is resolved with
// document.elementFromPoint at pointerup, so the pointer has to actually be over
// the target when the button comes up — which is why dragPointer ends with a
// Move to the destination before the Up.
func TestDraggingADagNodeOntoAnotherReparentsIt(t *testing.T) {
	covers(t, "dag", "drag to move")
	covers(t, "dag", "drop one node ON another to reparent")

	s := open(t, "tab=bb1")
	view := s.view("bb1")
	fitTheGraph(t, s)

	const child, newParent = "bb3", "bb4"
	if got := dagParent(t, child); got == newParent {
		t.Fatalf("%s is already parented to %s — this test would prove nothing", child, newParent)
	}

	from := s.centre(view.Locator(`.node-box[data-id="` + child + `"]`))
	to := s.centre(view.Locator(`.node-box[data-id="` + newParent + `"]`))
	s.dragPointer(from, to)

	eventually(t, "the new parent to reach the server", func() bool { return dagParent(t, child) == newParent })

	// Reparenting drops any pinned position, so the layout can re-tidy the node
	// under its new parent instead of leaving it stranded where it was dropped.
	if pos := dagNode(t, child)["pos"]; pos != nil {
		t.Errorf("the reparented node kept its pinned position %v, so the layout cannot tidy it", pos)
	}
}

// Dragging to EMPTY canvas pins a position instead — the other half of the same
// gesture, and the one that writes `pos`.
func TestDraggingADagNodeToEmptyCanvasPinsIt(t *testing.T) {
	s := open(t, "tab=bb1")
	view := s.view("bb1")
	fitTheGraph(t, s)

	const node = "bb5"
	svg := view.Locator("svg")
	from := s.centre(view.Locator(`.node-box[data-id="` + node + `"]`))
	// The bottom-left corner of the canvas: far from the tidied tree, and inside
	// the svg, so the drop lands on background rather than on a sibling.
	to := s.at(svg, 0.08, 0.9)
	if under := s.nodeUnder(to); under != "" {
		t.Skipf("the corner of the canvas is occupied by %s — nowhere empty to drop on", under)
	}
	s.dragPointer(from, to)

	eventually(t, "the pinned position to reach the server", func() bool {
		return dagNode(t, node)["pos"] != nil
	})
}

// Double-click opens an inline rename over the node. Enter commits.
func TestDoubleClickingADagNodeRenamesIt(t *testing.T) {
	covers(t, "dag", "double-click to rename")

	s := open(t, "tab=bb1")
	view := s.view("bb1")
	fitTheGraph(t, s)

	const node = "bb8"
	if err := view.Locator(`.node-box[data-id="` + node + `"]`).Dblclick(); err != nil {
		t.Fatalf("double-clicking the node: %v", err)
	}
	input := view.Locator(".rename-input")
	if err := expect.Locator(input).ToBeVisible(); err != nil {
		t.Fatalf("no rename input appeared: %v", err)
	}

	renamed := "Renamed in the graph"
	if err := input.Fill(renamed); err != nil {
		t.Fatalf("typing the new title: %v", err)
	}
	if err := input.Press("Enter"); err != nil {
		t.Fatalf("committing: %v", err)
	}
	eventually(t, "the new title to reach the server", func() bool {
		title, _ := dagNode(t, node)["title"].(string)
		return title == renamed
	})

	// Escape abandons. A rename editor that commits on Escape is the kind of
	// thing nobody notices until it has eaten a title.
	if err := view.Locator(`.node-box[data-id="` + node + `"]`).Dblclick(); err != nil {
		t.Fatalf("double-clicking again: %v", err)
	}
	if err := input.Fill("this should not be kept"); err != nil {
		t.Fatalf("typing: %v", err)
	}
	if err := input.Press("Escape"); err != nil {
		t.Fatalf("pressing Escape: %v", err)
	}
	if err := expect.Locator(input).ToHaveCount(0); err != nil {
		t.Errorf("the rename editor is still open after Escape: %v", err)
	}
	if title, _ := dagNode(t, node)["title"].(string); title != renamed {
		t.Errorf("Escape committed the rename anyway: %q", title)
	}
}

// Clicking a node selects it; clicking empty canvas deselects. Both are
// per-viewer state that must never reach the document.
func TestClickingSelectsADagNodeAndEmptyCanvasDeselects(t *testing.T) {
	covers(t, "dag", "click to select")

	s := open(t, "tab=bb1")
	view := s.view("bb1")
	fitTheGraph(t, s)

	node := view.Locator(`.node-box[data-id="bb6"]`)
	if err := node.Click(); err != nil {
		t.Fatalf("clicking the node: %v", err)
	}
	if err := expect.Locator(node).ToHaveAttribute("data-selected", "yes"); err != nil {
		t.Fatalf("the node did not select: %v", err)
	}
	if err := expect.Locator(view.Locator(".detail .id-chip")).ToContainText("bb6"); err != nil {
		t.Errorf("the detail panel is not showing the selected node: %v", err)
	}

	revBefore := readDoc(t)["rev"]
	empty := s.at(view.Locator("svg"), 0.03, 0.06)
	if err := s.page.Mouse().Click(empty.X, empty.Y); err != nil {
		t.Fatalf("clicking empty canvas: %v", err)
	}
	if err := expect.Locator(node).ToHaveAttribute("data-selected", "no"); err != nil {
		t.Errorf("clicking empty canvas did not deselect: %v", err)
	}
	if got := readDoc(t)["rev"]; got != revBefore {
		t.Errorf("selecting wrote to the board (rev %v -> %v) — selection is per-viewer state", revBefore, got)
	}
}

// Pan and zoom, which move the scene transform and nothing else. Asserted
// together because they share one guarantee: neither may write to the document.
func TestTheDagPansAndZoomsWithoutWritingToTheBoard(t *testing.T) {
	covers(t, "dag", "drag the background to pan")
	covers(t, "dag", "wheel to zoom")

	s := open(t, "tab=bb1")
	view := s.view("bb1")
	fitTheGraph(t, s)

	scene := view.Locator("svg > g")
	revBefore := readDoc(t)["rev"]
	before := attr(t, scene, "transform")

	svg := view.Locator("svg")
	s.dragPointer(s.at(svg, 0.05, 0.08), s.at(svg, 0.35, 0.45))
	panned := attr(t, scene, "transform")
	if panned == before {
		t.Errorf("dragging the background did not pan: transform is still %q", before)
	}

	s.wheel(svg, -240)
	zoomed := attr(t, scene, "transform")
	if zoomed == panned {
		t.Errorf("the wheel did not zoom: transform is still %q", panned)
	}
	if scaleOf(zoomed) <= scaleOf(panned) {
		t.Errorf("scrolling up did not zoom IN: %q -> %q", panned, zoomed)
	}

	if got := readDoc(t)["rev"]; got != revBefore {
		t.Errorf("panning or zooming wrote to the board (rev %v -> %v) — the viewport is per-viewer state", revBefore, got)
	}
}

// The delete confirmation is a real <dialog>, not a native confirm(): a
// destructive action in a renderer stays inside the page so it can say what will
// happen to the node's children.
//
// It operates on a node this test creates, so a failure cannot cost the example
// board a node — and creating it exercises the `add-root` control on the way in.
func TestDeletingADagNodeGoesThroughItsOwnDialog(t *testing.T) {
	s := open(t, "tab=bb1")
	view := s.view("bb1")

	before := dagNodeCount(t)
	if err := s.control("bb1", "add-root").Click(); err != nil {
		t.Fatalf("adding a root node: %v", err)
	}
	eventually(t, "the new node to reach the server", func() bool { return dagNodeCount(t) == before+1 })

	// The renderer selects what it just added, so the detail panel is already the
	// new node's.
	chip := view.Locator(".detail .id-chip")
	added, err := chip.TextContent()
	if err != nil {
		t.Fatalf("reading the new node's id: %v", err)
	}
	added = strings.TrimSpace(added)

	if err := s.control("bb1", "delete").Click(); err != nil {
		t.Fatalf("pressing Delete: %v", err)
	}
	dialog := view.Locator("dialog.sheet-dialog")
	if err := expect.Locator(dialog).ToBeVisible(); err != nil {
		t.Fatalf("the delete confirmation did not open: %v", err)
	}
	if err := expect.Locator(dialog).ToContainText(added); err != nil {
		t.Errorf("the dialog does not name the node it is about to delete: %v", err)
	}

	// Cancel first: a confirmation that deletes on Cancel is the worst possible
	// defect in this dialog, and it is one keystroke away.
	if err := dialog.GetByText("Cancel").Click(); err != nil {
		t.Fatalf("cancelling: %v", err)
	}
	if err := expect.Locator(dialog).ToBeHidden(); err != nil {
		t.Errorf("Cancel left the dialog open: %v", err)
	}
	if dagNodeCount(t) != before+1 {
		t.Fatalf("Cancel deleted the node anyway")
	}

	if err := s.control("bb1", "delete").Click(); err != nil {
		t.Fatalf("pressing Delete again: %v", err)
	}
	if err := dialog.GetByText("Delete", playwright.LocatorGetByTextOptions{
		Exact: new(true),
	}).Click(); err != nil {
		t.Fatalf("confirming: %v", err)
	}
	eventually(t, "the node to be deleted", func() bool { return dagNodeCount(t) == before })
}

// Right-click a node: the id, and a link that reopens this exact node.
func TestRightClickingADagNodeOffersALinkToIt(t *testing.T) {
	covers(t, "dag", "right-click for id, link, markdown")

	s := open(t, "tab=bb1")
	view := s.view("bb1")
	fitTheGraph(t, s)

	if err := view.Locator(`.node-box[data-id="bb2"]`).Click(playwright.LocatorClickOptions{
		Button: playwright.MouseButtonRight,
	}); err != nil {
		t.Fatalf("right-clicking a node: %v", err)
	}
	menu := s.page.Locator(".ctx-menu")
	if err := expect.Locator(menu).ToContainText("bb2"); err != nil {
		t.Fatalf("the menu does not head with the node's id: %v", err)
	}
	if err := expect.Locator(menu.GetByText("Copy link", playwright.LocatorGetByTextOptions{
		Exact: new(false),
	}).First()).ToBeVisible(); err != nil {
		t.Errorf("no link entry in the menu: %v", err)
	}
	if err := s.page.Keyboard().Press("Escape"); err != nil {
		t.Fatal(err)
	}
}

/* ---------- helpers ---------- */

// fitTheGraph presses the Fit control, so every node is inside the viewport
// before a test tries to point at one. Without it a node can sit outside the
// canvas and BoundingBox returns coordinates the mouse cannot reach — which
// fails as "the drag did nothing" rather than "the node was off screen".
func fitTheGraph(t *testing.T, s *session) {
	t.Helper()
	if err := s.control("bb1", "fit").Click(); err != nil {
		t.Fatalf("pressing Fit: %v", err)
	}
}

// nodeUnder answers which dag node, if any, is under a viewport point — the same
// question the renderer asks at pointerup, asked the same way.
func (s *session) nodeUnder(p point) string {
	s.t.Helper()
	var id string
	s.evalJSON(&id, `(p) => {
      const el = document.elementFromPoint(p.x, p.y);
      const g = el && el.closest ? el.closest('.node-box') : null;
      return g ? g.dataset.id : '';
    }`, map[string]any{"x": p.X, "y": p.Y})
	return id
}

func dagNode(t *testing.T, id string) map[string]any {
	t.Helper()
	list, _ := readDoc(t).state(t, "bb1")["nodes"].([]any)
	for _, raw := range list {
		if n, ok := raw.(map[string]any); ok && n["id"] == id {
			return n
		}
	}
	t.Fatalf("no node %s on the plan", id)
	return nil
}

func dagParent(t *testing.T, id string) string {
	t.Helper()
	parent, _ := dagNode(t, id)["parent"].(string)
	return parent
}

func dagNodeCount(t *testing.T) int {
	t.Helper()
	list, _ := readDoc(t).state(t, "bb1")["nodes"].([]any)
	return len(list)
}

func attr(t *testing.T, l playwright.Locator, name string) string {
	t.Helper()
	v, err := l.GetAttribute(name)
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return v
}

// scaleOf pulls k out of "translate(x y) scale(k)". Crude on purpose: the string
// is written by one line of dag.js, and a parser would be more code than the
// thing it parses.
func scaleOf(transform string) float64 {
	_, rest, found := strings.Cut(transform, "scale(")
	if !found {
		return 0
	}
	inner, _, closed := strings.Cut(rest, ")")
	if !closed {
		return 0
	}
	k, err := strconv.ParseFloat(strings.TrimSpace(inner), 64)
	if err != nil {
		return 0
	}
	return k
}
