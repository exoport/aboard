//go:build e2e

package e2e

import (
	"strings"
	"testing"

	"github.com/mxschmitt/playwright-go"
)

// The renderers whose gestures are typing, sorting and clicking rather than
// dragging. Every one of them is a "saves as you type" surface, and the shape of
// the assertion is the same throughout: do the thing a human does, then ask the
// SERVER what it holds. Reading it back out of the DOM would only prove the
// renderer agrees with itself.

/* ---------- table ---------- */

func TestSortingATableByItsHeader(t *testing.T) {
	covers(t, "table", "click a header to sort")

	s := open(t, "tab=ab111")
	view := s.view("ab111")

	header := view.Locator(`th`).Filter(playwright.LocatorFilterOptions{HasText: "number"}).First()
	if err := header.Click(); err != nil {
		t.Fatalf("clicking the header: %v", err)
	}
	if err := expect.Locator(header).ToHaveAttribute("data-sorted", "asc"); err != nil {
		t.Fatalf("the header does not report the sort: %v", err)
	}
	first := rowIDs(t, view)

	if err := header.Click(); err != nil {
		t.Fatalf("clicking the header again: %v", err)
	}
	if err := expect.Locator(header).ToHaveAttribute("data-sorted", "desc"); err != nil {
		t.Errorf("a second click did not reverse the sort: %v", err)
	}
	second := rowIDs(t, view)
	if strings.Join(first, ",") == strings.Join(second, ",") {
		t.Errorf("reversing the sort left the rows in the same order: %v", first)
	}

	// Sorting is per-viewer. It reorders what is on screen and must not rewrite
	// the document — the row order in `state.rows` belongs to whoever wrote them.
	stored := storedRowIDs(t)
	if strings.Join(stored, ",") == strings.Join(second, ",") && len(stored) > 1 {
		t.Errorf("sorting appears to have written the new order to the board: %v", stored)
	}
}

func TestTypingInATableCellSaves(t *testing.T) {
	covers(t, "table", "edits save as you type")

	s := open(t, "tab=ab111")
	view := s.view("ab111")

	cell := view.Locator(`tr[data-id="ab188"] .cell-input`).First()
	written := "text, edited by the suite"
	if err := cell.Fill(written); err != nil {
		t.Fatalf("typing in the cell: %v", err)
	}
	if err := cell.Blur(); err != nil {
		t.Fatalf("blurring: %v", err)
	}
	eventually(t, "the cell to reach the server", func() bool {
		return tableCell(t, "ab188", "thing") == written
	})
	// The cell flashes "saved" on blur, which is the only feedback a
	// saves-as-you-type surface gives — and it hangs off the promise ctx.save()
	// returns, the one that used to be stranded when a foreign write re-armed
	// the debounce.
	if err := expect.Locator(view.Locator(`tr[data-id="ab188"] .inline-flash`).First()).
		ToContainText("saved"); err != nil {
		t.Errorf("the cell never flashed saved: %v", err)
	}
}

func TestRightClickingATableRow(t *testing.T) {
	covers(t, "table", "right-click a row to copy or duplicate it")

	s := open(t, "tab=ab111")
	if err := s.view("ab111").Locator(`tr[data-id="ab189"]`).Click(playwright.LocatorClickOptions{
		Button: playwright.MouseButtonRight,
	}); err != nil {
		t.Fatalf("right-clicking the row: %v", err)
	}
	menu := s.page.Locator(".ctx-menu")
	if err := expect.Locator(menu).ToContainText("ab189"); err != nil {
		t.Errorf("the menu does not name the row: %v", err)
	}
	if err := s.page.Keyboard().Press("Escape"); err != nil {
		t.Fatal(err)
	}
}

