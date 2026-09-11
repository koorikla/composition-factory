# Oracle routines (Claude, cloud)

Two routines, both claude-opus-5 in the Anthropic cloud with a fresh checkout of this
repository and no MCP connectors (manage at https://claude.ai/code/routines):

| routine | schedule (UTC) | scope | cap |
|---|---|---|---|
| deep — `trig_015UP4NixEFmvaqUX96PTvZR` | `0 2 * * *` (05:00 Tallinn) | verify last 24 h of closes, two missions + one extra area, lint/lint-strict/test-short/govulncheck, dependabot PR/MR checks, drift spot-check | 2.5 h, ≤12 issues |
| light — see routine list | `0 6,10,14,18,22 * * *` | verify closes of the last 5 h, lint/lint-strict/test-short, one mission to Generate, dependabot PR/MR checks | 75 min, ≤6 issues |

Together they guarantee an oracle pass at least every 4 hours, so a half-fix merged by the
hourly overnight driver is caught within one cycle. The deep run is described below; the
light run is the same contract with the smaller scope in the table.

It re-verifies issues closed in the previous 24 h against real inputs, runs two rotating
canvas missions (`.claude/skills/canvas-ux-tester/missions.md`, day-of-month % 3) with a
scripted browser under the naive-eye rule, runs `make lint`, `make lint-strict`, the short Go
suite and `govulncheck`, audits open Dependabot merge requests / pull requests, and files at
most 12 issues per run with `severity:` and `scale:` labels, deduplicating against open and
closed issues. It never fixes code, never closes issues, never adds `verified` or
`in-progress`, never pushes to `main`; its reports land on a `routine/<date>` branch as a PR
titled `docs: routine run <date>`.

## Dependabot PR/MR checks

Both routines audit open Dependabot pull requests (`gh pr list --search "author:app/dependabot"`):

1. **Status check verification**: Run `gh pr checks <number>` for each open PR.
   - **Passing**: Note in the run report as green and ready for driver integration.
   - **Failing**: Inspect the failed jobs (`acceptance`, `cluster`, `docker-build`, `test`, `e2e`).
     - If the failure is a known intermittent CI flake (e.g. `e2e` canvas drag test flakiness), trigger a rerun (`gh run rerun <id> --failed`) or comment `@dependabot rebase`.
     - If the failure is a genuine regression caused by the bumped version (compilation failure, breaking API change, test or lint failure), investigate the root cause and file a GitHub issue (`severity:P1` or `severity:P2`, `scale:engine`, `verified`) per `.claude/skills/backlog-authoring/SKILL.md` detailing the failure mechanism, repro, and cost so the driver can schedule an issue to handle the bump.
2. **Merge conflicts & staleness**: If mergeable status is `CONFLICTING` or the branch is behind `main`, trigger a rebase by commenting `@dependabot rebase` on the PR.
3. **Vulnerability correlation**: Cross-reference open PRs with `govulncheck` output. If an open Dependabot PR resolves a vulnerability identified in required modules, flag it with high priority in the run report for the driver.
4. **Report inclusion**: The routine PR includes a dedicated `Dependabot PRs` section listing PR numbers, dependency names, old/new versions, CI check states, and recommended driver actions.

The driver routine (`issue-driver.md`) runs later the same day and resolves what the oracle
filed. Docker is absent in the cloud environment, so Validate reads "unavailable" there; that
is environment, not a finding (CF-093).

The full prompt lives in the routine itself; keep this file in step with it when either changes.
