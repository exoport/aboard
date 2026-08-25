# How to verify a release artifact

Every tagged release of `aboard` is signed with
[Cosign](https://docs.sigstore.dev/cosign/overview/) using GitHub Actions OIDC +
Sigstore Fulcio (keyless). The signature attests that the release was built and uploaded
by this repository's `release.yml` workflow on the corresponding tag.

You don't need to verify to use aboard — install via the
[release archive](install.md) and you get the same binary. But if you ship aboard to
others, into CI, or into a regulated environment, a verify step closes the supply-chain
gap: even if GitHub itself were compromised and a tampered archive substituted, the
signature on the checksums file would not match.

## What gets signed

Each release publishes:

- `aboard_<os>_<arch>.tar.gz` (or `.zip` on Windows) — the binary archive. The whole browser UI is embedded in the binary, so the archive is the binary plus its licence and NOTICE.
- `aboard_checksums.txt` — SHA-256 of every archive.
- **`aboard_checksums.txt.bundle`** — a Sigstore bundle over the checksums file: the short-lived Fulcio certificate, the signature, the certificate-transparency SCT, and the Rekor inclusion proof, all in one file. Verifiable **fully offline** against the Sigstore public-good trusted root.

The pattern matches kubectl, the gh CLI, and most goreleaser projects: sign the
checksums file, then verify each archive's hash against the signed file. One signature
covers the whole release.

## Prerequisites

```bash
# Install cosign (Linux/macOS — see https://docs.sigstore.dev/cosign/installation/ for other platforms).
curl -fsSL https://github.com/sigstore/cosign/releases/latest/download/cosign-linux-amd64 -o /tmp/cosign
sudo install -m 0755 /tmp/cosign /usr/local/bin/cosign
cosign version
```

## Verify a release

```bash
VERSION=v0.1.0
ASSET=aboard_linux_amd64.tar.gz
BASE="https://github.com/exoport/aboard/releases/download/${VERSION}"

# 1. Fetch the archive, the checksums file, and the signature bundle.
curl -fsSL -o "${ASSET}"                   "${BASE}/${ASSET}"
curl -fsSL -o aboard_checksums.txt         "${BASE}/aboard_checksums.txt"
curl -fsSL -o aboard_checksums.txt.bundle  "${BASE}/aboard_checksums.txt.bundle"

# 2. Verify the signature bundle on the checksums file.
cosign verify-blob \
  --bundle aboard_checksums.txt.bundle \
  --new-bundle-format \
  --certificate-identity-regexp \
    "^https://github\.com/exoport/aboard/\.github/workflows/release\.yml@refs/tags/v.*$" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  aboard_checksums.txt

# 3. Verify the archive's SHA-256 against the (now-trusted) checksums file.
sha256sum -c aboard_checksums.txt --ignore-missing
```

If both steps print `Verified OK` and `<asset>: OK`, the binary is authentic.

To pin one exact release rather than any tag, replace the regexp with the literal
identity:

```bash
  --certificate-identity \
    "https://github.com/exoport/aboard/.github/workflows/release.yml@refs/tags/v0.1.0"
```

## What the verify command checks

- **`--certificate-identity` / `--certificate-identity-regexp`** — pins the signer to this repo's `release.yml` workflow on a `v*` tag. A signature minted from any other workflow, a fork, or a different repo is rejected.
- **`--certificate-oidc-issuer`** — pins the OIDC issuer to GitHub Actions. A signature minted with a different issuer (a developer's personal Sigstore login, say) is rejected.
- **Rekor transparency log** — `cosign verify-blob` also checks that the signature was logged to Rekor at sign time. A signature created off-log will not verify.

## Verifying a `go install` build

There is nothing to verify: `go install` builds from source you can read, and the Go
module proxy's checksum database covers the module contents. What it does not give you
is a release identity — such a binary reports `Version=dev` from `aboard version`. If
you need a named, signed artifact, take the release archive.

## Why the local snapshot build is unsigned

`make snapshot` passes `--skip=sign` on purpose. Keyless signing exchanges an ambient
OIDC token for a short-lived Fulcio certificate, and a GitHub Actions runner has one
while your laptop does not — locally cosign would fall back to an interactive device
flow, which is not something a build should do. Snapshots exist to check the archive
layout; real signatures come from the release workflow.

## See also

- [How to install aboard](install.md) — the install paths this verifies.
