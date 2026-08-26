package aboard

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A tree using every shape the outline has an opinion about: layout nodes that
// contribute no line, text that comes from `value` and from `text`, a two-part
// node, a {bind} read, a field whose answer lives in state.data, each of the
// four item-bearing components, and a component the catalog does not have.
const uiTabDocument = `{"version":3,"rev":1,"nextId":40,"tabs":[
  {"id":"bb1","key":"panel","name":"Release","type":"ui","note":"What the outline has to carry.","state":{
    "data":{"count":25,"who":"agent-1","answers":{"name":"Diego"},"ticks":{"one":true}},
    "root":{"type":"col","children":[
      {"type":"card","title":"Today","children":[
        {"type":"row","children":[
          {"type":"stat","value":{"bind":"count"},"label":"shipped"},
          {"type":"meter","value":77,"max":100}
        ]},
        {"type":"text","value":"Two of the three are done."},
        {"type":"caption","text":"caption uses text, not value."},
        {"type":"notice","label":"Careful","value":"Nothing here executes."},
        {"type":"quote","value":"Prefer ui over html.","by":"CLAUDE.md"},
        {"type":"divider"},
        {"type":"list","items":["one","two"]},
        {"type":"kv","pairs":[{"key":"who","value":{"bind":"who"}},{"key":"missing","value":{"bind":"nope"}}]},
        {"type":"checklist","items":[
          {"label":"ticked","bind":"ticks.one"},
          {"label":"not ticked","bind":"ticks.two"},
          {"label":"no bind at all","done":true}
        ]},
        {"type":"table","columns":[{"id":"a","label":"Component"},{"id":"b","label":"Use"}],
         "rows":[{"a":"ui","b":"ordinary layout"},["html","interaction"]]},
        {"type":"field","label":"Your name","field":"text","bind":"answers.name"},
        {"type":"field","label":"Unanswered","field":"text","bind":"answers.role"},
        {"type":"button","label":"Ship it","intent":"cut the release"},
        {"type":"tabs","panels":[{"label":"First","children":[{"type":"text","value":"inside a panel"}]}]},
        {"type":"sparkline","values":[1,2,3]},
        "a bare string is a paragraph"
      ]}
    ]}
  }}
]}`

// The golden outline. Written out in full rather than asserted piecemeal,
// because the thing under test is the SHAPE — what is indented under what, which
// nodes get a line at all — and a handful of Contains checks would pass on an
// outline that had lost its nesting entirely.
const uiGolden = `# Release

What the outline has to carry.

- card: Today
  - stat: 25 · shipped
  - meter: 77 · 100
  - text: Two of the three are done.
  - caption: caption uses text, not value.
  - notice: Careful · Nothing here executes.
  - quote: Prefer ui over html. · CLAUDE.md
  - list
    - one
    - two
  - kv
    - who: agent-1
    - missing:
  - checklist
    - [x] ticked
    - [ ] not ticked
    - [x] no bind at all
  - table
    - **Component** · **Use**
    - ui · ordinary layout
    - html · interaction
  - field: Your name · Diego
  - field: Unanswered
  - button: Ship it · cut the release
  - tabs
    - panel: First
      - text: inside a panel
  - _unknown component ` + "`sparkline`" + `_
  - a bare string is a paragraph
`

func TestAUITreeExportsAsAnOutline(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "aboard.json")
	if err := os.WriteFile(state, []byte(uiTabDocument), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if err := Export(state, "panel", "md", &out, &errOut); err != nil {
		t.Fatalf("%v: %s", err, errOut.String())
	}
	if got := out.String(); got != uiGolden {
		t.Errorf("the outline is not what it should be:\n--- got ---\n%s\n--- want ---\n%s", got, uiGolden)
	}
}

// The type CLAUDE.md tells agents to PREFER was the one type export could not
// read. The gallery is the real tree, and "look at it instead" is what a tab with
// no text form gets — so seeing that sentence here means the ui case is gone.
func TestTheUIGalleryExportsSomethingRatherThanNothing(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := Export("example/aboard.json", "bb133", "md", &out, &errOut); err != nil {
		t.Fatalf("%v: %s", err, errOut.String())
	}
	body := out.String()
	if strings.Contains(body, "no useful text form") {
		t.Fatalf("the ui gallery still exports as nothing:\n%s", body)
	}
	for _, want := range []string{
		"- stat: 25 · components", // a literal
		"- stat: 25 · from state.data",
		"- field: select · medium", // a field's answer, out of state.data
		"- panel: Layout",          // a tabs panel, and its children under it
		"- [ ] tick these — they persist",
		"_unknown component `sparkline`_", // the deliberate one, reported not hidden
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the gallery outline is missing %q", want)
		}
	}
}

// `log`, `html` and `trace` have no text form and say so. Asserted because they
// are now explicit cases: a `case` that returns "" and a missing case behave
// identically, so nothing but a test stops someone "tidying" one away.
func TestTheTypesWithNoTextFormSaySo(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "aboard.json")
	if err := os.WriteFile(state, []byte(`{"version":3,"rev":1,"nextId":9,"tabs":[
	  {"id":"bb1","name":"Output","type":"log","state":{}},
	  {"id":"bb2","name":"Widget","type":"html","state":{"html":"<p>hi</p>"}},
	  {"id":"bb3","name":"Trace","type":"trace","state":{}}
	]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"bb1", "bb2", "bb3"} {
		var out, errOut bytes.Buffer
		if err := Export(state, id, "md", &out, &errOut); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "look at it instead") {
			t.Errorf("%s does not say it has no text form:\n%s", id, out.String())
		}
	}
}

// A markdown table's delimiter row must have exactly as many cells as its
// header. It had one more — "|---|---||" — which some renderers tolerate and a
// strict one refuses outright, printing five lines of literal pipes into
// whatever document the tab was promoted into.
func TestAnExportedTableHasAWellFormedDelimiterRow(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "aboard.json")
	if err := os.WriteFile(state, []byte(`{"version":3,"rev":1,"nextId":5,"tabs":[
	  {"id":"bb1","key":"rows","name":"Rows","type":"table","state":{
	    "columns":[{"id":"thing","label":"cell type"},{"id":"count","label":"number"}],
	    "rows":[{"id":"bb2","thing":"text","count":1}]}}
	]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if err := Export(state, "rows", "md", &out, &errOut); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var header, delimiter string
	for i, line := range lines {
		if strings.HasPrefix(line, "| ") && i+1 < len(lines) && strings.HasPrefix(lines[i+1], "|---") {
			header, delimiter = line, lines[i+1]
			break
		}
	}
	if header == "" {
		t.Fatalf("no table in the export:\n%s", out.String())
	}
	if got, want := strings.Count(delimiter, "|"), strings.Count(header, "|"); got != want {
		t.Errorf("the delimiter row has %d pipes and the header has %d:\n%s\n%s", got, want, header, delimiter)
	}
}
