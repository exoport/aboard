# Brief 10 — the make targets are the gate: update the pinned tools and make every gate run them

Read `COMMON.md` first. The human's instruction, verbatim in spirit: **"we must use the make so we
use the bingo go tooling, we must update the tools."** Two copies of a tool (a bingo pin and a
`$PATH` copy) were found disagreeing during item 9; the decision is now made — **the bingo pin,
reached through `make`, is authoritative everywhere**: the ladder, the pre-commit hook, CI, the
docs. Nothing may call a tool from `$PATH` any more.

## Already done by the orchestrator (uncommitted, in the tree)

`bingo get` moved the four pins: golangci-lint v2.6.0 → **v2.13.1**, gofumpt v0.10.0 → **v0.11.0**,
govulncheck v1.3.0 → **v1.7.0**, goreleaser v2.15.4 → **v2.17.1** (v2.18.0 requires Go 1.27, which
is gated on the human — record that beside the Go 1.27 entry in plan-2 §10). `.bingo/*` is
modified; do not re-run `bingo get`.

## Scope

1. **`make lint` at zero under v2.13.1, without weakening the linter set.**
   - `exhaustruct_v5` is v2.13's successor of `exhaustruct`, which `.golangci.yaml` already
     disables — add `exhaustruct_v5` to the disable list with a comment saying so (the same
     decision the `wsl`/`wsl_v5` pair already records). That is the ONLY config change allowed.
   - Every other finding is FIXED in code: the two gosec taints (`logs.go` G703 path traversal —
     the tab id is validated by `logTabRe`, make the safe path construction explicit through
     `layout.go`, since that file is the only one allowed to join paths; `server.go` G705 XSS on
     `w.Write(body)` — read what taints it and fix on its merits: content-type, `nosniff`, or a
     genuine false positive with a `//nolint:gosec // reason` that names WHY); the four
     `modernize` (`strings.Cut`, `errors.AsType`, `slices.Backward`); the four `prealloc`; and
     every `goconst` — name the repeated wire keys (`"error"`, `"ok"`, `"at"`, `"by"`, `"note"`,
     `"version"`, `"nextId"`, `"rev"`, `"updatedAt"`, `"lastEditedBy"`, `"tabs"`, `"reason"`,
     `"bytes"`, `"type"`, `"/aboard.json"`, `"/log"`, `"POST"`, `"string"`, `"bool"`, `"int"`,
     `"human, json or yaml"`, `"agent-1"`, `"timeout"`, `"printed"`, `"change"`, `"node"`,
     `"tab must be a plain id"`) as constants in ONE place per vocabulary (JSON keys of the state
     document; JSON keys of replies; flag types; routes) and use them. Do not change any wire value.
     Tests keep spelling wire values out (the config already excludes goconst for tests).
2. **`make fmt-check`** (new target): the pinned gofumpt `-l .`, non-zero with the file list when
   anything needs formatting. Add it to `ci-local`. The ladder in `COMMON.md` and in `CLAUDE.md`
   replaces the bare `gofumpt -l .` with `make fmt-check`; `make check` stays the no-tools gate.
3. **`.pre-commit-config.yaml`**: replace the `golangci-lint-mod` hook (it runs `$PATH`'s
   golangci-lint — the `.bingo` suffix in its rev only prunes `.bingo/` from the file list) with
   `local` hooks: `make lint` and `make fmt-check` (`language: system`, `pass_filenames: false`,
   `files: '(\.go$)|(\bgo\.mod$)'`, `require_serial: true`). Keep `config_secrets`.
   `make pre-commit` must exit 0 afterwards; run it.
4. **`.github/workflows/release.yml`**: `goreleaser-action`'s `version:` moves to v2.17.1 (its
   comment says to move it with the pin). Check `ci.yml` calls only make targets for lint, vet,
   fmt and govulncheck; if any step calls a tool from `$PATH`, route it through make.
5. **Docs**: `CLAUDE.md`'s pin-versus-`$PATH` paragraph and `development/README.md`'s entry become
   RESOLVED ("the make targets are the gate; the hook and CI run them; `bingo get` moves a pin and
   the release workflow together"); the tools table/bullets name the new versions where a version
   is written. `CHANGELOG.md` one line.
6. **The remote**: both repos DO have `origin` (`git@github.diegos_exo:exoport/aboard.git` and
   `…/aboard_vscode.git`); nothing has been pushed. Correct `CLAUDE.md:64` ("No remote exists
   yet…") and `plan-2_finish-line.md:56` ("no remote exists until §10") to: a remote exists,
   nothing has been pushed, and pushing waits for the human's manual test of both repos and their
   review of §10. `plan-1`, its briefs and `handoff-phase-e-finish.md` are historical — leave them.
7. **Never push.** Never touch the spike. Do not commit.

## Done when

`make lint` 0 · `make fmt-check` clean · `make pre-commit` exit 0 · `make ci-local` green (it now
runs the new goreleaser and govulncheck) · `make e2e` green once · `go test -race ./...` ok ·
`capabilities --check` 0 (no spec change expected; say if `capsHash` moved) · the docs above say
what is true.
