# Why two identities

The same board can be served by two different binaries: the standalone `aboard`, and the
same cobra tree mounted inside ape as `ape aboard`. They share **one `.aboard/` per
project** — the same state file, the same derived port, the same instance record — and
they are distinguishable by exactly one string.

| where                                | standalone | hosted by ape |
| ------------------------------------ | ---------- | ------------- |
| `/health` and `instance.json` `app`  | `aboard`   | `ape-aboard`  |
| the capability manifest's app name   | the board's own name, in both cases         ||
| `capsHash`                           | identical in both                            ||
| state file, port, journal, uploads   | identical in both                            ||

## Why distinguish them at all

The obvious design is one identity: the board is the board, who cares which binary is in
front of it. It was rejected for a small, concrete reason.

**The answer to "how do I talk to this thing" differs.** One is `aboard <command>`, the
other is `ape aboard <command>`. A client that finds a board on a port and wants to tell
the user how to reach it — or an error message that wants to name the command to run —
has to know which. With a single identity, every such message would be a guess, and a
guess in an error message is worse than silence: it sends the reader off to run something
that does not exist on their machine.

So the identity is carried in the two places a client can find it — the `/health`
response and the instance file — and `probeBoard` accepts either, so **discovery does not
care**. Only the prose does.

## Why the manifest's name is neither

The capability manifest describes **the board**, not the process serving it. Its app name
is the board's own, under both hosts.

That is not tidiness; it is what keeps `capsHash` host-independent. The hash fingerprints
the described surface, and a project's committed skill copy is stamped with the hash it
was generated for. If the host leaked into the manifest, then the same skill would read
"current" under one binary and "stale" under the other, in the same project, on the same
day — and the staleness warning would become noise. A warning that fires for a reason the
reader cannot act on is a warning they learn to skip, and that is the warning channel
where the real drift arrives.

It is worth checking after any change to the manifest:

```bash
aboard capabilities
ape aboard capabilities
```

The `capsHash` in both must match.

## Why identity is injected, never sniffed

The engine does not read `os.Args`. The host passes its identity in through `Options`, and
the process name belongs to the host.

A tree that sniffed `os.Args[0]` would work perfectly in the standalone binary and be
wrong everywhere else: mounted in ape it would call itself `ape` — or, worse, whatever the
binary happened to be renamed to — and print advice nobody can follow. Sniffing is the
kind of shortcut that is correct in exactly the configuration you tested it in.

The same rule produces the rest of the embedding constraints: no `os.Exit` outside the
entry point, no `flag.Parse`, no package-level cobra state, no global logger surgery. Each
is something a host cannot recover from, and each is a thing the standalone binary would
never notice. They are listed in [How to embed aboard in ape](../how-to/embed-in-ape.md).

## One board, two front doors

The sharing is the feature. A developer running ape can start a board with
`ape aboard serve` and a Claude Code session in the same project can drive it with
`aboard apply` without knowing or caring which binary is listening. Root discovery is the
same upward walk, the port is derived from the same root, and the instance file is written
to the same path.

That is why the identity difference has to stay **cosmetic**. The moment the two hosts
disagreed about a path, a port, or a schema, "shared" would become "two boards that look
like one" — the failure the single-resolved-root rule exists to prevent.

## See also

- [How to embed aboard in ape](../how-to/embed-in-ape.md) — the mount, and the constraints that make it possible.
- [The `.aboard/` layout](../reference/layout.md) — the paths and the instance record both hosts resolve.
- [The capability manifest](../reference/capabilities.md) — `capsHash`, and what it is a fingerprint of.
