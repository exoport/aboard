# How to give a project a house style

The board ships two palettes — dark and light — and a viewer picks between them with the
switch in the topbar. A **project** can go further and change the colours themselves, so
every board opened in that checkout looks like the thing it belongs to.

That is one file: `.aboard/theme.json`.

## Write the file

```console
$ cat > .aboard/theme.json <<'JSON'
{
  "version": 1,
  "default": "light",
  "light": { "--accent": "#1f5f8b", "--accent-ink": "#ffffff" },
  "dark":  { "--accent": "#7fb2ff", "--accent-ink": "#0b1420" }
}
JSON
```

It is a **patch**, not a replacement: name the tokens you disagree with and everything
else keeps its built-in value. The 21 token names and what each one is for are in
[the theme reference](../reference/theme.md); ask the binary if you would rather not
read a table:

```console
$ aboard capabilities --format json | jq -r '.theme.tokens[]'
```

`default` decides which variant a viewer who has **never pressed the switch** boots
into. It does not override anybody who has — a project default that beat a human's own
choice would be a preference that cannot be kept.

## Check it took

There is no restart. The file is watched, so an open board picks the change up over its
live stream — change a value, alt-tab, and the colour is there.

From the terminal:

```console
$ aboard status
aboard running at http://localhost:41596
  project /home/you/project
  state   /home/you/project/.aboard/aboard.json
  pid     1905115
  since   2026-08-26T00:37:49Z
  theme   /home/you/project/.aboard/theme.json (default light)
  caps    6d8593b1
```

If you misspell something, that is where it says so:

```
  theme   /home/you/project/.aboard/theme.json (default dark)
          ⚠ theme.json: dark.--accnet is not a token this board has — it will be ignored. Available: --accent, --accent-dim, …
```

The same warnings go to the serve log and to the browser console. **Nothing you can put
in this file will blank the board**: an unknown token is dropped, an unusable value is
dropped, and a file that is not valid JSON at all is ignored with a warning — the
built-in palette applies in every case.

## Commit it

`aboard init --gitignore` writes the line `.aboard/`, because a board is a local,
per-developer, non-authoritative channel and a committed one would be a whole-file JSON
conflict on every merge. A house style is the opposite: it describes the project, it is
the same for everyone, and it is worth reviewing. So un-ignore that one path — and
**change the directory line while you are there**:

```gitignore
.aboard/*
!.aboard/theme.json
```

The `*` is not decoration. Git will not re-include a file whose parent DIRECTORY is
excluded, so `.aboard/` followed by `!.aboard/theme.json` ignores the theme file exactly
as if the negation were not there, with no error and nothing to notice until a colleague
clones the project and the colours are gone. Ignoring the directory's CONTENTS instead
(`.aboard/*`) leaves the negation something to act on. Check it rather than trusting it:

```console
$ git check-ignore -v .aboard/theme.json
.gitignore:2:!.aboard/theme.json	.aboard/theme.json
```

That line — the rule that matched being the negation — is the file being kept. If it
names `.aboard/` instead, the `*` is missing.

Nothing else under `.aboard/` should follow it. The board document, the uploads and
everything under `run/` stay ignored — see
[why a local, non-authoritative channel](../explanation/why-a-local-non-authoritative-channel.md).

## Keep the roles, change the values

The one thing worth being careful about. Each token is a **semantic role**, not a slot:
`--agent` is the colour of everything an agent says on this board and `--mark` is the
colour of everything the human asks for, in both themes, whatever the hex values are.
Swapping two of them because the new hues look better apart does not rename anything —
it makes a request strip look like an agent's change banner, everywhere, for everyone
who opens that project.

And keep the contrast. Text on this board is small, so `--text`, `--muted` and `--dim`
are pinned to WCAG AAA (≥7:1) against the ground, `--sunken` and `--surface`; the four
hues that carry small text (`--accent`, `--agent`, `--mark`, `--focus`) are held to the
same bar. The measured table for both built-in variants is in
[the theme reference](../reference/theme.md#the-contrast-rule) — it is the standard your
own values are worth checking against, because nothing in the board will check it for
you.

## See also

- [Colour and themes](../reference/theme.md) — the tokens, the schema, the switch, the embedder message
- [The `.aboard/` layout](../reference/layout.md) — what else lives under `.aboard/`, and what is content rather than runtime
- [How to run aboard inside VS Code](run-in-vscode.md) — where a host's own theme comes into it
