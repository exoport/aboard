package aboard

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The CSV refusal used to hand the reader `-format md`, which is the spike's
// single-dash grammar and does not exist here. A message that offers a flag the
// binary rejects costs a round trip to discover the tool was wrong.
func TestCSVRefusalOffersAFlagThatExists(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "aboard.json")
	if err := os.WriteFile(state, []byte(`{"version":1,"nextId":2,"tabs":[
	  {"id":"ab1","name":"Notes","type":"notes","state":{"text":"nothing tabular here"}}
	]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	err := Export(state, "ab1", "csv", &out, &errOut)
	if err == nil {
		t.Fatal("a notes tab produced a csv")
	}
	if strings.Contains(err.Error(), " -format") {
		t.Errorf("the message offers the spike's single-dash grammar: %v", err)
	}
	if !strings.Contains(err.Error(), "--format md") {
		t.Errorf("the message does not offer the flag that works: %v", err)
	}
}

// Export reads the document from DISK, so a conclusion can be promoted with no
// server running — the same property `capabilities` has, and the reason late
// promotion is cheap enough to be the default.
func TestExportNeedsNoServer(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "aboard.json")
	if err := os.WriteFile(state, []byte(`{"version":1,"nextId":2,"tabs":[
	  {"id":"ab1","key":"decisions","name":"Decisions","type":"gate","state":{"pending":[],"decided":[
	    {"id":"ab2","title":"Ship it","verdict":"allow","reason":"because","at":"2026-08-25T00:00:00Z","by":"human"}
	  ]}}
	]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if err := Export(state, "decisions", "md", &out, &errOut); err != nil {
		t.Fatal(err)
	}
	// The reason is the whole point of a gate export: a verdict with no reason
	// is a decision nobody can act on later.
	if !strings.Contains(out.String(), "Why: because") {
		t.Errorf("the export carries no reason:\n%s", out.String())
	}
}

// A tab of rows exports as CSV, headed by the id column. Carried over from
// test/smoke.sh, where it was one of four export checks that ran only locally.
func TestATableExportsAsCSVHeadedByItsIds(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "aboard.json")
	if err := os.WriteFile(state, []byte(`{"version":1,"nextId":5,"tabs":[
	  {"id":"ab1","key":"table-example","name":"Rows","type":"table","state":{
	    "columns":[{"id":"thing","label":"cell type","type":"text"},{"id":"count","label":"number","type":"number"}],
	    "rows":[{"id":"ab2","thing":"text","count":1},{"id":"ab3","thing":"number","count":42}]}}
	]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if err := Export(state, "table-example", "csv", &out, &errOut); err != nil {
		t.Fatal(err)
	}
	first, _, _ := strings.Cut(out.String(), "\n")
	if !strings.HasPrefix(first, "id") {
		t.Errorf("the CSV is not headed by the id column: %q", first)
	}
	if !strings.Contains(out.String(), "ab3") {
		t.Errorf("a row is missing from the CSV:\n%s", out.String())
	}
}

// An unknown tab is REFUSED, not silently empty. An export that prints nothing
// and exits 0 gets pasted into a document as a blank section.
func TestExportRefusesAnUnknownTab(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "aboard.json")
	if err := os.WriteFile(state, []byte(`{"version":1,"nextId":2,"tabs":[
	  {"id":"ab1","name":"Notes","type":"notes","state":{"text":"hello"}}
	]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	err := Export(state, "definitely-not-a-tab", "md", &out, &errOut)
	if err == nil {
		t.Fatal("exporting a tab that does not exist succeeded")
	}
	if out.Len() > 0 {
		t.Errorf("the refusal still wrote %d bytes to stdout", out.Len())
	}
}

// A markdown export leads with a heading, so it can be pasted into a document
// without being re-formatted first — the property that makes promotion cheap.
func TestAMarkdownExportLeadsWithAHeading(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "aboard.json")
	if err := os.WriteFile(state, []byte(`{"version":1,"nextId":2,"tabs":[
	  {"id":"ab1","key":"decisions","name":"Decisions","type":"gate","state":{"pending":[],"decided":[]}}
	]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if err := Export(state, "decisions", "md", &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "#") {
		t.Errorf("the export does not lead with a heading:\n%s", out.String())
	}
}
