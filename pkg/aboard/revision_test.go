package aboard

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
)

// The compare-and-set token used to be `updatedAt`, a millisecond timestamp, and
// a millisecond is not a token: two writes inside the same one produce the same
// string, so a base built from the FIRST still matches after the SECOND landed —
// measured at 4 collisions in 60 sequential writes.
//
// The scenario, reproduced exactly: a reader holds the document at one revision,
// another writer lands, and the clock happens not to have moved. Under the old
// comparison the stale write was accepted with a 200 and the other writer's edit
// was gone.
func TestAStaleBaseIsRefusedEvenWhenTheTimestampIsUnchanged(t *testing.T) {
	srv := testServer(t, twoTabs)

	first := srv.postDocument(t, `{"tabs":[
	  {"id":"bb1","name":"Plan","type":"notes","state":{"text":"one"}},
	  {"id":"bb2","name":"Queue","type":"notes","state":{"text":"two"}}
	],"__by":"agent-1"}`)
	if first.Code != http.StatusOK {
		t.Fatalf("first write: %d %s", first.Code, first.Body)
	}
	stale := revOf(t, srv) // what a reader would have read at this point
	frozen := updatedAtOf(t, srv)

	second := srv.postDocument(t, `{"tabs":[
	  {"id":"bb1","name":"Plan","type":"notes","state":{"text":"MINE"}},
	  {"id":"bb2","name":"Queue","type":"notes","state":{"text":"two"}}
	],"__by":"agent-2","__base":"`+stale+`"}`)
	if second.Code != http.StatusOK {
		t.Fatalf("second write: %d %s", second.Code, second.Body)
	}

	// Freeze the clock the way a same-millisecond write would: the document has
	// moved on, the timestamp has not.
	freezeUpdatedAt(t, srv, frozen)

	third := srv.postDocument(t, `{"tabs":[
	  {"id":"bb1","name":"Plan","type":"notes","state":{"text":"CLOBBER"}},
	  {"id":"bb2","name":"Queue","type":"notes","state":{"text":"two"}}
	],"__by":"agent-3","__base":"`+stale+`"}`)
	if third.Code != http.StatusConflict {
		t.Fatalf("a stale base was accepted with %d — the write it clobbered is gone; body: %s",
			third.Code, third.Body)
	}
	if got := srv.readTabs(t)[0].State; !strings.Contains(string(got), "MINE") {
		t.Errorf("the refused write reached disk anyway: %s", got)
	}
}

// The token has to advance on every accepted write, or two writes in a row share
// one and the second reader's base is stale before it is used.
func TestEveryAcceptedWriteAdvancesTheRevision(t *testing.T) {
	srv := testServer(t, twoTabs)

	seen := map[string]bool{}
	prev := ""
	for i := range 5 {
		body := `{"tabs":[{"id":"bb1","name":"Plan","type":"notes","state":{"n":` +
			string(rune('0'+i)) + `}},{"id":"bb2","name":"Queue","type":"notes"}],"__by":"agent-1"`
		if prev != "" {
			body += `,"__base":"` + prev + `"`
		}
		rec := srv.postDocument(t, body+`}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("write %d: %d %s", i, rec.Code, rec.Body)
		}
		var reply struct {
			Rev int `json:"rev"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
			t.Fatal(err)
		}
		got := revOf(t, srv)
		if seen[got] {
			t.Fatalf("write %d reused revision %s", i, got)
		}
		if reply.Rev == 0 {
			t.Errorf("write %d did not report its rev to the caller: %s", i, rec.Body)
		}
		seen[got] = true
		prev = got
	}
}

// A board whose last write predates the counter has no `rev` for its readers to
// send, so the old timestamp base is honoured exactly once — the write that gives
// it a rev — and refused with an explanation afterwards. Accepting it forever
// would leave the same-millisecond hole open for anything still sending it.
func TestATimestampBaseWorksOnceOnAPreRevisionBoard(t *testing.T) {
	srv := testServer(t, twoTabs) // twoTabs has updatedAt "T0" and no rev

	ok := srv.postDocument(t, `{"tabs":[{"id":"bb1","name":"Plan","type":"notes"},
	  {"id":"bb2","name":"Queue","type":"notes"}],"__by":"agent-1","__base":"T0"}`)
	if ok.Code != http.StatusOK {
		t.Fatalf("a timestamp base on a document with no rev was refused: %d %s", ok.Code, ok.Body)
	}

	stamp := updatedAtOf(t, srv)
	again := srv.postDocument(t, `{"tabs":[{"id":"bb1","name":"Plan","type":"notes"},
	  {"id":"bb2","name":"Queue","type":"notes"}],"__by":"agent-1","__base":"`+stamp+`"}`)
	if again.Code != http.StatusConflict {
		t.Fatalf("a timestamp base was still accepted after the board had a rev: %d %s", again.Code, again.Body)
	}
	if !strings.Contains(again.Body.String(), "rev") {
		t.Errorf("the refusal does not say what the base should have been: %s", again.Body)
	}
}

