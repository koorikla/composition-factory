# Oracle routine (Claude, cloud)

Daily at 05:00 Europe/Tallinn (`0 2 * * *` UTC), model claude-opus-5, environment: Anthropic
cloud with a fresh checkout of this repository, no MCP connectors. Routine id
`trig_015UP4NixEFmvaqUX96PTvZR` (manage at https://claude.ai/code/routines).

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
