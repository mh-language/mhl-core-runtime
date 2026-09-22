# Security Policy

## Supported versions

**mhl is currently in beta** (`v1.4.0-beta.x`) — there is no stable release
yet. Security fixes land on the newest beta; older betas are not patched.
Once a stable `v1.x` line ships, this section will list which minor versions
receive security fixes and for how long.

| Version              | Supported          |
| --------------------- | ------------------ |
| latest `v1.4.0-beta.*` | :white_check_mark: |
| older betas            | :x:                 |

## Reporting a vulnerability

Please report suspected vulnerabilities **privately**, using GitHub's
[private vulnerability reporting](https://github.com/mh-language/mhl-core-runtime/security/advisories/new)
for this repository (Security tab → "Report a vulnerability"). Do not open a
public issue for a suspected vulnerability.

We aim to acknowledge a report within 5 business days and to share a
remediation plan or timeline once the report is triaged. If a report is
declined (e.g. not reproducible, out of scope, or working as intended),
we'll explain why.

## Scope and known limitations

A few things worth knowing before relying on mhl for anything security-
sensitive, while it is still in beta:

- **Extension processes are not sandboxed.** An extension's manifest may
  declare `permissions.secrets`, `network`, `filesystem`, and `subprocess`.
  Only `secrets` is actually enforced by the host — `network`, `filesystem`,
  and `subprocess` are advisory only, and a running extension has the full
  privileges of the host process. Isolate an untrusted extension yourself
  (a container, an unprivileged user, or similar) rather than relying on
  these fields as a sandbox boundary. See `Docs-Extensions.dc.html` for
  details.
- **Multi-replica coordination (`mhl serve mcp --http` without
  `--single-replica`) is still hardening.** Treat a single-writer deployment
  (`--single-replica`) as the well-tested default; the distributed run-lock
  path is newer and less battle-tested under real contention/latency.
- Release artifacts are checksummed (`checksums.txt` in each GitHub Release)
  but not yet signed or accompanied by an SBOM — verify the checksum against
  the release page before trusting a downloaded binary.
