# Handoff — the JSON hot paths: structure first, codec second, no per-tab resources

**Status: LIVE. Order: after the review fixes and `handoff-e2e-browser-suite.md`, before
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

_(fill in from the benchmark harness — at HEAD before (2), after each item, and after (1).)_

| when | tabs | POST one small tab | GET | watcher tick |
|---|---|---|---|---|
| before | | | | |
