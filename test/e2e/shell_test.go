//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// The shell: the tab strip, the per-tab notices, the native dialogs and the
// notify button. None of it belongs to a renderer, and none of it was reachable
// by the old suite, which could dump a DOM but never click one.

// Guarantee 2 — an agent cannot clear a `touched` marker; only the human's
// dismiss does — is enforced on the server and covered by Go tests. What no Go
// test can see is whether the BROWSER actually claims to be the human when it
// dismisses: `save()` posts `__by: 'human'`, and if that ever became `agent` or
// went missing the server would refuse the clear, the dot would come back on the
// next repaint, and the only symptom would be a notice that will not go away.
func TestDismissingANoticeWritesAsTheHuman(t *testing.T) {
	// An agent write, which is what stamps the marker in the first place.
	d := readDoc(t)
	d.state(t, "ab202")["text"] = "Touched by an agent at " + strconv.Itoa(len(d))
	apply(t, d)

	s := open(t, "tab=ab202")
	view := s.view("ab202")

	banner := view.Locator(".banner").Filter(playwright.LocatorFilterOptions{HasText: "agent-e2e changed this tab."})
	if err := expect.Locator(banner).ToBeVisible(); err != nil {
		t.Fatalf("no touched notice after an agent write: %v", err)
	}
	if err := expect.Locator(s.page.Locator(`#tabs .tab[data-id="ab202"] .tab-dot`)).ToBeVisible(); err != nil {
		t.Errorf("the tab strip shows no unread dot: %v", err)
	}

	if err := banner.GetByText("Dismiss").Click(); err != nil {
		t.Fatalf("clicking Dismiss: %v", err)
	}

	eventually(t, "the marker to clear on the server", func() bool {
		tab := readDoc(t).tab(t, "ab202")
		_, still := tab["touched"]
		return !still
	})
	// Dismissing records WHEN you looked, not only that you did: `seen.human` is
	// the timestamp another session compares against to tell "changed since the
	// human last read it" from "unread".
	if got := dig(readDoc(t).tab(t, "ab202"), "seen", "human"); got == nil {
		t.Error("dismissing left no seen.human stamp")
	}
	// `lastEditedBy` is the evidence, and NOT the journal — which is worth
	// writing down, because the journal was the obvious place to look and it is
	// empty here on purpose. changeSummary compares state, name, type,
	// stateFrom, note and pendingRemoval; `touched` and `seen` are in none of
	// them, so dismissing a notice is deliberately not a journal line. The
	// server stamps lastEditedBy from the same `__by` it enforces the guarantees
	// with, so it answers the actual question: who did the browser claim to be.
	if by := readDoc(t)["lastEditedBy"]; by != "human" {
		t.Errorf("the dismiss was written by %q, want \"human\" — the server refuses an agent's clear, so the notice would come back", by)
	}
	if err := expect.Locator(s.page.Locator(`#tabs .tab[data-id="ab202"] .tab-dot`)).ToBeHidden(); err != nil {
		t.Errorf("the unread dot survived the dismiss: %v", err)
	}
}

// A dropped tab comes back as a REQUEST. Guarantee 1: an agent cannot delete a
// tab, so the server restores it with a `pendingRemoval` the human answers —
// Keep, or Remove behind a confirm().
func TestAPendingRemovalCanBeKept(t *testing.T) {
	id := makeScratchTab(t, "Keep me")
	requestRemoval(t, id)

	s := open(t, "tab="+id)
	banner := s.view(id).Locator(".banner--removal")
	if err := expect.Locator(banner).ToContainText("asks to remove this tab"); err != nil {
		t.Fatalf("no removal request on the tab: %v", err)
	}
	// The reason is the server's, not the agent's: a dropped tab is restored with
	// "removal requested by an agent write", because the agent that dropped it
	// said nothing — it thought it was deleting a tab.
	if err := expect.Locator(banner).ToContainText("removal requested by an agent write"); err != nil {
		t.Errorf("the notice does not say why the tab is back: %v", err)
	}

	if err := banner.GetByText("Keep", playwright.LocatorGetByTextOptions{Exact: new(true)}).Click(); err != nil {
		t.Fatalf("clicking Keep: %v", err)
	}
	eventually(t, "the request to be answered", func() bool {
		tab := readDoc(t).tab(t, id)
		_, still := tab["pendingRemoval"]
		return !still
	})
	if err := expect.Locator(s.page.Locator(`#tabs .tab[data-id="` + id + `"]`)).ToBeVisible(); err != nil {
		t.Errorf("Keep removed the tab: %v", err)
	}
}

