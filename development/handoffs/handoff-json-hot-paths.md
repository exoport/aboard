# Handoff — the JSON hot paths: structure first, codec second, no per-tab resources

**Status: DONE (2026-08-26), reviewed and repaired the same day — see Measured at the
end for the before/after numbers and each acceptance line, and "Independent review"
after it for the three defects a second pass found. Was: LIVE, after the review fixes and `handoff-e2e-browser-suite.md`, before
`handoff-13-features.md` and `handoff-board-for-vscode-panel.md`** (see `development/README.md`). Decided with the human on 2026-08-25, after a
measured look at how the board moves JSON. Order inside this handoff is also decided:
**(2) the structural fixes first, then (1) the codec, and (3) per-tab resources is NOT to be
built** unless a problem is measured — the trigger is defined at the end.

The research behind this (a read-only map of the code and a verified survey of the Go JSON
libraries as of 2026-08-25) is summarised here so nobody re-derives it.

## Why: the codec is not the bottleneck, the algorithm is

At today's sizes (65 KB example board, 135 KB spike board) nothing below is measurable. The
point is what happens when a board grows — and every one of these costs is proportional to
the *whole document*, not to the edit:

| where | what it does today | anchor |
|---|---|---|
| `postState` | **7 full-document `Unmarshal`s + 1 `MarshalIndent` per POST**: the incoming decode, the CAS check on the current bytes, `reconcileTabs` (both sides), `reconcileNextID` (both sides), `changeSummary` | `pkg/aboard/server.go` `postState`; `tabs.go` `reconcileTabs`; `ids.go` `reconcileNextID`; `journal.go` `changeSummary` |
| `reconcileNextID` → `maxUsedID` | recursively walks the **entire decoded document twice** looking for `"id"` keys, on every write | `pkg/aboard/ids.go` |
| `jsonEqual`, `carryAcks` | **every tab, not just the edited one**, is unmarshalled and re-marshalled to canonicalise before comparing — and this runs twice per write (`reconcileTabs`, then again in `changeSummary`). One edit costs O(all tabs) codec work ×2 | `pkg/aboard/tabs.go` `jsonEqual`, `carryAcks` |
| file watcher | reads **and SHA-256s the whole file every 200 ms**, unconditionally — the comment says it gates on mtime; the code does not | `pkg/aboard/server.go` `watch`, `stateSignature` |
| `GET /aboard.json` | re-reads the file per request, `Cache-Control: no-store`, **no ETag, no 304** — the static-asset path has ETags (`writeAsset`), the document does not | `pkg/aboard/server.go` `getState` |
| browser | on every SSE event **refetches the whole document and deep-clones it** (`baseline = JSON.parse(JSON.stringify(doc))`) — and again after every save; `stateFrom` lookups are a linear `.find` over all tabs | `pkg/aboard/web/aboard.html` `load`, `pushDoc`, `stateOf` |
| journal | a changed tab's whole previous state is copied into `journal.jsonl` (fine — capped by rotation at 16 MiB; listed so nobody "fixes" it) | `pkg/aboard/journal.go` |
| ceiling | **`maxBodyBytes = 8 MiB`** — a large board is rejected by `MaxBytesReader` before any parser runs, so today the question is moot above 8 MiB | `pkg/aboard/server.go` |

Rough shape at 10 MB with the current code: 0.5–1 s per write, ~50 MB/s of sustained hashing
I/O at idle, a 10 MB refetch + clone in the browser per change. A faster codec makes each step
faster; it does not change how many steps there are.

## (2) Structural fixes — do these first

Each item: what, acceptance, test. Keep the compare-and-set semantics and the four tab
guarantees exactly as they are; this is a cost change, not a behaviour change, and the existing
`server_test.go`/`poststate_test.go`/`tabs_test.go` must keep passing untouched.

1. **A benchmark harness before any fix**, so the wins are measured, not claimed. A Go
   benchmark (`pkg/aboard/bench_test.go`) that synthesises a board of N tabs with mixed state
   sizes (a few 1 MB html/markup states, hundreds of small ones) at N = 15 / 500 / 5 000, and
   times: one POST that edits one small tab; one `GET`; one watcher tick. Record the numbers
   in this file's "Measured" section below at HEAD before touching anything.
