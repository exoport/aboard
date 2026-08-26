// document.go — the board as the server holds it between writes.
//
// The write path used to re-read and re-parse the whole state file for every
// question it asked about it: the incoming decode, the compare-and-set check,
// both sides of reconcileTabs, both sides of reconcileNextID, and the change
// summary — seven full-document Unmarshals and one MarshalIndent per POST, every
// one of them proportional to the WHOLE board rather than to the edit. On a 65 KB
// board that is invisible. On a 3.5 MB one it is 200 ms of an agent's write spent
// re-reading tabs nobody touched (bench_test.go).
//
// So the server keeps the document parsed. A stateDoc is:
//
//   - fields: every top-level key EXCEPT "tabs", kept as raw bytes. The board does
//     not know what an agent may have put at the root and has no business
//     re-encoding it, so it is spliced back out verbatim;
//   - tabs: decoded, with `state` left opaque — the renderer owns that, not Go;
//   - per tab, the two derived facts a write needs: the whitespace-normalised
//     form of its state, and the largest numeric id inside it. Both are carried
//     forward untouched when a write leaves the tab alone, which is what turns
//     "one small edit on a board of 5 000" into work proportional to the edit.
//
// The decode is ONE pass. encoding/json cannot give both a typed `tabs` and the
// unknown root keys from a single Unmarshal, so this walks the top-level object
// with a Decoder and decodes each member once: `tabs` into []docTab, everything
// else into a RawMessage. That is also what preserves an agent's key order inside
// a state blob, which the old map[string]any round trip alphabetised.

package aboard

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"sync/atomic"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

// writeOptions are the encoder settings that make a v2-written document
// byte-identical to the one encoding/json used to produce. Both are load-bearing
// and neither is cosmetic.
//
// Deterministic sorts map keys. v2 does NOT sort them by default — it shuffles
// them, deliberately, to stop callers depending on an order the language never
// promised — and the state file is a file a human reads and a tool diffs, so a
// root key order that changed on every write would be a new kind of noise.
//
// EscapeForHTML re-escapes `<`, `>` and `&` as v1 always did, and that one is a
// containment property rather than a style: htmltab.go splices a tab's
// `state.data` verbatim into a `<script>` element in the sandboxed frame it
// serves, and the escaping in the file is what stops a widget that stores the
// literal text `</script>` from closing that element. Do not drop it here
// without escaping at that splice first.
var writeOptions = jsonv2.JoinOptions(
	jsonv2.Deterministic(true),
	jsontext.EscapeForHTML(true),
)

// documentDecodes counts full-document decodes, so a test can assert the thing
// this file exists for: exactly one decode of the incoming body per POST, and
// none of the document already in memory.
//
// A counter in production code rather than a test double, because the claim is
// about the real write path and a double would only prove the double. One atomic
// add per decode is not a cost anybody can measure.
var documentDecodes atomic.Int64

// The two ways the codec can be made to look INSIDE a tab's state, counted
// separately because they cost an order of magnitude apart and are pinned by
// different tests.
//
// stateNormalisations is a json.Compact scan — cheap, and what makes an indented
// document on disk comparable with the compact one the browser posts.
// stateCanonicalisations is the unmarshal-and-re-marshal that answers "the same
// values in a different key order": correct, and what jsonEqual did for EVERY tab
// on EVERY write, twice.
//
// The number that matters in both is not the value but the shape: neither may
// grow with the number of tabs a write left alone.
var (
	stateNormalisations    atomic.Int64
	stateCanonicalisations atomic.Int64
)

func codecTouches() int64 { return stateNormalisations.Load() + stateCanonicalisations.Load() }

// idWalks counts the tabs whose state the id allocator walked. Same shape of
// claim: a new id can only appear in state a write changed, so a write that
// touches one tab must walk one tab, on a board of any size.
var idWalks atomic.Int64

var (
	errNotAnObject  = errors.New("the document is not a JSON object")
	errTabsNotArray = errors.New("the tabs key is not an array of tabs")
)