// The other answer, and the only irreversible gesture in the shell — so it is
// the one that asks first. It used to ask through window.confirm, which a VS
// Code webview SUPPRESSES: the call returned false, removeTab took its cancel
// path, and the human's report was "I clicked it but nothing happens". The
// question is drawn in the page now, and the session's own gate (see openReady)
// fails this test if a native dialog is raised at all.
//
// The message is asserted as well as answered: a confirmation nobody can read
// before an irreversible action is not a confirmation.
func TestAPendingRemovalIsRemovedThroughTheBoardsOwnConfirm(t *testing.T) {
	id := makeScratchTab(t, "Remove me")
	requestRemoval(t, id)

	s := open(t, "tab="+id)
	banner := s.view(id).Locator(".banner--removal")
	if err := expect.Locator(banner).ToBeVisible(); err != nil {
		t.Fatalf("no removal request on the tab: %v", err)
	}

	// Cancel first, because a dialog that removes the tab whichever button you
	// press is the failure mode that would otherwise pass every assertion below.
	if err := banner.GetByText("Remove tab").Click(); err != nil {
		t.Fatalf("clicking Remove tab: %v", err)
	}
	answer(t, s.boardDialog(), "Keep it")
	if err := expect.Locator(s.page.Locator(`#tabs .tab[data-id="` + id + `"]`)).ToBeVisible(); err != nil {
		t.Errorf("cancelling the confirmation removed the tab anyway: %v", err)
	}

	if err := banner.GetByText("Remove tab").Click(); err != nil {
		t.Fatalf("clicking Remove tab again: %v", err)
	}
	dialog := s.boardDialog()
	if err := expect.Locator(dialog).ToContainText("Remove me"); err != nil {
		t.Errorf("the confirmation does not name the tab it is about to delete: %v", err)
	}
	if err := expect.Locator(dialog).ToContainText("content is deleted"); err != nil {
		t.Errorf("the confirmation does not say what is about to be lost: %v", err)
	}

	// The modal owns the keyboard while it is up. window.confirm took the
	// keyboard away from the page entirely; a <dialog> does not, and showModal()
	// only makes the rest of the document inert for pointers and focus — it says
	// nothing about a listener bound to `document`, which is where every one of
	// the shell's hotkeys lives. So without dialog.js stopping propagation, `]`
	// walks to another tab BEHIND an unanswered question and `?` stacks the help
	// panel on top of the modal.
	if err := s.page.Keyboard().Press("]"); err != nil {
		t.Fatalf("pressing ] over the dialog: %v", err)
	}
	if err := s.page.Keyboard().Press("?"); err != nil {
		t.Fatalf("pressing ? over the dialog: %v", err)
	}
	if err := expect.Locator(s.page.Locator("#help-dialog[open]")).ToHaveCount(0); err != nil {
		t.Errorf("? opened the help panel on top of an unanswered question: %v", err)
	}
	if err := expect.Locator(s.view(id)).ToHaveCount(1); err != nil {
		t.Errorf("] switched the tab behind an unanswered question: %v", err)
	}

	answer(t, dialog, "Remove tab")

	eventually(t, "the tab to leave the board", func() bool {
		list, _ := readDoc(t)["tabs"].([]any)
		for _, raw := range list {
			if tab, ok := raw.(map[string]any); ok && tab["id"] == id {
				return false
			}
		}
		return true
	})
	if err := expect.Locator(s.page.Locator(`#tabs .tab[data-id="` + id + `"]`)).ToHaveCount(0); err != nil {
		t.Errorf("the tab is still in the strip: %v", err)
	}
}

