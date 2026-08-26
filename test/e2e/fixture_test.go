//go:build e2e

package e2e

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
)

// The interaction fixture, laid over the example board after `Init --example`
// seeds it.
//
// Why a separate file and not an edit to pkg/aboard/example/aboard.json: the
// example is what `aboard init --example` gives a real project on day one, and
// every line of this fixture would be wrong there. A gate row waiting on a
// verdict is a decision nobody asked for; marks on a mock screenshot are
// somebody else's annotations on somebody else's screen; a `notes` tab
// duplicates what the stack tab already demonstrates as a block. What the
// example DID need — a read-only kanban with cards in it, so the "no drag
// handles" assertions are not vacuous — went into the example itself under
// plan-2 item 3, because an empty queue made a bad demonstration too.
//
//go:embed testdata/fixture.json
var fixtureJSON []byte

// applyFixture merges the fixture into the seeded document, on disk, before the
// server starts. No compare-and-set involved and none needed: nothing is
// serving this file yet, which is the one condition under which writing a board
// document directly is allowed.
func applyFixture(stateFile string) error {
	var fixture struct {
		Patch  map[string]map[string]any `json:"patch"`
		Add    []map[string]any          `json:"add"`
		NextID int                       `json:"nextId"`
	}
	if err := json.Unmarshal(fixtureJSON, &fixture); err != nil {
		return fmt.Errorf("parsing the fixture: %w", err)
	}

	raw, err := os.ReadFile(stateFile)
	if err != nil {
		return err
	}
	var d map[string]any
	if err := json.Unmarshal(raw, &d); err != nil {
		return err
	}

	tabs, _ := d["tabs"].([]any)
	patched := map[string]bool{}
	for i, entry := range tabs {
		tab, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		id, _ := tab["id"].(string)
		patch, ok := fixture.Patch[id]
		if !ok {
			continue
		}
		tabs[i] = mergeInto(tab, patch)
		patched[id] = true
	}
	// A patch aimed at a tab that is not there is a fixture that has silently
	// stopped testing something — the example is edited by other people, and a
	// renamed id would otherwise turn every gesture test on that tab into a
	// no-op with a green tick.
	for id := range fixture.Patch {
		if !patched[id] {
			return fmt.Errorf("the fixture patches tab %q and the example board has no such tab", id)
		}
	}
	for _, add := range fixture.Add {
		tabs = append(tabs, any(add))
	}
	d["tabs"] = tabs
	if fixture.NextID > 0 {
		d["nextId"] = fixture.NextID
	}

	out, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	// 0o644: the board's own file mode, see the note in init.go.
	return os.WriteFile(stateFile, out, 0o644)
}

// mergeInto merges patch into base: objects merge key by key, everything else —
// arrays included — replaces.
//
// Arrays replace WHOLE on purpose. Merging them would need an identity rule
// ("same id" or "same index"), and both are wrong somewhere in this document:
// gate rows are identified by id, markup marks by id but ordered, kanban columns
// by position. A fixture that has to state a whole array is a fixture whose
// effect can be read off the file.
func mergeInto(base, patch map[string]any) map[string]any {
	for k, v := range patch {
		sub, isObj := v.(map[string]any)
		cur, wasObj := base[k].(map[string]any)
		if isObj && wasObj {
			base[k] = mergeInto(cur, sub)
			continue
		}
		base[k] = v
	}
	return base
}
