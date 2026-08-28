# How to run the browser suite

`make e2e` drives a real Chromium against a real board and clicks, drags, types
and scrolls its way through the whole interface. It is **local only** — it never
runs in GitHub CI, where Go unit tests are the gate — and it needs **no running
server and no project of its own**: the harness starts the engine in-process on a
temporary board it seeds itself.

```bash
make e2e
```

The first run downloads about 330 MB into `~/.cache/ms-playwright` and
`~/.cache/ms-playwright-go`: the Playwright driver and the Chromium build pinned
by the `playwright-go` version in `go.mod`. After that, startup is a few seconds
and the whole suite takes about a minute.

## What it needs

- **Nothing you have to start.** No `aboard serve`, no `PROJECT=`, no port. The
  suite seeds a temp directory with the example board plus an interaction
  fixture, serves it in-process on a free port, and deletes it at the end.
- **Chromium comes from Playwright**, not from your system. A snap-confined
  chromium is not used and would not work.
- **Node is not needed.** The old shell suite shelled out to `node -e` for every
  assertion; this one is Go.

## Knobs

| Variable | Effect |
| --- | --- |
| `E2E_HEADED=1` | A visible browser, slowed down enough to watch. Needs a display — on a headless machine Chromium will not start. |
| `E2E_TRACE=always` | Keep a Playwright trace for the tests that PASSED, not only the ones that failed. |
| `E2E_KEEP=1` | Keep the temporary board after the run, and print where it is. |
| `E2E_RUN='TestKanban.*'` | Run a subset. The gesture-coverage gate is skipped when you do — see below. |
| `E2E_CHROMIUM_ARGS='…'` | Extra Chromium switches. See the note on `IsolateSandboxedIframes` in `test/e2e/driver_test.go`. |

```bash
E2E_RUN='TestDraggingADagNode.*' make e2e     # two tests, ~2s
E2E_HEADED=1 E2E_RUN='TestDraggingADagNode.*' make e2e   # the same, where there is a display
```

## When something fails

Every failing test writes its evidence twice — once under the temporary board it
drove, and once under `<repo>/.aboard/run/e2e/<TestName>/`, which is gitignored
and survives the run:

| File | What it is |
| --- | --- |
| `trace.zip` | The Playwright trace: every action, a DOM snapshot at each one, network, console. Open it with `npx playwright show-trace <path>`. |
| `screen.png` | A full-page screenshot at the moment of failure. **Look at it.** Every visual regression this project has shipped passed the DOM assertions first. |
| `aboard.json` | The board document as it stood, so you can see what the write path actually stored. |
| `console.log` | Console messages, page errors and failed requests from that page. |

## The gesture-coverage gate

`views/<type>.spec.json` declares, for each renderer, the gestures a human can
perform that are not buttons — drag, drop, wheel, double-click, right-click,
type-and-it-saves. That list had **no mechanical consumer**, which is exactly why
it drifted: state fields are read by `aboard apply`'s write warnings, so a wrong
one produces a wrong warning and somebody fixes it, while a wrong sentence about
a gesture broke nothing at all.

The suite is that consumer. Each test calls `covers(t, "<renderer>", "<gesture>")`,
and after the run the harness asserts:

- **every renderer has at least one gesture test** — a new renderer with no test
  fails the run;
- **every gesture a test names is one its spec actually declares** — so a test
  still asserting a gesture that was removed fails too.

Declared gestures with no test of their own are **listed, not failed**. A
per-sentence requirement sounds stronger and is not: some of those sentences are
claims about rendering rather than gestures, and forcing one test per sentence
buys assertions written to satisfy a counter. As it happens the list is currently
empty — all 33 declared gestures across the 15 renderers have a test — but that
is a fact about today, not a gate.

The gate is skipped when `E2E_RUN` selects a subset, because with a filter set an
uncovered renderer is the filter doing its job.

## One board, and why to shuffle it now and then

The whole suite shares one temporary board and one server. Forty `init --example`
seedings on forty ports would be cleaner and much slower, so instead the tests
are written not to fight: each writes to its own tab where it can, ids come from
the board's own `nextId` rather than from any private counter, and the few that
must touch a shared tab put back what they found.

That is a discipline, not a mechanism, so check it occasionally:

```bash
go test -tags e2e -count=1 -timeout 10m -shuffle=on -v ./test/e2e
```

It has earned its keep twice. It found a helper allocating scratch tab ids from a
private counter that the board's own allocator walked into, producing two tabs
with one id; and it found a real product defect — a tab removed in the browser
came back on screen when a reload landed while the save was still in flight,
because the merge had no way to represent a local deletion
(`TestRemovingATabSurvivesAReloadArrivingMidSave` now holds the POST open with a
route interceptor and proves it deterministically).

The gesture-coverage gate is unaffected by order: it is an assertion about what
the tests registered, made after they have all run.

## What replaced `test/smoke.sh`