// Double-click a tab to rename it — the one prompt() the shell used to call, and
// the second gesture the panel silently lost (window.prompt returns null in a
// webview, which renameTab correctly reads as "cancelled").
func TestATabIsRenamedThroughTheBoardsOwnPrompt(t *testing.T) {
	id := makeScratchTab(t, "Before")

	s := open(t, "tab="+id)
	strip := s.page.Locator(`#tabs .tab[data-id="` + id + `"]`)

	if err := strip.Dblclick(); err != nil {
		t.Fatalf("double-clicking the tab: %v", err)
	}
	dialog := s.boardDialog()
	// The current name is IN the box and selected, which is what window.prompt's
	// second argument did — typing replaces it rather than appending to it.
	if err := expect.Locator(dialog.Locator("input")).ToHaveValue("Before"); err != nil {
		t.Errorf("the rename box does not start from the current name: %v", err)
	}
	if err := dialog.Locator("input").Fill("After"); err != nil {
		t.Fatalf("typing the new name: %v", err)
	}
	answer(t, dialog, "Rename")

	eventually(t, "the new name to reach the server", func() bool {
		return readDoc(t).tab(t, id)["name"] == "After"
	})
	if err := expect.Locator(strip).ToContainText("After"); err != nil {
		t.Errorf("the strip still shows the old name: %v", err)
	}

	// Cancelling must change nothing. askPrompt keeps window.prompt's contract —
	// null for cancelled, a string for answered, the empty string included — and
	// a cancel read as "" would rename the tab to nothing and the strip would say
	// "(unnamed)".
	if err := strip.Dblclick(); err != nil {
		t.Fatalf("double-clicking the tab again: %v", err)
	}
	cancelled := s.boardDialog()
	if err := cancelled.Locator("input").Fill("Never saved"); err != nil {
		t.Fatalf("typing into the second dialog: %v", err)
	}
	answer(t, cancelled, "Cancel")
	if got := readDoc(t).tab(t, id)["name"]; got != "After" {
		t.Errorf("cancelling the rename changed the name to %q", got)
	}

	// Enter, which is the other way in. window.prompt answered on Enter and the
	// replacement has to as well, or the gesture is only half restored — and this
	// is the path with no <form> under it, so nothing gives it for free.
	if err := strip.Dblclick(); err != nil {
		t.Fatalf("double-clicking the tab a third time: %v", err)
	}
	entered := s.boardDialog()
	if err := entered.Locator("input").Fill("Entered"); err != nil {
		t.Fatalf("typing into the third dialog: %v", err)
	}
	if err := s.page.Keyboard().Press("Enter"); err != nil {
		t.Fatalf("pressing Enter: %v", err)
	}
	if err := expect.Locator(entered).ToBeHidden(); err != nil {
		t.Errorf("Enter left the dialog open: %v", err)
	}
	eventually(t, "the Enter-confirmed name to reach the server", func() bool {
		return readDoc(t).tab(t, id)["name"] == "Entered"
	})

	// And Escape, which is the other exit and the one a modal without it becomes
	// a trap for.
	if err := strip.Dblclick(); err != nil {
		t.Fatalf("double-clicking the tab a fourth time: %v", err)
	}
	escaped := s.boardDialog()
	if err := escaped.Locator("input").Fill("Escaped away"); err != nil {
		t.Fatalf("typing into the third dialog: %v", err)
	}
	if err := s.page.Keyboard().Press("Escape"); err != nil {
		t.Fatalf("pressing Escape: %v", err)
	}
	if err := expect.Locator(escaped).ToBeHidden(); err != nil {
		t.Errorf("Escape left the dialog open — a modal you cannot dismiss is a trap: %v", err)
	}
	if got := readDoc(t).tab(t, id)["name"]; got != "Entered" {
		t.Errorf("escaping the rename changed the name to %q", got)
	}
	// The keyboard goes back where it was, so the next key is not swallowed by a
	// detached element.
	if !s.evalBool(`() => document.activeElement !== document.body`) {
		t.Error("closing the dialog left the focus on <body> — it should return to what opened it")
	}
}

