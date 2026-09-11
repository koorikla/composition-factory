# Continuous drivers — design

**Status:** approved 2026-09-11. Supersedes the single-driver model in
`docs/routines/issue-driver.md` and the One-Driver Rule in `AGENTS.md` §4.

## Problem

Antigravity runs the issue driver from ten daily scheduled tasks (04, 05, 06, 07, 09, 11,
14, 16, 18, 20 local), each with a 5-hour budget. Up to five drivers are alive at once,
each permitted six subagents. The rules they follow assume exactly one driver:

- **Drivers collide.** On 2026-09-11 the 00:14Z driver locked six issues; a second
  instance ignored the locks and landed all six itself
  (`docs/comp-runs/2026-09-11-driver-run-0014z.md`). The 02:07Z report records branches
  landed by another driver in the middle of its own wave.
- **Merges cannot be attributed.** "Never merge two branches without a CI run between them"
  is enforceable within one driver, not across five.
- **Locks go stale.** A driver killed by a quota error leaves `in-progress` on issues
  nobody works, and the routine says nothing about what to do with an old lock.
- **Work dies with the driver.** Subagents do not push, and handover reports live only in
  the dispatching driver's context.
- **The budget table is shaped by a quota that no longer applies.** Drivers run on
  Gemini, not on the Anthropic 5-hour session quota the table was written around.
- **Coverage has a hole.** The 20:00 shift ends at 01:00; nothing starts before 04:00.
- **Every subagent's gates run on one machine** (12 cores, 6 performance, 18 GiB), where
  CPU contention is the known cause of e2e flakes.

The fixed caps (six subagents, six issues per run) are both too low for the queue and
the wrong control: they bound each driver while the schedule multiplies drivers.

## Decision

Many concurrent drivers are the model. **GitHub issues are the only shared state.**
Three machine-local kernel locks serialize the three operations that must not race.
Concurrency is bounded by one global number read from GitHub, not by per-driver caps.

Alternatives rejected: raising the per-driver numbers (leaves every failure above in
place), and one long-lived supervisor (one crash stops all throughput, and it does not
fit how Antigravity schedules work).

## 1. Issue lifecycle

```
open ──claim──▶ in-progress ──subagent green──▶ handed-back ──land, CI green──▶ closed
                    │                                │
                    └── cap reached / gates red ─────┴── rebase conflict / CI red ──▶ parked
parked ──claim──▶ in-progress   (resumes the pushed branch)
```

New labels: `handed-back`, `parked`. Existing: `in-progress`.

**A claim** is the `in-progress` label plus one comment, machine-parseable on its first line:

```
taking — CF-173-dedupe-utils · driver 06-0300Z · lease until 2026-09-11T04:45Z · files: web-proto/js/util.js web-proto/js/regions/palette.js
```

- **Lease** = claim time + 120 min (subagent cap 90 min, plus slack). Never renewed.
- **Driver id** = `<scheduled task name>-<T0 as HHMMZ>`.
- **Files** = the brief's `May write` set, or the paths named in the issue body plus the
  test file the subagent will add.
- **Legacy locks.** An `in-progress` issue with no parseable claim line (claimed under the
  old routine) is leased until its newest `taking —` comment time + 120 min, or, with no
  such comment, the time the `in-progress` label was applied + 120 min. It has no file
  set, so it cannot participate in the overlap check; see §10.

**Takeover.** An `in-progress` issue whose newest claim lease has expired is claimable by
any driver. If `origin/CF-NNN-*` exists, the new claim resumes that branch; otherwise it
starts fresh. The claim comment records `takeover of <old driver id>`.

**Overlap.** A claim is refused if its file set intersects the file set of any unexpired
claim on any issue. `internal/emit/composition.go` and
`web-proto/js/regions/inspector.js` remain the usual collision points; the rule needs no
special case for them.

## 2. Subagent duties

Changes to `docs/task-execution-contract.md`:

- **Push your own topic branch** `CF-NNN-<slug>` after every green commit. After a rebase,
  `git push --force-with-lease` to that branch only. Never push `main`, never push
  another branch.
- **Hand back on the issue.** When done: post the handover report (failing run, passing
  run, gates, judgement calls, noticed-not-fixed) as a comment on your own issue, then
  replace `in-progress` with `handed-back`.