func revOf(t *testing.T, s *server) string {
	t.Helper()
	return docField(t, s, "rev")
}

func updatedAtOf(t *testing.T, s *server) string {
	t.Helper()
	return docField(t, s, "updatedAt")
}

func docField(t *testing.T, s *server, key string) string {
	t.Helper()
	raw, err := os.ReadFile(s.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	switch v := doc[key].(type) {
	case string:
		return v
	case float64:
		return strconv.Itoa(int(v))
	}
	return ""
}

// freezeUpdatedAt rewrites the timestamp on disk without touching anything else,
// which is what a second write inside the same millisecond looks like.
func freezeUpdatedAt(t *testing.T, s *server, stamp string) {
	t.Helper()
	raw, err := os.ReadFile(s.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	doc["updatedAt"] = stamp
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.stateFile, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The token stopped being a string, and `rev` is a JSON NUMBER in the document —
// so the obvious hand-written base is `"__base": 41`, a number. The server read
// `__base` with a string type assertion, which failed silently, left the base
// empty and skipped compare-and-set entirely: a 200, and the other writer's edit
// gone. The revision fix opened this door; nothing could reach it while the token
// was a timestamp.
func TestANumericBaseIsCompared(t *testing.T) {
	srv := testServer(t, twoTabs)

	body := func(text, base string) string {
		return `{"tabs":[{"id":"bb1","name":"Plan","type":"notes","state":{"text":"` + text + `"}},
		  {"id":"bb2","name":"Queue","type":"notes"}],"__by":"agent-1"` + base + `}`
	}
	if rec := srv.postDocument(t, body("one", "")); rec.Code != http.StatusOK {
		t.Fatalf("first write: %d %s", rec.Code, rec.Body)
	}
	if rec := srv.postDocument(t, body("MINE", `,"__base":1`)); rec.Code != http.StatusOK {
		t.Fatalf("a numeric base that MATCHES was refused: %d %s", rec.Code, rec.Body)
	}

	stale := srv.postDocument(t, body("CLOBBER", `,"__base":1`))
	if stale.Code != http.StatusConflict {
		t.Fatalf("a stale NUMERIC base was accepted with %d — compare-and-set was skipped; body: %s",
			stale.Code, stale.Body)
	}
	if got := srv.readTabs(t)[0].State; !strings.Contains(string(got), "MINE") {
		t.Errorf("the refused write reached disk anyway: %s", got)
	}
}

// And a `__base` that is present but is not a token at all is refused, not
// downgraded to "no base". Downgrading is the same silent clobber wearing a
// different type.
func TestAnUnusableBaseIsRefusedRatherThanIgnored(t *testing.T) {
	srv := testServer(t, twoTabs)
	for _, base := range []string{`true`, `{"rev":1}`, `[1]`, `1.5`} {
		rec := srv.postDocument(t, `{"tabs":[{"id":"bb1","name":"Plan","type":"notes"},
		  {"id":"bb2","name":"Queue","type":"notes"}],"__by":"agent-1","__base":`+base+`}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("__base %s was accepted with %d, want 400: %s", base, rec.Code, rec.Body)
		}
	}
	// Absent and null both still mean "no base", which is a legitimate
	// unconditional write (`apply --force`, a seeding script).
	for _, doc := range []string{
		`{"tabs":[{"id":"bb1","name":"Plan","type":"notes"},{"id":"bb2","name":"Q","type":"notes"}],"__by":"agent-1"}`,
		`{"tabs":[{"id":"bb1","name":"Plan","type":"notes"},{"id":"bb2","name":"Q","type":"notes"}],"__by":"agent-1","__base":null}`,
	} {
		if rec := srv.postDocument(t, doc); rec.Code != http.StatusOK {
			t.Errorf("an unconditional write was refused: %d %s", rec.Code, rec.Body)
		}
	}
}