// The New tab dialog, and the check it is the live half of: a tab made in the
// browser must start with the state its spec declares.
//
// `markup`'s TYPES.init returned { image, caption, regions, strokes } — four keys
// markup.js has never read — while its spec declared { layout, images } and the
// generated reference told every agent that was the starting shape. The renderer
// repaired it on mount, which is exactly why it survived. TestTypesInitMatchesTheSpecs
// compares all fifteen; this one proves the registry is what the dialog actually
// uses.
func TestANewTabStartsWithItsDeclaredState(t *testing.T) {
	s := open(t, "")

	if err := s.page.Locator("#add-tab").Click(); err != nil {
		t.Fatalf("opening the new-tab dialog: %v", err)
	}
	if err := s.page.Locator("#new-tab-name").Fill("Made in the browser"); err != nil {
		t.Fatalf("naming the tab: %v", err)
	}
	if _, err := s.page.Locator("#new-tab-type").SelectOption(playwright.SelectOptionValues{
		Values: &[]string{"markup"},
	}); err != nil {
		t.Fatalf("choosing the type: %v", err)
	}
	if err := s.page.Locator("#new-tab-note").Fill("Created by the browser suite."); err != nil {
		t.Fatalf("filling the note: %v", err)
	}
	if err := s.page.Locator(`#new-tab-form button[type="submit"]`).Click(); err != nil {
		t.Fatalf("submitting: %v", err)
	}

	var created map[string]any
	eventually(t, "the new tab to reach the server", func() bool {
		list, _ := readDoc(t)["tabs"].([]any)
		for _, raw := range list {
			tab, ok := raw.(map[string]any)
			if ok && tab["name"] == "Made in the browser" {
				created = tab
				return true
			}
		}
		return false
	})

	if created["type"] != "markup" {
		t.Fatalf("the dialog made a %v tab", created["type"])
	}
	state, _ := created["state"].(map[string]any)
	if _, ok := state["images"]; !ok {
		t.Errorf("a new markup tab has no `images`: %v — see markup.spec.json's init", keysOf(state))
	}
	if _, ok := state["layout"]; !ok {
		t.Errorf("a new markup tab has no `layout`: %v", keysOf(state))
	}
	if id, _ := created["id"].(string); !strings.HasPrefix(id, "ab") {
		t.Errorf("the browser allocated the id %q — ids are board-wide monotonic and tagged ab", id)
	}
}

