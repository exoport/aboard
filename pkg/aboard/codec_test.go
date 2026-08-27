package aboard

// codec_test.go — what changed when the codec became encoding/json/v2, and what
// deliberately did not.
//
// The point of the swap was speed, so the visible surface has to be pinned in
// both directions: the bytes it writes must be the bytes encoding/json wrote
// (or every board on disk changes shape on its next write, for no reason anybody
// asked for), and the parsing it does must be the stricter one (or the "duplicate
// keys are refused" line in the docs is a claim nothing enforces).

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// The parity that makes this a cost change and not a format change.
//
// v2 does NOT escape `<`, `>` and `&` by default and does NOT sort map keys by
// default, and either difference alone would rewrite every state file on its
// next write — the second one differently on every write. Both are turned back
// on (writeOptions), and this compares the result against what encoding/json
// produced for the same document, on the real example board rather than on a
// fixture chosen to agree.
func TestTheWrittenDocumentIsByteIdenticalToTheOldEncoder(t *testing.T) {
	raw := exampleDocument(t)

	doc, err := decodeDocument(raw)
	if err != nil {
		t.Fatalf("the example board did not decode: %v", err)
	}
	got, err := doc.marshalIndent()
	if err != nil {
		t.Fatal(err)
	}

	// The same document through encoding/json: the tabs spliced in as a raw
	// value and the lot indented. This is the ENCODER comparison, and it is worth
	// being exact about what it does and does not claim. server.go used to hold
	// the root in a map[string]any, so it also re-encoded every value it had
	// decoded — which alphabetised the keys inside a state blob and pushed every
	// number through float64. Those two differences are deliberate and are
	// asserted separately (TestAStateBlobKeepsItsAuthorsKeyOrder). What is pinned
	// here is that the CODEC swap on its own changes nothing: same indentation,
	// same escaping, same root key order.
	// The map stays typed as json.RawMessage because that is the v1 API's own
	// vocabulary and naming it is the point of this comparison — but since Go
	// 1.27 no conversion is needed to fill it, and that is not a tidy-up: v1's
	// RawMessage is now `= jsontext.Value` (encoding/json/v2_stream.go), an
	// ALIAS rather than a second []byte type. Writing the conversion back in is
	// what unconvert reports. What the alias does not change is what is being
	// compared: json.MarshalIndent below is still the v1 entry point with v1
	// defaults, which is the whole claim this test makes.
	obj := make(map[string]json.RawMessage, len(doc.fields)+1)
	maps.Copy(obj, doc.fields)
	tabs, err := json.Marshal(doc.tabs)
	if err != nil {
		t.Fatal(err)
	}
	obj["tabs"] = tabs
	want, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("the v2 encoder writes a different document; first difference at byte %d\n v2: %s\n v1: %s",
			firstDiff(got, want), excerpt(got, firstDiff(got, want)), excerpt(want, firstDiff(got, want)))
	}
}

