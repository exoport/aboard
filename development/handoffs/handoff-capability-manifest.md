# Handoff — capability manifest: the skill reads the binary, not a copy of it

## SUPERSEDED — kept as design rationale only

This was written 2026-08-23 on the `board` spike, proposing a mechanism that did not
exist yet. **It exists now, ported verbatim (plan-1 decision 2) and then extended.**
Nothing below should be read as a current gap; it is kept because the reasoning in
§1–§3 and §7 is still the reasoning behind the system as built, and a future session
extending it should understand *why* it looks the way it does, not just re-derive
that from the code.

**What exists today, in `aboard`, that this document only proposed:**

- **One spec file per renderer**, `pkg/aboard/web/views/<type>.spec.json` — label,
  blurb, `init` default, `state` fields, `gestures`, `keys`, `notes`, `deprecated`
  blocks, and (added after this document was written, see below) a declared
  `controls` list.
- **`aboard capabilities [type] [--format json|md|js] [--check]`** and
  `GET /capabilities` — the `js` format is new relative to this document's original
  proposal: it emits `views/controls.generated.js`, the module every renderer's
  buttons are drawn from (see "beyond the original proposal" below).
- **`capsHash`**, printed by `aboard status`, over the canonicalised aggregated
  manifest — moves only when the described surface moves, not on a whitespace edit,
  exactly as designed in §4.3.
- **A hand-declared command table**, `pkg/aboard/commands.go` — **this is new, and
  exists for a reason this document could not have anticipated**: the spike emitted
  its flag list by walking `flag.VisitAll` at runtime, which worked because there was
  one flat flag set and one binary. Plan-1's decision to move to cobra broke that
  assumption outright — cobra flags are per-command, the global registry is empty,
  and a manifest built by walking whatever happened to be registered on the path
  taken to print it would have a `capsHash` that moves for no reason a reader could
  see. So the CLI surface is now declared as data in `commands.go`, the same seam as
  a renderer's `.spec.json`, and a test asserts the actual cobra tree matches it. See
  `pkg/aboard/commands.go`'s own header comment for the fuller version of this
  argument — it is worth reading once, since it is the one place this system's design
  changed for a reason external to its own original proposal.
- **`writeWarnings`** (`pkg/aboard/caps.go`) — named differently from this document's
  original `unknownStateKeys` because it grew past that name: it descends into a `ui`
  component tree against the declared catalog and recurses into `stack` blocks,
  catching an unknown component, an unknown prop, a wrong item shape, a bad block
  field, and a `{bind}` that resolves nowhere — not just top-level unknown keys, which
  is all §4.8 below originally proposed.
- **`make caps`** regenerates the committed skill reference and the controls module;
  builds the binary twice, because `pkg/aboard/web` is embedded and the first build
  has to write `controls.generated.js` before the second build can embed it. The test
  suite fails if either is stale.

**Phase-by-phase status**, against §5's original plan:

- **Phase 1 (beacon and manifest) — done**, and its own "interim wrinkle" (hand-write
  the type table in Go as a stopgap) never survived to ship — plan-1 decision 2's
  verbatim-then-split order meant the specs came in already restructured beside the
  renderers, not bolted onto Go first.
- **Phase 2 (specs beside the renderers) — done.** `pkg/aboard/web/aboard.html`
  fetches `/capabilities` at boot and builds its help panel from it; the old
  hand-written `HELP`/prose table this document proposed replacing no longer exists
  to drift.
- **Phase 3 (write-time validation) — done**, and grew past its own proposal: see
  `writeWarnings` above.
- **Phase 4 (a DOM sweep asserting every `button[title]` appears in that type's
  `gestures`) — explicitly NOT built as proposed, and this is a decision, not an
  omission.** Measured before building it: on one renderer alone, the sweep surfaced
  roughly 17 candidate titles that were tab-strip chrome for every 4 real gaps — a
  signal ratio bad enough that the check would get muted, and a muted check is worse
  than none. **The fuzziness was removed at the source instead**: renderer buttons
  are now declared as data (a `controls` list per spec, drawn from by
  `pkg/aboard/web/views/controls.js`), so a button either matches a declaration or
  renders visibly flagged as undeclared — no DOM scrape needed, no title-text
  heuristics, no false-positive chrome. **Do not resurrect the sweep.** This is a
  closed decision inherited from the spike, not a gap this rewrite left open.
  **Mount receipts (`bb368`, plan-2 item 6) are not the sweep coming back**, and the
  distinction is the whole reason they were allowed: every id a receipt carries is
  already DECLARED in a `.spec.json`, nothing is matched against `gestures`, and no
  title text is read. The browser reports which declared controls it drew; it does not
  guess which buttons ought to have been documented.

