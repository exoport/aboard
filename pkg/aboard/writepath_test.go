package aboard

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// The three features of the write-path cluster, tested where each of them could
// actually be wrong.
//
//	bb361  the warnings travel with the write, scoped to what it touched
//	bb362  apply --check / --strict
//	bb371  a write carries a label, and the journal keeps it

// A board with one ui tab, whose state is a component tree — the surface where a
// write fails silently and successfully, which is the whole reason these checks
// exist.
const uiBoard = `{"version":3,"rev":1,"nextId":9,"updatedAt":"T0","tabs":[
  {"id":"bb1","name":"Gallery","type":"ui","state":{"root":{"type":"text","value":"hello"}}},
  {"id":"bb2","name":"Plan","type":"notes","state":{"text":"one"}}
]}`

func postAs(t *testing.T, srv *server, envelope, doc string) map[string]any {
	t.Helper()
	rec := srv.postDocument(t, `{`+envelope+`,`+strings.TrimSpace(doc)[1:])
	if rec.Code != 200 {
		t.Fatalf("POST %d: %s", rec.Code, rec.Body)
	}
	var reply map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
		t.Fatalf("reply is not json: %v", err)
	}
	return reply
}

// The gap this closes, stated as the handoff states it: postState never called
// writeWarnings at all, so a browser write, a raw POST, or an `apply` whose
// stderr nobody read all produced an empty box that only the human ever found.
//
// Fails before with `warnings` absent from the reply and from the journal entry.
func TestAWriteThatCannotRenderSaysSoToTheWriterAndInTheJournal(t *testing.T) {
	srv := testServer(t, uiBoard)

	reply := postAs(t, srv, `"__by":"agent-1","__base":"1"`, `{"version":3,"nextId":9,"tabs":[
	  {"id":"bb1","name":"Gallery","type":"ui","state":{"root":{"type":"stat","value":"3","caption":"widgets"}}},
	  {"id":"bb2","name":"Plan","type":"notes","state":{"text":"one"}}
	]}`)

	warnings, ok := reply["warnings"].(map[string]any)
	if !ok {
		t.Fatalf("the POST reply carries no warnings: %v", reply)
	}
	lines, _ := warnings["bb1"].([]any)
	if len(lines) == 0 {
		t.Fatalf("no warning for the tab that was written: %v", warnings)
	}
	if first, _ := lines[0].(string); !strings.Contains(first, `does not read "caption"`) {
		t.Errorf("the warning does not name the prop: %q", first)
	}

	entries, err := journalFromDisk(srv.root, srv.name, 10)
	if err != nil {
		t.Fatal(err)
	}
	last := entries[len(entries)-1]
	if len(last.Warnings["bb1"]) == 0 {
		t.Errorf("the journal entry kept no warnings: %+v", last)
	}
	// And nowhere near the document: a note about a write is not content, and a
	// warning stored on the tab would be copied forward by the next write as
	// though it were still true.
	for _, tab := range srv.readTabs(t) {
		if strings.Contains(string(tab.State), "warning") {
			t.Errorf("a warning was written into %s's state: %s", tab.ID, tab.State)
		}
	}
}

// The scoping, which is the hazard the handoff names: the example board ships a
// deliberately invalid `sparkline`, so an unscoped walk would attach that warning
// to every write anyone ever made — and a warning that always fires is one people
// learn to skip.
//
// Fails before if postState is given writeWarnings(assets, wholeBody): bb1 warns
// on a write that only touched bb2.
func TestAWriteIsOnlyWarnedAboutTheTabsItTouched(t *testing.T) {
	broken := `{"version":3,"rev":1,"nextId":9,"updatedAt":"T0","tabs":[
	  {"id":"bb1","name":"Gallery","type":"ui","state":{"root":{"type":"stat","value":"3","caption":"widgets"}}},
	  {"id":"bb2","name":"Plan","type":"notes","state":{"text":"one"}}
	]}`
	srv := testServer(t, broken)

	before := warningScans.Load()
	reply := postAs(t, srv, `"__by":"agent-1","__base":"1"`, `{"version":3,"nextId":9,"tabs":[
	  {"id":"bb1","name":"Gallery","type":"ui","state":{"root":{"type":"stat","value":"3","caption":"widgets"}}},
	  {"id":"bb2","name":"Plan","type":"notes","state":{"text":"two"}}
	]}`)
	scanned := warningScans.Load() - before

	if _, warned := reply["warnings"]; warned {
		t.Errorf("a write that only touched bb2 was warned about bb1: %v", reply["warnings"])
	}
	if scanned != 1 {
		t.Errorf("the checker looked inside %d tabs; the write changed 1", scanned)
	}
}