// Choosing a renderer needs more than its name. Fifteen types are called things
// like "Stack", "UI" and "Markup", and the sheet showed only the label — the
// blurb it already computed sat at the BOTTOM of the sheet, under the note
// field, where it read as a hint about the note. Reported 2026-08-27.
//
// Position is the whole fix, so position is what is asserted: between the type
// select and the note field, and non-empty for whatever type is selected.
func TestTheNewTabSheetSaysWhatEachTypeIs(t *testing.T) {
	s := open(t, "")

	if err := s.page.Locator("#add-tab").Click(); err != nil {
		t.Fatalf("opening the new-tab dialog: %v", err)
	}
	blurb := s.page.Locator("#new-tab-hint")
	if err := expect.Locator(blurb).ToBeVisible(); err != nil {
		t.Fatalf("the sheet shows no description at all: %v", err)
	}

	type box struct{ Y, H float64 }
	var sel, hint, note box
	read := func(out *box, sel string) {
		t.Helper()
		s.evalJSON(out, `(q) => {
			const r = document.querySelector(q).getBoundingClientRect();
			return { Y: r.top, H: r.height };
		}`, sel)
	}
	read(&sel, "#new-tab-type")
	read(&hint, "#new-tab-hint")
	read(&note, "#new-tab-note")

	if !(hint.Y > sel.Y && hint.Y < note.Y) {
		t.Errorf("the type description is at y=%.0f, outside the select (%.0f) and the note field (%.0f) it has to sit between",
			hint.Y, sel.Y, note.Y)
	}

	// It says something, and it changes with the choice — otherwise a stale
	// string in the right place would pass this.
	first, err := blurb.TextContent()
	if err != nil || strings.TrimSpace(first) == "" {
		t.Fatalf("the description is empty for the default type (err %v)", err)
	}
	if _, err := s.page.Locator("#new-tab-type").SelectOption(playwright.SelectOptionValues{
		Values: &[]string{"markup"},
	}); err != nil {
		t.Fatalf("choosing another type: %v", err)
	}
	if err := expect.Locator(blurb).ToContainText("image"); err != nil {
		t.Errorf("the description did not follow the choice to markup: %v", err)
	}

	// And the sheet does not change SIZE as you read it. It was `min-width`, so
	// the modal sized itself to its widest line — which is the description — and
	// picking a type slid the whole dialog sideways under the cursor. Every type,
	// because the widest blurb is the one that would prove it.
	types := []string{"dag", "kanban", "markup", "html", "stack", "ui", "notes", "gate"}
	widths := make([]float64, 0, len(types))
	heights := make([]float64, 0, len(types))
	for _, ty := range types {
		if _, err := s.page.Locator("#new-tab-type").SelectOption(playwright.SelectOptionValues{
			Values: &[]string{ty},
		}); err != nil {
			t.Fatalf("choosing %s: %v", ty, err)
		}
		var box struct{ W, H float64 }
		s.evalJSON(&box, `() => {
			const r = document.getElementById('new-tab-dialog').getBoundingClientRect();
			return { W: r.width, H: r.height };
		}`)
		widths = append(widths, box.W)
		heights = append(heights, box.H)
	}
	for i := range widths {
		if widths[i] != widths[0] {
			t.Errorf("the sheet is %.0fpx wide for one type and %.0fpx for another — its width follows the description",
				widths[0], widths[i])
			break
		}
	}
	// Height is allowed to move a little — a three-line floor cannot cover a
	// description that genuinely needs four — but not by half the dialog.
	for i := range heights {
		if diff := heights[i] - heights[0]; diff > 40 || diff < -40 {
			t.Errorf("the sheet's height swings %.0fpx between types (%.0f vs %.0f) — the Create button walks while you read",
				diff, heights[0], heights[i])
			break
		}
	}
}

// Making a tab and then having to go and find it is the wrong end of the
// gesture: you made it because you want to put something on it.
//
// Opened on a DEEP LINK, which is the case that can break it and the one nobody
// would reproduce by hand. A framing host addresses the board as
// `#tab=<id>&r=<n>`, so the fragment on the page says one tab while the human
// creates another — and the fragment is read at load AND on every `hashchange`.
// Anything that re-reads it after a create lands the human back where they
// started, with a tab they cannot see and no message saying so. The write also
// echoes back over SSE and repaints, which is the second way this can regress.
func TestCreatingATabSwitchesToIt(t *testing.T) {
	s := open(t, "#tab=ab13")

	// The premise: the deep link landed, so the fragment names a tab that is NOT
	// the one about to be created.
	if err := expect.Locator(s.view("ab13")).ToBeVisible(); err != nil {
		t.Fatalf("the deep link did not land, so this test is not testing the case it exists for: %v", err)
	}

	const name = "Switched to on creation"
	if err := s.page.Locator("#add-tab").Click(); err != nil {
		t.Fatalf("opening the new-tab dialog: %v", err)
	}
	if err := s.page.Locator("#new-tab-name").Fill(name); err != nil {
		t.Fatalf("naming the tab: %v", err)
	}
	if _, err := s.page.Locator("#new-tab-type").SelectOption(playwright.SelectOptionValues{
		Values: &[]string{"markup"},
	}); err != nil {
		t.Fatalf("choosing the type: %v", err)
	}
	if err := s.page.Locator(`#new-tab-form button[type="submit"]`).Click(); err != nil {
		t.Fatalf("submitting: %v", err)
	}

	// By NAME, because the id is the board's to allocate and the point of the
	// assertion is what the human is looking at.
	selected := s.page.Locator(`#tabs .tab[aria-selected="true"]`)
	if err := expect.Locator(selected).ToContainText(name); err != nil {
		t.Fatalf("the board did not switch to the tab it just created: %v", err)
	}

	// And it STAYS. The save echoes back as an SSE frame and repaints, and the
	// old fragment is still sitting in the URL the whole time.
	time.Sleep(settle)
	if err := expect.Locator(selected).ToContainText(name); err != nil {
		t.Errorf("the board switched away from the new tab after the write settled: %v", err)
	}
	if err := expect.Locator(s.view("ab13")).ToBeHidden(); err != nil {
		t.Errorf("the deep-linked tab came back on screen: %v", err)
	}
}