// Two halves of one rule, and they are tested together because a fix for either
// one alone is a regression in the other: a context menu is closed by a scroll
// that happens AFTER it opened, and is NOT closed by one that was already in
// flight when it opened.
//
// The second half is what a right-click on a partly visible row does — the
// browser brings the row into view, the scroll EVENT is dispatched at the next
// rendering opportunity, and by then the menu is already open. It closed itself,
// with no JavaScript of ours in the stack; on screen it read as a menu that
// flickers, and in the suite as a right-click test that failed only in some
// orders, on a board with enough tabs for the strip to wrap and the page to
// scroll at all.
func TestAContextMenuOutlivesTheScrollThatOpenedItAndClosesOnTheNextOne(t *testing.T) {
	s := open(t, "tab=ab111")

	// A short window, so the page is taller than the viewport and can scroll at
	// all. This is not a contrivance: it is the same condition a docked board or a
	// wrapped tab strip produces, and a page that cannot scroll cannot exhibit
	// either half of the rule.
	if err := s.page.SetViewportSize(900, 420); err != nil {
		t.Fatal(err)
	}

	if scrollable, err := s.page.Evaluate(`() => document.documentElement.scrollHeight > innerHeight`); err != nil {
		t.Fatal(err)
	} else if scrollable != true {
		t.Fatal("the page does not scroll, so this test asserts nothing")
	}

	// Scroll and right-click in ONE task, which is the hazard exactly: the scroll
	// EVENT for that scroll is not dispatched until the next rendering
	// opportunity, so it arrives after the handler has already opened the menu.
	// Driving it from the page rather than from the driver is deliberate — the
	// driver scrolls, then waits for the element to be stable, which drains the
	// event before it clicks and hides the very race being pinned. The
	// `contextmenu` event is a real one on the real row; only its timing is ours.
	if _, err := s.page.Evaluate(`() => {
		const row = document.querySelector('[data-tab="ab111"][data-active="yes"] tr[data-id="ab189"]');
		if (!row) throw new Error('no row to right-click');
		window.scrollTo(0, document.documentElement.scrollHeight);
		const at = row.getBoundingClientRect();
		row.dispatchEvent(new MouseEvent('contextmenu', {
			bubbles: true, cancelable: true, button: 2,
			clientX: at.left + 8, clientY: at.top + 8,
		}));
	}`); err != nil {
		t.Fatal(err)
	}
	menu := s.page.Locator(".ctx-menu")
	if err := expect.Locator(menu).ToContainText("ab189"); err != nil {
		t.Fatalf("the menu did not survive the scroll that was already in flight when it opened: %v", err)
	}
	// Still there after a frame has actually passed — an assertion that looked
	// once would pass against a menu that is about to close itself.
	if _, err := s.page.Evaluate(`() => new Promise((go) => requestAnimationFrame(() => requestAnimationFrame(go)))`); err != nil {
		t.Fatal(err)
	}
	if err := expect.Locator(menu).ToBeVisible(); err != nil {
		t.Fatalf("the menu closed itself a frame after opening: %v", err)
	}

	// The behaviour the delay must not have removed. A menu is anchored to a point
	// in the viewport, so a page that scrolls under it leaves it pointing at
	// nothing.
	moved, err := s.page.Evaluate(`() => { const was = window.scrollY; window.scrollTo(0, was > 100 ? 0 : document.documentElement.scrollHeight); return window.scrollY !== was; }`)
	if err != nil {
		t.Fatal(err)
	}
	if moved != true {
		t.Fatal("the page did not actually scroll, so this half asserts nothing")
	}
	if err := expect.Locator(menu).ToBeHidden(); err != nil {
		t.Errorf("scrolling the page must close the menu: %v", err)
	}
}

// The delete-row button — the control that shipped documented nowhere while the
// skill advertised the feature, which is the incident the whole declared-controls
// series exists to prevent.
func TestDeletingATableRow(t *testing.T) {
	s := open(t, "tab=ab111")
	view := s.view("ab111")

	before := len(storedRowIDs(t))
	if err := s.control("ab111", "add").Click(); err != nil {
		t.Fatalf("adding a row: %v", err)
	}
	eventually(t, "the new row to reach the server", func() bool { return len(storedRowIDs(t)) == before+1 })

	added := storedRowIDs(t)[before]
	if err := view.Locator(`tr[data-id="` + added + `"] [data-gesture="delete-row"]`).Click(); err != nil {
		t.Fatalf("deleting the row: %v", err)
	}
	eventually(t, "the row to be deleted", func() bool { return len(storedRowIDs(t)) == before })
}

func rowIDs(t *testing.T, view playwright.Locator) []string {
	t.Helper()
	handles, err := view.Locator("tbody tr[data-id]").All()
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(handles))
	for _, h := range handles {
		id, err := h.GetAttribute("data-id")
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	return out
}

