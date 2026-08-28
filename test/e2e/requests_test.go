//go:build e2e

package e2e

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mxschmitt/playwright-go"
)

// The human's notes to an agent — the one channel on this board that runs their
// way. Guarantee 5 is enforced on the server and covered by Go tests; what no Go
// test can see is whether the BROWSER writes them as the human, draws a stamp as
// struck through, and puts the outstanding count where the human will find it.

// The whole loop in one test, in the order it actually happens: they ask, an
// agent's ordinary write goes past without eating it, an agent answers it, and
// they throw it away.
func TestTheHumanAsksAnAgentForSomethingOnATab(t *testing.T) {
	id := makeScratchTab(t, "Ask me")

	s := open(t, "tab="+id)
	strip := s.page.Locator("#tab-asks")
	if err := expect.Locator(strip).ToBeVisible(); err != nil {
		t.Fatalf("no notes-for-the-agent strip on the tab: %v", err)
	}

	if err := s.page.Locator("#tab-asks-input").Fill("the third column is wrong"); err != nil {
		t.Fatalf("typing the note: %v", err)
	}
	if err := s.page.Keyboard().Press("Enter"); err != nil {
		t.Fatalf("pressing Enter: %v", err)
	}

	eventually(t, "the note to reach the server", func() bool {
		return len(requestsOn(t, id)) == 1
	})
	ask := requestsOn(t, id)[0]
	if ask["text"] != "the third column is wrong" {
		t.Errorf("the note reached the server as %v", ask["text"])
	}
	// `by` is what every guarantee in tabs.go turns on. If the browser ever wrote
	// these as an agent, the server would refuse the human's own note.
	if ask["by"] != "human" {
		t.Errorf("the browser wrote the note as %v, want \"human\"", ask["by"])
	}
	if idOf(ask) == "" || !strings.HasPrefix(idOf(ask), "ab") {
		t.Errorf("the note took the id %q — requests come from the board allocator", idOf(ask))
	}

	// The count on the tab button: the human has to be able to see an outstanding
	// note from another tab, or the channel only works while they are looking at
	// the right one.
	count := s.page.Locator(`#tabs .tab[data-id="` + id + `"] .tab-asks-count`)
	if err := expect.Locator(count).ToHaveText("1"); err != nil {
		t.Errorf("the tab button shows no outstanding count: %v", err)
	}

	// Guarantee 5 through the whole stack: an agent's ordinary read-modify-write
	// that never looked at `requests` must not take the note with it. This is the
	// shape the guarantee exists for — not malice, just a document handed back
	// without a field nobody read.
	d := readDoc(t)
	delete(d.tab(t, id), "requests")
	d.state(t, id)["text"] = "an agent rewrote the body"
	apply(t, d)

	eventually(t, "the agent's write to land", func() bool {
		return dig(readDoc(t).state(t, id), "text") == "an agent rewrote the body"
	})
	if got := requestsOn(t, id); len(got) != 1 {
		t.Fatalf("an agent write dropped the human's note: %v", got)
	}
	// And the page shows it again without being reloaded by hand, because that
	// write arrives over SSE like any other.
	row := strip.Locator(".ask").First()
	if err := expect.Locator(row).ToContainText("the third column is wrong"); err != nil {
		t.Errorf("the restored note is not on screen: %v", err)
	}

	// An agent answers it. This is the only feedback the human gets that anything
	// happened, so it has to be visible rather than merely stored.
	d = readDoc(t)
	stamp(t, d.tab(t, id), "flipped it")
	apply(t, d)

	if err := expect.Locator(row).ToHaveAttribute("data-done", "yes"); err != nil {
		t.Fatalf("a stamped note does not render as done: %v", err)
	}
	// Two elements, not one, and that split is the fix for the 2026-08-27 break
	// below: `.ask-meta` carries who and when — bounded — and `.ask-reply`
	// carries the sentence.
	if err := expect.Locator(row.Locator(".ask-reply")).ToContainText("flipped it"); err != nil {
		t.Errorf("the agent's one-line reply is not shown: %v", err)
	}
	if err := expect.Locator(row.Locator(".ask-meta")).ToContainText(agentActor); err != nil {
		t.Errorf("the stamp does not say which session acted: %v", err)
	}
	if err := expect.Locator(row.Locator(".ask-meta")).Not().ToContainText("flipped it"); err != nil {
		t.Errorf("the reply is back inside .ask-meta, which cannot hold a sentence: %v", err)
	}
	// Struck through, and it is a CSS rule rather than a character in the text —
	// so it is asserted as the computed style, which is the only thing a reader
	// actually sees.
	var decoration string
	s.evalJSON(&decoration,
		`(sel) => getComputedStyle(document.querySelector(sel)).textDecorationLine`,
		`#tab-asks .ask[data-done="yes"] .ask-text`)
	if !strings.Contains(decoration, "line-through") {
		t.Errorf("a done note is not struck through (text-decoration-line: %q)", decoration)
	}
	// Answered, so it is no longer outstanding, and the count goes with it.
	if err := expect.Locator(count).ToBeHidden(); err != nil {
		t.Errorf("a stamped note still counts as outstanding on the tab button: %v", err)
	}

	// The human throws it away. No confirmation on purpose — it is their own
	// sentence, and deleting it is the only way a stamp is ever cleared.
	if err := row.Locator(".ask-drop").Click(); err != nil {
		t.Fatalf("clicking the note's ✕: %v", err)
	}
	eventually(t, "the note to be deleted on the server", func() bool {
		return len(requestsOn(t, id)) == 0
	})
	if _, still := readDoc(t).tab(t, id)["requests"]; still {
		t.Error("the last note went but the `requests` key stayed — an empty array and no key must not be two states")
	}
	if err := expect.Locator(strip.Locator(".ask")).ToHaveCount(0); err != nil {
		t.Errorf("the deleted note is still on screen: %v", err)
	}
}