// The other half of the same claim: scoping must not mean silence. A write that
// DOES touch the bad tab is warned about it, every time, and no suppression
// mechanism exists for it.
func TestTheDeliberatelyInvalidTabStillWarnsWhenItIsWrittenTo(t *testing.T) {
	broken := `{"version":3,"rev":1,"nextId":9,"updatedAt":"T0","tabs":[
	  {"id":"bb1","name":"Gallery","type":"ui","state":{"root":{"type":"col","children":[{"type":"sparkline","values":[1,2]}]}}}
	]}`
	srv := testServer(t, broken)

	reply := postAs(t, srv, `"__by":"agent-1","__base":"1"`, `{"version":3,"nextId":9,"tabs":[
	  {"id":"bb1","name":"Gallery","type":"ui","state":{"root":{"type":"col","children":[{"type":"sparkline","values":[1,2,3]}]}}}
	]}`)
	warnings, _ := reply["warnings"].(map[string]any)
	if len(warnings) == 0 {
		t.Fatal("the deliberately invalid component stopped warning on a write that touched it")
	}
}

// bb371: the label rides the envelope, is stripped like __by and __base, and
// lands on the journal entry.
//
// Fails before with `__label` written through into the document as a root key no
// renderer reads, and with an empty Label on the entry.
func TestAWriteLabelReachesTheJournalAndNotTheBoard(t *testing.T) {
	srv := testServer(t, uiBoard)

	postAs(t, srv, `"__by":"agent-1","__base":"1","__label":"rebuilding the gallery"`,
		`{"version":3,"nextId":9,"tabs":[
		  {"id":"bb1","name":"Gallery","type":"ui","state":{"root":{"type":"text","value":"hello"}}},
		  {"id":"bb2","name":"Plan","type":"notes","state":{"text":"two"}}
		]}`)

	entries, err := journalFromDisk(srv.root, srv.name, 10)
	if err != nil {
		t.Fatal(err)
	}
	last := entries[len(entries)-1]
	if last.Label != "rebuilding the gallery" {
		t.Errorf("the journal entry has no label: %+v", last)
	}
	if !strings.Contains(JournalHuman([]JournalEntry{last}), "rebuilding the gallery") {
		t.Errorf("`aboard journal` does not print the label:\n%s", JournalHuman([]JournalEntry{last}))
	}

	var doc map[string]json.RawMessage
	raw := readFileForTest(t, srv.stateFile)
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if _, leaked := doc["__label"]; leaked {
		t.Error("__label was written into the board document")
	}
}

// A label is one line of navigation in a rotating local file, and it arrives from
// a caller. Unbounded, it is a way to fill the journal with a single POST;
// multi-line, it breaks the one-entry-per-line shape `aboard journal` prints.
func TestALabelIsClampedRatherThanTrusted(t *testing.T) {
	if got := clampLabel("  fixing\n the\tgallery  "); got != "fixing the gallery" {
		t.Errorf("clampLabel(%q) = %q", "fixing\n the\tgallery", got)
	}
	long := clampLabel(strings.Repeat("x", 5000))
	if len([]rune(long)) != maxLabelRunes+1 {
		t.Errorf("a 5000-character label survived at %d runes", len([]rune(long)))
	}
	if !strings.HasSuffix(long, "…") {
		t.Errorf("the truncation does not say it happened: %q", long[len(long)-8:])
	}
}

