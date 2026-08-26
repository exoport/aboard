package aboard

// journal_generations_test.go — the journal record has two generations, and
// every reader of it has to handle both.
//
// Generation 1 recorded a tab's bare `state` and carried no `schema` key at all.
// Generation 2 stamps `schema: 2` and records the whole tab, which is what lets
// `apply`'s 409 merge tell a foreign rename from one of its own.
//
// The mixed case is not hypothetical and is not a migration problem to be solved
// once: rotation keeps one older generation, so `journal.jsonl.1` can hold
// generation-1 lines for as long as the board lives while the live file holds
// generation-2 ones, and `/journal` concatenates them. Every reader dispatches
// per ENTRY, never per file — these tests are what says so.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// What this build writes. The stamp and the widened record are one change: a
// reader trusts `schema` to decide how to read `before`, so an entry that
// recorded a tab without saying so would be read as a state blob and would
// silently compare as garbage.
func TestTheWritePathRecordsTheWholeTabAndStampsTheSchema(t *testing.T) {
	srv := testServer(t, mergeBoard)

	doc := readBoard(t, srv.root)
	setTabText(t, doc, "bb1", "moved")
	doc["__by"] = "agent-1"
	doc["__base"] = revToken(t, doc["rev"])
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if rec := srv.postDocument(t, string(body)); rec.Code != http.StatusOK {
		t.Fatalf("the write answered %d: %s", rec.Code, rec.Body.String())
	}

	entries, err := journalFromDisk(srv.root, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("want one journal entry, got %d", len(entries))
	}
	e := &entries[0]
	if e.Schema != journalSchema {
		t.Errorf("the entry is stamped schema %d, want %d", e.Schema, journalSchema)
	}

	was, ok := e.recorded("bb1")
	if !ok {
		t.Fatal("the record holds nothing for the tab the write changed")
	}
	if !was.Fields {
		t.Fatal("a schema-2 record must carry the tab's own fields")
	}
	// The four fields the merge needs, and the state it always had.
	if was.Name != "Mine" || was.Type != "notes" {
		t.Errorf("the record lost the tab's identity: name %q type %q", was.Name, was.Type)
	}
	if !strings.Contains(string(was.State), "base") {
		t.Errorf("the record lost the state it replaced: %s", was.State)
	}
	if len(was.Whole) == 0 {
		t.Error("the record must keep the whole tab, for a restore that puts the name back")
	}
}

// A tab that existed with NO state is recorded too, and that is a fix rather
// than a detail. `before[<id>]` being present is how the merge tells a tab that
// was REPLACED from one that was CREATED; recording only tabs with a non-empty
// state made those two look identical, so a stateless tab moved on the board came
// back to `apply` as "created while you were writing" and refused a write that
// should have merged.
func TestATabWithNoStateIsStillRecordedAsHavingExisted(t *testing.T) {
	srv := testServer(t, `{"version":3,"rev":1,"nextId":9,"tabs":[
		{"id":"bb1","name":"Empty","type":"notes"}
	]}`)

	doc := readBoard(t, srv.root)
	setTabText(t, doc, "bb1", "now it has one")
	doc["__by"] = "agent-1"
	doc["__base"] = revToken(t, doc["rev"])
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if rec := srv.postDocument(t, string(body)); rec.Code != http.StatusOK {
		t.Fatalf("the write answered %d: %s", rec.Code, rec.Body.String())
	}

	entries, err := journalFromDisk(srv.root, 10)
	if err != nil {
		t.Fatal(err)
	}
	was, ok := entries[0].recorded("bb1")
	if !ok {
		t.Fatal("a tab with no state must still be recorded as having existed")
	}
	if len(was.State) != 0 {
		t.Errorf("it had no state, so the record must not invent one: %s", was.State)
	}
	if was.Name != "Empty" {
		t.Errorf("the record lost the name of a stateless tab: %q", was.Name)
	}
}