// The notify button's whole claim is "someone is listening", so with nobody
// waiting it must be disabled and say so rather than looking live. The live
// half — a real `aboard wait` released by pressing it — is TestTheNotifyButtonReleasesAWaitingSession.
func TestTheNotifyButtonIsDisabledWithNobodyWaiting(t *testing.T) {
	s := open(t, "")
	poke := s.page.Locator("#poke")
	if err := expect.Locator(poke).ToBeDisabled(); err != nil {
		t.Errorf("the notify button is live with nobody waiting: %v", err)
	}
	if err := expect.Locator(poke).ToHaveAttribute("data-live", "no"); err != nil {
		t.Errorf("data-live is not \"no\": %v", err)
	}
	if err := expect.Locator(poke).ToContainText("no session waiting"); err != nil {
		t.Errorf("the button does not say nothing is listening: %v", err)
	}
}

// An action strip RECORDS an intent; it never acts. That is the posture that
// makes a stray click harmless on a server with no authentication, and it is one
// of the few things in this project that would be catastrophic to get wrong
// quietly.
func TestAnActionStripRecordsAnIntentInsteadOfActing(t *testing.T) {
	s := open(t, "tab=ab26")

	strip := s.view("ab26").Locator(".action-strip")
	if err := expect.Locator(strip).ToBeVisible(); err != nil {
		t.Fatalf("the chat tab declares state.actions but no strip rendered: %v", err)
	}
	before := len(intents(t, "ab26"))

	if err := strip.Locator(".action-btn").First().Click(); err != nil {
		t.Fatalf("pressing an action: %v", err)
	}
	eventually(t, "the intent to be recorded", func() bool { return len(intents(t, "ab26")) == before+1 })

	last, _ := intents(t, "ab26")[before].(map[string]any)
	if last["by"] != "human" {
		t.Errorf("the intent was recorded by %v, want human", last["by"])
	}
	if last["intent"] == nil || last["action"] == nil {
		t.Errorf("the intent says neither what was asked nor which button: %v", last)
	}
	if err := expect.Locator(s.page.Locator("#save-state")).ToContainText("sent"); err != nil {
		t.Errorf("the shell did not confirm what it sent: %v", err)
	}
}

func intents(t *testing.T, tabID string) []any {
	t.Helper()
	list, _ := readDoc(t).state(t, tabID)["intents"].([]any)
	return list
}

/* ---------- fixtures these tests make for themselves ---------- */