/* ---------- bb362: apply --check and --strict ---------- */

const warningDoc = `{"version":3,"rev":1,"nextId":2,"tabs":[
  {"id":"bb1","name":"Gallery","type":"ui","state":{"root":{"type":"stat","value":"3","caption":"widgets"}}}]}`

const cleanDoc = `{"version":3,"rev":1,"nextId":2,"tabs":[
  {"id":"bb1","name":"Plan","type":"notes","state":{"text":"one"}}]}`

// --check is the cheap habit before a write: it says what is wrong and posts
// nothing. Fails before with "unknown flag: --check".
func TestApplyCheckReportsAndWritesNothing(t *testing.T) {
	root, last := applyTarget(t)

	out, errOut, err := runApplyWith(t, root, ApplyOptions{By: "agent-1", Check: true}, warningDoc)
	if err != nil {
		t.Fatalf("--check failed: %v", err)
	}
	if len(last.doc) != 0 {
		t.Errorf("--check posted the document: %v", last.doc)
	}
	if !strings.Contains(errOut, `does not read "caption"`) {
		t.Errorf("--check did not report the warning; stderr was:\n%s", errOut)
	}
	// It says what it did even when it found nothing: a command that prints
	// nothing on success is a command people stop believing they ran, and the
	// failure mode this flag is designed against is a session that never runs it.
	if !strings.Contains(out, "nothing was written") {
		t.Errorf("--check did not say it wrote nothing: %q", out)
	}

	out, _, err = runApplyWith(t, root, ApplyOptions{By: "agent-1", Check: true}, cleanDoc)
	if err != nil {
		t.Fatalf("--check on a clean document failed: %v", err)
	}
	if !strings.Contains(out, "no warnings") {
		t.Errorf("--check said nothing about a clean document: %q", out)
	}
}

// A document being built up has no `rev` yet, and refusing to CHECK it would
// make the cheap habit unavailable exactly where it is cheapest. --check answers
// a question about content, not about concurrency.
func TestApplyCheckNeedsNoBaseAndNoBoard(t *testing.T) {
	root := Root(t.TempDir())

	out, _, err := runApplyWith(t, root, ApplyOptions{By: "agent-1", Check: true},
		`{"tabs":[{"id":"bb1","name":"Plan","type":"notes","state":{"text":"one"}}]}`)
	if err != nil {
		t.Fatalf("--check on a base-less document with no board running failed: %v", err)
	}
	if !strings.Contains(out, "no warnings") {
		t.Errorf("--check did not answer: %q", out)
	}
}

// --strict is the guard for a loop that must stop rather than ship a wrong tab.
// Refused before the board is contacted, so "nothing was written" needs no
// qualification.
func TestApplyStrictRefusesAWarningDocumentAndWritesNothing(t *testing.T) {
	root, last := applyTarget(t)

	_, errOut, err := runApplyWith(t, root, ApplyOptions{By: "agent-1", Strict: true}, warningDoc)
	if err == nil {
		t.Fatal("--strict applied a document that warns")
	}
	if !errorIsWarnings(err) {
		t.Errorf("the refusal is not the typed one: %v", err)
	}
	if len(last.doc) != 0 {
		t.Errorf("--strict wrote anyway: %v", last.doc)
	}
	if !strings.Contains(errOut, `does not read "caption"`) {
		t.Errorf("--strict refused without saying why:\n%s", errOut)
	}

	// And it does not turn a clean write into a refusal, which is the whole
	// argument for it being opt-in per call rather than a change of default.
	if _, _, err := runApplyWith(t, root, ApplyOptions{By: "agent-1", Strict: true}, cleanDoc); err != nil {
		t.Errorf("--strict refused a clean document: %v", err)
	}
}

func errorIsWarnings(err error) bool { return err != nil && strings.Contains(err.Error(), "--strict") }

