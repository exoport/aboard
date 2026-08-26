package aboard

// document_test.go — the write path costs the EDIT, not the board.
//
// Every assertion here is about how much work a write does, not what it
// produces, and every one is counted rather than eyeballed: the old costs were
// invisible at the sizes anybody develops against (a 65 KB example board), which
// is exactly why seven full-document decodes per POST survived in code whose
// comments described three.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// board builds a document of n small tabs plus one the tests edit, in the shape
// the browser posts: compact JSON.
func manyTabs(n int) string {
	var b strings.Builder
	b.WriteString(`{"version":3,"rev":1,"nextId":9000,"updatedAt":"2026-08-26T00:00:00.000Z","lastEditedBy":"agent-1","tabs":[`)
	for i := 1; i <= n; i++ {
		if i > 1 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"id":"bb%d","name":"Tab %d","type":"notes","state":{"text":"body %d","items":[{"id":"bb%d"}]}}`,
			i, i, i, 1000+i)
	}
	b.WriteString(`]}`)
	return b.String()
}

// editOneTab returns the same document with tab bb1's text changed, plus the
// envelope a real POST carries.
func editOneTab(doc string, rev int, text string) string {
	edited := strings.Replace(doc, `"text":"body 1"`, `"text":"`+text+`"`, 1)
	return `{"__origin":"test","__by":"agent-1","__base":"` + strconv.Itoa(rev) + `",` + edited[1:]
}

func postOK(t *testing.T, srv *server, body string) {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.postState(rec, httptest.NewRequest(http.MethodPost, "/aboard.json", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %d: %s", rec.Code, rec.Body)
	}
}

// The item-2 acceptance, stated exactly: one decode of the incoming body per
// POST, and none of the document already in memory.
//
// It fails on the old write path with `decodes = 7`: the incoming body was
// unmarshalled once for the envelope, the current document once for the
// revision, both of them again inside reconcileTabs, both AGAIN inside
// reconcileNextID, and the current one a fifth time for the change summary.
func TestAPostDecodesTheBodyOnceAndTheBoardNotAtAll(t *testing.T) {
	doc := manyTabs(200)
	srv := testServer(t, doc)

	// Warm the cache: the first write after a start has to read and parse what is
	// on disk, and that is not the case being measured.
	postOK(t, srv, editOneTab(doc, 1, "first"))

	before := documentDecodes.Load()
	postOK(t, srv, editOneTab(strings.Replace(doc, `"text":"body 1"`, `"text":"first"`, 1), 2, "second"))
	got := documentDecodes.Load() - before

	if got != 1 {
		t.Errorf("a POST decoded %d documents, want exactly 1 (the incoming body)", got)
	}
}

// The item-3 acceptance: with N tabs and one edited, the codec looks inside one
// tab's state — the same number on a board of 15 and a board of 500.
//
// The old jsonEqual unmarshalled and re-marshalled EVERY tab's state to compare
// it, and did so twice per write (once in reconcileTabs, once in changeSummary),
// so this counted 2N before and does not grow at all now.
func TestOneEditTouchesOneTabsState(t *testing.T) {
	counts := map[int]int64{}
	for _, n := range []int{15, 500} {
		doc := manyTabs(n)
		srv := testServer(t, doc)
		postOK(t, srv, editOneTab(doc, 1, "first"))

		before := codecTouches()
		postOK(t, srv, editOneTab(strings.Replace(doc, `"text":"body 1"`, `"text":"first"`, 1), 2, "second"))
		counts[n] = codecTouches() - before
	}
	if counts[15] != counts[500] {
		t.Errorf("editing one tab cost %d codec touches on 15 tabs and %d on 500 — the work still scales with the board",
			counts[15], counts[500])
	}
	if counts[15] > 4 {
		t.Errorf("editing one tab cost %d codec touches; one tab has two sides and at most one canonical fallback", counts[15])
	}
}

// The item-4 acceptance: the id allocator walks the tabs a write changed, not
// the whole document twice. Same shape of assertion, and the same failure on the
// old code — maxUsedID recursed through every tab of both documents, per write.
func TestTheIDAllocatorWalksOnlyWhatChanged(t *testing.T) {
	walks := map[int]int64{}
	for _, n := range []int{15, 500} {
		doc := manyTabs(n)
		srv := testServer(t, doc)
		postOK(t, srv, editOneTab(doc, 1, "first"))

		before := idWalks.Load()
		postOK(t, srv, editOneTab(strings.Replace(doc, `"text":"body 1"`, `"text":"first"`, 1), 2, "second"))
		walks[n] = idWalks.Load() - before
	}
	if walks[15] != walks[500] || walks[15] > 1 {
		t.Errorf("editing one tab walked %d tabs for ids on a board of 15 and %d on a board of 500, want 1 and 1",
			walks[15], walks[500])
	}
}

// The counter still has to be right, not just cheap: a tab whose state gained a
// higher id than the document's counter must raise it, and that tab is the one
// the walk is now narrowed to.
func TestANewIDInAChangedTabStillRaisesTheCounter(t *testing.T) {
	doc := manyTabs(20)
	srv := testServer(t, doc)
	postOK(t, srv, editOneTab(doc, 1, "warm"))

	grown := strings.Replace(strings.Replace(doc, `"text":"body 1"`, `"text":"warm"`, 1),
		`{"id":"bb1001"}`, `{"id":"bb1001"},{"id":"bb40000"}`, 1)
	postOK(t, srv, `{"__origin":"test","__by":"agent-1","__base":"2",`+grown[1:])

	var got struct {
		NextID int `json:"nextId"`
	}
	body, err := os.ReadFile(srv.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.NextID != 40001 {
		t.Errorf("nextId = %d after an id of bb40000 arrived in a changed tab, want 40001", got.NextID)
	}
}

// The hazard the normalisation exists for, and the one that would have shipped
// as "every tab lit up after a restart".
//
// The state file is written INDENTED and the browser posts COMPACT, so the first
// write after a start compares an indented state against a compact one for every
// tab on the board. Comparing raw bytes alone would call all of them changed: a
// dot on every tab, a journal line for every tab, and a `touched` marker naming
// an agent that had done nothing.
func TestAnIndentedBoardIsNotAllChangedByACompactWrite(t *testing.T) {
	compact := manyTabs(6)
	var pretty map[string]any
	if err := json.Unmarshal([]byte(compact), &pretty); err != nil {
		t.Fatal(err)
	}
	indented, err := json.MarshalIndent(pretty, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	srv := testServer(t, string(indented))

	postOK(t, srv, `{"__origin":"test","__by":"agent-1","__base":"1",`+compact[1:])

	for _, got := range srv.readTabs(t) {
		if got.Touched != nil {
			t.Errorf("tab %s was marked as changed by a write that only reformatted it: %+v", got.ID, got.Touched)
		}
	}
}

// The other half of the same claim, and the reason the cheap normalisation layer
// exists at all: correctness alone does not need it — the canonical fallback
// below would give the right answer — but on the first write after a restart
// that fallback would run for EVERY tab on the board, because the file is
// indented and the browser posts compact. Normalisation is what keeps the
// expensive comparison down to the tabs that genuinely differ.
func TestAnIndentedBoardIsNotCanonicalisedTabByTab(t *testing.T) {
	compact := manyTabs(200)
	// Indented, key order untouched: exactly what the server writes, since a
	// state blob is spliced through as the bytes it arrived as.
	var indented bytes.Buffer
	if err := json.Indent(&indented, []byte(compact), "", "  "); err != nil {
		t.Fatal(err)
	}
	srv := testServer(t, indented.String())

	before := stateCanonicalisations.Load()
	postOK(t, srv, editOneTab(compact, 1, "first"))
	got := stateCanonicalisations.Load() - before

	if got > 1 {
		t.Errorf("the first write against an indented board canonicalised %d tabs; one tab changed", got)
	}
}

// A writer that reorders object keys has not changed anything, and the old
// canonicalising comparison knew that. The cheap byte compare does not, so the
// canonical fallback is what keeps the semantics — this is the test that says it
// is still there.
func TestReorderedKeysAreNotAChange(t *testing.T) {
	current := `{"version":3,"rev":1,"nextId":9,"tabs":[{"id":"bb1","name":"Plan","type":"notes","state":{"a":1,"b":2}}]}`
	srv := testServer(t, current)

	postOK(t, srv, `{"__origin":"test","__by":"agent-1","__base":"1","version":3,"nextId":9,"tabs":[`+
		`{"id":"bb1","name":"Plan","type":"notes","state":{"b":2,"a":1}}]}`)

	if got := srv.readTabs(t); got[0].Touched != nil {
		t.Errorf("reordering the keys of a state blob raised a change marker: %+v", got[0].Touched)
	}
}

// The state blob is the renderer's, and the server now splices it through as the
// bytes it arrived as instead of round-tripping it through map[string]any. The
// visible consequence is that an author's key order survives a write, where it
// used to be alphabetised — asserted rather than left to be discovered.
func TestAStateBlobKeepsItsAuthorsKeyOrder(t *testing.T) {
	srv := testServer(t, `{"version":3,"rev":1,"nextId":9,"tabs":[]}`)
	postOK(t, srv, `{"__origin":"test","__by":"agent-1","__base":"1","version":3,"nextId":9,"tabs":[`+
		`{"id":"bb1","name":"Plan","type":"ui","state":{"type":"panel","title":"Z","children":[]}}]}`)

	body, err := os.ReadFile(srv.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	typeAt := strings.Index(string(body), `"type": "panel"`)
	childrenAt := strings.Index(string(body), `"children"`)
	if typeAt < 0 || childrenAt < 0 {
		t.Fatalf("the state did not survive the write:\n%s", body)
	}
	if typeAt > childrenAt {
		t.Error("the state's keys were alphabetised; state is opaque to the server and its order is the author's")
	}
}

// The item-6 acceptance. A client that already holds this version gets 304 and
// no body; a write moves the tag.
func TestTheStateDocumentHasAnETagAndAnswers304(t *testing.T) {
	srv := testServer(t, manyTabs(4))

	first := httptest.NewRecorder()
	srv.route(first, httptest.NewRequest(http.MethodGet, "http://localhost/aboard.json", http.NoBody))
	etag := first.Header().Get("ETag")
	if first.Code != http.StatusOK || etag == "" {
		t.Fatalf("GET = %d with ETag %q", first.Code, etag)
	}
	if store := first.Header().Get("Cache-Control"); store == "no-store" {
		t.Error("no-store forbids the client from keeping a copy to revalidate, which makes the ETag useless")
	}

	again := httptest.NewRequest(http.MethodGet, "http://localhost/aboard.json", http.NoBody)
	again.Header.Set("If-None-Match", etag)
	rec := httptest.NewRecorder()
	srv.route(rec, again)
	if rec.Code != http.StatusNotModified {
		t.Errorf("a matching If-None-Match got %d, want 304", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("a 304 carried %d bytes of body", rec.Body.Len())
	}

	postOK(t, srv, editOneTab(manyTabs(4), 1, "moved"))
	after := httptest.NewRecorder()
	stale := httptest.NewRequest(http.MethodGet, "http://localhost/aboard.json", http.NoBody)
	stale.Header.Set("If-None-Match", etag)
	srv.route(after, stale)
	if after.Code != http.StatusOK {
		t.Errorf("after a write the old tag still matched: %d", after.Code)
	}
}

// The item-5 acceptance: a watcher tick reads nothing when the file has not
// moved. Proved by moving the CONTENT without moving the stat — if the tick
// still hashed the file, the signature would change.
func TestTheWatcherTickHashesOnlyWhenTheFileMoved(t *testing.T) {
	srv := testServer(t, manyTabs(4))

	first := srv.stateSignature()
	if first == "" {
		t.Fatal("no signature at all")
	}

	info, err := os.Stat(srv.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(srv.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	// Same length, different bytes, same mtime.
	swapped := strings.Replace(string(body), `"body 1"`, `"BODY 1"`, 1)
	if swapped == string(body) {
		t.Fatal("the fixture did not contain the text this test swaps")
	}
	if err := os.WriteFile(srv.stateFile, []byte(swapped), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(srv.stateFile, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}

	if got := srv.stateSignature(); got != first {
		t.Error("the tick re-hashed a file whose size and mtime had not moved; the idle cost is still the whole document")
	}

	// And it does notice a real change, so the gate is a gate and not a mute.
	if err := os.Chtimes(srv.stateFile, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := srv.stateSignature(); got == first {
		t.Error("the tick missed a change the stat announced")
	}
}

// Item 7. 8 MiB was the honest ceiling while a write cost a multiple of the
// document; it is not any more, and a board is a thing that grows. The number is
// asserted because a limit nobody tests is a limit that drifts back.
func TestTheBodyCeilingIsThirtyTwoMiB(t *testing.T) {
	if maxBodyBytes != 32<<20 {
		t.Fatalf("maxBodyBytes = %d, want 32 MiB", maxBodyBytes)
	}

	srv := testServer(t, `{"version":3,"rev":1,"nextId":9,"tabs":[]}`)
	// Comfortably over the old 8 MiB limit, which refused it before any parser
	// ran, and comfortably under the new one.
	filler := strings.Repeat("x", 9<<20)
	postOK(t, srv, `{"__origin":"test","__by":"agent-1","__base":"1","version":3,"nextId":9,"tabs":[`+
		`{"id":"bb1","name":"Widget","type":"html","state":{"html":"`+filler+`"}}]}`)
}

// The document a POST hands back has to be re-readable as the one on disk: the
// server now keeps it parsed rather than re-reading it, so a divergence between
// what it wrote and what it believes it wrote would be invisible until the next
// restart.
func TestTheCachedDocumentMatchesTheFileAfterEveryWrite(t *testing.T) {
	doc := manyTabs(8)
	srv := testServer(t, doc)
	for i, text := range []string{"one", "two", "three"} {
		body := editOneTab(doc, i+1, text)
		postOK(t, srv, body)
		doc = strings.Replace(doc, `"text":"body 1"`, `"text":"`+text+`"`, 1)
		doc = strings.Replace(doc, `"text":"`+text+`"`, `"text":"body 1"`, 0)

		onDisk, err := os.ReadFile(srv.stateFile)
		if err != nil {
			t.Fatal(err)
		}
		live := srv.live.Load()
		if live == nil {
			t.Fatal("no cached document after an accepted write")
		}
		if string(live.disk) != string(onDisk) {
			t.Fatalf("the cache and the file disagree after write %d", i+1)
		}
		if live.etag != etagOf(onDisk) {
			t.Fatalf("the cached ETag does not describe the file after write %d", i+1)
		}
	}
}

// An external edit between two writes must be SEEN by the reconciler, not read
// out of a stale cache.
//
// The write path re-reads the file and compares the bytes rather than trusting a
// stat, precisely so a change a stat cannot see — same size, same mtime tick —
// cannot be reconciled against a document that no longer exists. The tab an agent
// DROPS is where that shows: guarantee 1 restores it from the current document,
// so a cached copy would restore the version the editor had just replaced.
func TestAnExternalEditIsSeenByTheNextWrite(t *testing.T) {
	doc := manyTabs(3)
	srv := testServer(t, doc)
	postOK(t, srv, editOneTab(doc, 1, "warm"))

	// Somebody edits the file directly, keeping the byte count and the mtime.
	body, err := os.ReadFile(srv.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(srv.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	swapped := strings.Replace(string(body), `"body 3"`, `"BODY 3"`, 1)
	if swapped == string(body) {
		t.Fatal("the fixture did not contain the text this test swaps")
	}
	if err := os.WriteFile(srv.stateFile, []byte(swapped), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(srv.stateFile, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}

	// An agent write that drops bb3. It comes back — from the file, which is the
	// only copy that has the edit.
	twoTabs := strings.Replace(manyTabs(2), `"text":"body 1"`, `"text":"warm"`, 1)
	postOK(t, srv, `{"__origin":"test","__by":"agent-1","__base":"2",`+twoTabs[1:])

	after, err := os.ReadFile(srv.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), "BODY 3") {
		t.Error("the restored tab came from a cached document, not from the file the editor had just changed")
	}
}

// A write that lands INSIDE a read must not pin the read cache stale.
//
// The state file is replaced by rename (writeAtomic), so a reader that has
// already opened the old inode reads the old bytes in full while the path
// already names the new file. Stamping those bytes with a stat taken AFTER the
// read — which is what the first version of this cache did — records the new
// file's size and mtime against the old file's content, and every later request
// then stats, matches, and is served the superseded document. Not one poll
// interval stale and corrected by the next SSE frame, which is the trade the
// read cache was argued for: permanently stale, ETag and all, so the browser's
// own revalidation answers 304 for a board that no longer exists, until
// something else happens to move the file.
//
// Each document below is strictly LARGER than the one before, so no two stats
// can agree by coincidence and the assertion cannot pass for the wrong reason.
// Against the stat-after-read version this fails within a round or two, every
// run; readStable's stat-read-stat is what makes it green.
func TestAWriteLandingInsideAReadDoesNotPinTheCacheStale(t *testing.T) {
	mk := func(n int) string {
		return `{"version":3,"rev":1,"nextId":9,"tabs":[{"id":"bb1","name":"P","type":"notes","state":{"text":"` +
			strings.Repeat("x", (4<<20)+n*997) + `"}}]}`
	}
	srv := testServer(t, mk(0))
	dir := filepath.Dir(srv.stateFile)

	// Prepared up front so the burst below is nothing but renames: the window
	// being aimed at is the one inside a single read.
	var writes []string
	for i := 1; i <= 60; i++ {
		p := filepath.Join(dir, "next-"+strings.Repeat("i", i))
		if err := os.WriteFile(p, []byte(mk(i)), 0o644); err != nil {
			t.Fatal(err)
		}
		writes = append(writes, p)
	}

	for round := range 6 {
		var wg sync.WaitGroup
		stop := make(chan struct{})
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				rec := httptest.NewRecorder()
				srv.route(rec, httptest.NewRequest(http.MethodGet, "http://localhost/aboard.json", http.NoBody))
			}
		}()
		for _, p := range writes {
			// Hardlink then rename, so each round can reuse the same prepared
			// documents without writing megabytes again.
			if err := os.Link(p, p+"-live"); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(p+"-live", srv.stateFile); err != nil {
				t.Fatal(err)
			}
		}
		close(stop)
		wg.Wait()

		onDisk, err := os.ReadFile(srv.stateFile)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		srv.route(rec, httptest.NewRequest(http.MethodGet, "http://localhost/aboard.json", http.NoBody))
		if rec.Body.String() != string(onDisk) {
			t.Fatalf("round %d: a GET served %d bytes where the file holds %d — the cache is pinned to a document that no longer exists",
				round, rec.Body.Len(), len(onDisk))
		}
		if tag := rec.Header().Get("ETag"); tag != etagOf(onDisk) {
			t.Fatalf("round %d: the ETag describes bytes that are not the file's, so a revalidation would answer 304 for a stale board", round)
		}
	}
}

// A state file that is THERE and cannot be read is not an empty board.
//
// The read error was swallowed — `raw = nil` — and an empty `raw` decodes to a
// board with no tabs at all, which is exactly the document the guarantees
// restore dropped tabs FROM. So a write arriving while the file could not be
// opened would have been reconciled against nothing, and every tab the caller
// did not happen to include would have gone with no removal request, no marker
// and no journal line. A file that does not exist yet still has to be allowed
// through, because that is the first POST on a fresh root.
func TestAnUnreadableBoardIsRefusedRatherThanTakenAsEmpty(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, where a mode of 0 is not a refusal")
	}
	srv := testServer(t, manyTabs(3))
	if err := os.Chmod(srv.stateFile, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(srv.stateFile, 0o644) })

	rec := httptest.NewRecorder()
	srv.postState(rec, httptest.NewRequest(http.MethodPost, "/aboard.json",
		strings.NewReader(`{"__by":"agent-1","version":3,"nextId":9,"tabs":[]}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a write against a board that could not be read was answered %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "unreadable") {
		t.Errorf("the refusal does not say what was wrong: %s", rec.Body)
	}
}

// The other half: a board that does not exist yet is not an error, because the
// first POST on a fresh root is what creates it.
func TestAFirstWriteWithNoStateFileYetIsAccepted(t *testing.T) {
	srv := testServer(t, manyTabs(1))
	if err := os.Remove(srv.stateFile); err != nil {
		t.Fatal(err)
	}
	postOK(t, srv, `{"__by":"agent-1","version":3,"nextId":9,"tabs":[`+
		`{"id":"bb1","name":"Plan","type":"notes","state":{"text":"one"}}]}`)
	if got := srv.readTabs(t); len(got) != 1 {
		t.Fatalf("the first write created %d tabs", len(got))
	}
}
