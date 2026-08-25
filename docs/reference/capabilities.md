# The capability manifest

The manifest is the board describing itself: every tab type, every state field a
renderer actually reads, every control it draws, every route the server answers, and
every command the CLI offers. It is assembled from declarations that sit beside the code
they describe, so it cannot quietly disagree with the binary.

```bash
aboard capabilities              # everything, as JSON
aboard capabilities kanban       # one type — the cheap version
aboard capabilities --format md  # the markdown the skill's generated reference is made of
aboard capabilities --format js  # the generated control module the renderers import
aboard capabilities --check      # exit 1 if this project's committed skill copy is stale
```

`GET /capabilities` returns the same JSON from a running server. The CLI needs **no
server and no project**: it answers on a fresh checkout, and in a directory that has
never held a board. That is the property that lets an agent ask what a copied binary can
do before deciding to use it.

## What it contains

| field       | what it is                                                                                      |
| ----------- | ------------------------------------------------------------------------------------------------- |
| `app`       | The board's own name. **Not** the host's — the manifest describes the board, not the process serving it, which is what keeps `capsHash` identical under `aboard` and under `ape aboard`. |
| `schema`    | The state-document layout the renderers are written against.                                    |
| `capsHash`  | A fingerprint of the described surface (see below).                                             |
| `types`     | One entry per renderer.                                                                         |
| `commands`  | The declared command table.                                                                     |
| `rootFlags` | The flags on the root command, inherited by every subcommand.                                   |
| `routes`    | Every HTTP route, with its method and purpose.                                                  |

### A type entry

| field                       | what it is                                                                                |
| --------------------------- | ------------------------------------------------------------------------------------------- |
| `type`, `label`, `blurb`    | The renderer's id, its human name, and one line on what it is for.                        |
| `since`                     | The schema version it arrived in, where that matters.                                     |
| `init`                      | A minimal valid `state` for a new tab of this type.                                       |
| `state`                     | Every state field the renderer reads, each with a type and a doc line.                    |
| `controls`                  | Every button the renderer draws, **in toolbar order** — a list, not a map.                |
| `gestures`                  | What is left once controls carry themselves: drag, drop, wheel, double-click, right-click, type-and-it-saves. |
| `components`, `commonProps` | For a renderer whose state is a tree of nodes (`ui`): the catalog and the props every node takes. |
| `tones`, `colors`           | The palettes this renderer accepts **by name**.                                           |
| `keys`, `notes`             | Key bindings, and anything else worth stating.                                            |

Each entry comes from `pkg/aboard/web/views/<type>.spec.json`, which the renderer is
**rendered from** rather than merely described by: the controls a toolbar draws and the
swatches a palette offers are built from that declaration at runtime. A declaration with
no consumer is a declaration that goes stale, which is the whole argument —
[why the manifest is declared](../explanation/why-the-manifest-is-declared.md).

## `capsHash`

`capsHash` fingerprints the **described surface**, not the source bytes: the manifest is
serialised with its own hash field blanked, hashed, and the first four bytes are the id.
JSON marshalling sorts map keys, so the same surface always produces the same hash.

Deliberately *not* a hash of file contents. That would make a whitespace edit in a
renderer declare every skill copy stale, and a warning that fires for nothing trains its
reader to ignore it.

Two consequences worth knowing:

- **Reordering a spec's `controls` moves the hash**, and that is correct: the order is the toolbar's order, so it is part of the surface.
- **The host does not move the hash.** `aboard capabilities` and `ape aboard capabilities` print the same `capsHash`; if they ever differ, something host-specific has leaked into the manifest.

## `--check`, and the skill beacon

A project's copy of the skill carries a generated reference stamped with the `capsHash`
it was generated for. `aboard capabilities --check` compares that stamp with this
binary's:

| situation                                          | output                                                    | exit |
| -------------------------------------------------- | ----------------------------------------------------------- | ---- |
| The stamp matches.                                 | `current: … matches capsHash …`                            | 0    |
| The stamp differs.                                 | `stale: … no longer matches the binary … — run make caps`  | 1    |
| There is no skill reference in this project.       | `no skill reference in this project … — nothing to check`  | 0    |

A **missing** reference is not staleness. A project that never copied the skill has
nothing to be out of date, and a check that failed there would be noise in every project
that uses the binary without the skill.

`aboard status` prints the same beacon as one line. That placement is the point: an agent
runs `status` as its first act, so it hears about a stale skill inside a command it was
going to run anyway. If it warns, the skill is describing a board that no longer exists —
ask the binary (`aboard capabilities <type>`) and, in aboard's own checkout, regenerate:

```bash
make caps
```

`make caps` builds **twice**, and neither build is redundant: the web tree is embedded,
so the first binary emits the control module from the current declarations and the second
embeds the module it just wrote. It regenerates three files — the control module, the
skill's generated reference, and the skill's recipe index — and then runs `--check` as
the assertion.

## The declared command table

The CLI surface is declared as data in `pkg/aboard/commands.go` — name, arguments, doc,
flags with types and defaults, and the exit codes each command can produce — and the
cobra tree is **asserted equal to it** by a test.

That is a deliberate second declaration, and the reason is specific. The manifest used to
build its flag list by walking the global flag registry at runtime. Under cobra there is
no such registry: flags are per-command, so a walk returns whatever happened to be
registered on the path taken to reach the walk — a manifest whose contents depend on
which subcommand printed it, and therefore a `capsHash` that moves for no reason a reader
could see. Two things that can disagree, with a test that fails when they do, beats one
thing silently derived from the wrong source.

It is the same seam as the view declarations: the declaration is canonical and the code
is checked against it, rather than the code being scraped and the scrape believed.

## `--format js`

Emits `views/controls.generated.js`: every declared control, as an object keyed by id,
so the browser can look one up synchronously. Generated rather than fetched because
button **labels** are not something that can arrive late — they would render from a
fallback and visibly re-label. The file is generated, never edited; the browser suite
fails if it has drifted from the declarations.

## Write-time validation

The manifest is also what the write path checks against: `aboard apply` warns on stderr
when a document sets state no renderer reads — an unknown component, an unknown prop, a
wrong item shape, a bad block field, a `{bind}` that resolves nowhere, or a colour name
this board does not have. It warns and never refuses. See
[the state file](state-file.md#writes-are-validated-and-the-validation-warns).

## See also

- [CLI reference](cli.md) — the generated page the command table backs.
- [HTTP API](http-api.md) — the declared route list, in full.
- [Why the manifest is declared](../explanation/why-the-manifest-is-declared.md) — the argument, and what `gestures` costs.