func firstDiff(a, b []byte) int {
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

func excerpt(b []byte, at int) string {
	start := max(at-60, 0)
	end := min(at+60, len(b))
	return string(b[start:end])
}

func exampleDocument(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("example/aboard.json")
	if err != nil {
		t.Fatalf("the example board is the fixture this test needs: %v", err)
	}
	return raw
}

// The stricter defaults, all four, in one place — because "v2 is stricter" is
// the kind of sentence that gets written into a doc and never checked.
func TestTheStricterParserDefaults(t *testing.T) {
	t.Run("a duplicate object name is refused", func(t *testing.T) {
		_, err := decodeDocument([]byte(`{"nextId":1,"nextId":2,"tabs":[]}`))
		if err == nil {
			t.Fatal("a document setting nextId twice was accepted; which value wins is not a parser's call")
		}
		if !strings.Contains(err.Error(), "duplicate") {
			t.Errorf("the refusal does not say what was wrong: %v", err)
		}
	})

	t.Run("invalid UTF-8 is refused", func(t *testing.T) {
		if _, err := decodeDocument([]byte("{\"tabs\":[],\"note\":\"\xff\"}")); err == nil {
			t.Error("a document with an invalid UTF-8 byte was accepted; encoding/json replaced it with U+FFFD")
		}
	})

	t.Run("field matching is case sensitive", func(t *testing.T) {
		doc, err := decodeDocument([]byte(`{"tabs":[{"ID":"bb1","name":"Plan","type":"notes"}]}`))
		if err != nil {
			t.Fatal(err)
		}
		if len(doc.tabs) != 1 {
			t.Fatalf("got %d tabs", len(doc.tabs))
		}
		if doc.tabs[0].ID != "" {
			t.Errorf(`"ID" was matched to the "id" field: %q — v1 matched case-insensitively, v2 does not`, doc.tabs[0].ID)
		}
	})

	t.Run("map order is made deterministic on the way out", func(t *testing.T) {
		// v2 shuffles map keys unless told not to. Marshalling the same map twice
		// and getting the same bytes is the whole assertion.
		m := map[string]int{"z": 1, "a": 2, "m": 3, "q": 4, "b": 5}
		first, err := jsonv2.Marshal(m, writeOptions)
		if err != nil {
			t.Fatal(err)
		}
		for range 20 {
			again, err := jsonv2.Marshal(m, writeOptions)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(again, first) {
				t.Fatalf("two marshals of one map disagree: %s vs %s", first, again)
			}
		}
		if string(first) != `{"a":2,"b":5,"m":3,"q":4,"z":1}` {
			t.Errorf("the deterministic order is not the sorted one: %s", first)
		}
	})

	t.Run("the escaping htmltab relies on is still applied", func(t *testing.T) {
		doc, err := decodeDocument([]byte(`{"tabs":[{"id":"bb1","name":"W","type":"html",` +
			`"state":{"data":{"note":"</script><img>"}}}]}`))
		if err != nil {
			t.Fatal(err)
		}
		out, err := doc.marshalIndent()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(out), "</script>") {
			t.Error("a literal </script> reached the state file; htmltab.go splices state.data into a <script> element")
		}
	})
}

