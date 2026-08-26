package aboard

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/exoport/aboard/pkg/aboard/web"
)

// testServer is the write path with nothing else attached: a state file in a
// temp directory and the maps postState touches. Built directly rather than
// through Serve because the thing under test is one handler, and starting a
// listener would drag the port derivation and the file watcher in with it.
func testServer(t *testing.T, document string) *server {
	t.Helper()
	dir := t.TempDir()
	root := Root(dir)
	if err := os.MkdirAll(root.RunDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(root.Dir(), "aboard.json")
	if err := os.WriteFile(state, []byte(document), 0o644); err != nil {
		t.Fatal(err)
	}
	return &server{
		opts: Options{Logger: log.New(io.Discard, "", 0)},
		root: root,
		// The real embedded tree, because /events hands every client the UI
		// signature before anything else and a nil FS is a nil dereference there.
		assets:    web.FS,
		stateFile: state,
		clients:   map[chan string]struct{}{},
		watchers:  map[chan string]struct{}{},
		waits:     newWaitHub(),
		ui:        newUIWatcher(false),
		journal:   newJournal(root, ""),
		receipts:  newReceiptStore(root, ""),
	}
}

func (s *server) postDocument(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/aboard.json", strings.NewReader(body))
	s.postState(rec, req)
	return rec
}

func (s *server) readTabs(t *testing.T) []tab {
	t.Helper()
	raw, err := os.ReadFile(s.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	var doc board
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Tabs
}

const twoTabs = `{"version":3,"nextId":3,"updatedAt":"T0","tabs":[
  {"id":"bb1","name":"Plan","type":"notes","state":{"text":"one"}},
  {"id":"bb2","name":"Queue","type":"notes","state":{"text":"two"}}
]}`

// The defect: __by defaulted to "human", and "human" is not a label — it is the
// key every guarantee in tabs.go keys off. So a bare POST with no __by at all —
// a curl, a script, a half-written tool — could empty the board and leave no
// dot, no removal request and no trace that anything had been hidden.
func TestPostWithNoByCannotDeleteATab(t *testing.T) {
	srv := testServer(t, twoTabs)

	rec := srv.postDocument(t, `{"version":3,"nextId":3,"tabs":[
	  {"id":"bb1","name":"Plan","type":"notes","state":{"text":"one"}}
	]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}

	tabs := srv.readTabs(t)
	if len(tabs) != 2 {
		t.Fatalf("a POST with no __by deleted a tab: %d remain", len(tabs))
	}
	gone := tabByID(t, tabs, "bb2")
	if gone.PendingRemoval == nil {
		t.Fatal("the dropped tab came back with no removal request for the human to answer")
	}
	if gone.PendingRemoval.By != "unknown" {
		t.Errorf("the removal request is attributed to %q, want \"unknown\"", gone.PendingRemoval.By)
	}
}

// The other half of the same decision: the human's own write still works. They
// act in the browser, aboard.html's pushDoc always sends `__by: 'human'`
// explicitly, and deleting a tab is theirs to do.
func TestPostAsHumanMayDeleteATab(t *testing.T) {
	srv := testServer(t, twoTabs)

	rec := srv.postDocument(t, `{"version":3,"nextId":3,"__by":"human","tabs":[
	  {"id":"bb1","name":"Plan","type":"notes","state":{"text":"one"}}
	]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if tabs := srv.readTabs(t); len(tabs) != 1 {
		t.Fatalf("the human's deletion was undone: %d tabs remain", len(tabs))
	}
}

// An unattributed write is recorded as unattributed. "unknown" in the journal is
// a fact somebody can act on; "human" would have been a lie in the one record
// that exists to answer "who changed this while I was thinking?".
func TestUnattributedWriteIsJournaledAsUnknown(t *testing.T) {
	srv := testServer(t, twoTabs)

	rec := srv.postDocument(t, `{"version":3,"nextId":3,"tabs":[
	  {"id":"bb1","name":"Plan","type":"notes","state":{"text":"CHANGED"}},
	  {"id":"bb2","name":"Queue","type":"notes","state":{"text":"two"}}
	]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}

	entries, source, err := JournalEntries(t.Context(), srv.root, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if source != JournalFromDisk {
		t.Fatalf("journal source = %q, want %q with no server running", source, JournalFromDisk)
	}
	if len(entries) != 1 {
		t.Fatalf("%d journal entries, want 1", len(entries))
	}
	if entries[0].By != "unknown" {
		t.Errorf("journalled as %q, want \"unknown\"", entries[0].By)
	}
}

// The version stamp: the server owns the field, so a document copied from a
// stale example is corrected rather than written through. It used to be written
// through verbatim, and the shell blanks a board whose version it does not know.
func TestPostStampsTheSchemaVersion(t *testing.T) {
	srv := testServer(t, twoTabs)

	rec := srv.postDocument(t, `{"version":2,"nextId":3,"__by":"agent-1","tabs":[
	  {"id":"bb1","name":"Plan","type":"notes","state":{"text":"one"}},
	  {"id":"bb2","name":"Queue","type":"notes","state":{"text":"two"}}
	]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}

	raw, err := os.ReadFile(srv.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if v, _ := doc["version"].(float64); int(v) != SchemaVersion {
		t.Errorf("version = %v, want %d", doc["version"], SchemaVersion)
	}
}

// Compare-and-set, from the other side: a write whose base has moved on is
// refused with 409 rather than winning.
func TestPostRefusesAStaleBase(t *testing.T) {
	srv := testServer(t, twoTabs)

	rec := srv.postDocument(t, `{"version":3,"__base":"SOMETHING-ELSE","__by":"agent-1","tabs":[
	  {"id":"bb1","name":"Plan","type":"notes","state":{"text":"one"}},
	  {"id":"bb2","name":"Queue","type":"notes","state":{"text":"two"}}
	]}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409", rec.Code)
	}
}

// A removal REQUEST is a change. It used to be invisible to every trace the
// board has: dropping a tab changes nothing else about that tab, so the
// comparison found it identical and wrote no journal line — a banner on the
// human's screen and not one line anywhere a session could read.
//
// Found by running the ladder: a bare POST that dropped a tab produced the
// pendingRemoval correctly and an empty journal.
func TestARemovalRequestIsJournaled(t *testing.T) {
	srv := testServer(t, twoTabs)

	rec := srv.postDocument(t, `{"version":3,"nextId":3,"__by":"agent-1","tabs":[
	  {"id":"bb1","name":"Plan","type":"notes","state":{"text":"one"}}
	]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}

	entries, _, err := JournalEntries(t.Context(), srv.root, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("%d journal entries, want 1", len(entries))
	}
	var found bool
	for _, id := range entries[0].Tabs {
		if id == "bb2" {
			found = true
		}
	}
	if !found {
		t.Errorf("the tab whose removal was requested is not in the entry: %v", entries[0].Tabs)
	}
}

// And the human answering it is a change too — clearing the request is what
// closes the loop, and a record with only half of it is a record that shows
// every ask and no answer.
func TestAnsweringARemovalRequestIsJournaled(t *testing.T) {
	srv := testServer(t, `{"version":3,"nextId":3,"tabs":[
	  {"id":"bb1","name":"Plan","type":"notes","state":{"text":"one"}},
	  {"id":"bb2","name":"Queue","type":"notes","state":{"text":"two"},
	   "pendingRemoval":{"by":"agent-1","at":"T","reason":"spent"}}
	]}`)

	rec := srv.postDocument(t, `{"version":3,"nextId":3,"__by":"human","tabs":[
	  {"id":"bb1","name":"Plan","type":"notes","state":{"text":"one"}},
	  {"id":"bb2","name":"Queue","type":"notes","state":{"text":"two"}}
	]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}

	entries, _, err := JournalEntries(t.Context(), srv.root, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || len(entries[0].Tabs) != 1 || entries[0].Tabs[0] != "bb2" {
		t.Fatalf("the human keeping the tab was not journaled: %+v", entries)
	}
	if entries[0].By != "human" {
		t.Errorf("journalled as %q", entries[0].By)
	}
}