- **Park on the issue.** At the cap or with gates red: push what exists, post the exact
  state (which test fails, what is left), replace `in-progress` with `parked`.
- Unchanged: no merge, no close, no edits to other issues, no AI attribution, test first.

Subagents have network access and `gh` authenticated as `koorikla`.

## 3. Machine-local locks

`scripts/driver/lock.sh <pool> <slots> -- <cmd> [args…]`

- Lock files: `$(git rev-parse --git-common-dir)/cf-locks/<pool>.<n>`, shared by every
  worktree of the clone.
- Acquisition: try `lockf -s -t 0` on slots `1..slots`; when none is free, sleep 10 s and
  retry, printing `waiting for <pool>` once. The command runs as the lock holder's child;
  the kernel releases the lock when that process exits, including on `kill -9`. There is
  no stale-lock state and no expiry logic.
- On exit, prints `<pool> wait <seconds>s` to stderr so reports can record contention.
- Where `lockf` is absent (Linux CI) or `CF_GATE_SLOTS=off`, it runs the command directly.

| Pool | Slots | Holder | Held for |
|---|---|---|---|
| `claim` | 1 | `scripts/driver/claim.sh` | seconds |
| `merge` | 1 | `scripts/driver/land.sh` | one landing, ≈ 5–6 min |
| `gate` | measured (§6) | `make test-race`, `make test-e2e`, `make test-docker` | one gate run |

The `Makefile` wraps the three heavy targets in the `gate` pool, so the pool is enforced
by construction rather than by instruction. `make lint` and `make lint-strict` are not
wrapped.

## 4. Critical sections as scripts

A driver issues many separate shell commands; a kernel lock survives only one process.
Each operation that must not race is therefore a single script run under its lock.

### `scripts/driver/claim.sh <issue> <branch> <driver-id> <file>…`

Under `claim`: read the issue's labels and comments; refuse if it is `closed`, `wontfix`,
or `in-progress` with an unexpired lease; collect file sets from every unexpired claim on
every open `in-progress` issue; refuse on intersection; otherwise add `in-progress`,
remove `parked` if present, post the claim comment. Exit 0 `CLAIMED`, 3 `TAKEN`,
4 `OVERLAP <issue>`.

### `scripts/driver/land.sh <issue>`

Under `merge`:

1. Re-read labels; exit 5 `NOT-HANDED-BACK` unless `handed-back`.
2. Fetch; create a scratch worktree at `.worktrees/land-CF-NNN`; rebase the topic branch
   onto `origin/main`. Conflict → abort, remove the scratch worktree, exit 6 `PARKED`.
3. Fast gates: `make lint`, `make lint-strict`, `go test -short` over the packages the
   branch touches.
   Red → exit 6 `PARKED`.
4. Squash the rebased branch into one commit on `origin/main`, subject = the branch's
   newest commit subject with ` (CF-NNN, #n)` appended unless already present, body = the
   branch's commit bodies in order. `main` history is linear, one commit per issue; keep it
   so. Push `main`.
5. `gh run watch <id> --exit-status`. If the only failed job is `e2e`, rerun failed jobs
   once and watch again.
6. Red → `git revert` the landed commit, push, watch that run to green, exit 7
   `REVERTED <run-url>`. The topic branch is kept for the next attempt.
7. Green → delete the remote topic branch, exit 0 `LANDED <sha> <run-url>`.

The driver, outside the lock, then closes the issue (exit 0), labels it `parked` with a
comment (6), or labels it `parked` with the CI link (7), and removes the topic
worktree.

**Risk accepted:** full `test-race`, e2e and docker suites are not rerun locally after
the rebase; CI runs all five jobs in 3–4 min and the lock is held until `main` is green.
A red `main` exists for at most one CI run plus one revert, and nobody merges on top of it.

## 5. The driver loop

Each scheduled task is a **shift** of at most 5 h. Record T0 and driver id at start.
Every ~5 min until the shift ends:

1. **Land** every `handed-back` issue, oldest first, one `land.sh` at a time.
2. **Resume** `parked` issues: claim and dispatch against the existing branch.
3. **Take over** `in-progress` issues with an expired lease.
4. **Dispatch** new work by severity (`P0` → `P3`; `brief-ready`, then `verified`, then
   oldest) while live subagents < global cap and a non-overlapping claim succeeds.

