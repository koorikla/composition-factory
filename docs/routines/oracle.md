# Oracle routines (Claude, cloud)

Two routines, both claude-opus-5 in the Anthropic cloud with a fresh checkout of this
repository and no MCP connectors (manage at https://claude.ai/code/routines):

| routine | schedule (UTC) | scope | cap |
|---|---|---|---|
| deep — `trig_015UP4NixEFmvaqUX96PTvZR` | `0 2 * * *` (05:00 Tallinn) | verify last 24 h of closes, two missions + one extra area, lint/lint-strict/test-short/govulncheck, drift spot-check | 2.5 h, ≤12 issues |
| light — see routine list | `0 6,10,14,18,22 * * *` | verify closes of the last 5 h, lint/lint-strict/test-short, one mission to Generate | 75 min, ≤6 issues |

Together they guarantee an oracle pass at least every 4 hours, so a half-fix merged by the
hourly overnight driver is caught within one cycle. The deep run is described below; the
light run is the same contract with the smaller scope in the table.

It re-verifies issues closed in the previous 24 h against real inputs, runs two rotating
canvas missions (`.claude/skills/canvas-ux-tester/missions.md`, day-of-month % 3) with a
scripted browser under the naive-eye rule, runs `make lint`, `make lint-strict`, the short Go
suite and `govulncheck`, and files at most 12 issues per run with `severity:` and `scale:`
labels, deduplicating against open and closed issues. It never fixes code, never closes
issues, never adds `verified` or `in-progress`, never pushes to `main`; its reports land on a
`routine/<date>` branch as a PR titled `docs: routine run <date>`.

The driver routine (`issue-driver.md`) runs later the same day and resolves what the oracle
filed. Docker is absent in the cloud environment, so Validate reads "unavailable" there; that
is environment, not a finding (CF-093).

The full prompt lives in the routine itself; keep this file in step with it when either changes.
