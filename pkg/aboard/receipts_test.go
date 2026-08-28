package aboard

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func postReceipt(t *testing.T, srv *server, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.route(rec, httptest.NewRequest(http.MethodPost, "http://localhost/rendered", strings.NewReader(body)))
	return rec
}

// The whole point: what the browser DREW comes back to the agent that wrote the
// document, instead of only to the human looking at the screen.
func TestARenderedReceiptComesBackOutOfTheSidecar(t *testing.T) {
	srv := testServer(t, htmlTabBoard)

	if rec := postReceipt(t, srv, `{"tab":"ab1","type":"ui","mount":true,"controls":["fit","relayout"],
		"undeclared":["mystery"],"unknown":["sparkline"],"fired":{"fit":2}}`); rec.Code != http.StatusOK {
		t.Fatalf("POST /rendered answered %d: %s", rec.Code, rec.Body.String())
	}

	got, err := Rendered(t.Context(), srv.root, srv.name, "ab1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d receipts, want 1", len(got))
	}
	r := got[0]
	if r.Mounts != 1 || r.Type != "ui" {
		t.Errorf("unexpected receipt: %+v", r)
	}
	if strings.Join(r.Controls, ",") != "fit,relayout" {
		t.Errorf("controls = %v", r.Controls)
	}
	if strings.Join(r.Undeclared, ",") != "mystery" || strings.Join(r.Unknown, ",") != "sparkline" {
		t.Errorf("the markers were lost: %+v", r)
	}
	if r.Fired["fit"] != 2 {
		t.Errorf("fired = %v", r.Fired)
	}
}

// Two different lifetimes in one record, and getting them the wrong way round
// would make the receipt useless either way. Presses ACCUMULATE — one that
// happened did happen. Markers are REPLACED — a marker that was fixed must stop
// being reported, or the receipt becomes a list of things that were once wrong.
func TestReceiptsAccumulatePressesAndReplaceMarkers(t *testing.T) {
	srv := testServer(t, htmlTabBoard)
	postReceipt(t, srv, `{"tab":"ab1","type":"ui","mount":true,"unknown":["sparkline"],"fired":{"fit":1}}`)
	postReceipt(t, srv, `{"tab":"ab1","type":"ui","mount":true,"fired":{"fit":1,"relayout":3}}`)

	// A press report is not a mount. Counting one as a mount would make
	// "mounted 9×" mean "somebody clicked eight times".
	postReceipt(t, srv, `{"tab":"ab1","type":"ui","fired":{"relayout":1}}`)

	got, err := Rendered(t.Context(), srv.root, srv.name, "ab1")
	if err != nil || len(got) != 1 {
		t.Fatalf("reading back: %v %+v", err, got)
	}
	if got[0].Mounts != 2 {
		t.Errorf("mounts = %d, want 2", got[0].Mounts)
	}
	if got[0].Fired["fit"] != 2 || got[0].Fired["relayout"] != 4 {
		t.Errorf("presses must accumulate across mounts: %v", got[0].Fired)
	}
	if len(got[0].Unknown) != 0 {
		t.Errorf("a marker that is gone must stop being reported: %v", got[0].Unknown)
	}
}

