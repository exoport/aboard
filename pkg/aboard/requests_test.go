package aboard

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/exoport/aboard/pkg/aboard/web"
)

// Guarantee 5, both directions. The human's notes to an agent are the one thing
// on a tab that flows their way, and the failure that matters is not an agent
// being malicious: it is an agent doing a read-modify-write of the whole
// document and handing back a `requests` array it never looked at, exactly as
// used to happen to `touched`.

// asksOn is the requests on the one tab every probe in this file uses. Not
// parameterised: a helper whose argument only ever takes one value reads as a
// generality nobody has.
func asksOn(t *testing.T, tabs []tab) []requestAsk {
	t.Helper()
	return tabByID(t, tabs, "ab1").Requests
}

// one pending note, as the human's browser writes it.
func pending(id, text string) requestAsk {
	return requestAsk{ID: id, At: "2026-08-26T09:00:00Z", By: actorHuman, Text: text}
}

func TestAnAgentCannotDeleteTheHumansRequest(t *testing.T) {
	current := boardJSON(t, tab{
		ID: "ab1", Name: "Plan", Type: "notes",
		State:    json.RawMessage(`{"text":"one"}`),
		Requests: []requestAsk{pending("ab9", "fix the arrow")},
	})
	// The commonest shape by far: the agent edits the state and simply does not
	// carry the field it never read.
	incoming := boardJSON(t, tab{
		ID: "ab1", Name: "Plan", Type: "notes",
		State: json.RawMessage(`{"text":"two"}`),
	})

	var logged bytes.Buffer
	out, err := reconcileTabs(current, incoming, "agent-1", log.New(&logged, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	got := asksOn(t, out)
	if len(got) != 1 || got[0].ID != "ab9" || got[0].Text != "fix the arrow" {
		t.Fatalf("a write that never mentioned requests dropped one: %+v", got)
	}
	if !strings.Contains(logged.String(), "restored") {
		t.Errorf("nothing was logged about the restore; the log is the only channel this reaches: %q", logged.String())
	}
}

func TestAnAgentCannotEditOrReorderTheHumansRequests(t *testing.T) {
	current := boardJSON(t, tab{
		ID: "ab1", Name: "Plan", Type: "notes",
		Requests: []requestAsk{pending("ab9", "fix the arrow"), pending("ab10", "drop the third column")},
	})
	incoming := boardJSON(t, tab{
		ID: "ab1", Name: "Plan", Type: "notes",
		Requests: []requestAsk{
			{ID: "ab10", At: "REWRITTEN", By: "agent-1", Text: "already fine"},
			{ID: "ab9", At: "REWRITTEN", By: "agent-1", Text: "something I would rather do"},
		},
	})

	out, err := reconcileTabs(current, incoming, "agent-1", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	got := asksOn(t, out)
	if len(got) != 2 || got[0].ID != "ab9" || got[1].ID != "ab10" {
		t.Fatalf("the order is the human's and it moved: %+v", got)
	}
	for _, ask := range got {
		if ask.By != actorHuman || ask.At != "2026-08-26T09:00:00Z" {
			t.Errorf("%s was rewritten: %+v", ask.ID, ask)
		}
	}
	if got[0].Text != "fix the arrow" || got[1].Text != "drop the third column" {
		t.Errorf("the text was rewritten: %+v", got)
	}
}

func TestAnAgentCannotInventARequest(t *testing.T) {
	current := boardJSON(t, tab{ID: "ab1", Name: "Plan", Type: "notes"})
	incoming := boardJSON(t, tab{
		ID: "ab1", Name: "Plan", Type: "notes",
		Requests: []requestAsk{{ID: "ab9", By: actorHuman, Text: "the human definitely asked for this"}},
	})

	var logged bytes.Buffer
	out, err := reconcileTabs(current, incoming, "agent-1", log.New(&logged, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	if got := asksOn(t, out); len(got) != 0 {
		t.Fatalf("an agent put words in the human's mouth: %+v", got)
	}
	if !strings.Contains(logged.String(), "create") {
		t.Errorf("nothing was logged about the refusal: %q", logged.String())
	}
}

// The creation path is its own hole, exactly as it was for `seen`: a brand-new
// tab has no previous list to restore from, so the check cannot be a comparison
// and has to be its own branch.
func TestANewTabCannotArriveCarryingRequests(t *testing.T) {
	current := []byte(`{"nextId":1,"tabs":[]}`)
	incoming := boardJSON(t, tab{
		ID: "ab1", Name: "Plan", Type: "notes",
		Requests: []requestAsk{{ID: "ab9", By: actorHuman, Text: "invented"}},
	})

	out, err := reconcileTabs(current, incoming, "agent-1", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if got := asksOn(t, out); len(got) != 0 {
		t.Fatalf("a new tab arrived with requests on it: %+v", got)
	}
}

// The one change an agent MAY make. Without it the feature is a suggestion box
// with no way to answer.
func TestAnAgentMayStampARequestDone(t *testing.T) {
	current := boardJSON(t, tab{
		ID: "ab1", Name: "Plan", Type: "notes",
		Requests: []requestAsk{pending("ab9", "fix the arrow")},
	})
	incoming := boardJSON(t, tab{
		ID: "ab1", Name: "Plan", Type: "notes",
		Requests: []requestAsk{{
			ID: "ab9", At: "2026-08-26T09:00:00Z", By: actorHuman, Text: "fix the arrow",
			// `by` deliberately wrong: an agent claiming another session did it.
			Done: &doneStamp{By: "agent-9", Note: "flipped it"},
		}},
	})

	out, err := reconcileTabs(current, incoming, "agent-1", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	got := asksOn(t, out)
	if len(got) != 1 || got[0].Done == nil {
		t.Fatalf("the stamp was refused: %+v", got)
	}
	if got[0].Done.By != "agent-1" {
		t.Errorf("the stamp names %q; it must name the WRITER, or the human is reading an attribution nobody checked", got[0].Done.By)
	}
	if got[0].Done.At == "" {
		t.Error("a stamp with no time was written through; the server stamps the missing one")
	}
	if got[0].Done.Note != "flipped it" {
		t.Errorf("the agent's own reply was dropped: %+v", got[0].Done)
	}
}

func TestAnAgentCannotUnstampOrRestampARequest(t *testing.T) {
	done := &doneStamp{By: "agent-1", At: "2026-08-26T09:20:00Z", Note: "flipped it"}
	current := boardJSON(t, tab{
		ID: "ab1", Name: "Plan", Type: "notes",
		Requests: []requestAsk{{ID: "ab9", At: "T", By: actorHuman, Text: "fix the arrow", Done: done}},
	})

	for _, probe := range []struct {
		name string
		sent []requestAsk
	}{
		{"dropping the stamp", []requestAsk{{ID: "ab9", At: "T", By: actorHuman, Text: "fix the arrow"}}},
		{"replacing the stamp", []requestAsk{{
			ID: "ab9", At: "T", By: actorHuman, Text: "fix the arrow",
			Done: &doneStamp{By: "agent-2", At: "LATER", Note: "actually I did this"},
		}}},
	} {
		t.Run(probe.name, func(t *testing.T) {
			incoming := boardJSON(t, tab{ID: "ab1", Name: "Plan", Type: "notes", Requests: probe.sent})
			out, err := reconcileTabs(current, incoming, "agent-2", testLogger())
			if err != nil {
				t.Fatal(err)
			}
			got := asksOn(t, out)
			if len(got) != 1 || got[0].Done == nil {
				t.Fatalf("the stamp is gone: %+v", got)
			}
			if *got[0].Done != *done {
				t.Errorf("the stamp was rewritten to %+v; only the human deleting the whole note clears one", *got[0].Done)
			}
		})
	}
}

// The other direction, and the reason the field is worth having at all: the
// human creates, edits, reorders and deletes, and nothing here interferes.
func TestTheHumanOwnsTheirRequests(t *testing.T) {
	current := boardJSON(t, tab{
		ID: "ab1", Name: "Plan", Type: "notes",
		Requests: []requestAsk{
			pending("ab9", "fix the arrow"),
			{ID: "ab10", At: "T", By: actorHuman, Text: "done one", Done: &doneStamp{By: "agent-1", At: "T"}},
		},
	})
	// They delete both and add a third — a write the guarantees must not touch.
	incoming := boardJSON(t, tab{
		ID: "ab1", Name: "Plan", Type: "notes",
		Requests: []requestAsk{pending("ab11", "a new one")},
	})

	out, err := reconcileTabs(current, incoming, actorHuman, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	got := asksOn(t, out)
	if len(got) != 1 || got[0].ID != "ab11" {
		t.Fatalf("the human's own write was reconciled against them: %+v", got)
	}
}

// A stamp has to raise the dot and reach the journal, because it is the ONLY
// feedback the human gets that their note was read. Without `requests` in the
// change comparison the one write they are waiting on is the one that leaves no
// trace.
func TestStampingARequestMarksTheTabAndIsJournaled(t *testing.T) {
	cur, err := decodeDocument(boardJSON(t, tab{
		ID: "ab1", Name: "Plan", Type: "notes",
		State:    json.RawMessage(`{"text":"one"}`),
		Requests: []requestAsk{pending("ab9", "fix the arrow")},
	}))
	if err != nil {
		t.Fatal(err)
	}
	inc, err := decodeDocument(boardJSON(t, tab{
		ID: "ab1", Name: "Plan", Type: "notes",
		State: json.RawMessage(`{"text":"one"}`),
		Requests: []requestAsk{{
			ID: "ab9", At: "2026-08-26T09:00:00Z", By: actorHuman, Text: "fix the arrow",
			Done: &doneStamp{By: "agent-1", At: "2026-08-26T09:20:00Z"},
		}},
	}))
	if err != nil {
		t.Fatal(err)
	}

	plan := reconcileDoc(cur, inc, "agent-1", testLogger())
	got := plan.tabs[0]
	if !got.changed {
		t.Error("a done stamp was not recorded as a change, so the journal never sees it")
	}
	if got.Touched == nil {
		t.Error("a done stamp raised no dot; the human has no other signal that their note was read")
	}
	entry := summarise(cur, plan.tabs, "agent-1", "test")
	if len(entry.Tabs) != 1 || entry.Tabs[0] != "ab1" {
		t.Errorf("the journal entry names %v", entry.Tabs)
	}
}

// The id allocator has to see a request's id. It is the only tab field outside
// `state` that carries one, and the walk deliberately skipped every other field
// because none of them could.
func TestTheAllocatorCountsARequestsID(t *testing.T) {
	current := []byte(`{"nextId":1,"tabs":[{"id":"ab1","name":"Plan","type":"notes","state":{},` +
		`"requests":[{"id":"ab42","at":"T","by":"human","text":"fix it"}]}]}`)
	incoming := current

	if got := reconcileNextID(incoming, current); got != 43 {
		t.Fatalf("nextId = %d, want 43 — the next object allocated anywhere would take an id that already names the human's note", got)
	}
}

// The same claim one layer up, where the carry-forward lives: a write that adds
// a request and changes no state must still push the counter past it. A tab's
// `maxID` is carried forward untouched when its state is unchanged, and that is
// the optimisation which would otherwise skip exactly this case — idHigh is
// never asked, so the id on the human's new note is never counted.
//
// The incoming document's own `nextId` is deliberately STALE (42, not 43). It
// has to be, or the test proves nothing: nextIDFrom takes the larger of the
// declared counter and one past the highest id in use, so a document that has
// already bumped its own counter answers 43 whether the walk sees the request or
// not — which is how this test passed with the carry-forward guard removed. A
// counter that has fallen behind the ids in the document it arrives with is the
// case ids.go says this whole file is the safety net for: a hand-written
// document, or one built by a caller that never allocated through the browser.
func TestAddingARequestStillAdvancesTheCounter(t *testing.T) {
	current := []byte(`{"nextId":42,"tabs":[{"id":"ab1","name":"Plan","type":"notes","state":{"text":"one"}}]}`)
	incoming := []byte(`{"nextId":42,"tabs":[{"id":"ab1","name":"Plan","type":"notes","state":{"text":"one"},` +
		`"requests":[{"id":"ab42","at":"T","by":"human","text":"fix it"}]}]}`)

	cur, err := decodeDocument(current)
	if err != nil {
		t.Fatal(err)
	}
	inc, err := decodeDocument(incoming)
	if err != nil {
		t.Fatal(err)
	}
	plan := reconcileDoc(cur, inc, actorHuman, testLogger())

	// Their own write is also what `aboard wait --for request` fires on and what
	// the journal records, and both of those read this one flag. A note that
	// landed with the tab reported unchanged would be a note no waiting session
	// ever hears about.
	if !plan.tabs[0].changed {
		t.Error("adding a request left the tab reported as unchanged; nothing would wake a waiting session")
	}

	next := &stateDoc{fields: inc.fields, tabs: plan.tabs, byID: map[string]int{"ab1": 0}, hasTabs: true}
	next.nextID, _ = rawInt(next.fields[keyNextID])

	if got := nextIDFrom(next, cur); got != 43 {
		t.Fatalf("nextId = %d, want 43 — the next object allocated anywhere takes ab42, which already names the human's note", got)
	}
}

/* ---------- the listing, and stamping one ---------- */

func TestStampRequestFindsAndMarksOne(t *testing.T) {
	var doc map[string]any
	body := []byte(`{"tabs":[{"id":"ab1","name":"Plan","requests":[` +
		`{"id":"ab9","at":"T","by":"human","text":"fix it"}]}]}`)
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}

	tabID, already, err := stampRequest(doc, "ab9", "agent-1", "flipped it")
	if err != nil {
		t.Fatal(err)
	}
	if already != nil {
		t.Fatalf("a pending request reported as already stamped: %+v", already)
	}
	if tabID != "ab1" {
		t.Errorf("the stamp reports tab %q", tabID)
	}

	// Idempotent: a second run says so and writes nothing new.
	_, already, err = stampRequest(doc, "ab9", "agent-2", "me too")
	if err != nil {
		t.Fatal(err)
	}
	if already == nil || already.By != "agent-1" {
		t.Errorf("re-stamping did not report the first stamp: %+v", already)
	}

	if _, _, err := stampRequest(doc, "ab404", "agent-1", ""); err == nil {
		t.Error("an unknown request id was accepted")
	}
}

func TestPendingRequestPredicateReadsTheDocument(t *testing.T) {
	doc := []byte(`{"tabs":[
		{"id":"ab1","requests":[{"id":"ab9","text":"a","done":{"by":"agent-1","at":"T"}}]},
		{"id":"ab2","requests":[{"id":"ab10","text":"b"}]}
	]}`)

	if hasPendingRequest(doc, "ab1") {
		t.Error("a stamped request still counts as pending")
	}
	if !hasPendingRequest(doc, "ab2") {
		t.Error("a pending request on ab2 was not seen")
	}
	if !hasPendingRequest(doc, "") {
		t.Error("the board-wide form found nothing")
	}
	if hasPendingRequest([]byte(`{"tabs":[]}`), "") {
		t.Error("an empty board has a request on it")
	}
}

func TestRequestPredicateParses(t *testing.T) {
	for _, probe := range []struct {
		raw  string
		want predicate
	}{
		{"request", predicate{kind: predRequest}},
		{"request ab14", predicate{kind: predRequest, id: "ab14"}},
	} {
		got, err := parsePredicate(probe.raw)
		if err != nil {
			t.Fatalf("%q: %v", probe.raw, err)
		}
		if got != probe.want {
			t.Errorf("%q parsed to %+v, want %+v", probe.raw, got, probe.want)
		}
	}
	if _, err := parsePredicate("request ab14 ab15"); err == nil {
		t.Error("two tab ids were accepted; an unparseable predicate must be refused up front")
	}
}

/* ---------- the wait channel, and status ---------- */

// The one predicate that can be satisfied before it is asked. Every other form
// is about a write that has not happened yet; a note the human left an hour ago
// is a fact about the document, and blocking on it would be waiting for them to
// write the same note twice.
func TestWaitingOnARequestThatIsAlreadyThereReturnsAtOnce(t *testing.T) {
	srv := testServer(t, `{"version":1,"rev":1,"nextId":9,"tabs":[
		{"id":"ab1","name":"Plan","type":"notes","state":{},
		 "requests":[{"id":"ab8","at":"T","by":"human","text":"fix the arrow"}]}
	]}`)

	rec := httptest.NewRecorder()
	// `timeout=1` is not what is under test; it is what makes a REGRESSION
	// readable. Without it a broken build blocks for WaitDefault, the package's
	// own test timeout fires, and the report is a goroutine dump for every test
	// in the package rather than one line naming this one.
	srv.handleWait(rec, httptest.NewRequest(http.MethodGet, "/wait?for=request&timeout=1&by=agent-1", http.NoBody))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var ev pokeEvent
	if err := json.Unmarshal(rec.Body.Bytes(), &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Event != predRequest {
		t.Errorf("the waiter was released with %q, want %q", ev.Event, predRequest)
	}
	if srv.waits.count() != 0 {
		t.Error("a waiter was registered for a request that was already there — the notify button would claim a session is listening")
	}
}

// The other half: with nothing pending it must actually block, or the predicate
// is a no-op that returns immediately for every caller.
func TestWaitingOnARequestWithNonePendingBlocks(t *testing.T) {
	srv := testServer(t, `{"version":1,"rev":1,"nextId":9,"tabs":[
		{"id":"ab1","name":"Plan","type":"notes","state":{},
		 "requests":[{"id":"ab8","at":"T","by":"human","text":"done one",
		              "done":{"by":"agent-1","at":"T"}}]}
	]}`)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/wait?for=request&timeout=1&by=agent-1", http.NoBody)
	srv.handleWait(rec, req)

	var ev pokeEvent
	if err := json.Unmarshal(rec.Body.Bytes(), &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Event != eventTimeout {
		t.Errorf("a board with nothing pending released a waiter with %q", ev.Event)
	}
}

// `status` is the first command a resuming session runs, which is the whole
// reason the count is on it: a request nobody discovers is a request that was
// not made.
func TestStatusCountsPendingRequests(t *testing.T) {
	srv := testServer(t, `{"version":1,"rev":1,"nextId":9,"tabs":[
		{"id":"ab1","name":"Plan","type":"notes","state":{},
		 "requests":[{"id":"ab8","at":"T","by":"human","text":"one"},
		             {"id":"ab9","at":"T","by":"human","text":"two"},
		             {"id":"ab10","at":"T","by":"human","text":"three","done":{"by":"agent-1","at":"T"}}]}
	]}`)

	rep := Status(t.Context(), srv.root, "", web.FS)
	if rep.Requests != 2 {
		t.Fatalf("status reports %d pending requests, want 2 (the third is stamped)", rep.Requests)
	}
	if !strings.Contains(rep.Human(), "2 requests waiting") {
		t.Errorf("the human form says nothing about them:\n%s", rep.Human())
	}
	if !strings.Contains(rep.Human(), "aboard requests") {
		t.Errorf("the human form does not say how to read them:\n%s", rep.Human())
	}
}

/* ---------- the listing ---------- */

func TestListRequestsIsOldestFirstAndPendingByDefault(t *testing.T) {
	srv := testServer(t, `{"version":1,"rev":1,"nextId":9,"tabs":[
		{"id":"ab1","name":"Plan","type":"notes","state":{},
		 "requests":[{"id":"ab9","at":"2026-08-26T10:00:00Z","by":"human","text":"second"},
		             {"id":"ab8","at":"2026-08-26T09:00:00Z","by":"human","text":"first"}]},
		{"id":"ab2","name":"Screen","type":"notes","state":{},
		 "requests":[{"id":"ab10","at":"2026-08-26T11:00:00Z","by":"human","text":"stamped",
		              "done":{"by":"agent-1","at":"T","note":"did it"}}]}
	]}`)

	got, err := ListRequests(t.Context(), srv.root, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("the default listing has %d entries, want the 2 pending ones: %+v", len(got), got)
	}
	if got[0].ID != "ab8" || got[1].ID != "ab9" {
		t.Errorf("oldest first means ab8 then ab9, got %s then %s", got[0].ID, got[1].ID)
	}
	if got[0].TabName != "Plan" || got[0].Tab != "ab1" {
		t.Errorf("a request must name its tab as well as its id: %+v", got[0])
	}

	all, err := ListRequests(t.Context(), srv.root, "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Errorf("--all has %d entries, want 3", len(all))
	}

	one, err := ListRequests(t.Context(), srv.root, "", "ab2", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(one) != 1 || one[0].ID != "ab10" || one[0].Done == nil {
		t.Errorf("--tab ab2 --all: %+v", one)
	}

	// By name too, because that is what a human says out loud.
	byName, err := ListRequests(t.Context(), srv.root, "", "Screen", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(byName) != 1 {
		t.Errorf("--tab Screen: %+v", byName)
	}
}

// Nothing pending is an ANSWER, not an empty response — the same posture
// `aboard boards` takes about finding no board.
func TestRequestsHumanSaysNothingPendingOutLoud(t *testing.T) {
	got := RequestsHuman(nil, "", false, "")
	if !strings.Contains(got, "nothing pending") {
		t.Errorf("an empty listing printed %q", got)
	}
}

// The listing ends with a command, and a command in output is a claim — the one
// that has been got wrong here before is the missing --name, which reads the
// right board and writes the wrong one.
func TestRequestsHumanSplicesTheBoardName(t *testing.T) {
	list := []Request{{ID: "ab8", Tab: "ab1", TabName: "Plan", At: "T", Text: "fix it"}}
	if got := RequestsHuman(list, "", false, "review"); !strings.Contains(got, "--name review") {
		t.Errorf("a named board's listing prints an unqualified command:\n%s", got)
	}
	if got := RequestsHuman(list, "", false, ""); strings.Contains(got, "--name") {
		t.Errorf("the default board's listing invented a --name:\n%s", got)
	}
}

func TestCompleteRequestRefusesToActAsTheHuman(t *testing.T) {
	srv := testServer(t, `{"version":1,"rev":1,"nextId":9,"tabs":[]}`)
	var out bytes.Buffer
	err := CompleteRequest(t.Context(), srv.root, "", "ab8", actorHuman, "", &out)
	if err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("--by human was accepted: %v", err)
	}
}

// The predicate reads the BOARD, not the read cache.
//
// `cachedState` is allowed to be stale — it is gated on a stat, and a GET served
// bytes one interval old is corrected by the next SSE frame. There is no next
// frame here: a waiter answered from a stale copy goes to sleep on a note that
// is already on disk, and stays asleep until some unrelated write happens along.
// The cache is pinned by hand below because that is the only way to produce the
// state deterministically; the shape is a rewrite the stat cannot tell apart.
func TestTheRequestPredicateReadsTheBoardNotTheReadCache(t *testing.T) {
	srv := testServer(t, `{"version":1,"rev":1,"nextId":9,"tabs":[
		{"id":"ab1","name":"Plan","type":"notes","state":{}}
	]}`)
	stale, err := srv.cachedState()
	if err != nil {
		t.Fatal(err)
	}

	asked := []byte(`{"version":1,"rev":2,"nextId":9,"tabs":[
		{"id":"ab1","name":"Plan","type":"notes","state":{},
		 "requests":[{"id":"ab8","at":"T","by":"human","text":"fix the arrow"}]}
	]}`)
	if err := os.WriteFile(srv.stateFile, asked, 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(srv.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	srv.live.Store(&liveDoc{disk: stale.disk, etag: stale.etag, stamp: stampOf(info)})

	rec := httptest.NewRecorder()
	srv.handleWait(rec, httptest.NewRequest(http.MethodGet, "/wait?for=request&timeout=1&by=agent-1", http.NoBody))

	var ev pokeEvent
	if err := json.Unmarshal(rec.Body.Bytes(), &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Event != predRequest {
		t.Errorf("released with %q: the note is on disk and the waiter was answered from a cached copy that predates it", ev.Event)
	}
	if !strings.Contains(ev.Note, "already waiting") {
		t.Errorf("the event says %q rather than a sentence a person reads", ev.Note)
	}
}

// A `request <tab>` satisfied at once names the tab in a sentence rather than
// handing back the bare id as the event's note.
func TestAnAlreadyWaitingRequestNamesItsTab(t *testing.T) {
	if got := requestAlreadyWaiting(""); got != "a request was already waiting" {
		t.Errorf("board-wide: %q", got)
	}
	if got := requestAlreadyWaiting("ab14"); !strings.Contains(got, "ab14") || !strings.Contains(got, "already waiting") {
		t.Errorf("per-tab: %q", got)
	}
}

// The restore path is the one command whose job is to undo, and `requests` is
// the fifth thing it must not undo: the record holds the whole tab, so a restore
// that put its `requests` back would re-raise a note the human deleted and
// un-stamp one they had already been told was handled.
func TestARestoreDoesNotResurrectTheHumansRequests(t *testing.T) {
	root := Root(t.TempDir())
	if err := os.MkdirAll(root.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	// The board as it stands: one live note, and the one they deleted is gone.
	if err := os.WriteFile(root.StateFile(""), []byte(`{"version":1,"rev":9,"nextId":20,"tabs":[
		{"id":"ab2","name":"Plan","type":"notes","state":{"v":2},
		 "requests":[{"id":"ab11","at":"T","by":"human","text":"the live one"}]}
	]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// The record: an older list, with a note they have since thrown away and a
	// stamp on the one that is still there.
	journalWith(t, root, entryV2(8, "2026-08-26T09:03:00.000Z", "agent-1", "ab2",
		`{"id":"ab2","name":"Plan","type":"notes","state":{"v":1},`+
			`"requests":[{"id":"ab10","at":"T","by":"human","text":"deleted since"},`+
			`{"id":"ab11","at":"T","by":"human","text":"the live one",`+
			`"done":{"by":"agent-1","at":"T"}}]}`))

	// Restore called directly rather than through journal_generations_test.go's
	// helper: that one has a single caller by design, and a second one with the
	// same arguments turns it into a function with a constant parameter.
	var printed strings.Builder
	if err := Restore(t.Context(), root, "", "ab2", 1, &printed); err != nil {
		t.Fatalf("the restore failed: %v", err)
	}
	out, err := decodeDocument([]byte(printed.String()))
	if err != nil {
		t.Fatalf("the restore document does not parse: %v", err)
	}
	got := out.tabs[out.byID["ab2"]].Requests
	if len(got) != 1 || got[0].ID != "ab11" {
		t.Fatalf("the restore rewrote the human's list: %+v", got)
	}
	if got[0].Done != nil {
		t.Errorf("the restore put an old stamp back on a live note: %+v", got[0].Done)
	}
}

// The same wait, released the other way. `--for request` can be satisfied by a
// write OR by a note that was already sitting there, and those arrive through
// two different code paths — so the one thing a caller can branch on has to say
// the same word both times.
func TestAWaiterReleasedByANewRequestSaysSoAndNotJustChange(t *testing.T) {
	hub := newWaitHub()
	asking := hub.add("agent-1", predRequest, "", predicate{kind: predRequest}, time.Minute)
	watching := hub.add("agent-2", predChange, "", predicate{kind: predChange}, time.Minute)

	doc := []byte(`{"tabs":[{"id":"ab1","requests":[{"id":"ab9","by":"human","text":"fix it"}]}]}`)
	entry := JournalEntry{At: "2026-08-26T09:00:00.000Z", By: actorHuman, Tabs: []string{"ab1"}}
	if got := hub.releaseMatching(doc, entry); got != 2 {
		t.Fatalf("released %d waiters, want both", got)
	}

	if ev := <-asking.ch; ev.Event != predRequest {
		t.Errorf("a session waiting on a request was released with %q — the same wait must not answer two different words depending on the human's timing", ev.Event)
	}
	// And nothing else is renamed: `change` still means a write happened.
	if ev := <-watching.ch; ev.Event != predChange {
		t.Errorf("a session waiting on any change was released with %q", ev.Event)
	}
}
