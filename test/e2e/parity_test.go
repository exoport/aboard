//go:build e2e

package e2e

import (
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mxschmitt/playwright-go"
)

// What test/smoke.sh checked in the browser, checked here instead.
//
// The equivalence table lives in docs/how-to/run-the-browser-suite.md, not in a
// comment: the point of retiring the shell suite was that every one of its
// checks has somewhere to go, and a reader deciding whether that is true needs
// the table, not this file.
//
// The three things this file does that smoke.html could not: it mounts every tab
// IN THE REAL SHELL rather than in a harness page that reimplements the mount
// contract; it reads TYPES by calling it, instead of regexing the shell's source
// and evaluating the match in a Node vm; and it fails on a page error from any
// tab, rather than on a string that the page's own source also contains.

// Every tab in the board activates, mounts a renderer, and produces its
// characteristic output.
//
// The counts are THRESHOLDS, not exact numbers, and deliberately: they come from
// the board the suite is aimed at, so an exact table would fail the moment
// somebody added a card. What is being asserted is "this renderer produced its
// characteristic output at all", and 0-versus-1 is the whole distance between a
// renderer that works and one that mounted an empty box. Nine of these printed a
// bare number in the old harness and were read by nothing until 2026-08-26.
func TestEveryTabActivatesAndRendersItsOwnOutput(t *testing.T) {
	s := open(t, "")

	// What a mounted renderer of each type must have produced. Keyed by type
	// rather than by tab, so it survives a tab being renamed or removed.
	want := map[string]string{
		"diagram": "svg",
		"dag":     ".node-box",
		"kanban":  ".card",
		"form":    "input, select, textarea",
		"markup":  "svg",
		"chat":    "textarea",
		"notes":   "textarea",
		"html":    "iframe[sandbox]",
		"stack":   ".block",
		"table":   "tbody tr",
		"gate":    ".gate-list, .decided",
		"log":     ".log-line, .log-empty",
		"trace":   ".lane, .empty",
		"vote":    "tbody tr, .vote-empty",
		"ui":      ".uic-col, .uic-card",
	}

	tabs := allTabs(t)
	if len(tabs) < len(seededTabs) {
		t.Fatalf("only %d tabs on the board; it was seeded with %d", len(tabs), len(seededTabs))
	}
	seen := map[string]bool{}

	for _, tab := range tabs {
		id, _ := tab["id"].(string)
		kind, _ := tab["type"].(string)
		name, _ := tab["name"].(string)

		view := s.tab(id)
		if err := expect.Locator(view).ToHaveAttribute("data-view", kind); err != nil {
			t.Errorf("%s (%q) mounted as something other than a %s: %v", id, name, kind, err)
			continue
		}
		seen[kind] = true

		selector, ok := want[kind]
		if !ok {
			t.Errorf("%s is a %q tab and this test has no expectation for that type — add one", id, kind)
			continue
		}
		// Characteristic output is asserted only for the tabs the board was
		// SEEDED with. A tab another test made through the new-tab dialog starts
		// empty by design — a markup tab with no images has no <svg> — and
		// demanding output from it would make this test's result depend on
		// whether it ran before or after that one.
		if !seededTabs[id] {
			continue
		}
		// An auto-retrying assertion, not a Count(): `diagram` renders through
		// mermaid, which is loaded and run asynchronously, so a synchronous count
		// taken the instant the tab activates is zero on a renderer that works
		// perfectly. Measured — that is what this test did first time.
		if err := expect.Locator(view.Locator(selector).First()).ToBeAttached(); err != nil {
			t.Errorf("%s (%q, %s) mounted and produced none of its characteristic output (%s): %v",
				id, name, kind, selector, err)
		}
	}

	// A renderer with no tab on this board is a renderer nothing here tested.
	// Reported rather than failed: which types the example seeds is the example's
	// business, and it already seeds one of each.
	var missing []string
	for kind := range want {
		if !seen[kind] {
			missing = append(missing, kind)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("no tab on this board uses: %s — the example seeds one per renderer", strings.Join(missing, " "))
	}

	// Nothing threw on the way through. The old harness matched on strings in a
	// dumped DOM and had to exclude the page's own source, which built the same
	// messages; a real console has no such problem.
	if bad := s.consoleErrors(); len(bad) > 0 {
		t.Errorf("mounting every tab produced %d console error(s):\n  %s", len(bad), strings.Join(bad, "\n  "))
	}
}

// Every control on screen resolves to a declaration. The static half — every
// declared control is used, every button goes through the helper — is in Go
// (pkg/aboard/controls_test.go). This is the half a grep cannot do: an id could
// be built at runtime, and an undeclared one renders "?id" with data-undeclared
// rather than a blank button.
func TestEveryRenderedControlResolvesToADeclaration(t *testing.T) {
	s := open(t, "")

	drawn := 0
	var undeclared []string
	for _, tab := range allTabs(t) {
		id, _ := tab["id"].(string)
		view := s.tab(id)

		n, err := view.Locator("[data-gesture]").Count()
		if err != nil {
			t.Fatal(err)
		}
		drawn += n

		bad, err := view.Locator("[data-undeclared]").All()
		if err != nil {
			t.Fatal(err)
		}
		for _, el := range bad {
			gesture, _ := el.GetAttribute("data-gesture")
			kind, _ := tab["type"].(string)
			undeclared = append(undeclared, kind+"."+gesture+" (on "+id+")")
		}
	}

	if len(undeclared) > 0 {
		t.Errorf("controls rendered with no declaration: %s", strings.Join(undeclared, ", "))
	}
	if drawn == 0 {
		t.Error("no declared controls rendered anywhere — controls.generated.js is probably not loading")
	}
	t.Logf("%d declared controls rendered across %d tabs", drawn, len(allTabs(t)))
}

// The shell's renderer registry and the specs must agree on two things: WHICH
// types exist, and what a new tab of each STARTS with.
//
// The old version of this regexed `const TYPES = {…}` out of aboard.html and
// evaluated each `init:` arrow in a Node vm. This calls the function. The seam
// is `?probe=1`, which now exposes TYPES for exactly this.
//
// KEY SETS, not values: a spec's `init` is an illustration (html's body text,
// ui's example child), so asserting the prose would be asserting the wrong
// thing. Which keys a new tab starts with is the contract — markup's shell init
// returned four keys markup.js has never read while its spec declared two, and
// the renderer repaired it on mount, which is exactly why it survived.
func TestTypesInitMatchesTheSpecs(t *testing.T) {
	s := open(t, "probe=1")

	var mounted map[string][]string
	s.evalJSON(&mounted, `() => {
      const out = {};
      for (const [type, entry] of Object.entries(window.__aboardProbe.types)) {
        out[type] = Object.keys(entry.init ? entry.init() : {}).sort();
      }
      return out;
    }`)
	if len(mounted) == 0 {
		t.Fatal("the probe seam exposed no TYPES")
	}

	var manifest struct {
		Types []struct {
			Type string         `json:"type"`
			Init map[string]any `json:"init"`
		} `json:"types"`
	}
	getJSON(t, "/capabilities", &manifest)
	if len(manifest.Types) == 0 {
		t.Fatal("/capabilities declares no types")
	}

	declared := map[string][]string{}
	for _, spec := range manifest.Types {
		declared[spec.Type] = sortedKeys(spec.Init)
	}

	for kind := range mounted {
		if _, ok := declared[kind]; !ok {
			t.Errorf("the shell mounts %q and no views/%s.spec.json declares it — no agent will ever learn it exists", kind, kind)
		}
	}
	for kind := range declared {
		if _, ok := mounted[kind]; !ok {
			t.Errorf("%q is declared and TYPES does not mount it — a tab of that type renders nothing", kind)
		}
	}
	for kind, got := range mounted {
		wantKeys, ok := declared[kind]
		if !ok {
			continue
		}
		if strings.Join(got, ",") != strings.Join(wantKeys, ",") {
			t.Errorf("a new %s tab starts with {%s} and views/%s.spec.json declares {%s}",
				kind, strings.Join(got, ","), kind, strings.Join(wantKeys, ","))
		}
	}
}

// `kv` is the one display component that ever resolved its container but not its
// contents: it called String() on each key and value where every sibling calls
// asText(), so a {bind} rendered "[object Object]" — in the component whose
// entire job is "label: value", and which a live summary is a main reason to
// reach for `ui` at all.
//
// It survived because the gallery's kv used literal values. A component
// demonstrated only in the case that works is untested, so the gallery has both
// and this asserts the bound one.
func TestAKvComponentResolvesABind(t *testing.T) {
	s := open(t, "tab=bb133")
	openGalleryPanel(t, s, "Data")
	view := s.view("bb133")

	// The bound kv is the one whose first key names a field of state.data.
	bound := view.Locator(".uic-kv").Filter(playwright.LocatorFilterOptions{
		HasText: "text you typed",
	}).First()
	if err := expect.Locator(bound).ToBeVisible(); err != nil {
		t.Fatalf("the gallery has no kv with a bound value: %v", err)
	}

	text, err := bound.TextContent()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "[object Object]") {
		t.Errorf("kv rendered a {bind} as an object: %q", text)
	}

	// And it resolves the real value, not just "something". Written through the
	// document so the assertion is about resolution rather than about typing.
	written := "resolved by the browser suite"
	d := readDoc(t)
	data, _ := d.state(t, "bb133")["data"].(map[string]any)
	demo, _ := data["demo"].(map[string]any)
	demo["text"] = written
	apply(t, d)

	if err := expect.Locator(bound).ToContainText(written); err != nil {
		t.Errorf("kv did not resolve demo.text: %v", err)
	}
	// The literal side must still work — the change that broke the bound case
	// would have been caught by nothing if the literal one had been the only test.
	literal := view.Locator(".uic-kv").Filter(playwright.LocatorFilterOptions{HasText: "diagram"}).First()
	if err := expect.Locator(literal).ToContainText("shape asserted"); err != nil {
		t.Errorf("a literal kv pair stopped rendering: %v", err)
	}
}