// The generation-1 line, hand-written exactly as it sits on disk in a journal
// nobody has rotated yet, read by every reader that touches `before`.
//
// Hand-written and not produced, because there is no code left that can produce
// one — which is the whole reason this has to be pinned rather than assumed.
func TestAPreSchemaEntryIsReadAsABareState(t *testing.T) {
	const line = `{"at":"2026-08-25T10:00:00.000Z","by":"agent-1","rev":6,` +
		`"tabs":["bb1"],"names":{"bb1":"Plan renamed"},"before":{"bb1":{"v":1}}}`

	var e JournalEntry
	if err := json.Unmarshal([]byte(line), &e); err != nil {
		t.Fatalf("the shape already on disk must still decode: %v", err)
	}
	if e.Schema != 0 {
		t.Fatalf("a generation-1 line carries no schema key; got %d", e.Schema)
	}

	// recorded(): the state comes back as-is and nothing claims to know a name.
	was, ok := e.recorded("bb1")
	if !ok {
		t.Fatal("the record holds a state for bb1 and the reader must find it")
	}
	if was.Fields {
		t.Error("a generation-1 record cannot answer for name/type/note — Fields must stay false")
	}
	if string(was.State) != `{"v":1}` {
		t.Errorf("the bare state was mangled: %s", was.State)
	}
	if was.Name != "" || len(was.Whole) != 0 {
		t.Errorf("nothing may be invented from a narrow record: name %q whole %s", was.Name, was.Whole)
	}

	// historyFrom(): one listable version, marked as the older generation so a
	// consumer knows a restore of it will not carry a name.
	got := historyFrom([]JournalEntry{e}, "bb1", 0)
	if len(got.Versions) != 1 {
		t.Fatalf("want one version off a generation-1 entry, got %d", len(got.Versions))
	}
	v := got.Versions[0]
	if v.Schema != 0 || v.Was != "" || len(v.Tab) != 0 {
		t.Errorf("a version off a narrow record must say so: %+v", v)
	}
	if string(v.State) != `{"v":1}` || v.Bytes != len(`{"v":1}`) {
		t.Errorf("the version lost the state: %s (%d bytes)", v.State, v.Bytes)
	}
	// `names` is what the tab was called AFTER the write, and it stays that.
	if v.Name != "Plan renamed" {
		t.Errorf("v.Name = %q, want the post-write name the entry recorded", v.Name)
	}

	// The printers.
	if out := JournalHuman([]JournalEntry{e}); !strings.Contains(out, "bb1 (Plan renamed)") {
		t.Errorf("the journal listing does not print a generation-1 entry: %q", out)
	}
	if out := got.Human(); !strings.Contains(out, "replaced by agent-1") {
		t.Errorf("the history listing does not print a generation-1 version: %q", out)
	}

	// Restore(): the state moves and nothing else does. A narrow record has no
	// name to put back, and inventing "" would blank the tab's name.
	root := Root(t.TempDir())
	writeBoardFile(t, root)
	journalWith(t, root, e)
	out := restoreDocument(t, root, "bb1", 1)
	// Compared normalised: the restore prints an INDENTED document, so the bytes
	// differ from the record while the value does not.
	if got := string(normalise(out.tabs[out.byID["bb1"]].State)); got != `{"v":1}` {
		t.Errorf("the restored state = %s, want the recorded one", got)
	}
	if got := out.tabs[out.byID["bb1"]].Name; got != "Plan" {
		t.Errorf("the restored name = %q, want the board's own — a narrow record has none to give", got)
	}
}

// The whole point of widening: `history --at N | apply` puts the NAME back as
// well as the state, so undoing a rename is an undo rather than half of one.
//
// The recorded name must DIFFER from the one the board carries now, or this
// asserts nothing: the fixture board calls bb1 "Plan", so a record that also said
// "Plan" would pass with the restore leaving the name alone — which is exactly
// the behaviour this test exists to refuse. So the record holds the older name
// and the board holds the rename that replaced it, which is the real shape of an
// undo.
func TestARestoreFromASchema2RecordPutsTheNameBack(t *testing.T) {
	root := Root(t.TempDir())
	writeBoardFile(t, root)
	journalWith(t, root, entryV2(8, "2026-08-26T09:03:00.000Z", "agent-1", "bb1",
		`{"id":"bb1","name":"Draft plan","type":"notes","note":"what this is for","state":{"v":1}}`))

	out := restoreDocument(t, root, "bb1", 1)
	got := out.tabs[out.byID["bb1"]]
	if state := string(normalise(got.State)); state != `{"v":1}` {
		t.Errorf("state = %s, want the recorded one", state)
	}
	if got.Name != "Draft plan" || got.Note != "what this is for" || got.Type != "notes" {
		t.Errorf("the restore did not put the tab back: name %q note %q type %q", got.Name, got.Note, got.Type)
	}
	// Still a WHOLE document: the other tab is carried through, not dropped.
	if _, ok := out.byID["bb2"]; !ok {
		t.Error("the restore dropped a tab it never touched")
	}
}

// The markers are NOT restored, and that is the judgement this feature turns on.
// A schema-2 record holds `touched`, `pendingRemoval` and `seen` because it holds
// the whole tab — but putting them back would re-raise a notice the human
// dismissed and re-open a removal request they already answered, which is three
// of the four guarantees in tabs.go walked around by the one command whose job is
// to undo.
func TestARestoreDoesNotResurrectTheMarkers(t *testing.T) {
	root := Root(t.TempDir())
	writeBoardFile(t, root)
	journalWith(t, root, entryV2(8, "2026-08-26T09:03:00.000Z", "agent-1", "bb1",
		`{"id":"bb1","name":"Plan","type":"notes","state":{"v":1},`+
			`"touched":{"by":"agent-1","at":"2026-08-26T09:00:00.000Z"},`+
			`"pendingRemoval":{"by":"agent-1","at":"2026-08-26T09:00:00.000Z"},`+
			`"seen":{"human":"2026-08-26T08:00:00.000Z"}}`))

	out := restoreDocument(t, root, "bb1", 1)
	got := out.tabs[out.byID["bb1"]]
	if got.Touched != nil {
		t.Error("a restore must not re-raise a dot the human dismissed")
	}
	if got.PendingRemoval != nil {
		t.Error("a restore must not re-open a removal request the human answered")
	}
	if len(got.Seen) != 0 {
		t.Error("a restore must not rewrite another actor's read state")
	}
}

