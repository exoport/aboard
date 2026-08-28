//go:build e2e

package e2e

import (
	"fmt"
	"strings"
	"testing"
)

// The write path's two browser-facing halves.
//
// Until now a write warning could only ever reach the actor who ran the CLI. An
// `aboard apply` that set a prop no renderer reads printed a line on a terminal
// the human is not sitting at, the tab drew an empty box, and the person who
// found out was the person who could not fix it. These two tests are the claim
// that it now reaches the screen: on the tab it is about, and on the trace tab
// beside the write that caused it.

// makeUITab is a scratch `ui` tab with a tree that renders — the starting point
// for breaking it on purpose. Allocated through the board's own counter, like
// makeScratchTab, because a private counter walks into ids.go's.
func makeUITab(t *testing.T, name string) string {
	t.Helper()
	d := readDoc(t)
	next, ok := d["nextId"].(float64)
	if !ok {
		t.Fatalf("the board has no nextId to allocate from: %v", d["nextId"])
	}
	id := fmt.Sprintf("ab%d", int(next))
	d["nextId"] = int(next) + 1
	list, _ := d["tabs"].([]any)
	d["tabs"] = append(list, map[string]any{
		"id":    id,
		"name":  name,
		"type":  "ui",
		"note":  "Made by the browser suite; safe to delete.",
		"state": map[string]any{"root": map[string]any{"type": "text", "value": "fine so far"}},
	})
	apply(t, d)
	return id
}

// breakTheTree writes a `stat` with a prop the component does not read. Chosen
// because it is the failure that shows NOTHING: an unknown component draws a
// dashed marker naming the catalog, an unknown PROP draws an empty box and
// applies clean.
func breakTheTree(t *testing.T, id, label string) {
	t.Helper()
	d := readDoc(t)
	d.tab(t, id)["state"] = map[string]any{
		"root": map[string]any{"type": "stat", "value": "3", "caption": "widgets"},
	}
	applyLabelled(t, d, label)
}

// The banner. A foreign write that cannot render says so on the tab it is about,
// to the one person looking at it.
func TestAWarningFromAnAgentsWriteReachesTheTab(t *testing.T) {
	id := makeUITab(t, "Write warnings")

	s := open(t, "")
	s.tab(id)

	// The write happens with the page already up, because the channel under test
	// is the live one: the shell learns about a foreign write's warnings on the
	// change frame, not by polling anything.
	breakTheTree(t, id, "")

	banner := s.view(id).Locator(`.banner[data-notice="warnings"]`)
	if err := expect.Locator(banner).ToBeVisible(); err != nil {
		t.Fatalf("no warning banner on the tab the write broke: %v", err)
	}
	if err := expect.Locator(banner).ToContainText("caption"); err != nil {
		t.Fatalf("the banner does not name the prop that will not render: %v", err)
	}

	// And it comes DOWN when the next write to the same tab is clean. The banner
	// says "the last write to this tab", so a banner that outlives the mistake is
	// a false sentence — and the failure it produces is this feature's own,
	// backwards: the agent has fixed the tree and the human is still looking at a
	// warning about it. The shell can only do this because the change frame names
	// the tabs the checks RAN over as well as the ones that warned.
	fixTheTree(t, id)
	if err := expect.Locator(banner).ToHaveCount(0); err != nil {
		t.Fatalf("the warning outlived the write that repaired the tab: %v", err)
	}

	// Dismissible too, and only here: the journal keeps the warning whatever the
	// viewer does with the banner, which is what makes dismissing it safe.
	breakTheTree(t, id, "")
	if err := expect.Locator(banner).ToBeVisible(); err != nil {
		t.Fatalf("the banner did not come back for a second bad write: %v", err)
	}
	if err := banner.GetByRole("button").Click(); err != nil {
		t.Fatalf("dismissing the warning: %v", err)
	}
	if err := expect.Locator(banner).ToHaveCount(0); err != nil {
		t.Fatalf("the banner would not go away: %v", err)
	}
}

// fixTheTree writes the same `stat` with the prop it actually reads, so the tab
// is repaired without changing anything else about the write.
func fixTheTree(t *testing.T, id string) {
	t.Helper()
	d := readDoc(t)
	d.tab(t, id)["state"] = map[string]any{
		"root": map[string]any{"type": "stat", "value": "3", "label": "widgets"},
	}
	apply(t, d)
}

// The trace tab: the same write, with its label and its warnings, on the record
// of who wrote what. The dot is ringed so a reader can see which writes warned
// without clicking every one of them.
func TestTheTraceTabShowsAWritesLabelAndItsWarnings(t *testing.T) {
	covers(t, "trace", "")

	id := makeUITab(t, "Trace warnings")
	const why = "e2e: breaking the tree on purpose"

	s := open(t, "")
	s.tab(id)
	breakTheTree(t, id, why)

	s.tab("ab127")
	if err := s.control("ab127", "reload").Click(); err != nil {
		t.Fatalf("reloading the trace: %v", err)
	}

	// THIS write's dot, addressed by the label it carries — not the newest warned
	// one. The suite shares a board and other tests warn on purpose too, so
	// `.Last()` was picking somebody else's write whenever the order put one after
	// this: a test that passes only in some orders is one that pins nothing.
	dot := s.view("ab127").Locator(`.event[data-warned="yes"][title*="` + why + `"]`)
	if err := expect.Locator(dot).ToBeVisible(); err != nil {
		t.Fatalf("this write is not marked as having warned: %v", err)
	}
	// The tooltip carries the label and the warning count without a click,
	// because that is what a reader scanning the lanes actually has.
	title, err := dot.GetAttribute("title")
	if err != nil {
		t.Fatalf("reading the dot's tooltip: %v", err)
	}
	if !strings.Contains(title, why) || !strings.Contains(title, "warning") {
		t.Errorf("the tooltip says neither why nor that it warned: %q", title)
	}

	if err := dot.Click(); err != nil {
		t.Fatalf("selecting the write: %v", err)
	}
	detail := s.view("ab127").Locator(".detail")
	if err := expect.Locator(detail).ToContainText(why); err != nil {
		t.Fatalf("the entry does not say why the write happened: %v", err)
	}
	if err := expect.Locator(detail).ToContainText("caption"); err != nil {
		t.Fatalf("the entry does not carry the warning: %v", err)
	}
}
