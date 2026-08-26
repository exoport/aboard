# Brief 5 — JSON hot paths (plan-2 item 5)

Read `COMMON.md` first. Source: `development/handoffs/handoff-json-hot-paths.md` — the order
inside it is DECIDED: benchmark first, then (2) the structural fixes 2→7, then (1) the codec,
and (3) per-tab resources is NOT built. Items 1–4 have landed; `make e2e` is the browser suite
now (run it once per tool call, timeout 600000) and `make smoke` no longer exists.

## Scope

1. `pkg/aboard/bench_test.go`: synthesise a board of N tabs with mixed state sizes at
   N = 15 / 500 / 5000; time one POST editing one small tab, one GET, one watcher tick. Record
   the "before" numbers in the handoff's **Measured** table at the current HEAD before changing
   anything (`go test -run xxx -bench . -benchmem ./pkg/aboard/` — record the machine-independent
   shape too: how the POST time scales with N).
2. Parse once, keep the document in memory (bytes + parsed board with opaque state + per-tab
   canonical hash), replaced on every accepted write and on a watcher-detected external change.
   Acceptance: exactly one Unmarshal of the incoming body per POST, zero of the current one —
   count them in a test (a counting reader or a hook), not by inspection.
3. Compare by hash; `jsonEqual` becomes a hash compare; `carryAcks` touches only changed tabs.
4. `reconcileNextID` walks only changed tabs, or maintains the max-used-id invariant; the
   `ids_test.go` table (item 3) must still pass, including "an agent reused a lower nextId".
5. Watcher gated on mtime+size; idle cost ~0 on a 10 MB board (benchmark it).
6. `GET /aboard.json` served from the cache with an `ETag`; `If-None-Match` → 304. The
   browser's `JSON.parse(JSON.stringify(doc))` baseline replaced by a per-tab hash or a
   structural share — measure the browser side too (an e2e test can time it, or a probe).
   The revision token from item 2 is the natural ETag; say if you used it.
7. Raise `maxBodyBytes` deliberately, with the reason in the constant's comment; update
   `docs/reference/http-api.md`.
8. The codec: `github.com/go-json-experiment/json` (Go 1.27 is gated on the human — §10 — so
   the import, not the toolchain move). `jsontext.Value` for opaque state, `Canonicalize()` for
   the hash, native v2 calls (not the v1 shim). One test pass for the stricter defaults
   (case-sensitive fields, duplicate names rejected, invalid UTF-8 rejected, deterministic map
   order); a duplicate-key document now refused → a clear error from `apply` and a line in the
   skill and in `http-api.md`. If `capsHash` moves because a map marshals deterministically,
   `make caps` and say so. `CLAUDE.md`'s dependency line updated.
   **If the v2 module fails to build cleanly on Go 1.26.6 or its API is unstable in a way that
   would cost more than a day, STOP that sub-item, keep (2) shipped, and record precisely what
   blocked it in the handoff's Measured section and in your report** — that is a legitimate
   "needs the human" outcome.
9. Fill the "after" rows of the Measured table after each structural item and after the codec.
   Keep the semantics: the existing `server_test.go`/`poststate_test.go`/`tabs_test.go` and the
   item-1/2 concurrency tests pass UNTOUCHED (you may add, not edit — if an existing test must
   change, say exactly why in the report).

## Done when

The Measured table has before/after rows; every structural item's acceptance line holds under
the benchmark (state each with its number); `make e2e` and the whole ladder green; the handoff's
Status line updated; `make ci-local` green.