func storedRowIDs(t *testing.T) []string {
	t.Helper()
	rows, _ := readDoc(t).state(t, "ab111")["rows"].([]any)
	out := make([]string, 0, len(rows))
	for _, raw := range rows {
		if r, ok := raw.(map[string]any); ok {
			id, _ := r["id"].(string)
			out = append(out, id)
		}
	}
	return out
}

func tableCell(t *testing.T, rowID, col string) string {
	t.Helper()
	rows, _ := readDoc(t).state(t, "ab111")["rows"].([]any)
	for _, raw := range rows {
		if r, ok := raw.(map[string]any); ok && r["id"] == rowID {
			v, _ := r[col].(string)
			return v
		}
	}
	return ""
}

/* ---------- gate ---------- */

// Allow with a reason, then undo it. Nothing executes: the verdict is a RECORD
// the asking session reads, which is what makes a stray click on an
// unauthenticated server harmless.
func TestAllowingAGateRequestWithAReasonAndUndoingIt(t *testing.T) {
	covers(t, "gate", "a reason is optional but it is what the agent learns from")
	covers(t, "gate", "nothing here executes — the agent that asked reads your verdict")

	s := open(t, "tab=ab128")
	view := s.view("ab128")

	ask := view.Locator(`.ask[data-id="ab201"]`)
	if err := expect.Locator(ask).ToBeVisible(); err != nil {
		t.Fatalf("the pending request is not on the tab: %v", err)
	}

	const why = "The suite needs a verdict to test; nothing runs either way."
	if err := ask.Locator(".ask-reason").Fill(why); err != nil {
		t.Fatalf("typing the reason: %v", err)
	}
	if err := ask.Locator(`[data-gesture="allow"]`).Click(); err != nil {
		t.Fatalf("pressing Allow: %v", err)
	}

	eventually(t, "the verdict to reach the server", func() bool {
		return gateVerdict(t, "ab201") == "allow"
	})
	if got := gateReason(t, "ab201"); got != why {
		t.Errorf("the reason did not travel with the verdict: %q", got)
	}
	if err := expect.Locator(view.Locator(".decided")).ToContainText(why); err != nil {
		t.Errorf("the decided row does not show the reason: %v", err)
	}

	// Undo does NOT pretend the decision never happened: the asking agent may
	// already have acted on it, so the entry stays in the record marked `undone`
	// and the request goes back to the top of the queue.
	row := view.Locator(`.verdict-row`).First()
	if err := row.Locator(`[data-gesture="undo"]`).Click(); err != nil {
		t.Fatalf("pressing undo: %v", err)
	}
	eventually(t, "the request to return to the queue", func() bool { return gatePending(t, "ab201") })

	entry := gateDecidedEntry(t, "ab201")
	if entry["undone"] != true {
		t.Errorf("the reversed verdict was deleted rather than marked undone: %v", entry)
	}
	if entry["undoneAt"] == nil {
		t.Error("the reversal is not stamped with when it happened")
	}
}

func gateDecidedEntry(t *testing.T, id string) map[string]any {
	t.Helper()
	decided, _ := readDoc(t).state(t, "ab128")["decided"].([]any)
	for _, raw := range decided {
		if e, ok := raw.(map[string]any); ok && e["id"] == id {
			return e
		}
	}
	return map[string]any{}
}

func gateVerdict(t *testing.T, id string) string {
	t.Helper()
	v, _ := gateDecidedEntry(t, id)["verdict"].(string)
	return v
}

func gateReason(t *testing.T, id string) string {
	t.Helper()
	v, _ := gateDecidedEntry(t, id)["reason"].(string)
	return v
}

func gatePending(t *testing.T, id string) bool {
	t.Helper()
	pending, _ := readDoc(t).state(t, "ab128")["pending"].([]any)
	for _, raw := range pending {
		if p, ok := raw.(map[string]any); ok && p["id"] == id {
			return true
		}
	}
	return false
}

/* ---------- ui ---------- */

