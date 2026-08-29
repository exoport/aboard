package aboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/exoport/aboard/pkg/aboard/web"
)

// A board with two tabs, so "the other session wrote to a different tab" is a
// thing that can happen at all.
const mergeBoard = `{
  "version": 1,
  "rev": 1,
  "nextId": 9,
  "tabs": [
    {"id": "ab1", "name": "Mine", "type": "notes", "state": {"text": "base"}},
    {"id": "ab2", "name": "Theirs", "type": "notes", "state": {"text": "base"}}
  ]
}`

// liveBoard stands a real engine on a real port and records it where
// RunningInstance looks, so `Apply` runs its whole path — post, 409, re-read,
// journal, merge, retry — against the actual write path rather than a fake.
//
// The document is mergeBoard and is not a parameter: every test here needs two
// tabs and nothing else, and a parameter only one value is ever passed to is a
// choice nobody made.
func liveBoard(t *testing.T) (Root, *server, *interceptor) {
	t.Helper()
	srv := testServer(t, mergeBoard)
	spy := &interceptor{}
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.route(w, r)
		spy.after(r)
	}))
	t.Cleanup(front.Close)

	rec, err := json.Marshal(Instance{App: HostStandalone, URL: front.URL, Project: srv.root.String()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srv.root.InstanceFile(""), rec, 0o644); err != nil {
		t.Fatal(err)
	}
	return srv.root, srv, spy
}

// interceptor runs a hook after a request the test names, so a foreign write can
// be landed at an exact point in `apply`'s conflict path. Forced in rather than
// raced for: what the retry test pins is that the SECOND refusal stops, and a
// test that had to win a race to assert it would be a test that passes when it
// loses.
//
// The hook must NOT make an HTTP request of its own. It runs inside the handler,
// so the response the caller is blocked on has not been completed yet — a nested
// request over the same client wedges both sides, which is exactly what it did
// the first time this was written. It writes through postState in process
// instead, which is the same write path with none of the plumbing.
type interceptor struct {
	mu   sync.Mutex
	path string
	fn   func()
}

func (i *interceptor) once(path string, fn func()) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.path, i.fn = path, fn
}

func (i *interceptor) after(r *http.Request) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.fn == nil || r.URL.Path != i.path {
		return
	}
	fn := i.fn
	i.fn = nil
	fn()
}

// readBoard is what an agent does before editing: read the whole document,
// `rev` included.
func readBoard(t *testing.T, root Root) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(root.StateFile(""))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func applyDoc(t *testing.T, root Root, doc map[string]any, by string) (stdout, stderr string, err error) {
	t.Helper()
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	err = Apply(t.Context(), root, "", ApplyOptions{By: by}, web.FS, bytes.NewReader(body), &out, &errOut, DefaultInvocation)
	return out.String(), errOut.String(), err
}

func setTabText(t *testing.T, doc map[string]any, id, text string) {
	t.Helper()
	list, _ := doc["tabs"].([]any)
	for _, raw := range list {
		tab, ok := raw.(map[string]any)
		if ok && tab["id"] == id {
			tab["state"] = map[string]any{"text": text}
			return
		}
	}
	t.Fatalf("no tab %s in the document", id)
}

// setTabName renames a tab in a document the test is about to submit — the
// foreign edit the merge could not classify until the journal record widened.
func setTabName(t *testing.T, doc map[string]any, id, name string) {
	t.Helper()
	list, _ := doc["tabs"].([]any)
	for _, raw := range list {
		tab, ok := raw.(map[string]any)
		if ok && tab["id"] == id {
			tab["name"] = name
			return
		}
	}
	t.Fatalf("no tab %s in the document", id)
}

// tabName reads a tab's name off the board as it stands.
func tabName(t *testing.T, root Root, id string) string {
	t.Helper()
	list, _ := readBoard(t, root)["tabs"].([]any)
	for _, raw := range list {
		tab, ok := raw.(map[string]any)
		if !ok || tab["id"] != id {
			continue
		}
		name, _ := tab["name"].(string)
		return name
	}
	t.Fatalf("no tab %s on the board", id)
	return ""
}