// makeScratchTab allocates through the board's OWN counter, the way the browser
// and every agent do, and bumps it in the same write.
//
// It used to keep a private counter starting at 800, "well above the fixture's
// nextId so nothing the board allocates can collide". That reasoning is wrong in
// one direction it did not consider: ids.go drives nextId ABOVE every id in use
// on every accepted write, so the first scratch tab at ab801 pushed the board's
// counter to 802 — and the next tab the BROWSER made took ab802, which the next
// scratch tab then allocated again. Two tabs with one id, `strict mode
// violation: resolved to 2 elements`, and a board that breaks the project's
// oldest invariant. It was invisible in the declaration order and reproduced
// immediately under `go test -shuffle=on`, which is the argument for running the
// suite shuffled at least once.
func makeScratchTab(t *testing.T, name string) string {
	t.Helper()
	return makeScratchTabOfType(t, name, "notes", map[string]any{"text": "scratch\n"})
}

// makeScratchTabOfType is the same allocation for a tab that is not `notes`. The
// id reasoning above is the whole reason this is one function and not two.
func makeScratchTabOfType(t *testing.T, name, typ string, state map[string]any) string {
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
		"type":  typ,
		"note":  "Made by the browser suite; safe to delete.",
		"state": state,
	})
	apply(t, d)
	return id
}

// requestRemoval drops a tab from an AGENT's write. The server restores it with
// a pendingRemoval rather than deleting it — that restoration is the guarantee,
// and doing it this way means the notice under test arrived the way a real one
// does instead of being planted.
func requestRemoval(t *testing.T, id string) {
	t.Helper()
	d := readDoc(t)
	list, _ := d["tabs"].([]any)
	kept := make([]any, 0, len(list))
	for _, raw := range list {
		if tab, ok := raw.(map[string]any); ok && tab["id"] == id {
			continue
		}
		kept = append(kept, raw)
	}
	d["tabs"] = kept
	apply(t, d)

	tab := readDoc(t).tab(t, id)
	if _, ok := tab["pendingRemoval"]; !ok {
		t.Fatalf("an agent's write deleted tab %s outright — guarantee 1 is gone", id)
	}
}

func keysOf(m map[string]any) []string { return sortedKeys(m) }

// A removal must survive a reload that lands while its save is still in flight.
//
// `removeTab` filters the tab out of `doc`, then awaits an immediate save, then
// rebuilds the strip. Anything that replaces `doc` inside that await used to put
// the tab back: `mergeOntoFresh` builds the merged document from the SERVER's tab
// list and re-applies only the tabs `localEdits()` reports as changed or added —
// and a tab that is gone is neither. The result was a strip showing a tab the
// server did not have, for as long as the page stayed open.
//
// The race is made deterministic by HOLDING the POST open with a route
// interceptor rather than by trying to win it: the foreign write, the SSE ping
// and the reload it causes all happen while the save is parked, and only then is
// it let go. The suite found this by failing once under `go test -shuffle=on`,
// which is the argument for running it that way now and then.
func TestRemovingATabSurvivesAReloadArrivingMidSave(t *testing.T) {
	id := makeScratchTab(t, "Removed mid-save")
	requestRemoval(t, id)
	s := open(t, "probe=1&tab="+id)

	// The banner's Remove tab is the shell's only route into removeTab: an agent
	// asks, the human answers. There is no other way to delete a tab in this UI,
	// which is guarantee 1 seen from the browser.
	banner := s.view(id).Locator(".banner--removal")
	if err := expect.Locator(banner).ToBeVisible(); err != nil {
		t.Fatalf("no removal request on the tab: %v", err)
	}

	var once sync.Once
	inFlight := make(chan struct{})
	release := make(chan struct{})
	if err := s.page.Route("**/aboard.json", func(route playwright.Route) {
		if route.Request().Method() != http.MethodPost {
			_ = route.Continue()
			return
		}
		once.Do(func() {
			close(inFlight)
			<-release
		})
		_ = route.Continue()
	}); err != nil {
		t.Fatalf("intercepting the save: %v", err)
	}

	if err := banner.GetByText("Remove tab").Click(); err != nil {
		t.Fatalf("clicking Remove tab: %v", err)
	}
	answer(t, s.boardDialog(), "Remove tab")

	select {
	case <-inFlight:
	case <-time.After(10 * time.Second):
		t.Fatal("the removal never posted")
	}

	// A second actor writes while the save is parked. The page hears it on SSE
	// and reloads — which is the moment the removal used to be undone.
	d := readDoc(t)
	d.state(t, "ab126")["probeMidSave"] = time.Now().Format(time.RFC3339Nano)
	apply(t, d)
	eventually(t, "the page to take the foreign write", func() bool {
		return s.evalBool(`() => !!(window.__aboardProbe.doc.tabs
          .find((t) => t.id === 'ab126') || {}).state?.probeMidSave`)
	})
	if s.evalBool(`(id) => window.__aboardProbe.doc.tabs.some((t) => t.id === id)`, id) {
		t.Error("the reload put the removed tab back into the document — this is the defect")
	}

	close(release)

	eventually(t, "the tab to leave the board", func() bool {
		list, _ := readDoc(t)["tabs"].([]any)
		for _, raw := range list {
			if tab, ok := raw.(map[string]any); ok && tab["id"] == id {
				return false
			}
		}
		return true
	})
	if err := expect.Locator(s.page.Locator(`#tabs .tab[data-id="` + id + `"]`)).ToHaveCount(0); err != nil {
		t.Errorf("the strip still shows a tab the server does not have: %v", err)
	}
}

