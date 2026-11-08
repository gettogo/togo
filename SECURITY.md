# Security policy

## Reporting a vulnerability

Please report vulnerabilities privately. Do not open a public issue or discussion.

Open a private report with the **Report a vulnerability** button on the repository's [Security tab](https://github.com/gettogo/togo/security/advisories/new). Only the maintainers can see it.

Include what you found, how to reproduce it, and what an attacker gains. You will get an acknowledgement within a few days, and we will agree a disclosure date with you once a fix is ready. We credit reporters in the advisory unless you ask us not to.

## Scope

- The `togo` helper and its daemon (`helper/`)
- The push relay (`relay/`), including the answer mailbox
- The iOS, iPadOS and watchOS app and its extensions (`apps/ios`, `packages/`)
- The install script (`scripts/install.sh`) and `togo update`
- Release signing: the minisign signature over `SHA256SUMS` and the macOS code signature

Bugs in tmux, Mosh, OpenSSH, Apple's platforms or the agents themselves belong upstream, though we want to hear if Togo makes them worse. The threat model in [docs/security.md](docs/security.md) lists the limits we already know about.

## Supported versions

Only the latest release gets security fixes. The app, helper and relay share one version number.

## Verifying a release

Each GitHub release contains the helper archives, `install.sh`, `BUILDINFO.json`, `SHA256SUMS` and `SHA256SUMS.minisig`. `SHA256SUMS` is signed with minisign, and the trusted comment names the version, for example `togo v1.0.0 SHA256SUMS`, so an older signed file cannot pass for a newer release.

The release public key is pinned in the helper source at [helper/internal/release/minisign.pub](helper/internal/release/minisign.pub). The install script and `togo update` carry the same key and refuse to install anything that does not verify.

<!-- TODO(owner): paste the release public key line here once the release key exists; the file above holds a test key until then. -->

To check a download by hand:

```sh
minisign -Vm SHA256SUMS -P <release public key>
sha256sum -c SHA256SUMS --ignore-missing     # on macOS: shasum -a 256 -c SHA256SUMS --ignore-missing
```

Check that the trusted comment minisign prints names the version you downloaded. On macOS the binary is also signed with a Developer ID certificate and notarized.