// The strip belongs to the tab you are looking at, exactly as the purpose strip
// does. A note that stayed on screen while you switched tabs would be the worst
// version of this feature: an ask attached to the wrong thing.
// A long reply must reflow, and it must not squeeze the request into a column of
// single letters. Both were true on 2026-08-27, in a VS Code panel, from a stamp
// this session wrote — 250 characters of "what I did", which is a perfectly
// ordinary thing for an agent to say.
//
// The cause was one declaration: `.ask-meta` was `flex: 0 0 auto`, correct for
// the timestamp it was written for and catastrophic for a sentence. It refused
// to shrink, so it claimed its whole max-content width and ran off the right
// edge, and `.ask-text` next to it was squeezed to nearly nothing — where
// `overflow-wrap: anywhere`, which exists so a pasted URL cannot overflow,
// wrapped the human's own sentence one character per line.
//
// Measured rather than eyeballed, because "it looks wrong" is not a test: the
// request keeps a usable share of the strip, nothing overflows it horizontally,
// and the reply is on a line of its own BELOW the request rather than beside it.
func TestALongReplyReflowsInsteadOfShreddingTheRequest(t *testing.T) {
	id := makeScratchTab(t, "Long reply")

	d := readDoc(t)
	tab := d.tab(t, id)
	tab["requests"] = []any{map[string]any{
		"id":   "ab9001",
		"at":   "2026-08-27T10:00:00Z",
		"by":   "human",
		"text": "add the first note to the agent, include an example in the ui panel",
	}}
	applyAsHuman(t, d)

	d = readDoc(t)
	stamp(t, d.tab(t, id), "Filled ab199 with a three-panel ui example. Try it has four bound fields, "+
		"a six-item checklist and three intent buttons; How it works shows the JSON behind them "+
		"and four rules; Tones shows all seven tones, which are now the part of the palette a VS "+
		"Code theme may never repaint.")
	apply(t, d)

	s := open(t, "tab="+id)
	row := s.page.Locator("#tab-asks .ask").First()
	if err := expect.Locator(row).ToHaveAttribute("data-done", "yes"); err != nil {
		t.Fatalf("the seeded stamp did not render as done: %v", err)
	}

	type box struct{ X, Y, W, H float64 }
	var strip, text, reply box
	// Presence first, and named. Without this the reply's absence surfaces as a
	// TypeError inside an evaluate — "evaluating (sel) => …", which says nothing
	// about what is missing — and the first thing this test is for is that the
	// reply has an element of its own at all.
	read := func(out *box, sel string) {
		t.Helper()
		if n, err := s.page.Locator(sel).Count(); err != nil || n != 1 {
			t.Fatalf("%s matched %d elements (err %v) — the strip is not built the way this test measures", sel, n, err)
		}
		s.evalJSON(out, `(sel) => {
			const r = document.querySelector(sel).getBoundingClientRect();
			return { X: r.left, Y: r.top, W: r.width, H: r.height };
		}`, sel)
	}
	read(&strip, "#tab-asks")
	read(&text, "#tab-asks .ask .ask-text")
	read(&reply, "#tab-asks .ask .ask-reply")

	// The shredding, as a number. At the default 1400px viewport the request had
	// collapsed to roughly one character wide; a third of the strip is far below
	// what the layout gives it and far above what the bug left.
	if text.W < strip.W/3 {
		t.Errorf("the request is %.0fpx wide inside a %.0fpx strip — it is being squeezed to a column, not laid out",
			text.W, strip.W)
	}
	// And it is one line, not sixty-seven.
	if text.H > 4*reply.H {
		t.Errorf("the request is %.0fpx tall against a %.0fpx reply — it is wrapping mid-word", text.H, reply.H)
	}

	// Below, not beside: the reply gets the full width because it is an answer to
	// the request rather than a fact about it.
	if reply.Y <= text.Y {
		t.Errorf("the reply is on the request's own line (reply top %.0f, request top %.0f)", reply.Y, text.Y)
	}
	if reply.W < strip.W/2 {
		t.Errorf("the reply is only %.0fpx of a %.0fpx strip — it did not take a line of its own", reply.W, strip.W)
	}

	// Nothing runs off the right edge, which is the half the human reported as
	// "not reflowing". Half a pixel of slack for sub-pixel layout.
	for _, c := range []struct {
		what string
		b    box
	}{{"the request", text}, {"the reply", reply}} {
		if c.b.X+c.b.W > strip.X+strip.W+0.5 {
			t.Errorf("%s overflows the strip: ends at %.1f, strip ends at %.1f", c.what, c.b.X+c.b.W, strip.X+strip.W)
		}
	}

	// And again at the width it was REPORTED at. The board was a sidebar panel
	// beside an editor when this broke, which is both the narrowest it is ever
	// asked to be and the case least likely to be looked at — the suite's own
	// viewport is 1400px, where a shrinkable meta would have hidden the bug for
	// another month.
	if err := s.page.SetViewportSize(380, 900); err != nil {
		t.Fatalf("narrowing the viewport: %v", err)
	}
	read(&strip, "#tab-asks")
	read(&text, "#tab-asks .ask .ask-text")
	read(&reply, "#tab-asks .ask .ask-reply")

	// No mid-word shredding: `overflow-wrap: anywhere` is allowed to break a
	// pasted URL, and must never be reached by ordinary prose. Two characters of
	// a 0.85rem font is about 14px, so anything under a third of the strip at
	// this width is the column again.
	if text.W < strip.W/3 {
		t.Errorf("at 380px the request is %.0fpx wide inside a %.0fpx strip", text.W, strip.W)
	}
	for _, c := range []struct {
		what string
		b    box
	}{{"the request", text}, {"the reply", reply}} {
		if c.b.X+c.b.W > strip.X+strip.W+0.5 {
			t.Errorf("at 380px %s overflows the strip: ends at %.1f, strip ends at %.1f",
				c.what, c.b.X+c.b.W, strip.X+strip.W)
		}
	}
}