// The closest headless stand-in for a VS Code webview: the board inside a
// same-origin <iframe> whose sandbox does NOT include `allow-modals`.
//
// That one missing token is the whole bug. Inside such a frame window.confirm is
// suppressed — it returns false, draws nothing, logs nothing and throws nothing
// — so the pre-fix `removeTab` took its cancel path and the tab stayed. The
// board's own <dialog> is unaffected by `allow-modals`, which is why this test
// passes now and fails against the old code for a reason no other test in the
// suite can see: every other one runs at the top level, where confirm() works
// perfectly.
//
// Same-origin because that is what the real embedder is not, and it does not
// matter here: `allow-same-origin` is what lets the framed board read and write
// its own server, which a cross-origin wrapper would also allow. What is being
// tested is the SANDBOX, not the origin.
func TestRemovingATabWorksInsideAFrameWithNoAllowModals(t *testing.T) {
	id := makeScratchTab(t, "Removed in a webview")
	requestRemoval(t, id)

	s := openSandboxedWrapper(t, "chrome=notabs&tab="+id)
	frame := s.page.FrameLocator("#frame")

	banner := frame.Locator(`[data-tab="` + id + `"][data-active="yes"] .banner--removal`)
	if err := expect.Locator(banner).ToBeVisible(); err != nil {
		t.Fatalf("no removal request inside the framed board: %v", err)
	}
	if err := banner.GetByText("Remove tab").Click(); err != nil {
		t.Fatalf("clicking Remove tab inside the frame: %v", err)
	}

	dialog := frame.Locator(boardDialogSelector)
	if err := expect.Locator(dialog).ToBeVisible(); err != nil {
		t.Fatalf("the board drew no dialog inside a frame with no allow-modals — "+
			"this is what the human saw as \"I clicked it but nothing happens\": %v", err)
	}
	if err := expect.Locator(dialog).ToContainText("Removed in a webview"); err != nil {
		t.Errorf("the framed confirmation does not name the tab: %v", err)
	}
	if err := dialog.GetByRole("button", playwright.LocatorGetByRoleOptions{
		Name: "Remove tab", Exact: new(true),
	}).Click(); err != nil {
		t.Fatalf("confirming inside the frame: %v", err)
	}

	eventually(t, "the tab to leave the board", func() bool {
		list, _ := readDoc(t)["tabs"].([]any)
		for _, raw := range list {
			if tab, ok := raw.(map[string]any); ok && tab["id"] == id {
				return false
			}
		}
		return true
	})
	if err := expect.Locator(frame.Locator(`#tabs .tab[data-id="` + id + `"]`)).ToHaveCount(0); err != nil {
		t.Errorf("the framed strip still holds the tab: %v", err)
	}
}
