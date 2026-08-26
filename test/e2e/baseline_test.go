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
// Two things are asserted and two are measured, and the split is deliberate. The
// assertions are structural, so they cannot flake: the baseline holds strings,
// and a string cannot alias a tab a view is about to mutate — which is half of
// what the clone was buying. The timings are LOGGED rather than gated, because a
// wall-clock threshold in a browser on a shared machine is a test that fails for
// reasons that have nothing to do with the change; the one bound asserted is the
// loose one that would catch a genuine regression (the new path costing more
// than the old one it replaced).
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

	t.Logf("baseline over %d tabs: take %.2f ms → %.2f ms, compare %.2f ms → %.2f ms",
		got.Tabs, got.OldTakeMS, got.NewTakeMS, got.OldCompareMS, got.NewCompareMS)

	if got.NewTakeMS > got.OldTakeMS*1.5 {
		t.Errorf("taking a baseline costs %.2f ms where the deep clone it replaced cost %.2f ms",
			got.NewTakeMS, got.OldTakeMS)
	}
	if got.NewCompareMS > got.OldCompareMS {
		t.Errorf("comparing against the baseline costs %.2f ms where the old double canonicalisation cost %.2f ms",
			got.NewCompareMS, got.OldCompareMS)
	}
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