// writeInProcess lands a write through postState with no HTTP at all.
func writeInProcess(t *testing.T, srv *server, id, text, by string) {
	t.Helper()
	doc := readBoard(t, srv.root)
	setTabText(t, doc, id, text)
	doc["__by"] = by
	doc["__origin"] = "test"
	doc["__base"] = revToken(t, doc["rev"])
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.postState(rec, httptest.NewRequest(http.MethodPost, "/aboard.json", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("the in-process write answered %d: %s", rec.Code, rec.Body.String())
	}
}

func revToken(t *testing.T, rev any) string {
	t.Helper()
	n, ok := rev.(float64)
	if !ok {
		t.Fatalf("the document carries no usable rev (%T)", rev)
	}
	return strconv.Itoa(int(n))
}

func tabText(t *testing.T, root Root, id string) string {
	t.Helper()
	list, _ := readBoard(t, root)["tabs"].([]any)
	for _, raw := range list {
		tab, ok := raw.(map[string]any)
		if !ok || tab["id"] != id {
			continue
		}
		state, _ := tab["state"].(map[string]any)
		text, _ := state["text"].(string)
		return text
	}
	t.Fatalf("no tab %s on the board", id)
	return ""
}

// The case the feature exists for. Compare-and-set is whole-document, so a write
// to ab2 refuses a write to ab1 — and `apply` used to discard the agent's whole
// document over it, built from a board it can no longer read.
func TestApplyMergesAConflictOnAnotherTab(t *testing.T) {
	root, _, _ := liveBoard(t)

	mine := readBoard(t, root) // rev 1
	setTabText(t, mine, "ab1", "mine")

	theirs := readBoard(t, root)
	setTabText(t, theirs, "ab2", "theirs")
	if _, _, err := applyDoc(t, root, theirs, "agent-2"); err != nil {
		t.Fatalf("the second actor's write should have landed: %v", err)
	}

	out, errOut, err := applyDoc(t, root, mine, "agent-1")
	if err != nil {
		t.Fatalf("the merge should have rescued this write: %v (stderr %s)", err, errOut)
	}
	if !strings.Contains(out, "merged") {
		t.Errorf("a merged write must say so on stdout, got %q", out)
	}
	if !strings.Contains(errOut, "ab2") {
		t.Errorf("stderr must name the tab whose version the board kept, got %q", errOut)
	}
	if got := tabText(t, root, "ab1"); got != "mine" {
		t.Errorf("ab1 = %q, want the edit that was refused to have landed", got)
	}
	if got := tabText(t, root, "ab2"); got != "theirs" {
		t.Errorf("ab2 = %q, want the other session's write kept — our document still held its old state", got)
	}
}

// The browser's rule, kept: a genuine same-tab collision is never merged
// silently. Both sides changed ab1, and picking a winner is not a decision a
// retry gets to make.
func TestApplyRefusesASameTabCollisionByName(t *testing.T) {
	root, _, _ := liveBoard(t)

	mine := readBoard(t, root)
	setTabText(t, mine, "ab1", "mine")

	theirs := readBoard(t, root)
	setTabText(t, theirs, "ab1", "theirs")
	if _, _, err := applyDoc(t, root, theirs, "agent-2"); err != nil {
		t.Fatal(err)
	}

	_, _, err := applyDoc(t, root, mine, "agent-1")
	if err == nil {
		t.Fatal("a same-tab collision must refuse, not merge")
	}
	if !strings.Contains(err.Error(), "ab1") {
		t.Errorf("the refusal must NAME the tab, got %q", err)
	}
	if got := tabText(t, root, "ab1"); got != "theirs" {
		t.Errorf("ab1 = %q, want the other session's write left alone", got)
	}
}

// A tab created on the board since our base is not ours to drop: our document
// simply predates it, and omitting it would be read as a removal request.
func TestMergeKeepsATabCreatedSinceOurBase(t *testing.T) {
	root, _, _ := liveBoard(t)

	mine := readBoard(t, root)
	setTabText(t, mine, "ab1", "mine")

	theirs := readBoard(t, root)
	theirTabs, _ := theirs["tabs"].([]any)
	theirs["tabs"] = append(theirTabs, map[string]any{
		"id": "ab9", "name": "New", "type": "notes", "state": map[string]any{"text": "new"},
	})
	if _, _, err := applyDoc(t, root, theirs, "agent-2"); err != nil {
		t.Fatal(err)
	}

	if _, _, err := applyDoc(t, root, mine, "agent-1"); err != nil {
		t.Fatalf("merge failed: %v", err)
	}
	if got := tabText(t, root, "ab9"); got != "new" {
		t.Errorf("ab9 = %q, want the tab the other session created still there", got)
	}
	if got, _ := readBoard(t, root)["tabs"].([]any); len(got) != 3 {
		t.Errorf("%d tabs after the merge, want 3", len(got))
	}
}

// Only once. A board being written to continuously would otherwise have `apply`
// retrying against a moving target for as long as the writes kept coming, and an
// agent's command that never returns is worse than one that returns a refusal it
// can act on.
func TestMergeRetriesOnlyOnce(t *testing.T) {
	root, srv, spy := liveBoard(t)

	mine := readBoard(t, root)
	setTabText(t, mine, "ab1", "mine")

	theirs := readBoard(t, root)
	setTabText(t, theirs, "ab2", "theirs")
	if _, _, err := applyDoc(t, root, theirs, "agent-2"); err != nil {
		t.Fatal(err)
	}

	// The merge reads /journal between the refusal and the retry. Landing a third
	// write right after that read makes the retry's base stale by construction.
	spy.once("/journal", func() { writeInProcess(t, srv, "ab2", "third", "agent-3") })

	_, _, err := applyDoc(t, root, mine, "agent-1")
	if err == nil {
		t.Fatal("a second conflict must stop, not retry again")
	}
	if !strings.Contains(err.Error(), "refused twice") {
		t.Errorf("the second refusal must say it is the second, got %q", err)
	}
}

// A timestamp base belongs to a board written before the revision counter
// landed, and there is no way to ask the journal "since when" from one. The
// merge does not run, the plain conflict is reported, and stderr says why — an
// agent that gets a bare refusal on a board where merging is supposed to work
// would go looking for the wrong bug.
func TestAConflictWithNoRevisionBaseIsNotMerged(t *testing.T) {
	root, _, _ := liveBoard(t)

	theirs := readBoard(t, root)
	setTabText(t, theirs, "ab2", "theirs")
	if _, _, err := applyDoc(t, root, theirs, "agent-2"); err != nil {
		t.Fatal(err)
	}

	stale := readBoard(t, root)
	delete(stale, "rev")
	stale["updatedAt"] = "2020-01-01T00:00:00.000Z"
	setTabText(t, stale, "ab1", "mine")

	_, errOut, err := applyDoc(t, root, stale, "agent-1")
	if err == nil || !strings.Contains(err.Error(), "changed since you read it") {
		t.Fatalf("want the plain conflict refusal, got %v", err)
	}
	if !strings.Contains(errOut, "could not merge") {
		t.Errorf("stderr must say why the merge did not run, got %q", errOut)
	}
}

// --force writes unconditionally, and the document it sends still carries the
// `rev` it was read at. A base derived from the document rather than passed in
// would have turned every forced write back into a compare-and-set one — found
// while refactoring the post path for the merge, not by a report.
func TestForceStillWritesWithoutCompareAndSet(t *testing.T) {
	root, _, _ := liveBoard(t)

	stale := readBoard(t, root) // rev 1
	setTabText(t, stale, "ab1", "mine")

	theirs := readBoard(t, root)
	setTabText(t, theirs, "ab2", "theirs")
	if _, _, err := applyDoc(t, root, theirs, "agent-2"); err != nil {
		t.Fatal(err)
	}

	body, err := json.Marshal(stale)
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if err := Apply(t.Context(), root, "", ApplyOptions{By: "agent-1", Force: true}, web.FS, bytes.NewReader(body), &out, &errOut, DefaultInvocation); err != nil {
		t.Fatalf("--force must write over a stale document: %v (stderr %s)", err, errOut.String())
	}
	if got := tabText(t, root, "ab1"); got != "mine" {
		t.Errorf("ab1 = %q, want the forced write to have landed", got)
	}
}

// The seam between two features that were built in parallel: `apply --label`
// (ab371) puts the caller's reason on the journal entry, and the 409 merge
// (ab366) posts a document rebuilt from the board's own. A label written onto
// the caller's document would not survive that rebuild — mergeOnConflict copies
// the FRESH document's root fields — so the merged write would land unlabelled
// and the one entry a reader most wants a reason for, the one that had to be
// merged, is the one without it.
//
// Fails before with an empty Label on the merged entry: seen by moving the label
// back onto the document in Apply instead of threading it through postDocument.
func TestAMergedWriteKeepsTheLabelOfTheWriteItRedoes(t *testing.T) {
	root, _, _ := liveBoard(t)

	mine := readBoard(t, root) // rev 1
	setTabText(t, mine, "ab1", "mine")

	theirs := readBoard(t, root)
	setTabText(t, theirs, "ab2", "theirs")
	if _, _, err := applyDoc(t, root, theirs, "agent-2"); err != nil {
		t.Fatalf("the second actor's write should have landed: %v", err)
	}

	body, err := json.Marshal(mine)
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	options := ApplyOptions{By: "agent-1", Label: "restating ab1 after review"}
	if err := Apply(t.Context(), root, "", options, web.FS, bytes.NewReader(body), &out, &errOut, DefaultInvocation); err != nil {
		t.Fatalf("the merge should have rescued this write: %v (stderr %s)", err, errOut.String())
	}
	if !strings.Contains(out.String(), "merged") {
		t.Fatalf("this test only means anything on the merged path, got %q", out.String())
	}

	entries, err := journalFromDisk(root, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	last := entries[len(entries)-1]
	if last.Label != "restating ab1 after review" {
		t.Errorf("the merged write lost its label: %+v", last)
	}

	// And the label is still a note ABOUT the write, not content: the rebuilt
	// document is the board's own, so a control key surviving into it would be a
	// root field no renderer reads.
	if _, leaked := readBoard(t, root)["__label"]; leaked {
		t.Error("__label was written into the merged document")
	}
}

/* ---------- what the widened journal record bought ---------- */

// The case that was recorded as a limitation and is now a merge: somebody
// RENAMES a tab on the board while an agent writes to a DIFFERENT one.
//
// `JournalEntry.Before` used to hold a tab's `state` and nothing else, so the
// merge had to compare our copy's name against the FRESH tab's — and our copy
// carries the name as it was when we read the board, which is not the name it has
// now. One side had moved it, ours had not, and the merge stopped anyway. The
// record carries the name now (journalSchema 2), the comparison is against the
// record like the state's, and the write lands.
func TestMergeSurvivesAForeignRename(t *testing.T) {
	root, _, _ := liveBoard(t)

	mine := readBoard(t, root) // rev 1, and ab2 is still called "Theirs" in it
	setTabText(t, mine, "ab1", "mine")

	theirs := readBoard(t, root)
	setTabName(t, theirs, "ab2", "Renamed by the human")
	if _, _, err := applyDoc(t, root, theirs, "human-in-the-browser"); err != nil {
		t.Fatalf("the rename should have landed: %v", err)
	}

	out, errOut, err := applyDoc(t, root, mine, "agent-1")
	if err != nil {
		t.Fatalf("a foreign rename must not stop a write to another tab: %v (stderr %s)", err, errOut)
	}
	if !strings.Contains(out, "merged") {
		t.Errorf("a merged write must say so on stdout, got %q", out)
	}
	if got := tabText(t, root, "ab1"); got != "mine" {
		t.Errorf("ab1 = %q, want the edit that was refused to have landed", got)
	}
	if got := tabName(t, root, "ab2"); got != "Renamed by the human" {
		t.Errorf("ab2 is called %q — the merge must keep the rename it did not make", got)
	}
}

// And the half that must NOT change: both sides renaming the same tab is a
// collision, named as one. The merge picking a winner here is how one of the two
// names disappears with a 200 to say it went well.
func TestMergeStopsOnASameTabRename(t *testing.T) {
	root, _, _ := liveBoard(t)

	// Both names differ from the fixture's, or one "rename" would be a no-op and
	// the test would be asserting a collision that never happened.
	mine := readBoard(t, root)
	setTabName(t, mine, "ab1", "Mine, renamed")

	theirs := readBoard(t, root)
	setTabName(t, theirs, "ab1", "Theirs, renamed")
	if _, _, err := applyDoc(t, root, theirs, "agent-2"); err != nil {
		t.Fatal(err)
	}

	_, _, err := applyDoc(t, root, mine, "agent-1")
	if err == nil {
		t.Fatal("both sides renamed ab1; picking a winner is not the merge's call")
	}
	// Named, and named as what it is: the tab AND the field, so the caller is not
	// sent looking through a state blob for an edit that is in the title bar.
	for _, want := range []string{"ab1", "name", "while you were changing it too"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, err)
		}
	}
	if got := tabName(t, root, "ab1"); got != "Theirs, renamed" {
		t.Errorf("ab1 is called %q, want the other session's write left alone", got)
	}
}