// docTab is one tab plus what the server has already worked out about it.
type docTab struct {
	tab

	// norm is State with insignificant whitespace removed — the form two writes
	// are compared in. It is NOT canonical: object keys keep the order the writer
	// used, because normalising order would mean re-encoding every tab on every
	// write, which is the cost being removed. Order-insensitivity is preserved
	// where it matters by sameState, which falls back to a canonical comparison
	// for the one tab that failed the byte compare.
	//
	// Normalisation is not optional. The state file is written INDENTED and the
	// browser posts compact JSON, so comparing raw bytes alone would report every
	// tab on the board as changed on the first write after a restart — a dot on
	// all fifteen tabs and fifteen journal lines for one edit.
	norm []byte

	// maxID is the largest "<prefix><digits>" id anywhere in this tab, or -1 when
	// nobody has needed to know yet. Worked out per tab so the id allocator can
	// walk the tabs a write changed instead of the whole document twice.
	maxID int

	// changed records that this write altered the tab, for the journal. Transient:
	// it describes the write that produced this document, not the document.
	changed bool
}

// stateDoc is a parsed board document: the root fields and the tabs, each
// holding its own copy of its bytes. The source document is NOT retained — the
// server already keeps the bytes it served next to this (liveDoc.disk), and a
// second reference to them here would be a second thing to keep in step.
type stateDoc struct {
	fields map[string]jsontext.Value
	tabs   []docTab
	byID   map[string]int

	// hasTabs distinguishes `{"tabs":[]}` from a document with no tabs key at all.
	hasTabs bool

	rev    revision
	nextID int

	// fieldMax is the largest id among the root keys other than "tabs", worked out
	// lazily. In practice there are none — version, rev, nextId, updatedAt,
	// lastEditedBy — but the old walker scanned the whole document and dropping
	// that silently would be a change nobody asked for.
	fieldMax int
	fieldSet bool
}

// emptyDoc is what "there is no document yet" parses to: no tabs, no fields, and
// a revision of zero that has never been stamped.
func emptyDoc() *stateDoc {
	return &stateDoc{fields: map[string]jsontext.Value{}, byID: map[string]int{}}
}

// decodeDocument parses a whole board document in one pass.
//
// An empty or all-whitespace input is not an error — it is a board that has not
// been written yet, which the write path meets on the very first POST.
func decodeDocument(raw []byte) (*stateDoc, error) {
	doc := emptyDoc()
	if len(bytes.TrimSpace(raw)) == 0 {
		return doc, nil
	}

	documentDecodes.Add(1)

	dec := jsontext.NewDecoder(bytes.NewReader(raw))
	open, err := dec.ReadToken()
	if err != nil {
		return nil, err
	}
	if open.Kind() != '{' {
		return nil, errNotAnObject
	}
	for dec.PeekKind() != '}' {
		key, err := dec.ReadValue()
		if err != nil {
			return nil, err
		}
		var name string
		if err := jsonv2.Unmarshal(key, &name); err != nil {
			return nil, errNotAnObject
		}
		if name == keyTabs {
			// The SHAPE question first, and separately, because the two failures
			// deserve different sentences. `"tabs": {}` is "this is not a board";
			// a parse error inside the array is a duplicate name, a bad escape or
			// invalid UTF-8 somewhere in a tab, and v2 says which member and at
			// what offset. Folding the second into the first — which is what this
			// did — answered a duplicate key inside a tab's state, the commonest
			// shape a generated document falls into, with "expected a tabs array"
			// about an array that was right there, while the docs promised a
			// refusal naming the member.
			if dec.PeekKind() != '[' {
				return nil, errTabsNotArray
			}
			doc.tabs = nil
			if err := jsonv2.UnmarshalDecode(dec, &doc.tabs); err != nil {
				return nil, err
			}
			doc.hasTabs = true
			continue
		}
		value, err := dec.ReadValue()
		if err != nil {
			return nil, err
		}
		doc.fields[name] = value.Clone()
	}
	// The closing brace, and then the end. A Decoder is a stream and would happily
	// stop at the first value, so the check is explicit: a document with a second
	// value after it is a document somebody built wrongly, and accepting the first
	// half silently is how half a write lands.
	if _, err := dec.ReadToken(); err != nil {
		return nil, err
	}
	if _, err := dec.ReadToken(); err == nil {
		return nil, errors.New("trailing data after the document")
	}

	doc.finish()
	return doc, nil
}

// finish derives what the rest of the server asks a parsed document for.
func (d *stateDoc) finish() {
	for i := range d.tabs {
		d.tabs[i].maxID = -1
		d.byID[d.tabs[i].ID] = i
	}
	d.rev = revisionFromFields(d.fields)
	d.nextID, _ = rawInt(d.fields[keyNextID])
}

