# Security Policy

## Supported Versions

Only the latest release is actively maintained and receives security updates.

| Version | Supported |
|---|---|
| Latest release | ✅ |
| Older releases | ❌ |

## What this project handles

myDashboard can start and stop containers, so the security-relevant surface is
small but real:

| Asset | Where it lives | Handling |
|---|---|---|
| Docker daemon access | `docker.sock`, mounted read-only into the gateway container only | Gateway exposes four allowlisted operations; every action is re-checked against the manifest's `"control": "power"` entries |
| Container metadata | Gateway responses | Reduced to id, names, image, state, status and the Compose project label; all other labels (which may hold `basicauth` hashes or tokens) are dropped |
| Service inventory | The manifest, mounted read-only | Same-origin icons only, so the inventory is never leaked to a remote image host |
| Error details | Server logs | Docker and network errors are logged but never returned verbatim to the browser |

Both containers run non-root on a read-only root filesystem with all Linux
capabilities dropped and `no-new-privileges`. The gateway has no network path
to anything except the dashboard.

## Known hardening gaps

These are deliberate, documented trade-offs rather than oversights.

**The dashboard has no login.** It is designed to sit behind a VPN, tailnet or
reverse-proxy allowlist. The same-origin and `X-MyDashboard-Action` checks stop
cross-site requests from a victim's browser, but not a client that can reach the
dashboard directly. Never publish it on the open internet.

**The gateway needs group 0.** `docker.sock` is typically `root:root 0660`, so
the gateway runs as uid 65532 with gid 0. The `:ro` bind protects the socket
inode, not the API — the route allowlist is the boundary.

## Reporting a Vulnerability

Please **do not** open a public GitHub issue for security vulnerabilities.

Report security issues by opening a
[GitHub private security advisory](https://github.com/patbaumgartner/my-dashboard/security/advisories/new).
Include:

- A clear description of the vulnerability
- Steps to reproduce
- Potential impact
- Any suggested mitigations

You can expect an initial response within a few days. Once assessed, a fix will
be released promptly and the advisory will be published.
