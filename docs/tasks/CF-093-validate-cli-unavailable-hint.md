# CF-093 — Validate fix tip prescribes `curl … | sh` when running in a container

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (UX scale: misleading fix tip prescribing non-actionable in-container command) |
| **Closes** | `CF-093 — In the published image Validate always answers "validation check unavailable" and its fix tip prescribes curl … | sh in a container that has no curl and runs as uid 100.` |
| **Worktree** | `.worktrees/CF-093` on branch `CF-093-validate-cli-unavailable-hint` |
| **May write** | `Dockerfile`, `internal/api/`, `web-proto/`, `tests/` |
| **Merges after** | nothing |

## Symptom

When clicking **Validate** on the canvas in the published Docker image (or containerized deployments), `/api/render` returns:
`{"ok":false,"resources":0,"error":"","unavailable":"crossplane CLI not found on PATH: exec: \"crossplane\": executable file not found in $PATH"}`.

The visual canvas formats this with `💡 Environment Fix Tip: Install Crossplane CLI: curl -sL https://raw.githubusercontent.com/crossplane/crossplane/master/install.sh | sh`.

This advice is not actionable:
1. The container has no `curl` (`which curl` fails).
2. The container runs as unprivileged user `uid=100(cf)` and cannot write to `/usr/local/bin`.
3. The user viewing the canvas is on their host machine or accessing a cluster service, not running a shell inside the container.
4. Composition rendering validation (`crossplane composition render`) requires both the `crossplane` CLI and a Docker daemon / container runtime to execute pipeline functions. Standard container deployments cannot perform this check unless running with a mounted Docker socket.

## Evidence

Documented in `docs/ux-runs/2026-09-09-docker-image-journey.md:37-40`:
```
### P2-6 — Validate's fix tip is not actionable in the published image
Repro: click Validate.
Observed chip validation check unavailable, title/drawer: crossplane CLI not found on PATH: exec: "crossplane": executable file not found in $PATH + 💡 Environment Fix Tip: Install Crossplane CLI: curl -sL https://raw.githubusercontent.com/crossplane/crossplane/master/install.sh | sh. The image has no curl (which curl → exit 1), runs as uid 100, and the user is on the host, not in the container; nothing says "not available in the Docker image".
```

## Acceptance Test

`tests/cf093-validate-cli-unavailable-hint.spec.js`:
- In container deployments, clicking Validate when the Crossplane CLI is unavailable reports "validation check unavailable" and explains that the check is unavailable in container deployments because the container image does not bundle the Crossplane CLI or Docker runtime, recommending running `cf serve` or `cf gen --validate` on the host. It never prescribes `curl ... | sh` into the container.
- In host deployments, clicking Validate when the CLI is missing provides actionable host installation commands (`brew install crossplane-cli` on macOS or `curl -sL https://cli.crossplane.io/stable/current/install.sh | sh`), rather than the unpinned raw master script.

## Contract

1. `GET /api/version` returns `container: true` when executing inside a container (detected via `CF_CONTAINER`, `KUBERNETES_SERVICE_HOST`, `/.dockerenv`, or `/run/.containerenv`).
2. `POST /api/render` returns `container: true` when running inside a container.
3. `Dockerfile` sets `ENV CF_CONTAINER=1` so image-built instances are identified as containerized.
4. In `web-proto/js/regions/output.js`, `diagnoseError`:
   - When in a container deployment, explains that validation check is unavailable in container deployments and why, recommending running on a host with Crossplane CLI and Docker installed.
   - When in a host deployment, provides actionable host installation guidance.
   - Never outputs the raw unpinned `curl -sL https://raw.githubusercontent.com/crossplane/crossplane/master/install.sh | sh` tip.
5. All gates (`make lint`, `make lint-strict`, `make test-race`, `make test-e2e`) pass cleanly.