// A receipt is per-viewer, machine-local and true only for this moment — the
// same rule that keeps selection, zoom and chat drafts out of the document. The
// board must be byte-identical afterwards.
func TestAReceiptNeverTouchesTheBoardDocument(t *testing.T) {
	srv := testServer(t, htmlTabBoard)
	before, err := os.ReadFile(srv.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	postReceipt(t, srv, `{"tab":"ab1","type":"ui","controls":["fit"]}`)
	after, err := os.ReadFile(srv.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("posting a receipt rewrote the board document")
	}
	if _, err := os.Stat(srv.root.RenderedFile("")); err != nil {
		t.Errorf("the receipt did not land in the sidecar: %v", err)
	}
}

// The tab id becomes a key in a file a terminal prints and the string a
// `rendered <id>` predicate is compared against, so it is validated rather than
// sanitised — the same rule a sidecar log's filename gets.
func TestAReceiptRefusesATabIdThatIsNotOne(t *testing.T) {
	srv := testServer(t, htmlTabBoard)
	if rec := postReceipt(t, srv, `{"tab":"../../etc/passwd"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a path as a tab id answered %d, want 400", rec.Code)
	}
	if rec := postReceipt(t, srv, `not json`); rec.Code != http.StatusBadRequest {
		t.Errorf("a non-receipt answered %d, want 400", rec.Code)
	}
}

// The two limits travel WITH the output, not only in the docs: this is a command
// whose whole product is a claim about evidence, and a reader who takes it for
// more than it is stops looking at the board.
func TestRenderedPrintsWhatItIsNotEvidenceOf(t *testing.T) {
	got := RenderedHuman("ab1", nil)
	if !strings.Contains(got, "no mount receipt for ab1") {
		t.Errorf("an absent receipt must say so: %q", got)
	}
	for _, want := range []string{"nobody had the tab OPEN", "REACHED"} {
		if !strings.Contains(got, want) {
			t.Errorf("the output must carry the limit %q:\n%s", want, got)
		}
	}
}

// A session can block until a tab is actually drawn. Released from the receipt
// endpoint and not from the write path, because a mount is not a write: nothing
// about the document changed.
func TestRenderedReleasesAWaitingSession(t *testing.T) {
	srv := testServer(t, htmlTabBoard)
	pred, err := parsePredicate("rendered ab1")
	if err != nil {
		t.Fatal(err)
	}
	w := srv.waits.add("agent-1", "rendered ab1", "", pred, time.Minute)

	// The wrong tab must not release it: a predicate that fires on anything is a
	// predicate that tells the caller nothing.
	postReceipt(t, srv, `{"tab":"ab2","type":"ui"}`)
	select {
	case ev := <-w.ch:
		t.Fatalf("released by the wrong tab: %+v", ev)
	default:
	}

	postReceipt(t, srv, `{"tab":"ab1","type":"ui"}`)
	select {
	case ev := <-w.ch:
		if ev.Event != "rendered" || ev.Note != "ab1" {
			t.Errorf("unexpected release event: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("a receipt for ab1 did not release a session waiting on it")
	}
}

// An unknown predicate is refused up front rather than accepted and never fired,
// and the message has to list what there is — including the new one.
func TestTheRenderedPredicateIsInTheVocabulary(t *testing.T) {
	if _, err := parsePredicate("rendered"); err == nil {
		t.Error("`rendered` with no id must be refused")
	}
	if _, err := parsePredicate("nonsense ab1"); err == nil || !strings.Contains(err.Error(), "rendered <id>") {
		t.Errorf("the refusal must offer the whole vocabulary, got %v", err)
	}
	// It is not a write predicate: matching a write would release it on the next
	// unrelated edit.
	pred, err := parsePredicate("rendered ab1")
	if err != nil {
		t.Fatal(err)
	}
	if pred.matches([]byte(`{}`), JournalEntry{By: "human", Tabs: []string{"ab1"}}) {
		t.Error("`rendered` must not be released by a write to the tab")
	}
}

// The store is bounded. A renderer that drew a control per row could otherwise
// grow the sidecar without limit, and the answer stops being useful long before
// then anyway.
func TestReceiptIdsAreBounded(t *testing.T) {
	ids := make([]string, maxReceiptIDs+50)
	for i := range ids {
		ids[i] = "c" + strings.Repeat("x", i%5) + string(rune('a'+i%26)) + strconv.Itoa(i)
	}
	if got := capIDs(ids); len(got) != maxReceiptIDs {
		t.Errorf("capIDs kept %d ids, want the cap of %d", len(got), maxReceiptIDs)
	}
	if got := capIDs([]string{" ", "", strings.Repeat("z", 200)}); got != nil {
		t.Errorf("blank and over-long ids must be dropped, got %v", got)
	}
}