Live subagents = open `in-progress` issues with an unexpired lease, across all drivers.

| Checkpoint | Rule |
|---|---|
| T0 + 3:00 | last dispatch |
| T0 + 4:30 | last `land.sh` start |
| T0 + 4:45 | post the shift report; exit |

Nothing is stranded at shift end: other live drivers land what this shift dispatched.

**Quota back-off.** A subagent that dies on HTTP 429 with a reset time: its driver posts
the reset time in the Driver log issue, and no driver dispatches until then. Landing
continues.

**Coverage.** Add a 23:00 scheduled task (owner: Kaur, in the Antigravity UI). With it,
some driver is alive at every hour.

## 6. Capacity: measure before setting

Gate capacity, not the model quota, bounds safe concurrency on one machine. Before the
first shift under this design, time `make test-race`, `make test-e2e` and
`make test-docker` on an idle machine and record wall-clock and peak RSS
(`/usr/bin/time -l`). Then:

- `gate` slots = the largest N for which N concurrent heavy gates fit in memory with
  headroom and the e2e suite passes three times in a row at that concurrency.
- Global cap = gate slots × 90 min ÷ (heavy-gate minutes per subagent), rounded down,
  assuming 2.5 heavy gate runs per subagent until reports show otherwise.

Both numbers live in `docs/routines/issue-driver.md` §0 with the measurement that
produced them. Shift reports record gate wait (from `lock.sh`) and e2e flakes; either
rising is the signal to lower the numbers.

## 7. Reports

A pinned issue titled **Driver log** holds one comment per shift: driver id, T0, landed
(issue, sha, CI link), parked (issue, reason), reverted, takeovers, gate wait
p50/max, quota events, residues filed. Reports no longer commit to `main`: eleven shifts
a day would spend eleven merge locks and CI runs on documentation. `docs/comp-runs/`
remains as history and for oracle mission reports.

## 8. Changes

| File | Change |
|---|---|
| `scripts/driver/lock.sh`, `claim.sh`, `land.sh` | new |
| `scripts/driver/test/` | shell tests, run by `make test-driver` |
| `Makefile` | `gate` pool around `test-race`, `test-e2e`, `test-docker`; `test-driver` target |
| `docs/routines/issue-driver.md` | rewritten around §1, §4, §5, §6, §7 |
| `docs/task-execution-contract.md` | §6 push your own branch; §7 handover on the issue |
| `AGENTS.md` §2, §4 | lock pools; One-Driver Rule → one merge at a time, many drivers |
| GitHub | labels `handed-back`, `parked`; pinned issue "Driver log" |

Unchanged: the oracle routines, the brief format, the test-first loop, file-overlap as
the parallelism rule.

## 9. Testing

`make test-driver` runs, macOS-only where `lockf` is required (skips elsewhere):

- `lock.sh`: two holders of a 1-slot pool never overlap; a 2-slot pool admits two; a
  holder killed with `-9` frees its slot within one retry; with `lockf` absent, runs
  the command directly.
- `claim.sh` against a fake `gh` on `PATH` backed by fixture JSON: a clean claim posts the
  exact comment format; an unexpired lease → `TAKEN`; an expired lease → claim with
  `takeover of`; intersecting file sets → `OVERLAP`; `closed` → refused.
- `land.sh` against a local bare repository as `origin` and a fake `gh`: a clean branch →
  `LANDED` and `main` advanced; a conflicting branch → `PARKED` and `main` untouched;
  fake CI red → `REVERTED` and `main` content equal to its pre-merge tree; e2e-only red
  then green on rerun → `LANDED`; an issue not `handed-back` → refused without fetching.

## 10. Rollout

Drivers already running follow the old document until their shift ends. Both versions
treat `in-progress` as a lock, so they do not collide; old drivers do not land branches
other drivers handed back, which delays those by at most one shift. No migration of
existing issues is needed. Legacy locks (§1) carry no file set, so during the window a new
claim cannot be checked for overlap against them; the exposure is one shift, and the worst
outcome is a rebase conflict that `land.sh` parks rather than a bad merge.
