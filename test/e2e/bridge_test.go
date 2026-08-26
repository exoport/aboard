//go:build e2e

package e2e

import (
	"testing"
)

// THE WRITE HALF OF THE BRIDGE — a widget calling aboard.set(), through
// postMessage, through views/html.js, into state.data and onto disk.
//
// Every handoff since the rename called this "provable only by a human click",
// and it was true: the old suite could not reach into the frame at all. The
// frame is sandbox="allow-scripts" with no allow-same-origin, so it has an
// opaque origin and Chrome runs it out of process; a --dump-dom of the parent
// page contains no part of it. That sentence is now deleted from every document
// in the repo, and this is what replaced it.
func TestAWidgetWritesThroughTheBridge(t *testing.T) {
	covers(t, "html", "whatever the widget offers")

	s := open(t, "")
	s.tab("bb72")

	frame := s.widget("bb72")
	// The sketch pad ships with strokes, so Undo has something to remove. It
	// returns early on an empty canvas — a widget that has nothing to undo is
	// the one case where clicking the button proves nothing.
	before := strokeCount(t)
	if before == 0 {
		t.Fatal("the sketch pad has no strokes, so Undo is a no-op — the example board should ship with some")
	}

	if err := frame.Locator("#undo").Click(); err != nil {
		t.Fatalf("clicking Undo inside the sandboxed frame: %v", err)
	}

	// The widget flashes its own confirmation, which is the frame's half of the
	// round trip: aboard.set() returned and the parent acknowledged.
	if err := expect.Locator(frame.Locator("#saved")).ToContainText("saved into state.data"); err != nil {
		t.Errorf("the widget never reported the write: %v", err)
	}
	eventually(t, "the stroke to leave state.data", func() bool { return strokeCount(t) == before-1 })

	// And the parent's own view of it: html.js mirrors state.data into the
	// "stored data" panel behind Show source, which is the only place a human
	// ever sees what the bridge stored.
	if err := s.control("bb72", "source").Click(); err != nil {
		t.Fatalf("opening the source panel: %v", err)
	}
	if err := expect.Locator(s.view("bb72").Locator(".html-data")).ToContainText("strokes"); err != nil {
		t.Errorf("the stored-data panel does not show state.data: %v", err)
	}
}

func strokeCount(t *testing.T) int {
	t.Helper()
	list, _ := dig(readDoc(t).state(t, "bb72"), "data", "strokes").([]any)
	return len(list)
}

// The same bridge, one level down: an html BLOCK inside a stack.
//
// This is where it broke. views/html.js asks for /tab/${ctx.tab.id}/html, and
// inside a stack that id is "<tab>/<block>", which matched no tab — the frame
// 404'd and the block rendered BLANK, with no marker and nothing on any console.
// The fix was one lookup in serveTabHTML; the WRITE path needed no change at all,
// because stack.js's ctxForBlock already hands down a live state getter. A
// prediction that something is fine is not evidence that it is, which is why
// this stayed listed as a gap until a human clicked it — and why it is a test
// now.
func TestAWidgetInsideAStackBlockWritesThroughTheBridge(t *testing.T) {
	covers(t, "stack", "collapse a block by its header")

	s := open(t, "")
	s.tab("bb32")

	block := s.view("bb32").Locator(`[data-block-id="bb197"]`)
	if err := expect.Locator(block).ToHaveAttribute("data-open", "yes"); err != nil {
		t.Fatalf("the html block is not expanded: %v", err)
	}

	before := blockTicks(t)
	if err := block.FrameLocator("iframe").Locator("#tick").Click(); err != nil {
		t.Fatalf("clicking the block's widget: %v", err)
	}
	eventually(t, "the tick to reach blocks[].state.data", func() bool { return blockTicks(t) == before+1 })

	// The gesture the stack renderer declares, on the block that matters: a
	// collapse must not lose what the widget wrote.
	if err := block.Locator(".block-head").Click(); err != nil {
		t.Fatalf("collapsing the block: %v", err)
	}
	if err := expect.Locator(block).ToHaveAttribute("data-open", "no"); err != nil {
		t.Errorf("the block did not collapse: %v", err)
	}
	if got := blockTicks(t); got != before+1 {
		t.Errorf("collapsing the block changed its stored data: ticks = %d, want %d", got, before+1)
	}
}

func blockTicks(t *testing.T) int {
	t.Helper()
	blocks, _ := readDoc(t).state(t, "bb32")["blocks"].([]any)
	for _, raw := range blocks {
		b, ok := raw.(map[string]any)
		if !ok || b["id"] != "bb197" {
			continue
		}
		n, _ := dig(b, "state", "data", "ticks").(float64)
		return int(n)
	}
	t.Fatal("the Migration review tab has no html block bb197")
	return 0
}

// The containment, asserted from the browser rather than from a curl: a frame
// with allow-same-origin could remove its own sandbox, and connect-src 'none' is
// what actually stops a widget reaching a server that has no authentication.
//
// Go tests already assert the CSP header byte for byte (htmltab_csp_test.go).
// What they cannot see is whether the ATTRIBUTE survives on the element the
// renderer builds, which is the other half of the same guarantee.
func TestTheWidgetFrameIsSandboxedOnThePage(t *testing.T) {
	s := open(t, "")
	s.tab("bb72")

	frameEl := s.view("bb72").Locator("iframe")
	if err := expect.Locator(frameEl).ToHaveAttribute("sandbox", "allow-scripts"); err != nil {
		t.Errorf("the widget frame's sandbox is not exactly allow-scripts: %v", err)
	}

	// And it really is cross-origin: same-origin would mean document access, and
	// `contentDocument` is the cheapest proof either way.
	reachable := s.evalBool(`() => {
      const f = document.querySelector('[data-tab="bb72"][data-active="yes"] iframe');
      try { return !!(f && f.contentDocument); } catch { return false; }
    }`)
	if reachable {
		t.Error("the parent page can read the widget frame's document — the sandbox is not opaque")
	}
}