// Rotation with one generation in each file. The reader concatenates them, so a
// single history listing can hold versions of both shapes — and each one has to
// be read the way its own entry says, not the way the file it came from does.
func TestARotatedJournalMixesGenerations(t *testing.T) {
	root := Root(t.TempDir())
	j := newJournal(root)
	if err := os.MkdirAll(root.RunDir(), 0o755); err != nil {
		t.Fatal(err)
	}

	// The old file: a generation-1 entry, as one written before this landed.
	j.append(entry(5, "2026-08-25T10:00:00.000Z", "agent-1", "bb1", `{"v":1}`))
	j.mu.Lock()
	j.rotateLocked()
	j.mu.Unlock()
	// The live file: what this build writes.
	j.append(entryV2(6, "2026-08-26T09:00:00.000Z", "human", "bb1",
		`{"id":"bb1","name":"Plan then","type":"notes","state":{"v":2}}`))

	if _, err := os.Stat(root.JournalFile() + ".1"); err != nil {
		t.Fatalf("the rotated generation is not where this test thinks it is: %v", err)
	}

	got := mustHistory(t, root, "bb1")
	if len(got.Versions) != 2 {
		t.Fatalf("want a version from each generation, got %d: %+v", len(got.Versions), got.Versions)
	}
	// Newest first, so the live file's entry is version 1.
	if got.Versions[0].Schema != journalSchema || got.Versions[0].Was != "Plan then" {
		t.Errorf("the live file's entry must read as generation 2: %+v", got.Versions[0])
	}
	if got.Versions[1].Schema != 0 || got.Versions[1].Was != "" {
		t.Errorf("the rotated file's entry must read as generation 1: %+v", got.Versions[1])
	}
	if string(got.Versions[1].State) != `{"v":1}` {
		t.Errorf("the rotated entry's bare state was mangled: %s", got.Versions[1].State)
	}
}

// The endpoint carries the generation too, because the browser's change banner is
// a consumer that only ever has the JSON — a fact the terminal states and the
// machine form omitted would be the two halves disagreeing where it matters.
func TestTheHistoryEndpointCarriesTheGeneration(t *testing.T) {
	srv := testServer(t, restoreBoard)
	journalWith(t, srv.root, entryV2(8, "2026-08-26T09:03:00.000Z", "agent-1", "bb1",
		`{"id":"bb1","name":"Plan then","type":"notes","state":{"v":2}}`))

	rec := httptest.NewRecorder()
	srv.route(rec, httptest.NewRequest(http.MethodGet, "http://localhost/history?tab=bb1", http.NoBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /history?tab=bb1 answered %d: %s", rec.Code, rec.Body.String())
	}
	var got TabHistory
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Versions) != 1 {
		t.Fatalf("unexpected history payload: %+v", got)
	}
	if got.Versions[0].Schema != journalSchema || got.Versions[0].Was != "Plan then" {
		t.Errorf("the endpoint does not report what the record can restore: %+v", got.Versions[0])
	}
}

// A schema-2 line that will not decode as a tab answers "not recorded" rather
// than handing its raw bytes back as a state. A corrupt record is not a tab that
// was created and it is not a tab that held those bytes; every caller's refusal
// is the conservative answer, and returning the bytes would put them into a
// document.
func TestACorruptSchema2RecordIsNotReadAsAState(t *testing.T) {
	e := JournalEntry{
		Schema: journalSchema,
		At:     "2026-08-26T09:00:00.000Z", By: "agent-1",
		Tabs:   []string{"bb1"},
		Before: map[string]json.RawMessage{"bb1": json.RawMessage(`"not a tab"`)},
	}
	if _, ok := e.recorded("bb1"); ok {
		t.Error("a record that does not decode as a tab must not be read as one")
	}
	if got := historyFrom([]JournalEntry{e}, "bb1", 0); len(got.Versions) != 0 {
		t.Errorf("a corrupt record must not be offered as a restorable version: %+v", got.Versions)
	}
}

// restoreDocument runs `history --at N` and parses the document it prints, which
// is the only output this command has.
func restoreDocument(t *testing.T, root Root, tab string, at int) *stateDoc {
	t.Helper()
	var out strings.Builder
	if err := Restore(t.Context(), root, "", tab, at, &out); err != nil {
		t.Fatalf("the restore failed: %v", err)
	}
	doc, err := decodeDocument([]byte(out.String()))
	if err != nil {
		t.Fatalf("the restore document does not parse: %v", err)
	}
	return doc
}
