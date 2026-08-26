//go:build e2e

package e2e

import (
	"slices"
	"strings"
	"testing"

	"github.com/exoport/aboard/pkg/aboard"
)

// Neither test registers gesture coverage. Both drive CONTROLS — a declared
// button id — and the coverage gate is about the `gestures` list, which is the
// half of the manifest with no other consumer. dag and notes are both already
// covered by tests that drive their real gestures; claiming a second, invented
// sentence here would fail the gate for saying something no spec declares, which
// is the gate working.

// Mount receipts, end to end: the browser draws a tab and the agent finds out
// what it drew.
//
// This is the loop `aboard apply` could not close on its own. "applied", exit 0,
// is evidence a write was ACCEPTED — an unknown `ui` component draws a marker and
// an unknown PROP draws nothing at all — so the only reader who ever saw the
// result was the human, which is backwards: the agent is the one still holding
// the context to fix it.
//
// Asserted against a real Chromium mounting the real shell, because that is the
// only place the claim is made. A unit test of the sweep would assert that a
// function reads attributes, which is not the thing anybody doubts.
func TestTheBrowserReportsWhatItRendered(t *testing.T) {
	s := open(t, "")

	// The gallery first, for the plain case: a tab was mounted and said so.
	s.tab("bb133")
	eventually(t, "the ui gallery's receipt to arrive", func() bool {
		return receiptFor(t, "bb133").Mounts >= 1
	})
	if got := receiptFor(t, "bb133"); got.Type != "ui" {
		t.Errorf("unexpected receipt for the gallery: %+v", got)
	}

	// A tab with declared controls: the ids reported are the ones
	// views/dag.spec.json declares, which is what makes this not a DOM sweep.
	s.tab("bb1")
	eventually(t, "the dag's receipt to arrive", func() bool {
		return len(receiptFor(t, "bb1").Controls) > 0
	})
	dag := receiptFor(t, "bb1")
	if !containsID(dag.Controls, "relayout") {
		t.Errorf("the dag's declared controls were not reported: %+v", dag.Controls)
	}
	if len(dag.Undeclared) != 0 {
		t.Errorf("the dag drew an undeclared control: %v", dag.Undeclared)
	}

	// And a press. A recorded press proves the control was REACHED and nothing
	// more, which is one of the two limits the command prints about itself.
	if err := s.control("bb1", "relayout").Click(); err != nil {
		t.Fatalf("pressing the dag's relayout control: %v", err)
	}
	eventually(t, "the press to be reported", func() bool {
		return receiptFor(t, "bb1").Fired["relayout"] >= 1
	})
}

// The gallery's "Unknown" panel holds a `sparkline` on purpose, to demonstrate
// the unknown-component marker. It is a TRUE POSITIVE and must keep being
// reported — "fixing" it by deleting the demonstration is the temptation this
// pins against.
//
// It is also the reason a receipt is swept on ACTIVATION and not only on mount:
// `ui`'s `tabs` component builds only the open panel, so until the human reveals
// the fifth one the component is genuinely not drawn, and a mount-only sweep
// would report the board as clean forever. Reveal it, come back to the tab, and
// the marker reaches the agent.
func TestAnUnknownComponentTheHumanRevealedReachesTheAgent(t *testing.T) {
	s := open(t, "")
	view := s.tab("bb133")

	if err := view.Locator(`.uic-tab:has-text("Unknown")`).Click(); err != nil {
		t.Fatalf("opening the gallery's Unknown panel: %v", err)
	}
	if err := expect.Locator(view.Locator(".uic-unknown").First()).ToBeVisible(); err != nil {
		t.Fatalf("the unknown-component marker is not on screen: %v", err)
	}

	// Away and back: the re-sweep happens when a tab becomes active.
	s.tab("bb1")
	s.tab("bb133")

	eventually(t, "the unknown-component marker to be reported", func() bool {
		return containsID(receiptFor(t, "bb133").Unknown, "sparkline")
	})
}

// receiptFor reads the sidecar the same way `aboard rendered` does. Straight off
// disk: the file is written by the server, and reading it needs no server, which
// is the property that lets a session ask after the board has stopped.
func receiptFor(t *testing.T, tab string) aboard.Receipt {
	t.Helper()
	list, err := aboard.Rendered(t.Context(), board, "", tab)
	if err != nil {
		t.Fatalf("reading receipts: %v", err)
	}
	if len(list) == 0 {
		return aboard.Receipt{}
	}
	return list[0]
}

func containsID(list []string, want string) bool { return slices.Contains(list, want) }

// The change banner answers half a question — "somebody changed this" — and the
// journal has held the other half all along with nothing in the UI able to reach
// it. The link is READ-ONLY on purpose: it shows the previous state and prints
// the command that puts it back, because restoring from a button would be a
// write the human made without seeing the document it produces, on a board where
// a bad write is the thing history exists to recover from.
func TestTheChangeBannerLinksToWhatTheTabSaidBefore(t *testing.T) {
	id := makeScratchTab(t, "History probe")

	// A second agent write, so the journal holds a `before` for this tab: the
	// write that CREATES a tab has no previous state, and offering that as a
	// version would offer a restore that blanks it.
	d := readDoc(t)
	d.state(t, id)["text"] = "the second thing it said\n"
	apply(t, d)

	// After the writes, never before: a write issued once the page is up is a
	// foreign change racing the thing under test.
	s := open(t, "")
	s.tab(id)

	banner := s.view(id).Locator(".banner").First()
	if err := expect.Locator(banner).ToContainText("changed this tab"); err != nil {
		t.Fatalf("no change banner on a tab an agent just wrote to: %v", err)
	}

	link := s.view(id).Locator(`button:has-text("What it said before")`)
	if err := link.Click(); err != nil {
		t.Fatalf("pressing the history link: %v", err)
	}
	prev := s.view(id).Locator(".history-prev")
	if err := expect.Locator(prev).ToBeVisible(); err != nil {
		t.Fatalf("the previous state never appeared: %v", err)
	}
	if err := expect.Locator(prev).ToContainText("scratch"); err != nil {
		t.Errorf("the panel does not show what the tab said before: %v", err)
	}
	// The one command that puts it back, in the panel, because the id alone is
	// not enough going TO the human.
	if err := expect.Locator(prev).ToContainText("aboard history " + id + " --at 1"); err != nil {
		t.Errorf("the panel does not say how to restore it: %v", err)
	}

	text, err := prev.TextContent()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "the second thing it said") {
		t.Error("the panel is showing the CURRENT state, not the previous one")
	}

	// A second press hides it again: it is a toggle on a notice, not a panel the
	// human has to dismiss some other way.
	if err := link.Click(); err != nil {
		t.Fatal(err)
	}
	if err := expect.Locator(prev).ToBeHidden(); err != nil {
		t.Errorf("the panel did not toggle shut: %v", err)
	}
}
