//go:build e2e

package e2e

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"path"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/exoport/aboard/pkg/aboard/web"
)

// The gesture coverage gate.
//
// `gestures` in views/<type>.spec.json is the one part of the capability
// manifest with no mechanical consumer, and that is exactly why it went stale:
// state fields are READ by `aboard apply`'s write warnings, so a wrong one
// produces a wrong warning and somebody fixes it, while a wrong gesture sentence
// broke nothing at all. Controls got a consumer when they started being rendered
// FROM their declaration. This is the consumer for what is left.
//
// It is deliberately a check about RENDERERS, not about sentences. A per-gesture
// requirement — every one of the 33 declared strings needs a test — sounds
// stronger and is not: "a wide split is called out rather than averaged away" is
// a claim about rendering, "hover a truncated image name to see it in full" is a
// title attribute, and forcing a test per sentence buys assertions written to
// satisfy a counter. So: every renderer must have at least one gesture test, and
// every gesture NAMED by a test must be one the spec actually declares — which
// catches the other drift direction, a test still asserting a gesture that was
// removed. What is left over is logged, so the gap is visible without being a
// gate nobody can pass honestly.

type specFile struct {
	Type     string   `json:"type"`
	Gestures []string `json:"gestures"`
}

var (
	coverMu sync.Mutex
	covered = map[string]map[string]bool{} // renderer type -> gesture -> tested
)

// covers records that this test exercises one of a renderer's declared gestures.
// The string must match the spec's wording exactly; a typo fails the gate rather
// than quietly covering nothing.
//
// An EMPTY gesture means "this renderer declares none" — `trace` is the only one
// today. It still has to be registered, because the gate is about renderers
// having tests at all; it just has no sentence to match against.
func covers(t *testing.T, renderer, gesture string) {
	t.Helper()
	coverMu.Lock()
	defer coverMu.Unlock()
	if covered[renderer] == nil {
		covered[renderer] = map[string]bool{}
	}
	covered[renderer][gesture] = true
}

// loadSpecs reads the declarations out of the EMBEDDED web tree — the same bytes
// the server serves and `aboard capabilities` describes. Reading views/ off disk
// would test the working copy instead of the build, which is this repo's oldest
// gotcha in a new place.
func loadSpecs() (map[string]specFile, error) {
	entries, err := fs.ReadDir(web.FS, "views")
	if err != nil {
		return nil, err
	}
	out := map[string]specFile{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".spec.json") {
			continue
		}
		body, err := fs.ReadFile(web.FS, path.Join("views", e.Name()))
		if err != nil {
			return nil, err
		}
		var spec specFile
		if err := json.Unmarshal(body, &spec); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		out[spec.Type] = spec
	}
	if len(out) < 15 {
		return nil, fmt.Errorf("only %d renderer specs in the embedded tree; that looks wrong", len(out))
	}
	return out, nil
}

// reportGestureCoverage is the gate, run from TestMain after the tests.
//
// It is SKIPPED under -run, and that is not a loophole: with a filter set, an
// uncovered renderer is the filter doing its job, and a gate that fails every
// single-test run is a gate people learn to pass with -run=. and then ignore.
// `make e2e` runs the whole suite, which is where this bites.
func reportGestureCoverage(out *log.Logger) bool {
	// `make e2e` passes -run '$(E2E_RUN)', which defaults to `.` — a filter that
	// selects everything. Treating any non-empty -run as "a subset" would
	// therefore have skipped this gate on every single normal run, which is the
	// silent kind of dead check this file exists to replace.
	if sel := runFilter(); sel != "" {
		out.Printf("gesture coverage not checked: -run=%s selected a subset", sel)
		return true
	}

	specs, err := loadSpecs()
	if err != nil {
		out.Printf("gesture coverage: %v", err)
		return false
	}

	coverMu.Lock()
	defer coverMu.Unlock()

	ok := true
	var uncoveredGestures []string
	for _, renderer := range sortedKeys(specs) {
		spec := specs[renderer]
		got := covered[renderer]
		if len(got) == 0 {
			out.Printf("NO GESTURE TEST for renderer %q — add one and call covers(t, %q, <a gesture from views/%s.spec.json>)",
				renderer, renderer, renderer)
			ok = false
			continue
		}
		declared := map[string]bool{}
		for _, g := range spec.Gestures {
			declared[g] = true
		}
		for _, g := range sortedKeys(got) {
			if g == "" {
				if len(spec.Gestures) > 0 {
					out.Printf("%s: a test registered no gesture, but views/%s.spec.json declares %d — name the one it drives",
						renderer, renderer, len(spec.Gestures))
					ok = false
				}
				continue
			}
			if !declared[g] {
				out.Printf("%s: a test claims to cover the gesture %q, which views/%s.spec.json does not declare",
					renderer, g, renderer)
				ok = false
			}
		}
		for _, g := range spec.Gestures {
			if !got[g] {
				uncoveredGestures = append(uncoveredGestures, renderer+": "+g)
			}
		}
	}
	if len(uncoveredGestures) > 0 {
		out.Printf("declared gestures with no test of their own (%d) — not a failure, see coverage_test.go:\n  %s",
			len(uncoveredGestures), strings.Join(uncoveredGestures, "\n  "))
	}
	if ok {
		out.Printf("every one of the %d renderers has at least one gesture test", len(specs))
	}
	return ok
}

// runFilter is the -run pattern, or "" when it selects everything.
func runFilter() string {
	f := flag.Lookup("test.run")
	if f == nil {
		return ""
	}
	switch v := f.Value.String(); v {
	case "", ".", ".*", "^.*$", "Test":
		return ""
	default:
		return v
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