// bb371 end to end through the client: the label reaches the envelope, and only
// when there is one.
func TestApplySendsTheLabelOnlyWhenGiven(t *testing.T) {
	root, last := applyTarget(t)

	if _, _, err := runApplyWith(t, root, ApplyOptions{By: "agent-1", Label: "  ship the queue  "}, cleanDoc); err != nil {
		t.Fatal(err)
	}
	if got := last.doc["__label"]; got != "ship the queue" {
		t.Errorf("__label = %v, want the trimmed label", got)
	}

	if _, _, err := runApplyWith(t, root, ApplyOptions{By: "agent-1"}, cleanDoc); err != nil {
		t.Fatal(err)
	}
	if _, sent := last.doc["__label"]; sent {
		t.Errorf("an empty label was sent as a field: %v", last.doc["__label"])
	}
}

// The counting claim, in the shape document_test.go makes it: the warning
// checker's cost is the EDIT, not the board. Fails before at N per write.
func TestTheWarningCheckerScalesWithTheEditNotTheBoard(t *testing.T) {
	scans := map[int]int64{}
	for _, n := range []int{15, 500} {
		doc := manyTabs(n)
		srv := testServer(t, doc)
		postOK(t, srv, editOneTab(doc, 1, "first"))

		before := warningScans.Load()
		postOK(t, srv, editOneTab(strings.Replace(doc, `"text":"body 1"`, `"text":"first"`, 1), 2, "second"))
		scans[n] = warningScans.Load() - before
	}
	if scans[15] != scans[500] || scans[15] > 1 {
		t.Errorf("one edit scanned %d tabs on a board of 15 and %d on a board of 500, want 1 and 1",
			scans[15], scans[500])
	}
}

func readFileForTest(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// The other half of the banner, and the half that is easy to miss: a warning has
// to be able to come DOWN.
//
// The banner's own sentence is "the last write to this tab set something no
// renderer reads", so a later write that fixed the tree must clear it. The frame
// and the reply carry `warnings` for the tabs that warned and `checked` for every
// tab the checks ran over — and only the second one can lower a banner, because a
// clean tab is simply ABSENT from the first, which is the same shape as a tab
// this write never looked at.
//
// Fails before: with `checked` absent from the reply, the shell has nothing to
// distinguish "checked and clean" from "not checked", so the human keeps a
// warning about a tree the agent has already repaired — the disagreement between
// the two of them that this whole feature exists to end, pointing the other way.
func TestAWriteNamesTheTabsItsChecksRanOver(t *testing.T) {
	srv := testServer(t, uiBoard)

	broke := postAs(t, srv, `"__by":"agent-1","__base":"1"`, `{"version":3,"nextId":9,"tabs":[
	  {"id":"bb1","name":"Gallery","type":"ui","state":{"root":{"type":"stat","value":"3","caption":"widgets"}}},
	  {"id":"bb2","name":"Plan","type":"notes","state":{"text":"one"}}
	]}`)
	if got := checkedTabs(t, broke); !slices.Contains(got, "bb1") {
		t.Fatalf("the write that broke bb1 does not say it checked it: %v", got)
	}

	fixed := postAs(t, srv, `"__by":"agent-1","__base":"2"`, `{"version":3,"nextId":9,"tabs":[
	  {"id":"bb1","name":"Gallery","type":"ui","state":{"root":{"type":"stat","value":"3","label":"widgets"}}},
	  {"id":"bb2","name":"Plan","type":"notes","state":{"text":"one"}}
	]}`)
	if _, warned := fixed["warnings"]; warned {
		t.Fatalf("the repaired tree still warns: %v", fixed["warnings"])
	}
	if got := checkedTabs(t, fixed); !slices.Contains(got, "bb1") {
		t.Errorf("the write that repaired bb1 does not say it checked it, so nothing can take the banner down: %v", got)
	}
}

func checkedTabs(t *testing.T, reply map[string]any) []string {
	t.Helper()
	raw, ok := reply["checked"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, id := range raw {
		s, _ := id.(string)
		out = append(out, s)
	}
	return out
}