**One more thing beyond the original proposal, worth carrying forward explicitly:**
`controls` in a `.spec.json` is a **list**, not a map, because — unlike state
fields — a renderer's buttons have a meaningful order: it is literally the order they
sit in the toolbar and the order the help panel lists them in. Reordering a spec
therefore moves `capsHash`, which is correct, not a bug to work around.

---

## 1. The problem, stated precisely (as originally written)

The skill describes what the board can do. The board describes what the board can do
by *being* the board. Absent a mechanism connecting the two, the skill is a
hand-maintained copy that decays every time a renderer grows a field.

Measured on the spike, in one day of work: renderers went 9 → 15, Go files 5 → 9,
binary flags 7 → 16, and the two reference files grew by roughly 200 lines — every
one written by hand, from memory, after the fact.

**Two directions of drift, with very different costs**, and this is the part that
still matters regardless of implementation details: a capability the skill does not
know about is merely unused (cheap); a capability the skill describes *wrongly* is
expensive, because the agent writes state a renderer ignores, nothing errors, and the
agent reports having done something it did not do.

## 2. The precedent already in the repo (as originally written)

The spike's own `?` help panel already solved this problem once, for the human
audience, by keeping a declaration beside the renderer registry rather than in a
hand-written document. The shape of the eventual answer — extend that principle to
the agent-facing skill — was not novel; it was recognizing that the same fix applied
to a second audience.

## 3. The seam: the binary owns facts, the skill owns judgment (as originally written)

Still the load-bearing idea in the shipped system, unchanged by the port:

**Generate (facts — they drift):** flags and their usage, endpoints, tab types with
label/blurb/default state, each type's state fields, per-type affordances, schema
version, deprecations, and — added since — declared controls.

**Keep authored (judgment — it does not drift):** which renderer to reach for and
when, tab sprawl, `--by` naming, multi-session etiquette, the hard rules. This is the
valuable half and no generator produces it. `aboard`'s own `docs/explanation/why-*.md`
(plan-1 §14) is where this half now lives, carried over from the spike's `CLAUDE.md`
substantially verbatim.

**The trap avoided:** moving the prose into a Go string relocates drift rather than
removing it. Emission only fixes drift when the declaration lives in the same
directory as the code it describes, so the change that adds a capability physically
touches the file that documents it — `pkg/aboard/web/views/<type>.js` and
`<type>.spec.json`, side by side.

## 4. Design (as originally proposed — see the SUPERSEDED note above for what shipped)

### 4.1 Canonical form

One spec file per renderer, JSON so Go reads it with `encoding/json` and never parses
JavaScript, embedded the same way the rest of `pkg/aboard/web` is. Structure:
`type`, `label`, `blurb`, `since`, `init`, `state` (per-field type + doc), `gestures`,
`keys`, `notes`, `deprecated`. (Shipped form adds `controls` — see above.)

### 4.2 Who consumes it

```
views/*.spec.json
   ├─→ Go: embed → aggregate → GET /capabilities, aboard capabilities
   │        ├─→ generated markdown → committed into the skill
   │        ├─→ capsHash → printed by `aboard status`
   │        └─→ unknown/deprecated key warnings on apply
   └─→ aboard.html: fetch('/capabilities') at boot → builds the help panel
```

Go aggregates because Go already owns `apply`, so write-time validation comes free
once the spec is on that side. The browser takes one fetch at boot; no hand-written
help table lives in the shell.

### 4.3 The staleness beacon

The same problem `reload.go` already solves for an open browser tab — "a page keeps
running the code it loaded" — applies to a skill: "an agent keeps trusting the
reference it read." Same fix: a hash of the aggregated, canonicalised manifest,
printed by `aboard status` and compared against a stamp inside the committed skill
file. **Must not reuse a hash over source bytes** — that would call a whitespace edit
"stale" and train an agent to ignore the warning. It must hash the described surface,
not the files describing it.

### 4.4 Commands (as proposed; see the current grammar for what shipped)

