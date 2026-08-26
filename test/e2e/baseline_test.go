//go:build e2e

package e2e

import "testing"

// The browser's half of the JSON hot paths.
//
// `baseline` — the copy of the document that tells "the human changed this tab"
// apart from "an agent changed this tab" — used to be
// `JSON.parse(JSON.stringify(doc))`: a deep clone of the whole board, rebuilt on
// every load, every 409 merge and every accepted save. It is now one canonical
// string per tab.
//
// Two things are asserted and two are measured, and NOTHING about the two
// measurements is asserted. The assertions are structural, so they cannot flake:
// the baseline holds strings, and a string cannot alias a tab a view is about to
// mutate — which is half of what the clone was buying. The timings are LOGGED
// and nothing else, because a wall-clock threshold in a browser on a shared
// machine is a test that fails for reasons that have nothing to do with the
// change. There was a loose bound here once, on the theory that a bound loose
// enough is safe; it failed on a busy machine, which is what a loose bound on a
// shared CPU does eventually.
type baselineProbe struct {
	Tabs         int     `json:"tabs"`
	HoldsStrings bool    `json:"holdsStrings"`
	CannotAlias  bool    `json:"cannotAlias"`
	OldTakeMS    float64 `json:"oldTakeMs"`
	NewTakeMS    float64 `json:"newTakeMs"`
	OldCompareMS float64 `json:"oldCompareMs"`
	NewCompareMS float64 `json:"newCompareMs"`
	Err          string  `json:"err"`
}

const baselineProbeJS = `async () => {
  const out = { err: '' };
  try {
    const P = window.__aboardProbe;
    if (!P) throw new Error('the shell never exposed __aboardProbe');

    // A synthetic board big enough for the two costs to be distinguishable, and
    // shaped like a real one: mostly small tabs with a list in them.
    const big = { rev: 1, nextId: 1, tabs: [] };
    for (let i = 1; i <= 400; i++) {
      big.tabs.push({
        id: 'x' + i, name: 'Tab ' + i, type: 'notes',
        state: {
          text: 'lorem ipsum dolor sit amet '.repeat(12),
          rows: Array.from({ length: 20 }, (_, k) => ({ id: 'r' + i + '-' + k, v: k, label: 'row ' + k })),
        },
      });
    }
    out.tabs = big.tabs.length;

    const runs = 5;
    const time = (fn) => {
      const t0 = performance.now();
      let last;
      for (let r = 0; r < runs; r++) last = fn();
      return { ms: (performance.now() - t0) / runs, last };
    };

    // What it used to cost to TAKE a baseline, and what it costs now.
    const oldTake = time(() => JSON.parse(JSON.stringify(big)));
    const newTake = time(() => P.snapshotTabs(big));
    out.oldTakeMs = oldTake.ms;
    out.newTakeMs = newTake.ms;

    // And to answer the only question a baseline is ever asked: has this tab
    // changed? The old comparison canonicalised BOTH sides, per tab, every time.
    const clone = oldTake.last;
    const cloneById = new Map(clone.tabs.map((t) => [t.id, t]));
    const snap = newTake.last;
    const oldCompare = time(() => {
      let n = 0;
      for (const t of big.tabs) if (P.canonKey(cloneById.get(t.id)) === P.canonKey(t)) n++;
      return n;
    });
    const newCompare = time(() => {
      let n = 0;
      for (const t of big.tabs) if (snap.tabs.get(t.id) === P.canonKey(t)) n++;
      return n;
    });
    out.oldCompareMs = oldCompare.ms;
    out.newCompareMs = newCompare.ms;
    if (oldCompare.last !== big.tabs.length || newCompare.last !== big.tabs.length) {
      throw new Error('the comparison pass did not match every tab against itself');
    }

    out.holdsStrings = typeof snap.tabs.get('x1') === 'string';
    // The clone existed partly because the baseline held live tab objects that a
    // view would go on to mutate in place. A string cannot be mutated from
    // underneath the comparison.
    big.tabs[0].name = 'renamed after the baseline was taken';
    out.cannotAlias = snap.tabs.get('x1') !== P.canonKey(big.tabs[0]);
  } catch (e) { out.err = String(e && e.message || e); }
  return out;
}`

func TestTheBaselineIsPerTabAndNotADeepClone(t *testing.T) {
	s := open(t, "probe=1")

	var got baselineProbe
	s.evalJSON(&got, baselineProbeJS)
	if got.Err != "" {
		t.Fatalf("the probe failed: %s", got.Err)
	}

	if !got.HoldsStrings {
		t.Error("the baseline still holds tab objects, so it is still a copy of the document")
	}
	if !got.CannotAlias {
		t.Error("the baseline changed when the document it was taken from was mutated — it is aliasing live tabs")
	}

	// MEASURED AND LOGGED, NEVER ASSERTED. These two numbers used to be
	// t.Errorf comparisons — the new take had to be within 1.5x of the deep
	// clone, the new compare had to beat the old one outright — and they failed
	// once under `-v` on a machine doing something else, in a suite that drives
	// a real browser through a websocket. A wall-clock threshold measured inside
	// a shared CPU's Chromium is a coin toss with a slope, and a check that
	// fails for reasons the code cannot cause is the check people learn to
	// re-run rather than read.
	//
	// The structural properties above are the real proof and they cannot flake:
	// a baseline that holds STRINGS is not a deep clone of the document, and one
	// that does not move when the document it came from is mutated is not
	// aliasing live tabs. Those are the two things item 5 changed. The numbers
	// below are for a reader deciding whether the change was worth making, and
	// they belong to a run, not to a threshold — say so out loud, because a
	// logged number in a suite invites somebody to "just" compare it later.
	// Nothing benchmarks the browser side; the Go benchmarks in
	// pkg/aboard/bench_test.go cover the server half of the same item.
	t.Logf("baseline over %d tabs: take %.2f ms → %.2f ms, compare %.2f ms → %.2f ms",
		got.Tabs, got.OldTakeMS, got.NewTakeMS, got.OldCompareMS, got.NewCompareMS)
}

// The conditional GET the ETag exists for, from the browser's own fetch: the
// shell asks with `cache: 'no-cache'`, which revalidates every time and keeps the
// copy it already has. A board nobody has written to therefore costs a 304 and no
// body, where `no-store` forbade keeping a copy at all and re-transferred the
// whole document on every reload.
func TestARefetchOfAnUnchangedBoardIs304(t *testing.T) {
	s := open(t, "probe=1")

	var got struct {
		First  int    `json:"first"`
		Second int    `json:"second"`
		Tag    string `json:"tag"`
		Err    string `json:"err"`
	}
	s.evalJSON(&got, `async () => {
	  const out = { err: '' };
	  try {
	    const a = await fetch('aboard.json', { cache: 'no-store' });
	    out.first = a.status;
	    out.tag = a.headers.get('ETag') || '';
	    const b = await fetch('aboard.json', { cache: 'no-store', headers: { 'If-None-Match': out.tag } });
	    out.second = b.status;
	  } catch (e) { out.err = String(e && e.message || e); }
	  return out;
	}`)
	if got.Err != "" {
		t.Fatalf("the probe failed: %s", got.Err)
	}
	if got.Tag == "" {
		t.Fatal("GET /aboard.json carried no ETag, so nothing can revalidate")
	}
	if got.First != 200 {
		t.Errorf("the first read was %d", got.First)
	}
	if got.Second != 304 {
		t.Errorf("a read with the document's own ETag was %d, want 304", got.Second)
	}
}
