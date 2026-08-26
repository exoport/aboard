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
	d.state(t, "bb202")["text"] = "Touched by an agent at " + strconv.Itoa(len(d))
	apply(t, d)

	s := open(t, "tab=bb202")
	view := s.view("bb202")

	banner := view.Locator(".banner").Filter(playwright.LocatorFilterOptions{HasText: "agent-e2e changed this tab."})
	if err := expect.Locator(banner).ToBeVisible(); err != nil {
		t.Fatalf("no touched notice after an agent write: %v", err)
	}
	if err := expect.Locator(s.page.Locator(`#tabs .tab[data-id="bb202"] .tab-dot`)).ToBeVisible(); err != nil {
		t.Errorf("the tab strip shows no unread dot: %v", err)
	}

	if err := banner.GetByText("Dismiss").Click(); err != nil {
		t.Fatalf("clicking Dismiss: %v", err)
	}

	eventually(t, "the marker to clear on the server", func() bool {
		tab := readDoc(t).tab(t, "bb202")
		_, still := tab["touched"]
		return !still
	})
	// Dismissing records WHEN you looked, not only that you did: `seen.human` is
	// the timestamp another session compares against to tell "changed since the
	// human last read it" from "unread".
	if got := dig(readDoc(t).tab(t, "bb202"), "seen", "human"); got == nil {
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
	if err := expect.Locator(s.page.Locator(`#tabs .tab[data-id="bb202"] .tab-dot`)).ToBeHidden(); err != nil {
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
// the one that goes through a native confirm(). Playwright auto-dismisses an
// unhandled dialog, which would make this test pass while removing nothing, so
// the message is asserted as well as answered.
func TestAPendingRemovalCanBeRemovedBehindAConfirm(t *testing.T) {
	id := makeScratchTab(t, "Remove me")
	requestRemoval(t, id)

	s := open(t, "tab="+id)
	banner := s.view(id).Locator(".banner--removal")
	if err := expect.Locator(banner).ToBeVisible(); err != nil {
		t.Fatalf("no removal request on the tab: %v", err)
	}

	dialogs := s.onDialog(true, "")
	if err := banner.GetByText("Remove tab").Click(); err != nil {
		t.Fatalf("clicking Remove tab: %v", err)
	}
	seen := dialogs.only(t)
	if seen.Kind != "confirm" {
		t.Errorf("removing a tab asked a %s, want a confirm", seen.Kind)
	}
	if !strings.Contains(seen.Message, "Remove me") || !strings.Contains(seen.Message, "content is deleted") {
		t.Errorf("the confirmation does not say what is about to be lost: %q", seen.Message)
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
	if err := expect.Locator(s.page.Locator(`#tabs .tab[data-id="` + id + `"]`)).ToHaveCount(0); err != nil {
		t.Errorf("the tab is still in the strip: %v", err)
	}
}

// Double-click a tab to rename it — the one prompt() in the shell.
func TestATabIsRenamedThroughAPrompt(t *testing.T) {
	id := makeScratchTab(t, "Before")

	s := open(t, "tab="+id)
	strip := s.page.Locator(`#tabs .tab[data-id="` + id + `"]`)

	dialogs := s.onDialog(true, "After")
	if err := strip.Dblclick(); err != nil {
		t.Fatalf("double-clicking the tab: %v", err)
	}
	seen := dialogs.only(t)
	if seen.Kind != "prompt" {
		t.Errorf("renaming asked a %s, want a prompt", seen.Kind)
	}

	eventually(t, "the new name to reach the server", func() bool {
		return readDoc(t).tab(t, id)["name"] == "After"
	})
	if err := expect.Locator(strip).ToContainText("After"); err != nil {
		t.Errorf("the strip still shows the old name: %v", err)
	}

	// Cancelling must change nothing. A prompt that returns null and is treated
	// as an empty string renames the tab to "" and the strip reads "(unnamed)".
	cancelled := s.onDialog(false, "")
	if err := strip.Dblclick(); err != nil {
		t.Fatalf("double-clicking the tab again: %v", err)
	}
	if got := cancelled.only(t); got.Kind != "prompt" {
		t.Errorf("the second gesture asked a %s", got.Kind)
	}
	if got := readDoc(t).tab(t, id)["name"]; got != "After" {
		t.Errorf("cancelling the rename changed the name to %q", got)
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
	if id, _ := created["id"].(string); !strings.HasPrefix(id, "bb") {
		t.Errorf("the browser allocated the id %q — ids are board-wide monotonic and tagged bb", id)
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
	s := open(t, "tab=bb26")

	strip := s.view("bb26").Locator(".action-strip")
	if err := expect.Locator(strip).ToBeVisible(); err != nil {
		t.Fatalf("the chat tab declares state.actions but no strip rendered: %v", err)
	}
	before := len(intents(t, "bb26"))

	if err := strip.Locator(".action-btn").First().Click(); err != nil {
		t.Fatalf("pressing an action: %v", err)
	}
	eventually(t, "the intent to be recorded", func() bool { return len(intents(t, "bb26")) == before+1 })

	last, _ := intents(t, "bb26")[before].(map[string]any)
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
// on every accepted write, so the first scratch tab at bb801 pushed the board's
// counter to 802 — and the next tab the BROWSER made took bb802, which the next
// scratch tab then allocated again. Two tabs with one id, `strict mode
// violation: resolved to 2 elements`, and a board that breaks the project's
// oldest invariant. It was invisible in the declaration order and reproduced
// immediately under `go test -shuffle=on`, which is the argument for running the
// suite shuffled at least once.
func makeScratchTab(t *testing.T, name string) string {
	t.Helper()
	d := readDoc(t)
	next, ok := d["nextId"].(float64)
	if !ok {
		t.Fatalf("the board has no nextId to allocate from: %v", d["nextId"])
	}
	id := fmt.Sprintf("bb%d", int(next))
	d["nextId"] = int(next) + 1
	list, _ := d["tabs"].([]any)
	d["tabs"] = append(list, map[string]any{
		"id":    id,
		"name":  name,
		"type":  "notes",
		"note":  "Made by the browser suite; safe to delete.",
		"state": map[string]any{"text": "scratch\n"},
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

	s.onDialog(true, "")
	if err := banner.GetByText("Remove tab").Click(); err != nil {
		t.Fatalf("clicking Remove tab: %v", err)
	}

	select {
	case <-inFlight:
	case <-time.After(10 * time.Second):
		t.Fatal("the removal never posted")
	}

	// A second actor writes while the save is parked. The page hears it on SSE
	// and reloads — which is the moment the removal used to be undone.
	d := readDoc(t)
	d.state(t, "bb126")["probeMidSave"] = time.Now().Format(time.RFC3339Nano)
	apply(t, d)
	eventually(t, "the page to take the foreign write", func() bool {
		return s.evalBool(`() => !!(window.__aboardProbe.doc.tabs
          .find((t) => t.id === 'bb126') || {}).state?.probeMidSave`)
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