```
aboard capabilities                    # full manifest, JSON
aboard capabilities --format md        # the markdown reference (what gets committed)
aboard capabilities dag                # one type's slice, token-cheap
aboard capabilities --check            # exit 1 if the committed reference is stale
```

None of these require a running server — they read embedded assets and the declared
command table only. `GET /capabilities` serves the same JSON to anything else that
wants it.

### 4.5–4.7 Endpoints, flags, the committed reference (as proposed)

Endpoints and flags are both derivable without new authoring once declared as data —
§4.5 proposed converting a route `switch` into a table; the shipped `declaredRoutes`
in `pkg/aboard/caps.go` is exactly that, and needed no rename beyond the route path
itself (`/board.json` → `/aboard.json`, per plan-1 decision 4). The generated
reference stays committed inside `.claude/skills/aboard/references/reference.generated.md`
rather than fetched per session — readable with no server running, greppable,
reviewable in a normal diff, no per-session token cost. Runtime emission
(`aboard capabilities`) is the escape hatch for the one type a session actually
picked, not the primary path.

### 4.8 Deprecation — the half documentation cannot do (as proposed; shipped as `writeWarnings`)

A doc can announce a removal; only the write path catches an agent still using it.
**A warning on stderr, never a refusal by default** — tabs are data, agents invent
state, and an unknown key is a smell, not an error. `apply --strict` (build-queue
item `bb362` in `handoff-13-features.md`) is where an opt-in refusal belongs; the
default stays warn-and-write.

## 5. Phasing — see the SUPERSEDED status table above for what shipped and what didn't

## 6. Tests

Unchanged in spirit from the original proposal, now exercised against the shipped
system: `aboard capabilities` exits 0 with no server running and emits valid JSON;
every type in the manifest has a mount and vice versa; `aboard capabilities --format
md` is byte-identical to the committed reference (the actual drift check, and the
whole point); `aboard status` prints a `caps` line and warns on a doctored stamp;
`aboard capabilities dag` returns only `dag`; applying a tab with a bogus top-level
state key warns on stderr and still writes.

`make caps` is the one-word regenerate-and-check-in-CI target this section originally
asked for by name.

## 7. Decisions, and what was rejected (as originally written — still true)

- **JSON specs, not JS.** Go must never parse JavaScript.
- **Specs beside the renderer, not centralized in Go.** Centralizing would split
  renderer knowledge from the renderer file — the exact cause of the drift being fixed.
- **The generated file is committed, not fetched per session.** Correct-at-read-time
  for the same reason a lockfile is; works with no server; costs nothing per session.
- **`capsHash` over the canonicalised spec, not over source bytes.** A beacon that
  cries stale on a whitespace change gets ignored.
- **Warnings, not rejections, on unknown state keys**, by default. Tabs are data;
  agents invent state; refusing writes would break that premise. (`--strict` is the
  opt-in escape hatch, added after this document was written — see
  `handoff-13-features.md` `bb362`.)
- **Judgment stays hand-written.** Generating "when to use `dag` vs `diagram`" would
  produce something worse than what a human writes.

## 8. The question this document posed, and how it was actually answered

This document asked whether the skill should stay project-scoped or eventually ship
as something usable across differing board checkouts, and said the answer changes how
far to build the generation machinery. **Plan-1 answers it directly, for `aboard`
specifically**: the skill is `.claude/skills/aboard/`, a hand-copied directory, not
embedded in the binary, with no `skill install` step — and the human's own skill
framework is what derives an `ape aboard`-flavoured variant from it, not this
project's build. So the committed generated file plus a drift check is the right
level of investment here, same conclusion this document reached for "phases 1–3 are
sufficient" — just settled by an explicit decision rather than left open.

## 9. Anchors (updated)

| what | where, in `aboard` |
|---|---|
| the renderer registry and mount lookup | `pkg/aboard/web/aboard.html` |
| the CLI surface, declared as data | `pkg/aboard/commands.go` |
| aggregation, `capsHash`, write-time warnings | `pkg/aboard/caps.go` |
| the write path `apply` goes through | `pkg/aboard/server.go` (`applyStdin`, `postState`) |
| signature machinery this beacon's design mirrors (but does not reuse) | `pkg/aboard/reload.go` |
| skill files | `.claude/skills/aboard/SKILL.md`, `references/reference.generated.md` |
| per-renderer declarations | `pkg/aboard/web/views/<type>.spec.json` |
