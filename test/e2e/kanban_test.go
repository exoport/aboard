//go:build e2e

package e2e

import (
	"testing"

	"github.com/mxschmitt/playwright-go"
)

// Kanban is the HTML5 drag-and-drop renderer — the only one. dag, markup and the
// sketch canvas all use pointer capture instead, and the two need different
// drivers: Locator.DragTo here, a composed down/move×N/up there. Reaching for
// the wrong one produces a card that does not move and a test that reads like
// the feature is broken.
//
// The tab under test is `ab13`, which has no state of its own: it borrows the
// dag's nodes through `stateFrom`. That makes it the better subject, not a
// worse one — a drop writes through the borrow into `ab1`, which is the path
// that would break silently if stateFrom were ever mishandled.
func TestAKanbanCardDragsBetweenColumns(t *testing.T) {
	covers(t, "kanban", "drag a card between columns")

	s := open(t, "tab=ab13")
	view := s.view("ab13")

	// A card that is NOT already in the target column, so the drop has work to do.
	const nodeID = "ab7"
	before := nodeStatus(t, nodeID)
	target := "todo"
	if before == target {
		target = "done"
	}

	card := view.Locator(`.card[data-id="` + nodeID + `"]`)
	column := view.Locator(`.column[data-status="` + target + `"]`)
	if err := card.DragTo(column); err != nil {
		t.Fatalf("dragging the card: %v", err)
	}

	eventually(t, "the card's new column to reach the server", func() bool {
		return nodeStatus(t, nodeID) == target
	})
	if err := expect.Locator(column.Locator(`.card[data-id="` + nodeID + `"]`)).ToBeVisible(); err != nil {
		t.Errorf("the card is not in the column it was dropped on: %v", err)
	}

	// And the borrow really is a borrow: the dag holds the state, so its own view
	// must agree without anything reloading the page.
	s.tab("ab1")
	if err := expect.Locator(s.view("ab1").Locator(`.node-box[data-id="` + nodeID + `"] .node-id`)).
		ToContainText(target); err != nil {
		t.Errorf("the dag does not show the status the kanban wrote through stateFrom: %v", err)
	}
}

func nodeStatus(t *testing.T, nodeID string) string {
	t.Helper()
	list, _ := readDoc(t).state(t, "ab1")["nodes"].([]any)
	for _, raw := range list {
		if n, ok := raw.(map[string]any); ok && n["id"] == nodeID {
			status, _ := n["status"].(string)
			return status
		}
	}
	t.Fatalf("no node %s on the plan", nodeID)
	return ""
}

// A card title is contenteditable and saves on blur — no button, no dialog.
func TestAKanbanCardTitleIsRenamedInPlace(t *testing.T) {
	covers(t, "kanban", "click a title to rename")

	s := open(t, "tab=ab13")
	const nodeID = "ab3"
	title := s.view("ab13").Locator(`.card[data-id="` + nodeID + `"] .card-title`)

	if err := expect.Locator(title).ToHaveAttribute("contenteditable", "true"); err != nil {
		t.Fatalf("the title is not editable: %v", err)
	}
	renamed := "Renamed by the browser suite"
	if err := title.Click(); err != nil {
		t.Fatalf("clicking the title: %v", err)
	}
	if err := s.page.Keyboard().Press("Control+a"); err != nil {
		t.Fatalf("selecting the title: %v", err)
	}
	if err := s.page.Keyboard().Type(renamed); err != nil {
		t.Fatalf("typing: %v", err)
	}
	// Enter blurs, which is what commits — the same keystroke a human uses.
	if err := s.page.Keyboard().Press("Enter"); err != nil {
		t.Fatalf("pressing Enter: %v", err)
	}

	eventually(t, "the new title to reach the server", func() bool {
		return nodeTitle(t, nodeID) == renamed
	})
}

