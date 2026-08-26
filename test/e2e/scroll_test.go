//go:build e2e

package e2e

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// Per-tab scroll memory.
//
// Every tab shares one scrolling document — `.view` elements are shown and
// hidden, they do not scroll independently — so leaving a long list half way
// down, glancing at another tab and coming back used to land at the top with no
// record of where you had got to.
//
// PER VIEWER, so nothing here may reach the board document: two people looking
// at one board in the same second must disagree about scroll while agreeing
// about content. That half is asserted too, because it is the half a future
// change could break without anyone noticing on screen.

// scrollTab makes a tab tall enough to scroll, deterministically.
//
// Not one of the seeded example tabs: how tall those render depends on the
// viewport, on mermaid, and on whichever test ran before — and a scroll test
// whose subject is only sometimes scrollable is a test that only sometimes tests
// anything. A table of 120 rows is taller than any viewport this suite uses.
func scrollTab(t *testing.T, name string) string {
	t.Helper()
	rows := make([]any, 0, 120)
	for i := range 120 {
		rows = append(rows, map[string]any{
			"id":   fmt.Sprintf("row-%d", i),
			"item": fmt.Sprintf("row %d of a deliberately long table", i),
		})
	}
	return makeScratchTabOfType(t, name, "table", map[string]any{
		"columns": []any{map[string]any{"id": "item", "label": "Item", "type": "text"}},
		"rows":    rows,
	})
}

func TestTheBoardRemembersWhereYouWereOnEachTab(t *testing.T) {
	long := scrollTab(t, "Long one")
	other := scrollTab(t, "The other long one")

	s := open(t, "tab="+long)

	scrollTo(s, 600)
	if got := scrollY(s); got < 500 {
		t.Fatalf("the page did not scroll (scrollY %v) — the subject tab is not tall enough for this test to mean anything", got)
	}
	remembered(t, s, long, 600)

	// A tab you have never opened starts at the TOP, rather than wherever the
	// last one happened to leave the page. Without that half of the fix you would
	// still arrive in the middle of a tab you have not read.
	switchTab(s, other)
	if got := scrollY(s); got > 5 {
		t.Errorf("a never-visited tab opened at scrollY %v, want the top", got)
	}

	scrollTo(s, 300)
	remembered(t, s, other, 300)

	switchTab(s, long)
	if got := scrollY(s); got < 550 || got > 650 {
		t.Errorf("coming back to the first tab landed at scrollY %v, want ~600", got)
	}
	switchTab(s, other)
	if got := scrollY(s); got < 250 || got > 350 {
		t.Errorf("coming back to the second tab landed at scrollY %v, want ~300", got)
	}

	// It has to survive the page throwing itself away, because the page does
	// exactly that on its own whenever the UI signature moves — a rebuild during
	// a reading session is the commonest way this position is lost.
	if _, err := s.page.Reload(); err != nil {
		t.Fatalf("reloading: %v", err)
	}
	if err := s.page.Locator(`#tabs .tab`).First().WaitFor(); err != nil {
		t.Fatalf("the board never came back after the reload: %v", err)
	}
	switchTab(s, long)
	if got := scrollY(s); got < 550 || got > 650 {
		t.Errorf("after a reload the first tab landed at scrollY %v, want ~600 — sessionStorage is what carries this across one", got)
	}

	// And none of it is on the board. Per-viewer state in the document would mean
	// one viewer's scrolling moving somebody else's page.
	if raw, ok := readDoc(t).tab(t, long)["scroll"]; ok {
		t.Errorf("the tab carries a `scroll` field (%v) — per-viewer state never goes in the state file", raw)
	}
	var stored []string
	s.evalJSON(&stored, `() => Object.keys(sessionStorage).filter((k) => k.startsWith('aboard.scroll.'))`)
	if len(stored) == 0 {
		t.Error("nothing under aboard.scroll.* — the position is being remembered somewhere else than the brief says")
	}
}

func scrollTo(s *session, y float64) {
	s.t.Helper()
	if _, err := s.page.Evaluate(`(y) => window.scrollTo(0, y)`, y); err != nil {
		s.t.Fatalf("scrolling to %v: %v", y, err)
	}
}

