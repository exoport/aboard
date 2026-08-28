# How to install aboard

aboard ships as a single static binary with the whole browser UI compiled in, so
installing it is putting one file on your `PATH`. There is nothing to install alongside
it — no Node, no `node_modules`, no asset directory — and nothing it needs from the
network at runtime.

## Option 1 — `go install` (simplest if you have Go)

```bash
go install github.com/exoport/aboard/cmd/aboard@latest
```

The binary lands at `$(go env GOPATH)/bin/aboard`; make sure that directory is on your
`PATH`. Needs Go 1.27 or later — the board's JSON codec is the standard library's
`encoding/json/v2`, which is not present before that.

A `go install` build carries no goreleaser ldflags, so its identity comes from Go's own
build information: `aboard version` reports the **module version** it was installed at.
`@v0.1.0` reports `0.1.0`; `@latest` on an untagged commit reports a pseudo-version like
`0.0.0-20260826031230-f67e682b8f8a`, which names the commit but is not a release. Pin a tag
with `@v0.1.0` if you want a version you can say out loud. `dev` is what a binary with no
module and no VCS information at all reports — you will not normally see it.

## Option 2 — Release archive

```bash
VERSION=$(curl -fsSL https://api.github.com/repos/exoport/aboard/releases/latest | jq -r .tag_name)
curl -fsSL "https://github.com/exoport/aboard/releases/download/${VERSION}/aboard_linux_amd64.tar.gz" \
  | sudo tar -xz -C /usr/local/bin aboard
aboard version
```

To pin a specific version, set `VERSION` directly. The Linux asset is
`aboard_linux_amd64.tar.gz`; replace it with `aboard_darwin_amd64.tar.gz`,
`aboard_darwin_arm64.tar.gz`, `aboard_linux_arm64.tar.gz` or `aboard_windows_amd64.zip`
as needed (Windows ships a zip, not a tar.gz).

## Option 3 — Pin it in a project with `bingo`

If you want every contributor and CI run to get the same aboard, pin it with
[bingo](https://github.com/bwplotka/bingo):

```bash
# In your project repo:
bingo get -l github.com/exoport/aboard/cmd/aboard@latest
```

This writes a per-tool `.mod` file under `.bingo/` and a Makefile-friendly variable for
invoking the pinned binary. Commit the generated `.bingo/` files so the pin travels with
the repo. It is the same pattern aboard uses for its own dev tooling.

## Option 4 — Build from source

```bash
git clone https://github.com/exoport/aboard.git
cd aboard
make install        # → /usr/local/bin/aboard
```

This is the only one that works before the first release, and then only from a clone
you already have — `make install` in the checkout, skipping the `git clone`.

Override the destination with `make install INSTALL_DIR=/opt/local/bin` if you cannot
write to `/usr/local/bin`. The directory has to exist already — `install` does not create
it, so `mkdir -p` first.

## Check the install

```bash
aboard version
aboard capabilities
```

`version` prints the build identity. `capabilities` is the more interesting check: it
prints what this binary's board can do — every tab type, every state field, every
control, every route — **with no server running and no project**. If that answers, the
embedded UI came along with the binary, which is the thing an install can get wrong.

## Give a project a board

The binary on its own does nothing to a project until you create one:

```bash
cd ~/work/your-project
aboard init --example --gitignore
aboard serve
```

`init` creates `.aboard/`, `--example` seeds it from the example board so there is
something to look at, and `--gitignore` adds `.aboard/` to the project's `.gitignore` —
a board is local and per-developer, and a committed one is a whole-file JSON conflict
on every merge. `serve` refuses to start without a state file and tells you to run
`init`.

## Verifying release authenticity (optional)

Every release's checksums file is signed with keyless cosign by this repo's release
workflow. To confirm the archive you downloaded is the one that workflow built rather
than a substitute, see [How to verify a release artifact](verify.md). Worth doing when
you ship aboard into CI or a hardened environment; optional for everyday installs.

## Uninstalling

```bash
sudo rm /usr/local/bin/aboard      # or wherever you installed it
```

aboard writes nothing outside the projects you pointed it at. To remove a project's
board as well, delete its `.aboard/` directory — that takes the board, its uploads, its
recipes and its journal with it.

## Next steps

- [Your first board](../tutorials/first-board.md) — the full loop, ten minutes.
- [How to run aboard inside VS Code](run-in-vscode.md) — docking it beside your code.
- Copy `.claude/skills/aboard/` into your project so a Claude Code session knows how to use the board it now has.