// A `ui` button RECORDS an intent and a `ui` field writes into the data model.
// Neither does anything else, and that is the posture the whole component
// catalog is built on — no iframe, no script, nothing in the browser executes.
func TestAUiButtonRecordsAnIntentAndAFieldWritesData(t *testing.T) {
	covers(t, "ui", "whatever the agent described — buttons record an intent, fields write into the data model")

	s := open(t, "tab=ab133")
	view := s.view("ab133")

	// The buttons and fields live in the gallery's "Input" panel, which is not
	// the one that opens by default — so the test has to get there the way a
	// human does.
	openGalleryPanel(t, s, "Input")

	before := len(intents(t, "ab133"))
	btn := view.Locator(".uic-panel .action-btn").First()
	label, err := btn.TextContent()
	if err != nil {
		t.Fatalf("reading the button label: %v", err)
	}
	if err := btn.Click(); err != nil {
		t.Fatalf("pressing the button: %v", err)
	}
	eventually(t, "the intent to be recorded", func() bool { return len(intents(t, "ab133")) == before+1 })

	recorded, _ := intents(t, "ab133")[before].(map[string]any)
	if recorded["by"] != "human" {
		t.Errorf("the intent was recorded by %v", recorded["by"])
	}
	// The label is the AGENT's, from the component tree — that is why a ui button
	// is a plain button() and not a declared control.
	if !strings.Contains(label, "✓") && strings.TrimSpace(label) == "" {
		t.Errorf("the button rendered with no label at all")
	}

	// A field writes into state.data at the path it is bound to.
	field := view.Locator(`.uic-panel .uic-field`).Filter(playwright.LocatorFilterOptions{
		HasText: "text",
	}).Locator("input").First()
	written := "written by the browser suite"
	if err := field.Fill(written); err != nil {
		t.Fatalf("typing in the field: %v", err)
	}
	if err := field.Blur(); err != nil {
		t.Fatalf("blurring: %v", err)
	}
	eventually(t, "the field to reach state.data", func() bool {
		return dig(readDoc(t).state(t, "ab133"), "data", "demo", "text") == written
	})
}

// A `tabs` component remembers which panel you had open — per VIEWER, in
// localStorage, never in the document. Per-viewer UI state in the state file is
// the one thing this project has been consistent about refusing.
func TestAUiTabsComponentRemembersItsPanelPerViewer(t *testing.T) {
	covers(t, "ui", "a tabs component remembers which panel you had open, per viewer")

	s := open(t, "tab=ab133")

	revBefore := readDoc(t)["rev"]
	openGalleryPanel(t, s, "Data")
	if got := readDoc(t)["rev"]; got != revBefore {
		t.Errorf("switching a ui panel wrote to the board (rev %v -> %v)", revBefore, got)
	}

	// It survives a reload of the same viewer, which is what "remembers" means:
	// the choice is in this browser's localStorage, not in the document, so
	// another viewer of the same board keeps whichever panel they were on.
	if _, err := s.page.Reload(); err != nil {
		t.Fatalf("reloading: %v", err)
	}
	s.tab("ab133")
	if err := expect.Locator(s.view("ab133").Locator(`.uic-tabs button[aria-selected="true"]`)).
		ToContainText("Data"); err != nil {
		t.Errorf("the chosen panel was not remembered across a reload: %v", err)
	}
}

// openGalleryPanel switches the UI gallery's tabs component to the named panel.
// A helper rather than a line in each test because "the component I want is
// behind a panel nobody opened" is the commonest way a `ui` assertion fails, and
// it fails as "no such element" rather than as "wrong panel".
func openGalleryPanel(t *testing.T, s *session, label string) {
	t.Helper()
	btn := s.view("ab133").Locator(".uic-tabs button").Filter(playwright.LocatorFilterOptions{
		HasText: label,
	}).First()
	if err := btn.Click(); err != nil {
		t.Fatalf("opening the %q panel: %v", label, err)
	}
	if err := expect.Locator(btn).ToHaveAttribute("aria-selected", "true"); err != nil {
		t.Fatalf("the %q panel did not open: %v", label, err)
	}
}

/* ---------- vote ---------- */

// Scores are recorded per actor, and a wide split is CALLED OUT rather than
// averaged away — the whole reason this renderer exists instead of a number.
func TestScoringAVoteAndSeeingTheSplit(t *testing.T) {
	covers(t, "vote", "a wide split is called out rather than averaged away")

	s := open(t, "tab=ab132")
	view := s.view("ab132")

	row := view.Locator("tbody tr").First()
	pips, err := row.Locator(`[data-gesture="score"]`).All()
	if err != nil || len(pips) == 0 {
		t.Fatalf("the vote offers no scores to give: %v", err)
	}
	if err := pips[0].Click(); err != nil {
		t.Fatalf("scoring 1: %v", err)
	}
	eventually(t, "the score to reach the server", func() bool {
		return dig(readDoc(t).state(t, "ab132"), "ballots", "human", "opt-visual") != nil
	})

	// A second actor disagrees sharply. Through the HTTP API, because that is how
	// the other participant in a real vote arrives.
	d := readDoc(t)
	ballots, _ := d.state(t, "ab132")["ballots"].(map[string]any)
	if ballots == nil {
		ballots = map[string]any{}
		d.state(t, "ab132")["ballots"] = ballots
	}
	ballots["agent-e2e"] = map[string]any{"opt-visual": 5}
	apply(t, d)

	if err := expect.Locator(view.Locator(".spread").First()).ToContainText("split by"); err != nil {
		t.Errorf("a five-point disagreement was averaged away instead of called out: %v", err)
	}
}