func TestTheNotesStripFollowsTheActiveTab(t *testing.T) {
	mine := makeScratchTab(t, "Has a note")
	other := makeScratchTab(t, "Has none")

	d := readDoc(t)
	// The id comes from the board's own allocator, like every other object here:
	// a hand-picked one would either collide with something or teach the next
	// reader that these are free-form.
	next, ok := d["nextId"].(float64)
	if !ok {
		t.Fatalf("the board has no nextId to allocate from: %v", d["nextId"])
	}
	d["nextId"] = int(next) + 1
	d.tab(t, mine)["requests"] = []any{map[string]any{
		"id": fmt.Sprintf("ab%d", int(next)), "at": "2026-08-26T09:00:00Z",
		"by": "human", "text": "only on this tab",
	}}
	// As the human, because an agent may not create one — which is the guarantee
	// under test everywhere else and a fixture constraint here.
	applyAsHuman(t, d)

	s := open(t, "tab="+mine)
	if err := expect.Locator(s.page.Locator("#tab-asks .ask")).ToHaveCount(1); err != nil {
		t.Fatalf("the note is not on its own tab: %v", err)
	}
	s.tab(other)
	if err := expect.Locator(s.page.Locator("#tab-asks .ask")).ToHaveCount(0); err != nil {
		t.Errorf("a note from another tab is showing here: %v", err)
	}
	s.tab(mine)
	if err := expect.Locator(s.page.Locator("#tab-asks .ask")).ToHaveCount(1); err != nil {
		t.Errorf("the note did not come back with its tab: %v", err)
	}
}