// A window covered by a PRE-SCHEMA-2 entry still cannot attribute a rename, and
// still refuses — the honest answer for the record it has. The wording changed
// with it: "the journal does not record which side changed it" was true of every
// entry once and is now true only of these, so it names the generation.
//
// The old entry is made by rewriting the one the foreign write just appended,
// which is the only way to get one: nothing in this build writes that shape any
// more, and a rotated journal.jsonl.1 full of them is the real case.
func TestAPreSchema2RecordStillCannotAttributeARename(t *testing.T) {
	root, _, _ := liveBoard(t)

	mine := readBoard(t, root)
	setTabText(t, mine, "ab1", "mine")

	theirs := readBoard(t, root)
	setTabName(t, theirs, "ab2", "Renamed by the human")
	if _, _, err := applyDoc(t, root, theirs, "human-in-the-browser"); err != nil {
		t.Fatal(err)
	}
	narrowTheJournal(t, root)

	_, _, err := applyDoc(t, root, mine, "agent-1")
	if err == nil {
		t.Fatal("a record that cannot attribute the rename must refuse rather than guess")
	}
	for _, want := range []string{"ab2", "name", "schema 1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, err)
		}
	}
}

// narrowTheJournal rewrites every entry on disk into the generation-1 shape:
// `schema` dropped, and `before[<id>]` cut back to the bare state it used to be.
func narrowTheJournal(t *testing.T, root Root) {
	t.Helper()
	raw, err := os.ReadFile(root.JournalFile(""))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	for line := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var e JournalEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatal(err)
		}
		for id, was := range e.Before {
			var full tab
			if err := json.Unmarshal(was, &full); err != nil {
				t.Fatal(err)
			}
			if len(full.State) == 0 {
				delete(e.Before, id)
				continue
			}
			e.Before[id] = full.State
		}
		e.Schema = 0
		body, err := json.Marshal(&e)
		if err != nil {
			t.Fatal(err)
		}
		out.Write(body)
		out.WriteByte('\n')
	}
	if err := os.WriteFile(root.JournalFile(""), out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A `done` stamp is a write whose ENTIRE content is a change to the human's
// request list, so it is the one write the merge could lose completely: state
// unchanged, name unchanged, note unchanged — every field the classifier looked
// at agreeing — and the board's copy of the tab silently winning. `aboard
// requests done` would then have printed "applied (merged)" having stamped
// nothing at all, which is worse than refusing, because the agent has told the
// human it answered them.
func TestAStampCollidingWithTheHumansOwnEditIsRefused(t *testing.T) {
	root, srv := boardWithARequest(t)

	// The agent reads the board and stamps the note done.
	mine := readBoard(t, root)
	stampFirstRequest(t, mine, "agent-1")

	// The human, meanwhile, edits the same tab — anything at all, as long as it
	// lands on ab1 and makes the journal record it.
	writeInProcess(t, srv, "ab1", "the human typed while we were stamping", actorHuman)

	_, _, err := applyDoc(t, root, mine, "agent-1")
	if err == nil {
		t.Fatal("the stamp was merged away silently; a same-tab collision must refuse")
	}
	if !strings.Contains(err.Error(), "requests") || !strings.Contains(err.Error(), "ab1") {
		t.Errorf("the refusal must name the tab AND say it was the requests, got %q", err)
	}
	if stamped := firstRequestStamp(t, root); stamped != nil {
		t.Errorf("the board carries a stamp from a refused write: %+v", stamped)
	}
}

// boardWithARequest is liveBoard plus one of the human's notes on ab1, written
// as the human so guarantee 5 lets it through.
func boardWithARequest(t *testing.T) (Root, *server) {
	t.Helper()
	root, srv, _ := liveBoard(t)

	doc := readBoard(t, root)
	list, _ := doc["tabs"].([]any)
	for _, raw := range list {
		tab, ok := raw.(map[string]any)
		if !ok || tab["id"] != "ab1" {
			continue
		}
		tab["requests"] = []any{map[string]any{
			"id": "ab8", "at": "2026-08-26T09:00:00Z", "by": actorHuman, "text": "fix the arrow",
		}}
	}
	doc["__by"] = actorHuman
	doc["__origin"] = "test"
	doc["__base"] = revToken(t, doc["rev"])
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.postState(rec, httptest.NewRequest(http.MethodPost, "/aboard.json", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("seeding the request answered %d: %s", rec.Code, rec.Body.String())
	}
	return root, srv
}

func stampFirstRequest(t *testing.T, doc map[string]any, by string) {
	t.Helper()
	if _, _, err := stampRequest(doc, "ab8", by, "flipped it", DefaultInvocation); err != nil {
		t.Fatal(err)
	}
}

func firstRequestStamp(t *testing.T, root Root) map[string]any {
	t.Helper()
	list, _ := readBoard(t, root)["tabs"].([]any)
	for _, raw := range list {
		tab, ok := raw.(map[string]any)
		if !ok || tab["id"] != "ab1" {
			continue
		}
		asks, _ := tab["requests"].([]any)
		if len(asks) == 0 {
			t.Fatal("the human's request is gone from the board")
		}
		ask, _ := asks[0].(map[string]any)
		done, _ := ask["done"].(map[string]any)
		return done
	}
	t.Fatal("no tab ab1 on the board")
	return nil
}