2. **Parse once, keep the current document in memory.** The server holds the current
   document as bytes + the parsed `board` (state kept opaque as raw bytes) + a per-tab
   canonical hash, loaded at start and replaced on every accepted write (and on a
   watcher-detected external change). A POST decodes the incoming document **once**; CAS,
   `reconcileTabs`, `reconcileNextID` and `changeSummary` all take the two parsed structs.
   Acceptance: exactly one `Unmarshal` of the incoming body per POST, zero of the current
   one; the benchmark's POST time no longer scales with the number of unchanged tabs.
3. **Compare by hash, not by re-marshal.** Canonicalise a tab's state once, when it is
   accepted, and store the hash beside the tab in memory. `jsonEqual` becomes a hash compare;
   `carryAcks` only touches tabs whose hash changed. Acceptance: with N tabs and one edited,
   the codec touches one tab's state.
4. **`reconcileNextID` stops walking the whole document.** Walk only the tabs whose hash
   changed (new ids can only appear in changed state), or maintain the max-used-id as an
   invariant updated at write time. Acceptance: the id allocator test (`ids_test.go`) still
   passes, including the "an agent reused a lower nextId" case.
5. **Watcher gated on `mtime`+size.** Hash only when they moved (the rename-based save the
   spike worried about still changes mtime). Acceptance: idle CPU/IO on a 10 MB board is ~0.
6. **`GET /aboard.json` from the cache with an ETag; `If-None-Match` → 304.** The ETag is the
   document's `updatedAt` or the cached bytes' hash. The browser's post-SSE refetch then costs
   nothing when it already holds the version. Drop the `JSON.parse(JSON.stringify(doc))`
   baseline in `aboard.html` for a per-tab hash (or a structural share) — measure the browser
   side too, in `test/smoke.sh` or a probe page, not by assertion.
7. **Raise `maxBodyBytes` deliberately** once 2–6 are in — pick a number (32 MiB?) and state
   the reason in the constant's comment. Until then 8 MiB is the honest ceiling.

## (1) The codec — after (2)

**Adopt `encoding/json/v2`.** Go 1.27 (released 2026-08-19) makes it the default
`encoding/json`; on this project's Go 1.26.6 it is `GOEXPERIMENT=jsonv2`. The bridge is
`github.com/go-json-experiment/json` — the Go team's published mirror, plain import, zero
dependencies, and it aliases the stdlib implementation once the toolchain is 1.27. Either
import it now, or move the module to Go 1.27 (only if `apex_process_ape` moves too — the two
must stay on one toolchain for the `ape aboard` mount).

