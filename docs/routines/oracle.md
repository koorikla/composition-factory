# Oracle routines (Claude, cloud)

Two routines, both claude-opus-5 in the Anthropic cloud with a fresh checkout of this
repository and no MCP connectors beyond the built-in GitHub one (manage at
https://claude.ai/code/routines):

| routine | schedule (UTC) | Tallinn | scope | cap |
|---|---|---|---|---|
| deep — `trig_015UP4NixEFmvaqUX96PTvZR` | `29 */7 * * *` → 00:29, 07:29, 14:29, 21:29 | 03:29, 10:29, 17:29, 00:29 | closes of the last 8 h, two missions + one extra area, all gates + govulncheck, Dependabot PRs, drift spot-check | 2.5 h, ≤12 issues |
| light — `trig_01KVajeVJRv3MLTd3ed7KrQv` | `0 4,11,18 * * *` | 07:00, 14:00, 21:00 | closes of the last 4 h, lint/lint-strict/test-short, one mission to Generate | 75 min, ≤6 issues |

Passes land at 00:29, 04:00, 07:29, 11:00, 14:29, 18:00, 21:29 UTC: never more than 3.5 h
apart, never two at once (a deep run ends by :59+2 h, the next light starts ≥1 h later), and
any five-hour window holds at most one deep and one light run. Mission rotation is per run
slot (`(day*4 + hour/7) % 3`), so the four daily deep runs cover all three missions.

## Constraints learned on 2026-09-11

- **Quota.** Cloud runs draw on the owner's five-hour Claude session limit, shared with
  interactive sessions. Every run on the first day died within seconds on
  `rate_limit: rejected (five_hour)` because an interactive session had spent the window.
  The timetable above spaces runs for that; heavy interactive work in the half hour before a
  deep run means the routine may exit at once — that shows in the run log, not as a PR.
- **Egress.** The cloud environment denies the ghcr.io blob CDN
  (`pkg-containers.githubusercontent.com`), so `cf provider add` cannot fetch schemas there.
  Missions run only as far as native kinds until a seeded schema cache is checked in
  (CF-183, #68). Package-fetching tests and `make lint-strict`'s toolchain download fail for the
  same reason; the prompts require a comparison with `main`'s CI before any gate is called red.
- **Browser.** `npx playwright install` cannot download; the preinstalled Chromium under
  `/opt/pw-browsers/` is used via `executablePath`. Docker is absent, so Validate reads
  "unavailable" (CF-093) and missions stop at Generate.

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

Drivers (`issue-driver.md`) run around the clock and resolve what the oracle filed. Docker is absent in the cloud environment, so Validate reads "unavailable" there; that
is environment, not a finding (CF-093).

The full prompt lives in the routine itself; keep this file in step with it when either changes.
