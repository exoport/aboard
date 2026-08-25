# How to embed aboard in ape

aboard's cobra tree is built by a function, not assembled into package-level variables,
so another CLI can mount the whole command set as one subcommand. That is how
`ape aboard <command>` works, and the same shape works for any host.

## Mount it

Add the module, then add one command:

```bash
go get github.com/exoport/aboard@latest
```

```go
import (
    "github.com/exoport/aboard/pkg/aboard"
    "github.com/exoport/aboard/pkg/aboard/cli"
)

func newRootCmd() *cobra.Command {
    root := &cobra.Command{Use: "ape"}
    // ... the host's own commands ...
    root.AddCommand(cli.NewRootCmd(cli.Options{Host: aboard.HostApe}))
    return root
}
```

That is the whole integration. The mounted tree carries its own `--cwd`, its own
subcommands and its own help; the host owns everything else.

## What `Options` carries

Everything in `Options` is something the engine must **not** go and find out on its own:

| field                     | what it is                                                                                     |
| ------------------------- | ------------------------------------------------------------------------------------------------ |
| `Host`                    | `aboard.HostApe` when ape is serving, `aboard.HostStandalone` (the zero value) otherwise.      |
| `Argv0`                   | The command the user actually typed, for error messages and the instance record.                |
| `Logger`                  | Where the server's operational output goes. `nil` means the standard logger.                    |
| `Stdin`, `Stdout`, `Stderr` | The streams the client-side commands read and write. `nil` means the process's own.           |

`Options` is aliased between `cli` and `aboard`, so there is one shape and no conversion
to keep in step.

**Identity is injected, never sniffed.** The engine does not read `os.Args`, because the
process name belongs to the host: a tree that guessed its own name from `os.Args[0]`
would call itself `ape` and print advice nobody can follow.

## What the engine promises a host

These are constraints the engine holds itself to, and each one is something a host
cannot recover from:

- **No `os.Exit` outside `cli.Execute` and `main`.** A library that exits takes the host's process down mid-flight, deferred cleanup included. Command bodies return typed errors; `Execute` maps them to a status.
- **No `flag.Parse`, no package-level cobra variables, no `init()` that registers commands.** A host owns its own flag set, package-level command state cannot be mounted twice, and an `init()` would register commands whether or not the host wanted them.
- **No global logger surgery.** `log.SetFlags` / `log.SetOutput` are the host's call; server logging goes through `Options.Logger`.
- **No generated `completion` subcommand.** Shell completion belongs to the host's root.
- **Web assets arrive as an `fs.FS`**, not as an assumption about the filesystem — the same seam that makes `serve --dev` work.

If you are extending aboard, those are the rules to keep. They are stated in
`pkg/aboard/aboard.go` beside the code that honours them.

## One `.aboard/` per project

The hosted tree and the standalone binary **share everything that matters**: the same
upward walk for `.aboard/`, the same state file, the same derived port, the same
instance record. A board started by `ape aboard serve` is the board `aboard status`
reports, and either binary can write to it.

What differs is one string. `/health` and the instance file carry:

```json
{ "app": "ape-aboard", "host": "ape-aboard", "argv0": "ape", "...": "..." }
```

where the standalone binary writes `"aboard"`. Clients accept either, so discovery does
not care — but an error message can name the command you actually have, which is the
whole reason the two are distinguished rather than merged.

The **capability manifest's** app name is neither of those: it is the board's own name,
because the manifest describes the board and not the process serving it. That is what
keeps `capsHash` identical under both hosts, and it is worth checking after you mount:

```bash
aboard capabilities
ape aboard capabilities
```

The `capsHash` in both outputs must match. If it does not, something host-specific has
leaked into the manifest — see
[why two identities](../explanation/why-two-identities.md) for why that matters more
than it looks.

## Check the mount

```bash
ape aboard --help          # the board's own help, under the host's name
ape aboard init --example
ape aboard serve
ape aboard status          # the same URL a bare `aboard status` reports
```

If the host has its own `--cwd`-like flag, note that the mounted tree carries its own on
its own root; they do not collide, but do not try to make one drive the other.

## See also

- [Why two identities](../explanation/why-two-identities.md) — what must stay identical and why.
- [The `.aboard/` layout](../reference/layout.md) — the paths both hosts resolve.
- [The capability manifest](../reference/capabilities.md) — including the declared command table the mounted tree is checked against.