// remembered waits until the shell has recorded an offset for a tab.
//
// A real condition rather than a sleep. The recorder is debounced, so something
// has to be waited for either way — and "the value is in sessionStorage" is the
// thing the next assertion depends on, where a fixed pause is a guess that goes
// stale the moment the debounce moves.
func remembered(t *testing.T, s *session, tab string, want float64) {
	t.Helper()
	key := "aboard.scroll." + tab
	eventually(t, fmt.Sprintf("%s to be recorded at ~%v", tab, want), func() bool {
		var raw string
		s.evalJSON(&raw, `(k) => sessionStorage.getItem(k) || ""`, key)
		parts := strings.Split(raw, ",")
		if len(parts) != 2 {
			return false
		}
		y, err := strconv.ParseFloat(parts[1], 64)
		return err == nil && y > want-50 && y < want+50
	})
}

// switchTab clicks a tab the way a person does — and NOT the way s.tab() does,
// which is the one place in this suite where that difference matters.
//
// Playwright scrolls a target into view before clicking it, using the element's
// LAYOUT box. The tab strip is `position: sticky`, and a sticky element's layout
// box stays where it was in the document while only its PAINTED position
// follows the viewport — so the strip is on screen the whole time and the
// scroll-into-view still drags the page back to the top of the document before
// every click. That is a fact about the driver, not about the board: a person
// clicking a visible tab scrolls nothing. Measured here as a remembered offset
// of 131 where the human had left 600.
//
// So the click is dispatched in the page. It is a real click event on the real
// button with the real handler; only the driver's scrolling is left out.
func switchTab(s *session, id string) {
	s.t.Helper()
	if _, err := s.page.Evaluate(
		`(id) => document.querySelector('#tabs .tab[data-id="' + id + '"]').click()`, id); err != nil {
		s.t.Fatalf("clicking tab %s: %v", id, err)
	}
	if err := s.view(id).WaitFor(); err != nil {
		s.t.Fatalf("tab %s never became the active view: %v", id, err)
	}
}

// scrollY is the document scroller's offset, read the way the shell writes it.
func scrollY(s *session) float64 {
	s.t.Helper()
	var y float64
	s.evalJSON(&y, `() => (document.scrollingElement || document.documentElement).scrollTop`)
	return y
}

// The case the restore RETRIES for, and the reason one apply is not enough.
//
// A scroll past the current bottom of the page is clamped silently, and a
// renderer that lays out asynchronously is shorter at the moment it mounts than
// it is a few frames later. Mermaid is the honest example: `diagram` hands its
// source to the bundle and the SVG appears when it appears, so a restore that
// fired once, on mount, would put the page back somewhere near the top of a tab
// the human had left half way down — and it would look exactly like the feature
// not existing.
func TestTheScrollRestoreOutlastsARendererThatLaysOutLate(t *testing.T) {
	var src strings.Builder
	src.WriteString("graph TD\n")
	for i := range 60 {
		fmt.Fprintf(&src, "    N%d[\"step %d of a deliberately tall graph\"] --> N%d[\"step %d\"]\n", i, i, i+1, i+1)
	}
	late := makeScratchTabOfType(t, "Lays out late", "diagram", map[string]any{"source": src.String()})
	short := makeScratchTab(t, "Somewhere else")

	s := open(t, "tab="+late)
	// Wait for the diagram to actually be on screen before scrolling, so the
	// offset being remembered is one the page really had.
	if err := expect.Locator(s.view(late).Locator("svg")).ToBeVisible(); err != nil {
		t.Fatalf("the diagram never rendered: %v", err)
	}
	scrollTo(s, 500)
	if got := scrollY(s); got < 400 {
		t.Fatalf("the diagram tab did not scroll (scrollY %v); this test needs a page taller than the viewport", got)
	}
	remembered(t, s, late, 500)

	switchTab(s, short)
	switchTab(s, late)
	if got := scrollY(s); got < 450 || got > 550 {
		t.Errorf("the diagram tab came back at scrollY %v, want ~500", got)
	}

	// The reload is where the asynchrony actually bites, and it is worth saying
	// why rather than testing the switch twice: coming BACK to a tab finds its
	// SVG already in the DOM from the first mount, so the page is its full height
	// the instant the view is shown. A reload has no such copy — the renderer
	// starts from the source, and the page is short until mermaid answers. That is
	// also the case that happens by itself, every time the board reloads its own
	// code under someone who was reading.
	if _, err := s.page.Reload(); err != nil {
		t.Fatalf("reloading: %v", err)
	}
	if err := expect.Locator(s.view(late).Locator("svg")).ToBeVisible(); err != nil {
		t.Fatalf("the diagram never rendered after the reload: %v", err)
	}
	if got := scrollY(s); got < 450 || got > 550 {
		t.Errorf("after a reload the diagram tab landed at scrollY %v, want ~500 — the restore gave up before the renderer had its height", got)
	}
}