// A deliberately-invalid node renders a VISIBLE marker rather than nothing.
//
// `bb133`'s "Unknown" panel contains a `sparkline` on purpose, and every write
// touching that tab warns about it — that is the checker working, not a defect
// to fix by deleting the demonstration. What the browser has to do with it is
// show something: an unknown component type shows a marker, and an unknown PROP
// shows nothing at all, which is why `apply` succeeding is not evidence that
// anything rendered.
func TestAnUnknownUiComponentRendersAMarker(t *testing.T) {
	s := open(t, "tab=bb133")
	openGalleryPanel(t, s, "Unknown")

	if err := expect.Locator(s.view("bb133").Locator(".uic-unknown").First()).ToBeVisible(); err != nil {
		t.Fatalf("an unknown component drew nothing at all: %v", err)
	}
	if err := expect.Locator(s.view("bb133").Locator(".uic-unknown").First()).ToContainText("sparkline"); err != nil {
		t.Errorf("the marker does not name the component nobody implemented: %v", err)
	}
}

// The journal records the write that just happened — the right author, the right
// tab. Counting entries alone passed on history: a journal file left from an
// earlier session satisfied ">= 1" without the write path being exercised at all.
func TestTheJournalRecordsTheWriteThatJustHappened(t *testing.T) {
	s := open(t, "tab=bb202")

	written := "journalled at " + strconv.Itoa(len(readDoc(t)))
	if err := s.view("bb202").Locator(".notes-area").Fill(written); err != nil {
		t.Fatalf("typing: %v", err)
	}
	eventually(t, "the edit to reach the server", func() bool {
		text, _ := readDoc(t).state(t, "bb202")["text"].(string)
		return text == written
	})

	var body struct {
		Entries []struct {
			By   string   `json:"by"`
			Tabs []string `json:"tabs"`
		} `json:"entries"`
	}
	getJSON(t, "/journal?limit=5", &body)
	if len(body.Entries) == 0 {
		t.Fatal("the journal is empty after a write")
	}
	last := body.Entries[len(body.Entries)-1]
	if last.By != "human" {
		t.Errorf("the newest journal entry is by %q, want the human who typed it", last.By)
	}
	found := false
	for _, id := range last.Tabs {
		if id == "bb202" {
			found = true
		}
	}
	if !found {
		t.Errorf("the newest journal entry names %v, not the tab that changed", last.Tabs)
	}
}

/* ---------- helpers ---------- */

func allTabs(t *testing.T) []map[string]any {
	t.Helper()
	raw, _ := readDoc(t)["tabs"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, entry := range raw {
		if tab, ok := entry.(map[string]any); ok {
			out = append(out, tab)
		}
	}
	return out
}

// consoleErrors is what the page complained about, filtered to the lines that
// are actually defects. A failed request at teardown (the SSE stream being cut)
// is normal and would otherwise fail every test that reads this.
func (s *session) consoleErrors() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, line := range s.console {
		if strings.HasPrefix(line, "console.error") || strings.HasPrefix(line, "pageerror") {
			out = append(out, line)
		}
	}
	return out
}
