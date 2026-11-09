# Contributing to myDashboard

Thank you for taking the time to contribute! This guide will help you get up and
running quickly and explains the conventions used in this project.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Ways to contribute](#ways-to-contribute)
- [Getting started](#getting-started)
- [Development workflow](#development-workflow)
- [Commit messages](#commit-messages)
- [Pull request guidelines](#pull-request-guidelines)
- [Project conventions](#project-conventions)
- [Releasing](#releasing)

---

## Code of Conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md). By
participating you agree to abide by its terms. Please report unacceptable
behaviour to the maintainer.

---

## Ways to contribute

- **Bug reports** — found something broken? Open an issue using the
  [Bug Report](.github/ISSUE_TEMPLATE/bug_report.yml) template.
- **Feature requests** — have an idea? Use the
  [Feature Request](.github/ISSUE_TEMPLATE/feature_request.yml) template.
- **Code contributions** — fix a bug, implement a feature, improve tests or
  documentation. See the workflow below.
- **Documentation** — improve the README, add examples, fix typos.

Security issues go through a private advisory, never a public issue — see
[SECURITY.md](SECURITY.md).

---

## Getting started

### 1. Fork and clone

```bash
# Fork the repo on GitHub, then:
git clone https://github.com/<your-username>/my-dashboard.git
cd my-dashboard
```

### 2. Install the toolchains

- Go — the version in [`go.mod`](go.mod)
- Node.js 24 with npm
- Docker with Compose v2 (for image builds and end-to-end checks)

```bash
npm ci
go mod download
```

### 3. Run it locally

See [Local development](README.md#local-development) in the README. The
example manifest in `config/services.example.json` works out of the box.

---

## Development workflow

```bash
# Create a feature branch from main
git checkout -b feature/my-change

# Make your changes, then run the same gates CI runs:

# Go
gofmt -l .            # must print nothing
go vet ./...
go test -race ./...

# Frontend
npm run check
npm run build
npm audit --audit-level=high

# Image (also runs the checks above inside the build)
docker build -t my-dashboard:dev .

# Commit and push
git commit -m "feat(gate): explain why a service is still starting"
git push origin feature/my-change

# Open a Pull Request on GitHub
```

For UI changes, check the dashboard in light and dark mode on a desktop and a
narrow mobile viewport, and include before/after screenshots in the PR.

---

## Commit messages

We use [Conventional Commits](https://www.conventionalcommits.org/). A commit
message should look like:

```
<type>(<scope>): <short description>

[optional body]

[optional footer(s)]
```

| Type | When to use |
|---|---|
| `feat` | A new feature |
| `fix` | A bug fix |
| `docs` | Documentation only |
| `style` | Formatting, no logic change |
| `refactor` | Code change without feature or fix |
| `test` | Adding or improving tests |
| `chore` | Build process, dependency updates |
| `ci` | CI / GitHub Actions changes |

**Examples:**

```
feat(manifest): support per-service startup hints
fix(gate): do not redirect while the route still returns 404
docs: document the extra_hosts workaround for Tailscale
test(gateway): reject containers outside the manifest
ci: publish arm64 images
```

---

## Pull request guidelines

1. **One PR per concern** — keep PRs small and focused.
2. **Tests required** — new features and bug fixes must include Go tests where
   the backend changes. Keep `go test -race` green.
3. **All CI checks must pass** — formatting, vet, tests, `svelte-check`, the
   Vite build, `npm audit` and the Docker smoke test.
4. **Update documentation** — if your change affects user-facing behaviour or
   the manifest format, update `README.md` and
   `config/services.example.json`.
5. **Describe what and why** — fill in the PR template so reviewers have context.

---

## Project conventions

### Go (`cmd/`, `internal/`)

| Convention | Tool |
|---|---|
| Formatting | `gofmt` |
| Static analysis | `go vet` |
| Tests | Standard library `testing`, run with `-race` |
| Dependencies | Standard library only — think twice before adding a module |
| Configuration | `MYDASHBOARD_*` environment variables plus the JSON manifest |

### Frontend (`src/`)

| Convention | Tool |
|---|---|
| Framework | Svelte 5 + TypeScript, built with Vite |
| Type checking | `svelte-check` and `tsc` via `npm run check` |
| Icons | [Lucide](https://lucide.dev) via `@lucide/svelte` |
| Assets | Same-origin only — the Content Security Policy blocks remote images, scripts and fonts |
| Accessibility | Keyboard reachable, visible focus, `prefers-reduced-motion` respected |

### Security boundaries

- Only the gateway may mount `docker.sock`. Never add Docker access to the web
  process.
- The gateway allowlist (list, start, stop, restart) is the security boundary.
  New Docker operations need an issue and a design discussion first.
- Never add credentials or secrets to the Dockerfile, Compose files or the
  example manifest.

### Docker

- `docker-compose.yml` — **production**, pulls the image from GHCR.
- `docker-compose.build.yml` — **override** that builds the image from source.
- Compose reads `.env` for *variable substitution only*. A variable a user is
  told to set in `.env` must also be wired into the service definition.

### Branch naming

| Prefix | Purpose |
|---|---|
| `feature/` | New functionality |
| `fix/` | Bug fixes |
| `docs/` | Documentation only |
| `ci/` | CI / GitHub Actions |
| `deps/` | Dependency upgrades |

---

## Releasing

Maintainers only:

1. Bump `version` in `package.json` (and `package-lock.json` via
   `npm version <x.y.z> --no-git-tag-version`) and merge to `main`.
2. Tag the merge commit: `git tag -s vX.Y.Z -m "vX.Y.Z" && git push origin vX.Y.Z`.
3. The release workflow verifies the code, pushes
   `ghcr.io/patbaumgartner/my-dashboard:X.Y.Z` (plus `X.Y` and `latest` for final
   releases) and publishes the GitHub release.

---

Thank you for contributing! ❤️
