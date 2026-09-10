# CF-135 — GitHub Actions workflows are not hardened

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P3 |
| **Closes** | `#9` — `CF-135 — GitHub Actions workflows are not hardened: every uses: is a floating tag (actions/checkout@v7 …), contents: write is granted at workflow level in ci.yml and catalogue.yml, actions/checkout keeps credentials persisted, ci.yml:29 pipes curl … install.sh | sh for the crossplane CLI, release/catalogue jobs have no timeout-minutes, and function images are pulled by tag not digest.` |
| **Worktree** | `.worktrees/CF-135` on branch `CF-135-harden-workflows` |
| **May write** | `.github/workflows/ci.yml`, `.github/workflows/catalogue.yml`, `.github/workflows/release.yml`, `Makefile` |
| **Merges after** | `nothing` |

## Symptom

GitHub Actions workflows violate security best practices: floating tags for action dependencies, broad workflow-level write permissions, unverified curl-to-sh script execution for crossplane CLI, unpinned function images, missing job timeouts, and `actions/checkout` persisting GitHub credentials.

## Evidence

Identified by the skylos security audit in `docs/research/2026-09-10-skylos-trial.md`:
1. Floating action tags (`actions/checkout@v7`, `actions/setup-go@v7`, `actions/setup-node@v7`, `azure/setup-helm@v5`, `docker/build-push-action@v7`, etc.) instead of pinned full commit SHAs with version comments.
2. Top-level `permissions: write` in `catalogue.yml` and `release.yml`.
3. `actions/checkout` does not specify `with: persist-credentials: false` on jobs that do not push.
4. `ci.yml` line 29: pipes `curl -sL https://raw.githubusercontent.com/crossplane/crossplane/main/install.sh | sh` directly without hash or version verification.
5. No `timeout-minutes` on jobs in `ci.yml`, `catalogue.yml`, and `release.yml`.

## Contract

1. In `.github/workflows/*.yml`, pin each external GitHub Action to its full commit SHA with an inline version comment:
   - `actions/checkout`
   - `actions/setup-go`
   - `actions/setup-node`
   - `actions/upload-artifact`
   - `actions/download-artifact`
   - `docker/setup-buildx-action`
   - `docker/login-action`
   - `docker/metadata-action`
   - `docker/build-push-action`
   - `azure/setup-helm`
2. Restrict permissions to read-only (`contents: read`) at the workflow level; grant write permissions only to specific jobs that require them (`packages: write`, `contents: write`, `pull-requests: write`).
3. Set `persist-credentials: false` on `actions/checkout` across all jobs except where git push is explicitly needed.
4. In `ci.yml`, replace unpinned curl-to-sh crossplane install with pinned release download (e.g. download specific release tarball from github.com/crossplane/crossplane/releases/download/v1.18.0/... or verify checksum).
5. Add explicit `timeout-minutes` to every job in `ci.yml`, `catalogue.yml`, and `release.yml` (e.g. 10–15 minutes).
6. Ensure `make lint` and `make lint-strict` pass cleanly.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

Modifying application code or build outputs.

## Handover

Branch `CF-135-harden-workflows`, committed, not pushed, not merged. In your final report: list changes made to each workflow file and output of `make lint` and `make lint-strict`.
