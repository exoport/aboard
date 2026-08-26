//go:build e2e

package e2e

import (
	"testing"

	"github.com/mxschmitt/playwright-go"
)

// The two browser-side defects the deep review of d6c2f84 reproduced BY HAND in
// Chromium, because nothing in the old suite could reach them:
//
//  1. an SSE reload arriving inside the 250 ms save debounce replaced `doc`
//     wholesale, so the human's half-typed edit was swapped out from behind the
//     textarea and the pending save posted the server's own copy back over it —
//     with a 200 and a "Saved" flash;
//  2. `baseline` was never advanced after a 409 merge, so the SECOND conflict
//     classified the copy the first merge had just adopted from an agent as the
//     human's own edit, and "Restore mine" handed back the agent's text under
//     their name.
//
// Both are driven through `?probe=1`, the shell's own test seam. That is a
// deliberate choice over "type in a textarea and race a write": the bug is one of
// TIMING between a fetch, a debounce and an SSE frame, and a test that has to win
// a race to observe it is a test that goes green when the race is lost. The probe
// arms the debounce and delivers the ping at a known point, so a regression fails
// every time rather than one run in five. The un-raced, real-typing half of the
// same behaviour is TestASecondActorsWriteAppearsWithoutAReload.
//
// The foreign write is issued by fetch() from inside the page rather than by the
// Go process, for the same reason: keeping the whole scenario in one Evaluate
// removes the round trip that could otherwise land outside the debounce window.
// It is the same route and the same envelope an agent's write uses — __by,
// __origin and a __base of the document's own rev — so the server cannot tell the
// difference, which is what makes it a real second writer.

type mergeProbe struct {
	ReorderIsNotAnEdit bool   `json:"reorderIsNotAnEdit"`
	SaveArmed          bool   `json:"saveArmed"`
	Kept               bool   `json:"kept"`
	OnScreen           bool   `json:"onScreen"`
	SawTheirs          bool   `json:"sawTheirs"`
	BaselineMoved      bool   `json:"baselineMoved"`
	BaselineIsServer   bool   `json:"baselineIsServer"`
	Rearmed            bool   `json:"rearmed"`
	Typed              string `json:"typed"`
	Err                string `json:"err"`
}

const mergeProbeJS = `async (ids) => {
  const out = { err: '' };
  try {
    const P = window.__aboardProbe;
    if (!P) throw new Error('the shell never exposed __aboardProbe');
    const theirs = P.doc.tabs.find((t) => t.id === ids.theirs);
    if (!theirs) throw new Error('no tab ' + ids.theirs);

    // A key REORDERING is not an edit. aboard init writes the example board as
    // authored JSON and GET /aboard.json serves those bytes, but the server
    // re-marshals through its own structs on the first accepted write — so
    // "note" is a tab's third key before and its last key after, with no value
    // changed. Comparing the JSON text made every tab the human had touched
    // compare unequal to its own baseline on a freshly initialised board, and
    // the merge called that a collision.
    let mine = P.doc.tabs.find((t) => t.id === ids.mine);
    const reordered = {};
    for (const k of Object.keys(mine).reverse()) reordered[k] = mine[k];
    P.doc.tabs = P.doc.tabs.map((t) => (t.id === mine.id ? reordered : t));
    out.reorderIsNotAnEdit = P.localEdits().changed.length === 0;
    mine = P.doc.tabs.find((t) => t.id === ids.mine);

    const typed = 'probe note ' + Date.now();
    out.typed = typed;
    const revBefore = P.baseline.rev;

    // The human types, and the save is DEBOUNCED — not yet sent.
    mine.note = typed;
    P.repaint();
    P.save();
    out.saveArmed = P.saveArmed;

    // Another writer lands inside that window, through the same route an agent
    // uses, so the reload below is the real thing.
    const fresh = await (await fetch('aboard.json', { cache: 'no-store' })).json();
    fresh.tabs.find((t) => t.id === ids.theirs).state = {
      ...(theirs.state || {}), probeForeign: typed,
    };
    const wrote = await fetch('aboard.json', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ...fresh, __by: 'agent-probe', __origin: 'e2e-probe', __base: String(fresh.rev) }),
    });
    if (!wrote.ok) throw new Error('the foreign write was refused: ' + wrote.status);

    // The ping the SSE handler would deliver.
    await P.load({ announce: true, merge: true });

    const after = P.doc.tabs.find((t) => t.id === ids.mine);
    out.kept = !!after && after.note === typed;
    out.onScreen = document.getElementById('tab-note-text').textContent === typed;
    out.sawTheirs = (P.doc.tabs.find((t) => t.id === ids.theirs) || {}).state?.probeForeign === typed;
    out.baselineMoved = Number(P.baseline.rev) > Number(revBefore);
    // The baseline is the SERVER's document, not the merged one: an unsaved edit
    // that became the baseline would be classified as already-saved and dropped
    // by the next merge, silently.
    out.baselineIsServer = !P.baseline.tabs.some((t) => t.id === ids.mine && t.note === typed);
    out.rearmed = P.saveArmed;
  } catch (e) { out.err = String(e && e.message || e); }
  return out;
}`

