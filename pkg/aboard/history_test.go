package aboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// journalWith writes a journal file straight to disk, which is what the server
// would have appended. Written rather than produced by real POSTs because the
// property under test is the READ path over the record, and building the record
// through the write path would make every history assertion depend on the
// write path's own correctness as well.
func journalWith(t *testing.T, root Root, entries ...JournalEntry) {
	t.Helper()
	if err := os.MkdirAll(root.RunDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	for i := range entries {
		body, err := json.Marshal(&entries[i])
		if err != nil {
			t.Fatal(err)
		}
		b.Write(body)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(root.JournalFile(""), b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// entry writes a GENERATION-1 record — no `schema` key, and `before` is a tab's
// bare state. Deliberately kept as the default for these tests: it is what every
// journal already on disk looks like, and a rotated generation can hand one to
// every reader here long after the live file has moved on. Hand-written rather
// than produced, because there is no code left that can produce one.
func entry(rev int, at, by, tab, before string) JournalEntry {
	return JournalEntry{
		At: at, By: by, Rev: rev,
		Tabs:   []string{tab},
		Names:  map[string]string{tab: "Plan"},
		Before: map[string]json.RawMessage{tab: json.RawMessage(before)},
	}
}

// entryV2 writes the record this build produces: `schema` stamped, and `before`
// holding the whole tab.
func entryV2(rev int, at, by, tab, before string) JournalEntry {
	e := entry(rev, at, by, tab, before)
	e.Schema = journalSchema
	return e
}

// The listing is newest first and names who replaced each version. Both halves
// matter: "1" has to be the undo somebody reaches for, and the actor on a
// journal entry is the one who OVERWROTE the state it carries, never the one
// who wrote it.
func TestHistoryListsPriorStatesNewestFirst(t *testing.T) {
	root := Root(t.TempDir())
	journalWith(t, root,
		entry(2, "2026-08-26T09:00:00.000Z", "agent-1", "ab1", `{"v":1}`),
		entry(3, "2026-08-26T09:01:00.000Z", "human", "ab1", `{"v":2}`),
		entry(4, "2026-08-26T09:02:00.000Z", "agent-2", "ab2", `{"other":true}`),
		entry(5, "2026-08-26T09:03:00.000Z", "agent-1", "ab1", `{"v":3}`),
	)

	got, err := History(t.Context(), root, "", "ab1", 0, DefaultInvocation)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Versions) != 3 {
		t.Fatalf("got %d versions, want the 3 entries that touched ab1: %+v", len(got.Versions), got.Versions)
	}
	if got.Versions[0].N != 1 || string(got.Versions[0].State) != `{"v":3}` {
		t.Errorf("version 1 must be the most recent state replaced, got %+v", got.Versions[0])
	}
	if got.Versions[0].By != "agent-1" || got.Versions[1].By != "human" {
		t.Errorf("each version must name who replaced it: %+v", got.Versions)
	}
	if string(got.Versions[2].State) != `{"v":1}` {
		t.Errorf("the oldest version is wrong: %s", got.Versions[2].State)
	}
}

// A tab being CREATED is journalled with no `before`, and offering that as a
// version would offer a restore that blanks the tab.
func TestHistorySkipsTheWriteThatCreatedTheTab(t *testing.T) {
	root := Root(t.TempDir())
	journalWith(t, root,
		JournalEntry{At: "2026-08-26T09:00:00.000Z", By: "agent-1", Rev: 2, Tabs: []string{"ab1"}},
		entry(3, "2026-08-26T09:01:00.000Z", "human", "ab1", `{"v":1}`),
	)
	got, err := History(t.Context(), root, "", "ab1", 0, DefaultInvocation)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Versions) != 1 {
		t.Fatalf("the creating write has no previous state and must not be listed: %+v", got.Versions)
	}
}

// The listing has to say where the record stops, in both forms. An empty list is
// otherwise indistinguishable from "everything about this tab rotated away", and
// those call for opposite next moves.
func TestHistorySaysWhereTheRecordEnds(t *testing.T) {
	root := Root(t.TempDir())
	if got := mustHistory(t, root, "ab1"); !strings.Contains(got.Ends, "journal is empty") {
		t.Errorf("an empty journal must say so, got %q", got.Ends)
	}
	journalWith(t, root, entry(2, "2026-08-26T09:00:00.000Z", "agent-1", "ab1", `{"v":1}`))
	got := mustHistory(t, root, "ab1")
	if !strings.Contains(got.Ends, "2026-08-26T09:00:00.000Z") || !strings.Contains(got.Ends, "rotated generation") {
		t.Errorf("the end of the record must name where it stops and why, got %q", got.Ends)
	}
	if !strings.Contains(got.Human(DefaultInvocation), got.Ends) {
		t.Error("the human form must print the same sentence the JSON carries")
	}
}

func mustHistory(t *testing.T, root Root, tab string) TabHistory {
	t.Helper()
	got, err := History(t.Context(), root, "", tab, 0, DefaultInvocation)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

const restoreBoard = `{
  "version": 1,
  "rev": 7,
  "nextId": 9,
  "tabs": [
    {"id": "ab1", "name": "Plan", "type": "notes", "state": {"v": 3}},
    {"id": "ab2", "name": "Other", "type": "notes", "state": {"keep": true}}
  ]
}`

// The restore is a WHOLE document. This is the whole risk of the feature: a
// journal entry holds one tab's state, and wrapping that alone as a document is
// a document that says the board has one tab — which reconcileTabs answers with
// a removal request on every other one, in front of the human.
func TestRestorePrintsTheWholeDocumentWithOneTabReplaced(t *testing.T) {
	root := Root(t.TempDir())
	writeBoardFile(t, root)
	journalWith(t, root, entry(7, "2026-08-26T09:03:00.000Z", "agent-1", "ab1", `{"v":2}`))

	var out bytes.Buffer
	if err := Restore(t.Context(), root, "", "ab1", 1, &out, DefaultInvocation); err != nil {
		t.Fatal(err)
	}
	doc, err := decodeDocument(out.Bytes())
	if err != nil {
		t.Fatalf("the restore document does not parse: %v", err)
	}
	if len(doc.tabs) != 2 {
		t.Fatalf("the restore dropped tabs: %d left, want 2", len(doc.tabs))
	}
	if got := string(doc.tabs[doc.byID["ab1"]].State); !strings.Contains(got, `"v": 2`) && !strings.Contains(got, `"v":2`) {
		t.Errorf("ab1 was not restored: %s", got)
	}
	if got := string(doc.tabs[doc.byID["ab2"]].State); !strings.Contains(got, "keep") {
		t.Errorf("ab2 must be carried through untouched: %s", got)
	}
	// Carrying `rev` is what makes a restore refusable rather than a clobber.
	if rev, ok := rawInt(doc.fields["rev"]); !ok || rev != 7 {
		t.Errorf("the restore must keep the document's rev, got %v", doc.fields["rev"])
	}
}

// A tab that is gone is not rebuilt from the journal, and the reason moved when
// the record widened. It used to be that the journal held a name and a state and
// never a `type`, so a rebuilt tab would have mounted as "No renderer"; a
// schema-2 record holds the type, so that sentence stopped being true. The
// refusal stands on the honest reason: an agent cannot delete a tab, so a tab
// that is gone is one the human removed by answering a removal request, and
// putting it back is theirs to decide rather than a pipeline's to perform.
func TestRestoreRefusesATabThatIsNoLongerOnTheBoard(t *testing.T) {
	root := Root(t.TempDir())
	writeBoardFile(t, root)
	journalWith(t, root, entryV2(7, "2026-08-26T09:03:00.000Z", "human", "ab9",
		`{"id":"ab9","name":"Gone","type":"notes","state":{"v":1}}`))

	err := Restore(t.Context(), root, "", "ab9", 1, &bytes.Buffer{}, DefaultInvocation)
	if err == nil || !strings.Contains(err.Error(), "removed by the human") {
		t.Errorf("want a refusal saying who removed it, got %v", err)
	}
}

func TestRestoreRefusesAVersionThatIsNotThere(t *testing.T) {
	root := Root(t.TempDir())
	writeBoardFile(t, root)
	journalWith(t, root, entry(7, "2026-08-26T09:03:00.000Z", "human", "ab1", `{"v":1}`))

	err := Restore(t.Context(), root, "", "ab1", 4, &bytes.Buffer{}, DefaultInvocation)
	if err == nil || !strings.Contains(err.Error(), "1 recorded version") {
		t.Errorf("want a refusal that says how many versions there are, got %v", err)
	}
}

// The endpoint the browser's change banner reads. A whole-board history is what
// /journal already is, so a request without a tab is a mistake worth naming.
func TestHistoryEndpointNeedsATabAndAnswersWithVersions(t *testing.T) {
	srv := testServer(t, restoreBoard)
	journalWith(t, srv.root, entry(7, "2026-08-26T09:03:00.000Z", "agent-1", "ab1", `{"v":2}`))

	rec := httptest.NewRecorder()
	srv.route(rec, httptest.NewRequest(http.MethodGet, "http://localhost/history", http.NoBody))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("GET /history with no tab answered %d, want 400", rec.Code)
	}

	rec = httptest.NewRecorder()
	srv.route(rec, httptest.NewRequest(http.MethodGet, "http://localhost/history?tab=ab1", http.NoBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /history?tab=ab1 answered %d: %s", rec.Code, rec.Body.String())
	}
	var got TabHistory
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Versions) != 1 || got.Versions[0].By != "agent-1" {
		t.Fatalf("unexpected history payload: %+v", got)
	}
	if got.Ends == "" {
		t.Error("the JSON must carry the same end-of-record sentence the terminal prints")
	}
}

// writeBoardFile puts restoreBoard where the engine expects to find a document.
//
// Not a parameter: every caller wants the same two-tab board — one tab to restore
// and one to prove the restore did not drop it — and a parameter with one value
// ever passed to it is a choice nobody made.
func writeBoardFile(t *testing.T, root Root) {
	t.Helper()
	if err := os.MkdirAll(root.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root.StateFile(""), []byte(restoreBoard), 0o644); err != nil {
		t.Fatal(err)
	}
}
