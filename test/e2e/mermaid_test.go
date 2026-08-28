//go:build e2e

package e2e

import (
	"strings"
	"testing"

	"github.com/mxschmitt/playwright-go"
)

// A ```mermaid fence in a note renders as a diagram, and a fence mermaid cannot
// read shows its source verbatim.
//
// The board has vendored mermaid since the port, and a diagram could still only
// be a whole tab — so a write-up with one figure in it needed two tabs, and the
// figure could not travel with the prose being promoted into a document.
//
// Both halves are asserted in one test on purpose: "it rendered" and "it failed
// visibly" are the same feature, and a fence that silently emptied itself on a
// syntax error would pass the first assertion on the good fence alone.
func TestAMermaidFenceInANoteRendersAndFallsBackToItsSource(t *testing.T) {
	id := makeScratchTab(t, "Mermaid fence")

	const broken = "this is not mermaid at all {{{"
	// Every write happens BEFORE open(): a write issued after the page is up is a
	// foreign change racing the thing under test.
	setNote(t, id, strings.Join([]string{
		"# A note with a figure",
		"",
		"```mermaid",
		"flowchart LR",
		"  A[one] --> B[two]",
		"```",
		"",
		"And one it cannot read:",
		"",
		"```mermaid",
		broken,
		"```",
		"",
		"```",
		"a plain fence stays a code block",
		"```",
		"",
	}, "\n"))

	s := open(t, "tab="+id)
	view := s.view(id)

	// The bundle is committed at pkg/aboard/web/lib/, so this renders with no
	// network at all.
	if err := expect.Locator(view.Locator(".md .md-mermaid svg").First()).ToBeVisible(); err != nil {
		t.Fatalf("the mermaid fence did not render as a diagram: %v", err)
	}
	// Exactly one of the two fences rendered; the other kept its words.
	if err := expect.Locator(view.Locator(".md .md-mermaid svg")).ToHaveCount(1); err != nil {
		t.Errorf("wrong number of rendered fences: %v", err)
	}
	if err := expect.Locator(view.Locator(".md .md-mermaid code")).ToContainText(broken); err != nil {
		t.Errorf("the unreadable fence did not fall back to its source: %v", err)
	}
	// A fence with no info string is still a code block, not a diagram.
	if err := expect.Locator(view.Locator(".md pre:not(.md-mermaid pre)").
		Filter(playwright.LocatorFilterOptions{HasText: "a plain fence stays a code block"})).
		ToHaveCount(1); err != nil {
		t.Errorf("a plain fence stopped being a code block: %v", err)
	}
	if bad := s.consoleErrors(); len(bad) > 0 {
		t.Errorf("rendering a fence produced console errors:\n  %s", strings.Join(bad, "\n  "))
	}
}

// The same thing inside a stack's notes BLOCK, which is where a figure beside
// prose is most useful — the example board's Migration review tab carries one.
// A block mounts through a different ctx than a tab, and that difference is
// exactly what left an html block rendering blank once.
func TestAMermaidFenceRendersInsideAStackNotesBlock(t *testing.T) {
	s := open(t, "")
	s.tab("ab32")

	block := s.view("ab32").Locator(`[data-block-id="ab40"]`)
	if err := expect.Locator(block.Locator(".md .md-mermaid svg")).ToBeVisible(); err != nil {
		t.Fatalf("the example's notes block did not render its mermaid figure: %v", err)
	}
}

// setNote rewrites a notes tab as an agent would, and turns markdown on.
func setNote(t *testing.T, id, text string) {
	t.Helper()
	d := readDoc(t)
	tab := d.tab(t, id)
	tab["state"] = map[string]any{"text": text, "markdown": true}
	apply(t, d)
}