/* ---------- chat ---------- */

func TestSendingAChatMessageWithEnter(t *testing.T) {
	covers(t, "chat", "Enter sends, Shift/Alt/Ctrl+Enter for a newline")

	s := open(t, "tab=ab26")
	view := s.view("ab26")

	composer := view.Locator(".composer-input")
	// Shift+Enter must NOT send: it is the newline, and a composer that sends on
	// it makes every multi-line message three messages.
	if err := composer.Fill("first line"); err != nil {
		t.Fatalf("typing: %v", err)
	}
	before := len(chatMessages(t))
	if err := composer.Press("Shift+Enter"); err != nil {
		t.Fatalf("pressing Shift+Enter: %v", err)
	}
	if got := len(chatMessages(t)); got != before {
		t.Fatalf("Shift+Enter sent the message")
	}
	if err := composer.PressSequentially("second line"); err != nil {
		t.Fatalf("typing the second line: %v", err)
	}
	if err := composer.Press("Enter"); err != nil {
		t.Fatalf("pressing Enter: %v", err)
	}

	eventually(t, "the message to reach the server", func() bool { return len(chatMessages(t)) == before+1 })
	sent, _ := chatMessages(t)[before].(map[string]any)
	text, _ := sent["text"].(string)
	if !strings.Contains(text, "first line") || !strings.Contains(text, "second line") {
		t.Errorf("the newline did not survive: %q", text)
	}
	if sent["by"] != "human" {
		t.Errorf("the message was sent as %v", sent["by"])
	}
	if err := expect.Locator(view.Locator(".messages .msg-human").Last()).ToContainText("second line"); err != nil {
		t.Errorf("the message is not in the transcript: %v", err)
	}
}

func chatMessages(t *testing.T) []any {
	t.Helper()
	list, _ := readDoc(t).state(t, "ab26")["messages"].([]any)
	return list
}

/* ---------- notes ---------- */

func TestTypingInNotesSavesAndTabIndents(t *testing.T) {
	covers(t, "notes", "type freely")
	covers(t, "notes", "Tab indents (Escape then Tab to leave)")

	s := open(t, "tab=ab202")
	view := s.view("ab202")

	area := view.Locator(".notes-area")
	if err := area.Fill("a line"); err != nil {
		t.Fatalf("typing: %v", err)
	}
	eventually(t, "the note to reach the server", func() bool {
		text, _ := readDoc(t).state(t, "ab202")["text"].(string)
		return text == "a line"
	})

	// Tab indents rather than moving focus: this is a writing surface, not a
	// form. Two spaces, at the caret.
	if err := area.Press("Tab"); err != nil {
		t.Fatalf("pressing Tab: %v", err)
	}
	eventually(t, "the indent to reach the server", func() bool {
		text, _ := readDoc(t).state(t, "ab202")["text"].(string)
		return text == "a line  "
	})
	if err := expect.Locator(area).ToBeFocused(); err != nil {
		t.Errorf("Tab moved focus out of the note: %v", err)
	}

	// Escape arms one Tab to leave normally, so a keyboard user is never trapped.
	if err := area.Press("Escape"); err != nil {
		t.Fatalf("pressing Escape: %v", err)
	}
	if err := area.Press("Tab"); err != nil {
		t.Fatalf("pressing Tab after Escape: %v", err)
	}
	if err := expect.Locator(area).Not().ToBeFocused(); err != nil {
		t.Errorf("Escape then Tab did not release focus: %v", err)
	}
}

/* ---------- form ---------- */

