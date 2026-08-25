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
	if err := os.WriteFile(state, []byte(`{"version":3,"nextId":2,"tabs":[
	  {"id":"bb1","name":"Notes","type":"notes","state":{"text":"nothing tabular here"}}
	]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	err := Export(state, "bb1", "csv", &out, &errOut)
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
	if err := os.WriteFile(state, []byte(`{"version":3,"nextId":2,"tabs":[
	  {"id":"bb1","key":"decisions","name":"Decisions","type":"gate","state":{"pending":[],"decided":[
	    {"id":"bb2","title":"Ship it","verdict":"allow","reason":"because","at":"2026-08-25T00:00:00Z","by":"human"}
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
