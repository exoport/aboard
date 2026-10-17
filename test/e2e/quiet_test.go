//go:build e2e

package e2e

import (
	"strings"
	"testing"

	"github.com/mxschmitt/playwright-go"
)

// `quiet`, end to end. The rules are the server's and the Go tests pin them;
// what only a browser can say is whether the shell DRAWS them — one dot when the
// tab arrives quiet, none for the agent writes after it — and whether the human's
// own toggle reaches the server as the human's.
func TestAQuietTabIsAnnouncedOnceAndTheHumanCanToggleIt(t *testing.T) {
	id := makeScratchTab(t, "Quiet status")
	// The tab goes quiet in a write of its own, before the page opens: setup
	// issued after open() is a foreign write racing the thing under test.
	d := readDoc(t)
	d.tab(t, id)["quiet"] = true
	apply(t, d)

	s := open(t, "tab="+id)
	strip := s.page.Locator(`#tabs .tab[data-id="` + id + `"]`)
	dot := strip.Locator(".tab-dot")

	// The write that made it quiet is announced, and says so.
	banner := s.view(id).Locator(".banner").Filter(playwright.LocatorFilterOptions{HasText: "agent-e2e changed this tab."})
	if err := expect.Locator(banner).ToContainText("quiet from now on"); err != nil {
		t.Fatalf("going quiet was not announced in the tab: %v", err)
	}
	if err := expect.Locator(dot).ToBeVisible(); err != nil {
		t.Errorf("going quiet raised no dot on the strip: %v", err)
	}
	if title, _ := strip.GetAttribute("title"); !strings.Contains(title, "quiet:") {
		t.Errorf("the tab's title does not say it is quiet: %q", title)
	}

	if err := banner.GetByText("Dismiss").Click(); err != nil {
		t.Fatalf("clicking Dismiss: %v", err)
	}
	eventually(t, "the marker to clear on the server", func() bool {
		_, still := readDoc(t).tab(t, id)["touched"]
		return !still
	})

	// The case the flag is for: an agent rewrites the contents, the page shows
	// the new contents, and no dot comes back.
	d = readDoc(t)
	d.state(t, id)["text"] = "rewritten by an agent"
	apply(t, d)
	if err := expect.Locator(s.view(id).Locator(".notes-area")).ToHaveValue("rewritten by an agent"); err != nil {
		t.Fatalf("the agent's write never reached the page, so nothing below means anything: %v", err)
	}
	if err := expect.Locator(dot).ToBeHidden(); err != nil {
		t.Errorf("an agent write to a quiet tab raised the dot: %v", err)
	}
	if err := expect.Locator(banner).ToBeHidden(); err != nil {
		t.Errorf("an agent write to a quiet tab raised the banner: %v", err)
	}
	if _, marked := readDoc(t).tab(t, id)["touched"]; marked {
		t.Error("the server stamped a marker on a quiet tab's state change")
	}

	// The human turns it off and on again from the tab's right-click menu. Each
	// is the human's write — the server takes a human write as is and stamps
	// nothing — and each is visible in the title.
	toggle := func(label string, want bool) {
		t.Helper()
		if err := strip.Click(playwright.LocatorClickOptions{Button: playwright.MouseButtonRight}); err != nil {
			t.Fatalf("right-clicking the tab: %v", err)
		}
		item := s.page.Locator(".ctx-menu").GetByText(label, playwright.LocatorGetByTextOptions{Exact: new(false)})
		if err := item.Click(); err != nil {
			t.Fatalf("choosing %q: %v", label, err)
		}
		eventually(t, "quiet to become "+sprint(want)+" on the server", func() bool {
			got := readDoc(t)
			quiet, _ := got.tab(t, id)["quiet"].(bool)
			return quiet == want && got["lastEditedBy"] == "human"
		})
		eventually(t, "the tab's title to follow", func() bool {
			title, _ := strip.GetAttribute("title")
			return strings.Contains(title, "quiet:") == want
		})
		if _, marked := readDoc(t).tab(t, id)["touched"]; marked {
			t.Errorf("the human's %q stamped a marker", label)
		}
	}
	toggle("Announce agent changes again", false)
	toggle("Quiet: agent changes raise no dot", true)
}
