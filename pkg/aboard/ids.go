package aboard

import (
	"encoding/json"
	"regexp"
	"strconv"
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

// reconcileNextID returns the value of nextId to persist: never lower than what
// either document already had, and always above every numeric id in use in
// either of them.
//
// Both arguments are RAW JSON on purpose. The caller has by then replaced the
// tabs entry with a typed []tab, which a generic walker cannot see into — an
// earlier version of this took the decoded map and silently found no ids at all.
// Scanning the bytes keeps the walk honest and shape-agnostic.
func reconcileNextID(incomingRaw, currentRaw []byte) int {
	high := 0

	for _, raw := range [][]byte{incomingRaw, currentRaw} {
		if len(raw) == 0 {
			continue
		}
		var doc map[string]any
		if json.Unmarshal(raw, &doc) != nil {
			continue
		}
		// Whatever that document recorded.
		if n, ok := toInt(doc["nextId"]); ok && n > high {
			high = n
		}
		// And always above every id actually present, so a document that was
		// hand-edited or predates the counter still allocates safely. A tab the
		// server is about to restore lives in the current doc, not the incoming
		// one, which is why both are scanned.
		if used := maxUsedID(doc); used >= high {
			high = used + 1
		}
	}

	if high < 1 {
		high = 1
	}
	return high
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

// maxUsedID walks the whole document for "<prefix><digits>" ids and returns the
// largest number seen. It scans generically rather than knowing each renderer's
// shape, so a new tab type gets this for free.
func maxUsedID(doc map[string]any) int {
	highest := 0
	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			for k, child := range t {
				if k == "id" {
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
	walk(doc)
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