func TestAnsweringAFormFieldSaves(t *testing.T) {
	covers(t, "form", "answer and it saves")

	s := open(t, "tab=ab15")
	view := s.view("ab15")

	box := view.Locator(`input[type="checkbox"]`).First()
	before := formFieldValue(t, "keep")
	if err := box.Click(); err != nil {
		t.Fatalf("ticking the field: %v", err)
	}
	eventually(t, "the answer to reach the server", func() bool {
		return formFieldValue(t, "keep") != before
	})
	if err := expect.Locator(view.Locator(".save-note")).ToContainText("saved"); err != nil {
		t.Errorf("the form gave no feedback that it saved: %v", err)
	}
}

// The form's Reset is the third gesture the panel lost: it asked through
// window.confirm, which a webview suppresses, so the button did nothing at all
// and said nothing about why.
//
// Its own scratch tab rather than ab15, because resetting the fixture's form
// would reach into whatever TestAnsweringAFormFieldSaves is doing with it, and
// the suite is run shuffled.
func TestResettingAFormAsksInThePageAndCancelKeepsTheAnswers(t *testing.T) {
	id := makeScratchTabOfType(t, "Reset me", "form", map[string]any{
		"title": "Scratch questions",
		"fields": []any{
			map[string]any{"id": "keep", "type": "checkbox", "label": "Keep it", "value": true},
			map[string]any{"id": "polish", "type": "range", "label": "Polish", "min": 2, "max": 10, "value": 7},
			map[string]any{"id": "why", "type": "text", "label": "Why", "value": "because"},
		},
	})
	field := func(fieldID string) any {
		fields, _ := readDoc(t).state(t, id)["fields"].([]any)
		for _, raw := range fields {
			if f, ok := raw.(map[string]any); ok && f["id"] == fieldID {
				return f["value"]
			}
		}
		return nil
	}

	s := open(t, "tab="+id)
	reset := s.control(id, "reset")

	// Cancel first: a reset that clears the answers whichever button you press
	// would satisfy every assertion after this one.
	if err := reset.Click(); err != nil {
		t.Fatalf("pressing Reset answers: %v", err)
	}
	cancelled := s.boardDialog()
	if err := expect.Locator(cancelled).ToContainText("neutral defaults"); err != nil {
		t.Errorf("the reset confirmation does not say what it is about to do: %v", err)
	}
	answer(t, cancelled, "Keep them")
	if err := expect.Locator(s.view(id).Locator(`input[type="checkbox"]`)).ToBeChecked(); err != nil {
		t.Errorf("cancelling the reset cleared the form anyway: %v", err)
	}
	if got := field("keep"); got != true {
		t.Errorf("cancelling the reset wrote to the server: keep = %v", got)
	}

	if err := reset.Click(); err != nil {
		t.Fatalf("pressing Reset answers again: %v", err)
	}
	answer(t, s.boardDialog(), "Reset answers")

	eventually(t, "the neutral values to reach the server", func() bool {
		return field("keep") == false
	})
	// Neutral is per TYPE, not "empty": a range goes to its own minimum, which is
	// 2 here and not 0, and a text field goes to "".
	if got := field("polish"); got != float64(2) {
		t.Errorf("the range reset to %v, want its minimum (2)", got)
	}
	if got := field("why"); got != "" {
		t.Errorf("the text field reset to %q, want empty", got)
	}
}

func formFieldValue(t *testing.T, fieldID string) any {
	t.Helper()
	fields, _ := readDoc(t).state(t, "ab15")["fields"].([]any)
	for _, raw := range fields {
		if f, ok := raw.(map[string]any); ok && f["id"] == fieldID {
			return f["value"]
		}
	}
	return nil
}

/* ---------- diagram ---------- */

