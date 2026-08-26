package aboard

import (
	"regexp"
	"strconv"

	jsonv2 "github.com/go-json-experiment/json"
)

// Ids are board-wide monotonic, not per-container.
//
// Every renderer used to allocate "highest existing suffix + 1" within its own
// slice of state. Delete every mark on an image and the next one is r1 again;
// delete the last kanban node and the new one takes its id. That silently
// re-points any instruction that referenced the old object — and since ids are
// how a human and several agents refer to things across turns, a reused id is a
// correctness bug, not an inconvenience.
//
// So a single counter lives at the document root and every new object anywhere
// takes the next value. Two consequences worth having:
//
//   - An id is unique across the whole board, so nothing has to be qualified by
//     its tab. That is the only thing that works for a stack tab holding two
//     kanbans, where the tab cannot disambiguate at all.
//   - One constant namespace tag, "bb", and still no TYPE prefix. A per-kind
//     vocabulary (node-7, tab-3) is a closed set in a system where an agent can
//     invent new kinds of object, so it gets guessed ad hoc and stops meaning
//     anything, and it duplicates what the object's position already says. A
//     single tag says nothing about kind and cannot be guessed wrong; it exists
//     so an id survives being written in a sentence — "bb49" is unmistakably
//     this board's object where "49" is any number at all.
//
// Ids are strings, so DOM attributes and Map keys agree with the document.
//
// Form field ids are deliberately exempt — an agent names those semantically
// ("strategy", "window") and reads answers back by them.
//
// The counter is allocated client-side and protected by the same compare-and-set
// as everything else: two writers picking the same number means the second write
// is refused and retries against fresh state. This file is the safety net for
// what CAS cannot catch — a stale or hand-edited document whose counter has
// fallen behind the ids already in use.

// "bb147" is the current form; bare "147" and legacy "n147" are still read, which
// is why this was always prefix-tolerant — the migration to "bb" needed no change
// here, nor in any renderer, since they all parse ids with the same shape.
var idSuffix = regexp.MustCompile(`^[a-z]*(\d+)$`)

// idKey is the one field name an id can live under. Named because the walk below
// and the root scan both have to agree on it, and a rule spelled twice is a rule
// that can differ once.
const idKey = "id"

// reconcileNextID returns the value of nextId to persist: never lower than what
// either document already had, and always above every numeric id in use in
// either of them.
//
// The byte-level form, and the one the invariant is specified against: the table
// in ids_test.go is the statement of what "the counter never goes backwards"
// means, and it is written in documents rather than in structs. The write path
// calls nextIDFrom with the documents it already holds parsed.
func reconcileNextID(incomingRaw, currentRaw []byte) int {
	inc, incErr := decodeDocument(incomingRaw)
	if incErr != nil {
		inc = nil
	}
	cur, curErr := decodeDocument(currentRaw)
	if curErr != nil {
		cur = nil
	}
	return nextIDFrom(inc, cur)
}

// nextIDFrom is the same rule over two documents already in memory.
//
// This used to walk both whole documents for "id" keys on every single write —
// two recursive passes over everything, to answer a question only the tabs a
// write TOUCHED can change the answer to. Each document now carries the largest
// id per tab, worked out when that tab's state was accepted and carried forward
// untouched otherwise, so the walk costs the edit rather than the board.
func nextIDFrom(incoming, current *stateDoc) int {
	high := 0
	for _, doc := range []*stateDoc{incoming, current} {
		if doc == nil {
			continue
		}
		if doc.nextID > high {
			high = doc.nextID
		}
		if used := doc.maxUsed(); used >= high {
			high = used + 1
		}
	}
	if high < 1 {
		high = 1
	}
	return high
}

// maxUsed is the largest numeric id anywhere in the document.
func (d *stateDoc) maxUsed() int {
	highest := d.rootMax()
	for i := range d.tabs {
		if n := d.tabs[i].idHigh(); n > highest {
			highest = n
		}
	}
	return highest
}

// rootMax covers the root keys other than "tabs". There are none carrying an id
// today — version, rev, nextId, updatedAt, lastEditedBy — but the walker this
// replaced scanned the whole document, and narrowing that silently is how a
// future root field starts handing out an id somebody already has.
func (d *stateDoc) rootMax() int {
	if d.fieldSet {
		return d.fieldMax
	}
	highest := 0
	for name, raw := range d.fields {
		if name == idKey {
			if n := idCounter(rawString(raw)); n > highest {
				highest = n
			}
			continue
		}
		var v any
		if jsonv2.Unmarshal(raw, &v) != nil {
			continue
		}
		if n := maxUsedID(v); n > highest {
			highest = n
		}
	}
	d.fieldMax, d.fieldSet = highest, true
	return highest
}

// idHigh is the largest numeric id this tab uses: its own, every "id" inside its
// state, and every request on it.
//
// `requests` is the one tab field outside `state` that carries an id, and it had
// to be added here the day it landed. The rest — `touched`, `pendingRemoval`,
// `seen` — hold actors and timestamps, which is why this used to be "the state
// blob and nothing else". Leaving a request out would not have failed anything
// loudly: `nextId` would simply have stopped short of the id the human's last
// note took, and the next object allocated ANYWHERE on the board would have been
// handed an id that already names something.
func (t *docTab) idHigh() int {
	if t.maxID >= 0 {
		return t.maxID
	}
	highest := idCounter(t.ID)
	for i := range t.Requests {
		if n := idCounter(t.Requests[i].ID); n > highest {
			highest = n
		}
	}
	if len(t.State) > 0 {
		idWalks.Add(1)
		var v any
		if jsonv2.Unmarshal(t.State, &v) == nil {
			if n := maxUsedID(v); n > highest {
				highest = n
			}
		}
	}
	t.maxID = highest
	return highest
}

// idCounter reads the numeric tail of an id value, or 0 for anything that is not
// one. Anything that is not an id shaped like "<prefix><digits>" simply does not
// raise the counter — a non-id "id" field is somebody else's data, not an error.
func idCounter(v any) int {
	s, ok := v.(string)
	if !ok {
		return 0
	}
	m := idSuffix.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return n
}

// maxUsedID walks a decoded value for "<prefix><digits>" ids and returns the
// largest number seen. It scans generically rather than knowing each renderer's
// shape, so a new tab type gets this for free.
//
// A key named "id" is READ and not descended into, which is deliberate and is
// what the "a semantic form field id does not raise the counter" row depends on.
func maxUsedID(v any) int {
	highest := 0
	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			for k, child := range t {
				if k == idKey {
					if n := idCounter(child); n > highest {
						highest = n
					}
					continue
				}
				walk(child)
			}
		case []any:
			for _, child := range t {
				walk(child)
			}
		}
	}
	walk(v)
	return highest
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case string:
		if p, err := strconv.Atoi(n); err == nil {
			return p, true
		}
	}
	return 0, false
}