The old suite was one-shot `chromium --headless --dump-dom` plus `curl` and
`node -e`. It could not click, drag, wheel, double-click, right-click or type; it
could not reach into the sandboxed widget frame; and it switched SSE *off* rather
than exercising it. It also had to be aimed at a real board with `PROJECT=` and
**wrote to it**.

It was retired only once every check it made had somewhere to go. Some went to
Go tests, which is strictly better — those run in CI, where the shell suite never
did. The rest became browser tests.

| `smoke.sh` check | Now |
| --- | --- |
| views mount, no `THREW` / `MISSING EXPORT` / `ERROR` / `REJECTION` | `e2e` `TestEveryTabActivatesAndRendersItsOwnOutput` (in the real shell, with the real console) |
| the `COUNT-LOW` render thresholds (9 renderers) | same test — a per-type "characteristic output" table covering all 15 |
| each tab activates in the real shell | same test |
| `kv resolves a bind` | `e2e` `TestAKvComponentResolvesABind` (both the bound and the literal case) |
| `undeclared controls rendered: none` | `e2e` `TestEveryRenderedControlResolvesToADeclaration` |
| `declared controls rendered: > 0` | same test |
| a key reordering is not an edit | `e2e` `TestAForeignWriteInsideTheSaveDebounceKeepsTheHumansEdit` |
| an SSE reload merges instead of replacing | same test |
| the interrupted save is re-armed and lands | same test |
| a collision never overwrites the human's stashed copy | `e2e` `TestASecondCollisionStillOffersTheHumansOwnText`, which also presses **Restore mine** |
| a tab exports as markdown with no server | Go `TestAMarkdownExportLeadsWithAHeading`, `TestExportNeedsNoServer` |
| a rows tab exports as csv | Go `TestATableExportsAsCSVHeadedByItsIds` |
| an unknown tab is refused, not silently empty | Go `TestExportRefusesAnUnknownTab` |
| a gate export carries the reasons | Go `TestExportNeedsNoServer` |
| `frame-ancestors` lists the vscode webview | Go `TestHTMLTabCSP` |
| `connect-src 'none'` / `sandbox allow-scripts` | Go `TestHTMLTabCSP`; on the page, `e2e` `TestTheWidgetFrameIsSandboxedOnThePage` |
| the widget script is terminated (a real `</script>`) | Go `TestTheServedFrameTerminatesItsScript` |
| the bridge is served under its aboard names | Go `TestTheServedFrameCarriesTheBridgeUnderItsAboardNames` |
| no pre-rename bridge name survives | same test |
| an html block inside a stack serves its own document | Go `TestAStackBlockIsContainedByteIdenticallyToATab` |
| the block path is not the empty-tab placeholder | Go `TestABlockPathDoesNotServeTheEmptyTabPlaceholder` |
| a block's document is contained exactly like a tab's | Go `TestAStackBlockIsContainedByteIdenticallyToATab` |
| a wrong block id says so instead of 404ing blankly | Go `TestAWrongHTMLPathSaysWhatWasWrong` |
| the manifest answers with no server needed | Go `TestManifestHasTypes`, `TestManifestMarshals` |
| one type can be asked for on its own | Go `TestOneTypeCanBeAskedForOnItsOwn` |
| the committed skill reference matches the binary | Go `TestTheGeneratedSkillReferenceIsNotStale` |
| every type has a spec and a mount | `e2e` `TestTypesInitMatchesTheSpecs` |
| every `TYPES.init` starts with the keys its spec declares | same test — by CALLING `init()`, not by regexing the shell and evaluating it in a Node vm |
| every button goes through `controls.js` | Go `TestEveryButtonGoesThroughTheHelper`, `TestNoRendererWritesALiteralButtonTag` |
| every declared control is used by its renderer | Go `TestEveryDeclaredControlIsUsedByItsRenderer` |
| plain (undeclared) buttons — advisory | Go `TestWhichRenderersStillUsePlainButtons` (a `t.Log`, never a failure) |
| the advertised routes answer | Go `TestEveryAdvertisedRouteAnswers` — all 15, not the 4 the shell checked |
| the nine `apply` write warnings | Go `TestWriteWarningsPerDetector` |
| a field's write path is not a broken bind | Go `TestWriteWarningsStaysQuietOnCorrectDocuments` |
| a bind to an initialised-null value is not broken | same test |
| valid tone and colour names are not reported | same test |
| a stale schema version is reported to the writer | Go `TestAStaleSchemaVersionIsReportedToTheWriter` |
| a version the board does not write is stamped, not stored | Go `TestPostStampsTheSchemaVersion` |
| a rename applies and reverts through the running board | Go `TestApplySendsTheRevisionAsItsBase`, `TestPostRefusesAStaleBase` |
| the journal recorded the write that just happened | `e2e` `TestTheJournalRecordsTheWriteThatJustHappened` |
| the journal records writes | same test |
| a valid predicate is accepted and times out cleanly | Go `TestAnUnknownWaitPredicateIsRefusedRatherThanAwaited` |
| a malformed / unknown predicate is refused | same test, plus Go `TestParsePredicateRefusesEverythingElse` |
| an image upload lands in `uploads/` | Go `TestAnUploadLandsInUploadsAndServesAsAnImage`; the human's half is `e2e` `TestPastingAnImageUploadsItAndAddsIt` |
| the uploaded file serves as an image | same tests |
| a non-image upload is refused | Go `TestANonImageUploadIsRefused` |
| an encoded traversal under `uploads/` is refused | Go `TestAnEncodedTraversalUnderUploadsIsRefused` |
| the action strip renders | `e2e` `TestAnActionStripRecordsAnIntentInsteadOfActing` — and it now asserts the intent is RECORDED, which the shell could not do |
| the first SSE frame carries the ui signature | Go `TestTheFirstSSEFrameCarriesTheUISignature`; the page's use of it is `e2e` `TestADevStylesheetChangeRelinksWithoutReloading` and `TestADevCodeChangeReloadsThePage` |
| a read-only kanban: badge, no drag, no edit, cards > 0 | `e2e` `TestAReadOnlyKanbanOffersNothingToEdit` |
| poke with nobody waiting releases 0 | Go `TestPokingWithNobodyWaitingReleasesNobody` — the count, which the browser cannot assert because the button it would press is disabled |
| a `wait` session registers, declares its deadline and reason | `e2e` `TestTheNotifyButtonReleasesAWaitingSession` |
| poke releases it; the session prints the poke and exits 0 | same test |
| nobody is left waiting after a poke | same test |
| the notify button renders disabled with nobody waiting | `e2e` `TestTheNotifyButtonIsDisabledWithNobodyWaiting` |