func TestTypingMermaidSourceSavesAndRerenders(t *testing.T) {
	covers(t, "diagram", "type in the source editor — it saves as you type and re-renders after a short pause")
	covers(t, "diagram", "hover a node for its mermaid key, which is what you need in order to edit the source")

	s := open(t, "tab=ab14")
	view := s.view("ab14")

	// The mermaid bundle is committed at pkg/aboard/web/lib/, so this renders
	// with no network at all — which is the reason it is vendored.
	if err := expect.Locator(view.Locator(`[data-role="render"] svg`)).ToBeVisible(); err != nil {
		t.Fatalf("the diagram did not render: %v", err)
	}
	// Hovering a node gives its mermaid key, which is what you need to edit the
	// source at all — an svg node with no name is unreachable from the text.
	title := view.Locator(`[data-role="render"] svg .node title, [data-role="render"] svg g[id] title`).First()
	if n, err := title.Count(); err == nil && n == 0 {
		t.Log("no <title> in the rendered svg — hovering a node names nothing")
	}

	source := view.Locator(`[data-role="source"]`)
	next := "graph TD\n    A[\"Written by the suite\"] --> B[\"And rendered\"]"
	if err := source.Fill(next); err != nil {
		t.Fatalf("typing the source: %v", err)
	}
	eventually(t, "the source to reach the server", func() bool {
		got, _ := readDoc(t).state(t, "ab14")["source"].(string)
		return got == next
	})
	if err := expect.Locator(view.Locator(`[data-role="status"]`)).ToContainText("rendered"); err != nil {
		t.Errorf("the diagram did not re-render after the edit: %v", err)
	}
	if err := expect.Locator(view.Locator(`[data-role="render"]`)).ToContainText("Written by the suite"); err != nil {
		t.Errorf("the rendered diagram is not the edited source: %v", err)
	}
}

/* ---------- log ---------- */

func TestFilteringAndUnfollowingALog(t *testing.T) {
	covers(t, "log", "type in the filter to narrow")
	covers(t, "log", "scroll up to stop following, scroll to the bottom to resume")

	s := open(t, "tab=ab126")
	view := s.view("ab126")

	if err := expect.Locator(view.Locator(".log-line").First()).ToBeVisible(); err != nil {
		t.Fatalf("the log tab shows no lines — the sidecar file is seeded in TestMain: %v", err)
	}

	if err := view.Locator(".log-filter").Fill("error"); err != nil {
		t.Fatalf("typing in the filter: %v", err)
	}
	if err := expect.Locator(view.Locator(".log-meta")).ToContainText(" of "); err != nil {
		t.Errorf("the filter does not say how much it hid: %v", err)
	}
	hidden, err := view.Locator(`.log-line[data-match="no"]`).Count()
	if err != nil {
		t.Fatal(err)
	}
	if hidden == 0 {
		t.Error("the filter matched everything, so it narrowed nothing")
	}
	if err := view.Locator(".log-filter").Fill(""); err != nil {
		t.Fatal(err)
	}

	// Scrolling up stops following, so a log that is still being written does not
	// yank the line you are reading off the screen.
	follow := view.Locator(`[data-gesture="follow"]`)
	if err := expect.Locator(follow).ToHaveAttribute("aria-pressed", "true"); err != nil {
		t.Fatalf("the log did not start following: %v", err)
	}
	if _, err := view.Locator(".log-wrap").Evaluate(
		`(el) => { el.scrollTop = 0; el.dispatchEvent(new Event('scroll')); }`, nil,
	); err != nil {
		t.Fatalf("scrolling up: %v", err)
	}
	if err := expect.Locator(follow).ToHaveAttribute("aria-pressed", "false"); err != nil {
		t.Errorf("scrolling up did not stop the log following: %v", err)
	}
}

/* ---------- trace ---------- */

// The trace tab reads the journal, so by the time the suite gets here there is
// real history to draw: every test above it wrote to this board.
func TestTheTraceTabDrawsTheJournalAndFiltersByActor(t *testing.T) {
	covers(t, "trace", "")

	s := open(t, "tab=ab127")
	view := s.view("ab127")

	if err := s.control("ab127", "reload").Click(); err != nil {
		t.Fatalf("reloading the trace: %v", err)
	}
	if err := expect.Locator(view.Locator(".lane").First()).ToBeVisible(); err != nil {
		t.Fatalf("the trace drew no lanes: %v", err)
	}

	chip := view.Locator(`[data-gesture="actor"]`).First()
	name, err := chip.TextContent()
	if err != nil {
		t.Fatal(err)
	}
	if err := expect.Locator(chip).ToHaveAttribute("aria-pressed", "true"); err != nil {
		t.Errorf("an actor chip does not start shown: %v", err)
	}
	if err := chip.Click(); err != nil {
		t.Fatalf("hiding an actor: %v", err)
	}
	if err := expect.Locator(view.Locator(`[data-gesture="actor"]`).First()).
		ToHaveAttribute("aria-pressed", "false"); err != nil {
		t.Errorf("clicking the %q chip did not hide that actor: %v", strings.TrimSpace(name), err)
	}
}