func nodeTitle(t *testing.T, nodeID string) string {
	t.Helper()
	list, _ := readDoc(t).state(t, "ab1")["nodes"].([]any)
	for _, raw := range list {
		if n, ok := raw.(map[string]any); ok && n["id"] == nodeID {
			title, _ := n["title"].(string)
			return title
		}
	}
	return ""
}

// Right-click a card: its id, a link that reopens it, and its subtree as
// markdown. Nothing here writes; it is how a board object gets INTO a sentence,
// which is what the "name the thing, put the id beside it" rule needs.
func TestRightClickingAKanbanCardOffersItsIdAndLink(t *testing.T) {
	covers(t, "kanban", "right-click for id, link, markdown, subtree")

	s := open(t, "tab=ab13")
	if err := s.view("ab13").Locator(".card").First().Click(playwright.LocatorClickOptions{
		Button: playwright.MouseButtonRight,
	}); err != nil {
		t.Fatalf("right-clicking a card: %v", err)
	}

	menu := s.page.Locator(".ctx-menu")
	if err := expect.Locator(menu).ToBeVisible(); err != nil {
		t.Fatalf("no context menu: %v", err)
	}
	for _, item := range []string{"Copy id", "Copy link"} {
		if err := expect.Locator(menu.GetByText(item, playwright.LocatorGetByTextOptions{
			Exact: new(false),
		}).First()).ToBeVisible(); err != nil {
			t.Errorf("the menu has no %q entry: %v", item, err)
		}
	}
	if err := s.page.Keyboard().Press("Escape"); err != nil {
		t.Fatalf("closing the menu: %v", err)
	}
	if err := expect.Locator(menu).ToHaveCount(0); err != nil {
		t.Errorf("Escape did not close the menu: %v", err)
	}
}

// READ-ONLY: the affordances are GONE, not disabled. A card you can drag which
// then snaps back reads as a bug, so `state.readOnly` removes the controls and a
// badge says why.
//
// `cards > 0` is the load-bearing line, and it is why the example board gained
// three cards under plan-2 item 3: the fixture used to ship ab71 with an empty
// node list, so every negative below was trivially true and a renderer patched
// to emit drag handles passed this check.
func TestAReadOnlyKanbanOffersNothingToEdit(t *testing.T) {
	s := open(t, "tab=ab71")
	view := s.view("ab71")

	cards := view.Locator(".card")
	n, err := cards.Count()
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("the read-only kanban has no cards, so every assertion below is vacuous — see pkg/aboard/example/aboard.json")
	}

	if err := expect.Locator(view.Locator(".ro-badge")).ToBeVisible(); err != nil {
		t.Errorf("nothing says why this board cannot be edited: %v", err)
	}
	if err := expect.Locator(view.Locator("[data-readonly]")).ToHaveCount(1); err != nil {
		t.Errorf("the read-only marker is missing: %v", err)
	}
	if err := expect.Locator(view.Locator(`[draggable="true"]`)).ToHaveCount(0); err != nil {
		t.Errorf("a read-only card is still draggable: %v", err)
	}
	if err := expect.Locator(view.Locator(`[contenteditable="true"]`)).ToHaveCount(0); err != nil {
		t.Errorf("a read-only card title is still editable: %v", err)
	}
	// In read-only mode a card's foot holds its id chip and nothing else, so one
	// chip per card means no controls were rendered at all.
	if err := expect.Locator(view.Locator(".id-chip")).ToHaveCount(n); err != nil {
		t.Errorf("a read-only card carries something besides its id chip: %v", err)
	}
	// `:visible`, not a bare count. The Add button is hidden rather than removed
	// from the DOM — it is one element the renderer keeps a handle on and toggles
	// as `readOnly` flips under a live page — and `hidden` is display:none, so it
	// cannot be seen, focused or clicked. "Affordances are removed rather than
	// disabled" is a claim about what the human can reach, and that is what this
	// asserts; a DOM count would be asserting the implementation instead.
	if err := expect.Locator(view.Locator("[data-gesture]:visible")).ToHaveCount(0); err != nil {
		t.Errorf("a read-only kanban still offers a control to press: %v", err)
	}
}