Why this one: the jsonbench numbers for the **RawValue path** — 10–21× faster unmarshal,
5.6–12× faster marshal than v1 — and RawValue is exactly the board's shape (`tab.State` is
opaque). Replace `json.RawMessage` with `jsontext.Value`; its `Canonicalize()` (RFC 8785) is
the tool item (2).3 needs, in place of the unmarshal→marshal round trip. Use the native v2
calls, not the v1 compatibility shim (the shim enables `AllowDuplicateNames`, which disables
v2's fast `any` path).

Budget one test pass for the **stricter defaults**: case-sensitive field matching, duplicate
object names rejected, invalid UTF-8 rejected, deterministic map order. Agent-written
documents with duplicate keys will now be refused instead of silently last-wins — right for
this project, but a behaviour change that needs a warning in `aboard apply`'s output and a line
in the skill. Deterministic map order will also move `capsHash` if the manifest marshals a
map; regenerate with `make caps` and say so.

**Rejected, so nobody re-researches** (all verified 2026-08-25): `json-iterator/go` — archived
2025-12-15. `minio/simdjson-go` — no release since 2023, AVX2 amd64 only, no fallback.
`goccy/go-json` — pre-1.0, heavy `unsafe`, five open memory-safety/panic issues, no declared
Go-version policy; only 1.3–1.8× over v2 on struct unmarshal. `bytedance/sonic` — JIT +
`linkname`, amd64/arm64 only with a *silent* stdlib fallback elsewhere, open arm64 crash on Go
1.26.5, no tagged release for 1.27, marshal now 1.2–3.4× slower than v2; also a poor fit for a
tool whose containment story is "no network, no auth, few deps". `segmentio/encoding` — alive
and edges v2 on RawValue unmarshal, but not worth a dependency against stdlib-bound v2.
`tidwall/sjson`+`gjson`, `buger/jsonparser` — healthy, but only relevant to (3).

## (3) Per-tab resources — NOT to be built

`GET/POST /tab/<id>` with per-tab `updatedAt` compare-and-set, the browser and agents moving
one tab instead of the document, `sjson` for a server-side single-path patch. **Do not build
this** — DECIDED. The trigger that reopens it: a real board (not a synthetic one) where, after
(2) and (1) are in, a single-tab write measured by the benchmark harness exceeds ~200 ms or the
document exceeds the raised `maxBodyBytes`. Until someone records such a measurement in this
file, an option on this list is a closed question, for the same reason the diff renderer is.

## Measured

**Status: DONE, 2026-08-26.** (2) items 1–7 are in, and (1) the codec is in too.
(3) per-tab resources is still NOT built and its trigger has NOT fired — the
largest single-tab write measured below is 29 ms against a ~200 ms threshold.

The harness is `pkg/aboard/bench_test.go`:

```sh
go test -run xxx -bench . -benchmem ./pkg/aboard/
```

It synthesises N tabs with **three 1 MB html states at every size** — a constant,
not a fraction of N, which is what makes the rows comparable: 15 tabs is 3.00 MiB
and 500 tabs is 3.05 MiB, so those two differ by 485 unchanged tabs and almost no
bytes, and "does a POST scale with the tabs it did not touch" is a question the
table can actually answer. 5 000 tabs is 3.54 MiB.

Intel Core Ultra 5 125U, Go 1.26.6, `-count=2`, best of two.

| when | tabs | POST one small tab | GET | watcher tick |
|---|---|---|---|---|
| **before** | 15 | 197 ms · 83 MB · 2 686 allocs | 0.64 ms | 2.14 ms |
| **before** | 500 | 210 ms · 88 MB · 74 962 allocs | 0.66 ms | 2.18 ms |
| **before** | 5 000 | 279 ms · 140 MB · 745 603 allocs | 0.83 ms | 2.55 ms |
| **before** | 10 MiB | — | — | 7.71 ms |
| **after (2)** | 15 | 61.8 ms · 40 MB · 478 allocs | 1.43 µs | 0.50 µs |
| **after (2)** | 500 | 63.7 ms · 42 MB · 2 784 allocs | 1.39 µs | 0.50 µs |
| **after (2)** | 5 000 | 78.5 ms · 50 MB · 25 121 allocs | 0.83 µs* | 0.50 µs |
| **after (2)** | 10 MiB | — | — | 0.52 µs |
| **after (1)** | 15 | **17.2 ms** · 31 MB · 374 allocs | 1.41 µs | 0.50 µs |
| **after (1)** | 500 | **17.3 ms** · 32 MB · 1 918 allocs | 1.44 µs | 0.50 µs |
| **after (1)** | 5 000 | **28.5 ms** · 44 MB · 16 487 allocs | 1.36 µs | 0.50 µs |
| **after (1)** | 10 MiB | — | — | 0.52 µs |

\* the GET figure is the SERVER's own work per request — a stat and a write to a
discarding sink. It no longer includes reading the file; copying the bytes to a
real socket is unchanged and is not what moved.

**The machine-independent shape**, which is the number that actually matters:

| | marginal cost of one UNCHANGED tab | ratio 15 → 500 tabs |
|---|---|---|
| before | 15.8 µs | 197 ms → 210 ms (+6.6 %) |
| after (2) | 3.6 µs | 61.8 ms → 63.7 ms (+3.1 %) |
| after (1) | 2.5 µs | 17.2 ms → 17.3 ms (+0.6 %) |

A POST now scales with the BYTES it has to read, parse and write back, which is
irreducible, and no longer with the tabs it left alone. What is left of the
per-tab cost is the struct decode and encode of the tab list itself.

### Each acceptance line, with its number

1. **Benchmark harness** — `pkg/aboard/bench_test.go`, three benchmarks, the
   before rows above taken at `cdabc6f` before any code changed.
2. **Parse once.** *Exactly one Unmarshal of the incoming body per POST, zero of
   the current one.* Counted, not inspected: `documentDecodes` in `document.go`
   and `TestAPostDecodesTheBodyOnceAndTheBoardNotAtAll`. It was **7**; it is
   **1**. Verified by mutation — disabling the parse cache makes it 2 and the
   test fails.
3. **Compare by hash, not by re-marshal.** *With N tabs and one edited, the codec
   touches one tab's state.* `stateNormalisations` + `stateCanonicalisations`,
   asserted EQUAL at N=15 and N=500 and ≤ 4 in total
   (`TestOneEditTouchesOneTabsState`). It was 2N. `carryAcks` now runs only for a
   tab whose state differs, and the comparison is re-made after the carry so a
   write whose only difference was a dropped ack still raises no dot.
4. **`reconcileNextID` stops walking the whole document.** *One edited tab → one
   tab walked, on a board of any size* (`TestTheIDAllocatorWalksOnlyWhatChanged`).
   `ids_test.go` passes untouched, including "an agent reused a lower nextId".
5. **Watcher gated on mtime+size.** *Idle cost on a 10 MB board is ~0*: **7.71 ms
   → 0.52 µs per tick**, one `stat` and no bytes read — 2.6 µs of CPU per second
   at the 200 ms poll, against ~50 MB/s of sustained reading and hashing before.
   The signature stays a CONTENT hash, so a save that rewrites identical bytes
   still wakes nobody (`TestTheWatcherTickHashesOnlyWhenTheFileMoved`, which
   changes the content without moving the stat).
6. **`GET /aboard.json` from the cache with an ETag.** 0.64 ms → 1.4 µs of server
   work; `If-None-Match` → `304` with no body. **The ETag is a hash of the bytes,
   NOT the `rev` counter** — `rev` moves only on an accepted POST, and a person
   editing the file, a `git checkout` or another tool changes the document without
   it, so a `rev` tag would answer 304 for a board that no longer exists.
   Browser side: `baseline` is one canonical string per tab instead of
   `JSON.parse(JSON.stringify(doc))`. Measured in Chromium over 400 tabs
   (`test/e2e/baseline_test.go`, which logs it): **taking a baseline 2.58 → 2.02 ms,
   answering "did this tab change" for all of them 2.96 → 1.52 ms** (the old
   comparison canonicalised both sides; the new one only the live tab), and one
   fewer whole copy of the document retained.
7. **`maxBodyBytes` 8 → 32 MiB**, with the reasoning in the constant's comment
   (what a board can grow to, against what one POST can make the process
   allocate) and in `docs/reference/http-api.md`.

### (1) The codec

Adopted: `github.com/go-json-experiment/json` at
`v0.0.0-20260820222146-c27c302e5fc3`, plain import, builds clean on Go 1.26.6.
Native v2 calls throughout, never the v1 shim.

- `jsontext.Decoder` for the one-pass document decode; `jsontext.Value` for the
  root fields; `Canonicalize()` for the canonical comparison (4.05 µs → 1.23 µs
  on one tab's state, 64 allocations → 1); `Value.Compact()` for the
  normalisation.
- **`tab.State` stayed a `json.RawMessage`.** The handoff said to replace it with
  `jsontext.Value`; measured, v2 puts `encoding/json.RawMessage` on the same
  raw-value fast path — 4.49 ms against 4.96 ms decoding 5 000 tabs, 3.89 ms
  against 4.06 ms marshalling them — so the type change bought nothing and would
  have rippled into every fixture in the tests.
- **The bytes it writes are byte-identical to what `encoding/json` wrote**,
  asserted against the real example board
  (`TestTheWrittenDocumentIsByteIdenticalToTheOldEncoder`). That needed two
  options, both load-bearing: `Deterministic(true)`, because v2 SHUFFLES map keys
  by default and the root key order would otherwise change on every write; and
  `EscapeForHTML(true)`, which is containment rather than style — `htmltab.go`
  splices a tab's `state.data` verbatim into a `<script>` element, and the
  escaping in the file is what stops a widget storing the literal text
  `</script>` from closing it.
- Stricter defaults, all four pinned in `TestTheStricterParserDefaults`:
  duplicate object names refused, invalid UTF-8 refused, case-sensitive field
  matching, deterministic output order. The duplicate-name refusal needed the
  same parse in `apply` — it decodes stdin into a map and re-marshals it, so a
  lenient parse there would have collapsed the duplicate before the server could
  ever see it. Documented in `docs/reference/http-api.md` and in the skill.
- **`capsHash` did not move** (`6ff337ed` before and after), so `make caps` was
  not needed: the manifest is built in `caps.go`, which still uses
  `encoding/json`.

### (3) Per-tab resources — still NOT built, trigger not fired

The condition was "a single-tab write measured by the benchmark harness exceeds
~200 ms, or the document exceeds the raised `maxBodyBytes`". The worst measured
single-tab write is **28.5 ms on a 3.54 MiB board of 5 000 tabs**, and the ceiling
is now 32 MiB. Nothing here reopens it.

## Independent review, 2026-08-26

Reviewed against this handoff and `brief-5-json-hot-paths.md` by a second session,
which re-ran the whole ladder and re-took the numbers. **Every measurement above
reproduced** on the same machine — HEAD (`cdabc6f`) exported to a scratch tree with
the bench harness dropped in gives 199/207/280 ms and 2 632/74 965/745 608 allocs
for the POST rows, 0.56–0.88 ms for `GET`, 2.13/2.18/2.60 ms and **7.69 ms** at
10 MiB for the tick; the working tree gives 17.6/18.0/27.5 ms, 1.38 µs and
0.50 µs. All fourteen claimed mutations were re-applied and each named test was
seen to fail. **Three defects were found and repaired**, all of them in code the
review's own mutations did not reach — which is the point of a second pass.

1. **The read cache could be pinned to a document that no longer existed.**
   `cachedState` read the file and then stat'd it, storing the bytes under the
   stamp that stat returned. The state file is replaced by RENAME, so a reader
   that had already opened the old inode reads the old bytes in full while the
   path already names the new file — and the stat after that read describes the
   NEW file. Every later request then stats, matches, and is served the
   superseded document, **permanently**, ETag and all, so the browser's own
   revalidation answers `304` for a board that no longer exists. Not the bounded
   staleness the read cache was argued for. Reproduced under a concurrent reader
   with a burst of renames — 4 runs out of 4 — and fixed by `readStable`:
   stat, read, stat, believe the stamp only when the two agree, and hand back an
   unusable stamp rather than a wrong one when the file is moving faster than it
   can be read. `cachedState` also swaps the cache with a compare-and-swap now,
   so a reader can no longer publish its older copy over the document the write
   path has just written. Test:
   `TestAWriteLandingInsideAReadDoesNotPinTheCacheStale`.
2. **A state file that could not be READ was taken as an empty board.**
   `currentLocked` swallowed the read error to `raw = nil`, which decodes to a
   board with no tabs — and an empty current document is exactly what guarantee 1
   restores dropped tabs FROM. So a write arriving while the file could not be
   opened would have been reconciled against nothing and every tab the caller did
   not include would have gone, with no removal request, no marker and no journal
   line. The comment above it already claimed this was refused. A file that does
   not EXIST is still allowed through, because that is the first POST on a fresh
   root. Tests: `TestAnUnreadableBoardIsRefusedRatherThanTakenAsEmpty`,
   `TestAFirstWriteWithNoStateFileYetIsAccepted`.
3. **The stricter parser's reason was thrown away for the commonest case.** Every
   failure of the `tabs` decode was mapped to `errTabsNotArray`, so a duplicate
   object name inside a TAB — the shape a generated document falls into, and the
   one `apply`'s own test uses — was answered `{"error":"expected a tabs array"}`
   about an array that was right there, while `http-api.md` and the skill promised
   a refusal naming the member. The shape question is asked separately now
   (`PeekKind`), and a parse error carries its own reason:
   `duplicate object member name "text" within "/tabs/0/state"`. This also
   restored a refusal the rewrite had lost — `{"tabs": null}` was answered `200`,
   where the v1 path required a `[]any` and refused it. Tests:
   `TestADuplicateKeyInsideATabIsNamedRatherThanCalledAShapeError`,
   `TestATabsKeyThatIsNotAnArrayIsAShapeError`.

Two smaller repairs: `stateDoc.src` was written and never read, and its comment
claimed the tabs and fields were views into it when both hold their own copies —
removed. And the encoder-parity test's comment said it reproduced what
`server.go` used to do, when `server.go` held the root in a `map[string]any` and
therefore also alphabetised state keys and pushed every number through
`float64` — corrected to say what it actually pins, which is that the CODEC swap
alone changes nothing.

`capsHash` still `6ff337ed`; `make caps` and `make docs-cli` leave the tree clean.