Two things the shell suite did that are deliberately **not** reproduced:

- **It poked the live board**, releasing any session genuinely blocked on
  `aboard wait`. The e2e suite has its own board and its own waiter, so it cannot.
- **It ran `aboard apply` against whatever `PROJECT` named.** Everything here
  writes to a temp root.

## Why playwright-go, and the drivers that were rejected

Kept here because it is the question anybody arriving at this file asks second, and
because re-running the survey costs a day. All of it was verified on 2026-08-25.

The suite is **playwright-go inside `go test`, behind an `e2e` build tag**. What decided
it: `Locator`/`FrameLocator` reach the sandboxed widget frame transparently — and that
frame is the hardest part of this application, because it is `sandbox="allow-scripts"`
with no `allow-same-origin`, so Chromium's sandboxed-iframe isolation puts it in a
separate process; `Locator.DragTo` covers HTML5 drag-and-drop while `page.Mouse` covers
the pointer-capture drags; assertions auto-retry; and tracing writes the same `trace.zip`
the Playwright trace viewer opens. No Node at runtime.

The alternatives, and why each one lost:

| driver | why not |
| --- | --- |
| **go-rod** | No functional commit since 2024-12, and its pinned Chromium is a 2024 build. |
| **chromedp** | Active and pure Go, but out-of-process frames need the `Target.setAutoAttach{flatten:true}` dance by hand and there is no auto-wait. Hand-rolling the frame path is precisely the wrong place to hand-roll. |
| **@playwright/test** (Node) | The higher capability ceiling — trace viewer UI, `--ui`, codegen, built-in visual snapshots — at the cost of a second toolchain (`package.json`, `node_modules`) in a Go repository. The only thing lost by staying in Go is a UI that opens a Go-produced trace anyway. |
| **Puppeteer** | Caretaker mode, with open bugs attaching to out-of-process frames. |
| **Cypress** | Cannot automate a cross-origin frame at all, and its events are simulated rather than driven through the browser. |
| **WebdriverIO / Selenium-Go** | Node plus BiDi frame bugs, and an unofficial Go binding with no releases. |

One thing deliberately **not** built, and it is not queued: visual regression against
pixel baselines. Font rendering makes it the flakiest layer there is, and nothing has
asked for it. `screen.png` on a failure plus a human looking at it is the coverage that
exists, and it is the coverage that has actually caught things.

## The exploratory complement

Driving the board by hand with an agent — `chrome-devtools-mcp` attached to a
running board with `--browser-url`, or Playwright MCP — is a good way to hunt for
bugs nobody thought to write a test for. It is not a gate: agent exploration
repeats itself often enough that the cost per new finding is high.

The pattern is **explore once, codify forever**: whatever the agent finds becomes
a test in `test/e2e/`. See the skill's multi-session reference.

## Screenshots of your own board

`make e2e` screenshots only a temporary board that is then deleted. To look at
the board you are actually working on, that is still `make shot`:

```bash
make shot SHOT_TABS="ab133 ab22#help"
```

It needs a running server, takes an optional `PROJECT=`, and only reads the board
— it writes pictures into that project's `.aboard/run/shots/`.