// The write path's own refusal, and the message it gives. A 400 saying only
// "invalid json" about a multi-megabyte document is a message that sends
// somebody to a diff tool.
func TestAPostWithADuplicateKeyIsRefusedWithAReason(t *testing.T) {
	srv := testServer(t, `{"version":3,"rev":1,"nextId":9,"tabs":[]}`)

	rec := httptest.NewRecorder()
	srv.postState(rec, httptest.NewRequest(http.MethodPost, "/aboard.json",
		strings.NewReader(`{"__by":"agent-1","__base":"1","version":3,"nextId":9,"nextId":10,"tabs":[]}`)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a duplicate key was answered %d, want 400", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body["reason"], "duplicate") {
		t.Errorf("the refusal does not name the problem: %v", body)
	}
}

// Canonicalize is the tool the canonical comparison is built on now, so its two
// properties are asserted rather than assumed: order-insensitive, and NOT
// value-insensitive.
func TestCanonicalComparisonIsOrderInsensitiveAndNothingMore(t *testing.T) {
	same := []struct{ a, b string }{
		{`{"a":1,"b":2}`, `{"b":2,"a":1}`},
		{`{"a":{"x":1,"y":2}}`, `{"a":{"y":2,"x":1}}`},
		{`{"a":[1,2]}`, ` { "a" : [ 1 , 2 ] } `},
	}
	for _, tc := range same {
		if !canonicalEqual(json.RawMessage(tc.a), json.RawMessage(tc.b)) {
			t.Errorf("%s and %s are the same values in a different order", tc.a, tc.b)
		}
	}
	differ := []struct{ a, b string }{
		{`{"a":1}`, `{"a":2}`},
		{`{"a":[1,2]}`, `{"a":[2,1]}`}, // array order IS meaning
		{`{"a":1}`, `{"a":"1"}`},
	}
	for _, tc := range differ {
		if canonicalEqual(json.RawMessage(tc.a), json.RawMessage(tc.b)) {
			t.Errorf("%s and %s were called equal", tc.a, tc.b)
		}
	}
}

// jsontext.Value is the type the raw paths are built on; this pins that it is
// still the byte slice everything here assumes, rather than something with a
// representation of its own.
func TestARawValueIsItsBytes(t *testing.T) {
	v := jsontext.Value(`{"a":1}`)
	if string(v) != `{"a":1}` {
		t.Errorf("a jsontext.Value no longer holds its own bytes: %q", v)
	}
}

// `apply` refuses a duplicate-key document IN THE CALLER'S TERMINAL, and that
// placement is the whole point of the test.
//
// The server refuses it too, but apply would never have let it get there: it
// decodes stdin into a map and re-marshals what it decoded, so a v1 parse
// silently collapsed the duplicate — last one wins — and posted a document with
// one key. The write landed, exit 0, and the field the agent believed it had set
// was the other one. A stricter parser on the server alone would not have caught
// that; the parse the CALLER does has to be the same parse.
func TestApplyRefusesADuplicateKeyDocument(t *testing.T) {
	root, _ := applyTarget(t)

	_, _, err := runApply(t, root, false,
		`{"rev":1,"version":3,"nextId":9,"tabs":[{"id":"bb1","name":"Plan","type":"notes","state":{"text":"a","text":"b"}}]}`)
	if err == nil {
		t.Fatal("a document setting one key twice was applied")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("the refusal does not say what was wrong: %v", err)
	}
	// And it says which key, because a 4 MB document is not something to bisect.
	if !strings.Contains(err.Error(), `"text"`) {
		t.Errorf("the refusal does not name the key: %v", err)
	}
}

// A duplicate name INSIDE a tab is the shape a generated document actually falls
// into — an upsert helper that appends a key it already wrote — and it is the
// one `apply`'s own test uses. The refusal has to name it.
//
// It did not. Every failure of the tabs decode was mapped to errTabsNotArray, so
// the server answered `{"error":"expected a tabs array"}` about an array that was
// right there, and the caller went looking for the wrong thing. `apply` catches
// stdin before the server, but nothing else does: the browser, a script, another
// tool posting directly all arrive here.
func TestADuplicateKeyInsideATabIsNamedRatherThanCalledAShapeError(t *testing.T) {
	srv := testServer(t, `{"version":3,"rev":1,"nextId":9,"tabs":[]}`)

	rec := httptest.NewRecorder()
	srv.postState(rec, httptest.NewRequest(http.MethodPost, "/aboard.json",
		strings.NewReader(`{"__by":"agent-1","__base":"1","version":3,"nextId":9,"tabs":[`+
			`{"id":"bb1","name":"P","type":"notes","state":{"text":"a","text":"b"}}]}`)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a duplicate key inside a tab was answered %d, want 400", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body["reason"], "duplicate") {
		t.Errorf("the refusal does not name the problem: %v", body)
	}
	if !strings.Contains(body["reason"], `"text"`) {
		t.Errorf("the refusal does not name the key: %v", body)
	}
}

// And the shape error it was hiding behind still exists, with its own sentence.
func TestATabsKeyThatIsNotAnArrayIsAShapeError(t *testing.T) {
	for _, doc := range []string{`{"version":3,"tabs":{}}`, `{"version":3,"tabs":5}`, `{"version":3,"tabs":null}`} {
		srv := testServer(t, `{"version":3,"rev":1,"nextId":9,"tabs":[]}`)
		rec := httptest.NewRecorder()
		srv.postState(rec, httptest.NewRequest(http.MethodPost, "/aboard.json", strings.NewReader(doc)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s was answered %d, want 400", doc, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "expected a tabs array") {
			t.Errorf("%s was refused as %s", doc, rec.Body)
		}
	}
}