// marshalIndent writes the document back out in the shape it arrived in: the
// root keys alphabetised by encoding/json exactly as the old map[string]any path
// produced them, every unknown root field spliced through verbatim, and each
// tab's state re-indented rather than re-encoded.
//
// The one deliberate difference from the old path: keys INSIDE a state blob keep
// the order the writer gave them, where the map round trip alphabetised them.
// State is the renderer's, not Go's, and a server that reorders it is a server
// that has an opinion about data it explicitly treats as opaque.
func (d *stateDoc) marshalIndent() ([]byte, error) {
	obj := make(map[string]jsontext.Value, len(d.fields)+1)
	maps.Copy(obj, d.fields)
	tabs, err := jsonv2.Marshal(d.tabs, writeOptions)
	if err != nil {
		return nil, err
	}
	obj[keyTabs] = tabs
	return jsonv2.Marshal(obj, writeOptions, jsontext.WithIndent("  "))
}

// setField replaces one root key with a value the server decides.
func (d *stateDoc) setField(name string, v any) error {
	raw, err := jsonv2.Marshal(v, writeOptions)
	if err != nil {
		return err
	}
	d.fields[name] = raw
	return nil
}

/* ---------- comparing states ---------- */

// normalise strips insignificant whitespace from a state blob, aliasing the
// input when it is already compact so the common case costs no allocation.
func normalise(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return nil
	}
	stateNormalisations.Add(1)
	compact := jsontext.Value(raw).Clone()
	if err := compact.Compact(); err != nil {
		// Not JSON the compactor recognises. Compare it as the bytes it is; the
		// write itself will fail elsewhere if it is genuinely malformed.
		return raw
	}
	if bytes.Equal(compact, raw) {
		return raw
	}
	return compact
}

// normalised returns the tab's normalised state, working it out on first ask.
func (t *docTab) normalised() []byte {
	if t.norm == nil {
		t.norm = normalise(t.State)
	}
	return t.norm
}

// sameState reports whether two tabs hold the same state.
//
// Three steps, cheapest first, and the ordering is the point:
//
//  1. the raw bytes are equal — the steady state, since a writer that changed
//     nothing echoes back what it read;
//  2. the whitespace-normalised bytes are equal — the disk document is indented
//     and the browser posts compact, so this is what makes the first write after
//     a restart not mark every tab;
//  3. only then, a canonical comparison of that ONE tab: unmarshal both and
//     re-marshal, which sorts object keys. That is what the old jsonEqual did for
//     EVERY tab on EVERY write, twice. Keeping it as the last step preserves the
//     semantics exactly — a writer that reorders keys has not changed anything —
//     while charging for it only where the cheap answers disagree.
func sameState(a, b *docTab) bool {
	if bytes.Equal(a.State, b.State) {
		return true
	}
	if bytes.Equal(a.normalised(), b.normalised()) {
		return true
	}
	return canonicalEqual(a.State, b.State)
}

// canonicalEqual is order-insensitive equality for two state blobs.
func canonicalEqual(a, b json.RawMessage) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	stateCanonicalisations.Add(1)
	// RFC 8785 canonicalisation, in place on a copy of the bytes. This used to be
	// an unmarshal into `any` and a re-marshal — building a tree of interface
	// values to throw it away, 64 allocations against 1, and the thing jsonEqual
	// did for every tab on the board twice per write.
	x, y := jsontext.Value(a).Clone(), jsontext.Value(b).Clone()
	if x.Canonicalize() != nil || y.Canonicalize() != nil {
		return bytes.Equal(a, b)
	}
	return bytes.Equal(x, y)
}

/* ---------- the bytes on disk ---------- */

// etagOf is the entity tag for a document: a strong tag over the exact bytes
// served.
//
// The revision counter was the obvious candidate and it is the wrong one. `rev`
// moves only on an accepted POST, and the state file is a file — a human editing
// it, `git checkout`, another tool — so a document can change without `rev`
// changing, and an ETag that missed that would answer 304 for a board that no
// longer exists. The content hash cannot be wrong about its own content.
func etagOf(body []byte) string {
	sum := sha256.Sum256(body)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

/* ---------- small readers ---------- */

// rawInt reads a JSON number, or a JSON string holding one.
func rawInt(raw jsontext.Value) (int, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var v any
	if jsonv2.Unmarshal(raw, &v) != nil {
		return 0, false
	}
	return toInt(v)
}

// rawString reads a JSON string, or "" for anything else.
func rawString(raw jsontext.Value) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if jsonv2.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}