func TestAForeignWriteInsideTheSaveDebounceKeepsTheHumansEdit(t *testing.T) {
	// BEFORE the page opens, never after. Restoring the note is an agent write,
	// and an agent write to the tab under test lands on the open page as an SSE
	// merge — so doing it second put a foreign change in flight against the very
	// merge the probe is about to drive, and the human's edit came back
	// classified as a collision. It passed in file order only because the note
	// was already right by then and restoreNote returned without writing; under
	// -shuffle it wrote, and the test failed with the defect's own message.
	restoreNote(t, "bb202")
	s := open(t, "probe=1&tab=bb202")

	var got mergeProbe
	s.evalJSON(&got, mergeProbeJS, map[string]string{"mine": "bb202", "theirs": "bb126"})
	if got.Err != "" {
		t.Fatalf("the probe failed: %s", got.Err)
	}

	if !got.ReorderIsNotAnEdit {
		t.Error("a tab whose keys were reordered reads as a human edit — canon() is not canonicalising")
	}
	if !got.SaveArmed {
		t.Fatal("save() did not arm the debounce, so nothing here was tested")
	}
	if !got.Kept {
		t.Error("the human's edit was discarded by the reload — this is the defect")
	}
	if !got.OnScreen {
		t.Error("the edit survived in the document but not on screen")
	}
	if !got.SawTheirs {
		t.Error("the other writer's change was not adopted")
	}
	if !got.BaselineMoved {
		t.Error("the baseline did not advance past the foreign write")
	}
	if !got.BaselineIsServer {
		t.Error("the baseline is the MERGED document, so the next merge will drop the unsaved edit silently")
	}
	if !got.Rearmed {
		t.Error("the interrupted save was not re-armed, so the edit never reaches the server")
	}

	// And the re-armed save actually lands, on top of the write that interrupted
	// it. This is the half the old probe checked with a sleep; here it is the
	// server's own copy that decides.
	eventually(t, "the re-armed save to reach the server", func() bool {
		return readDoc(t).tab(t, "bb202")["note"] == got.Typed
	})
	if got := dig(readDoc(t).state(t, "bb126"), "probeForeign"); got == nil {
		t.Error("the foreign writer's state is not on the server")
	}
}

type collisionProbe struct {
	StashedFirst string `json:"stashedFirst"`
	StashedAfter string `json:"stashedAfter"`
	OnScreen     string `json:"onScreen"`
	Human        string `json:"human"`
	Err          string `json:"err"`
}

const collisionProbeJS = `async (ids) => {
  const out = { err: '' };
  try {
    const P = window.__aboardProbe;
    if (!P) throw new Error('the shell never exposed __aboardProbe');
    const human = 'the human wrote this ' + Date.now();
    out.human = human;

    P.doc.tabs.find((t) => t.id === ids.mine).note = human;

    // The agent changes the SAME tab: a genuine collision.
    const fresh = await (await fetch('aboard.json', { cache: 'no-store' })).json();
    fresh.tabs.find((t) => t.id === ids.mine).note = 'the agent wrote this';
    const wrote = await fetch('aboard.json', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ...fresh, __by: 'agent-probe', __origin: 'e2e-probe', __base: String(fresh.rev) }),
    });
    if (!wrote.ok) throw new Error('the colliding write was refused: ' + wrote.status);

    await P.load({ announce: true, merge: true });
    out.stashedFirst = (P.stashed.get(ids.mine) || {}).note || '';
    await P.load({ announce: true, merge: true });   // a second pass over the same tab
    out.stashedAfter = (P.stashed.get(ids.mine) || {}).note || '';
    out.onScreen = document.getElementById('tab-note-text').textContent;
  } catch (e) { out.err = String(e && e.message || e); }
  return out;
}`

func TestASecondCollisionStillOffersTheHumansOwnText(t *testing.T) {
	// Before the page opens — see the note in the test above.
	restoreNote(t, "bb202")
	s := open(t, "probe=1&tab=bb202")

	var got collisionProbe
	s.evalJSON(&got, collisionProbeJS, map[string]string{"mine": "bb202"})
	if got.Err != "" {
		t.Fatalf("the probe failed: %s", got.Err)
	}

	if got.StashedFirst != got.Human {
		t.Errorf("the first collision stashed %q, want the human's own text %q", got.StashedFirst, got.Human)
	}
	if got.StashedAfter != got.Human {
		t.Errorf("a second merge overwrote the stash with %q — this is the defect: the agent's text under the human's name",
			got.StashedAfter)
	}
	if got.OnScreen != "the agent wrote this" {
		t.Errorf("the server's committed copy is not what is on screen: %q", got.OnScreen)
	}

	// The notice itself, and the button the human actually presses. Everything
	// above is about the shell's bookkeeping; this is the only assertion that
	// says the recovery is REACHABLE.
	notice := s.view("bb202").Locator(".banner--removal").Filter(playwright.LocatorFilterOptions{
		HasText: "Your edit to this tab collided.",
	})
	if err := expect.Locator(notice).ToBeVisible(); err != nil {
		t.Fatalf("no collision notice on the tab: %v", err)
	}
	if err := notice.GetByText("Restore mine").Click(); err != nil {
		t.Fatalf("clicking Restore mine: %v", err)
	}
	eventually(t, "Restore mine to put the human's words back on the server", func() bool {
		return readDoc(t).tab(t, "bb202")["note"] == got.Human
	})
	if err := expect.Locator(notice).ToBeHidden(); err != nil {
		t.Errorf("the notice is still there after restoring: %v", err)
	}
}

// restoreNote puts the scratch tab's note back to a known sentence before a
// probe rewrites it. The suite shares one board, and a note is the human's words
// about their own tab — leaving "probe note 1756…" in the strip is the one
// artefact here that would read as somebody's writing rather than test debris.
func restoreNote(t *testing.T, tabID string) {
	t.Helper()
	const note = "A notes tab of its own, so the driver can type into one without opening a stack first."
	d := readDoc(t)
	if d.tab(t, tabID)["note"] == note {
		return
	}
	d.tab(t, tabID)["note"] = note
	apply(t, d)
}