/* ---------- helpers ---------- */

func requestsOn(t *testing.T, id string) []map[string]any {
	t.Helper()
	raw, _ := readDoc(t).tab(t, id)["requests"].([]any)
	out := []map[string]any{}
	for _, entry := range raw {
		if ask, ok := entry.(map[string]any); ok {
			out = append(out, ask)
		}
	}
	return out
}

func idOf(ask map[string]any) string {
	id, _ := ask["id"].(string)
	return id
}

// stamp adds a `done` to the first request on a tab, the way `aboard requests
// done` does — the server rewrites `by` to the writer whatever we claim here.
func stamp(t *testing.T, tab map[string]any, note string) {
	t.Helper()
	asks, _ := tab["requests"].([]any)
	if len(asks) == 0 {
		t.Fatal("no request to stamp")
	}
	ask, _ := asks[0].(map[string]any)
	ask["done"] = map[string]any{"by": agentActor, "at": "2026-08-26T10:00:00Z", "note": note}
}

// applyAsHuman posts a document with the human's powers. Used ONLY to seed what
// the human would have typed: an agent cannot create a request, which is the
// point, so a fixture that needed one had to be able to say it was them.
func applyAsHuman(t *testing.T, d doc) {
	t.Helper()
	postAs(t, d, "human", "")
}

// The help panel's row for the strip.
//
// Its own assertion because the panel is scrollable and the new row sits below
// the fold: a screenshot of it proves the rows above it, which is exactly how a
// Buttons section shipped broken here once, on a claim that it could not be
// checked. The panel is generated, so the check is that the row is generated.
func TestTheHelpPanelNamesTheNotesStrip(t *testing.T) {
	id := makeScratchTab(t, "Help me")
	s := open(t, "tab="+id)

	if err := s.page.Keyboard().Press("?"); err != nil {
		t.Fatalf("opening the help panel: %v", err)
	}
	if err := expect.Locator(s.page.Locator("#help-dialog[open]")).ToBeAttached(); err != nil {
		t.Fatalf("the help panel did not open: %v", err)
	}

	term := s.page.Locator("#help-dialog dt", playwright.PageLocatorOptions{
		HasText: "notes for the agent",
	})
	if err := expect.Locator(term).ToHaveCount(1); err != nil {
		t.Fatalf("the help panel does not name the notes strip: %v", err)
	}
	desc := term.Locator("xpath=following-sibling::dd[1]")
	if err := expect.Locator(desc).ToContainText("ask for something to be fixed"); err != nil {
		t.Errorf("the row is there and says nothing useful: %v", err)
	}
}
