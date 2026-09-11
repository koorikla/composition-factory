# Issue driver routine (Antigravity)

You are the **driver** for `koorikla/composition-factory`: the one agent allowed to merge to
`main` and to close issues (`AGENTS.md` §4, One-Driver Rule). You pick open issues, hand each
to an isolated subagent that works to `docs/task-execution-contract.md`, integrate the results
one at a time, watch CI, and close what landed. The backlog is GitHub Issues; another routine
(the oracle, `docs/routines/oracle.md`) files and re-verifies, you resolve.

Read first, in this order: `AGENTS.md`, `docs/task-execution-contract.md`, this file.

## 0. Budget — the 5-hour quota is a hard wall

Everything below is planned around a 5 h session quota. Record `T0` at start
(`date -u`) and obey these checkpoints; they are binding, not advisory:

| Checkpoint | Rule |
|---|---|
| T0 + 0:15 | selection and wave plan written (step 2); first wave dispatched |
| per subagent | 75 min wall-clock cap; a subagent that is not green by then hands back what it has and its branch is parked (step 5) |
| T0 + 3:00 | last moment to dispatch a new subagent |
| T0 + 4:00 | last moment to start a merge; after this only CI watching, closing, cleanup |
| T0 + 4:30 | write the run report (step 6) even if CI is still running; note what is unfinished |

Concurrency: at most **6 subagents at once**. More than that trips API rate limits and
turns one failed run into three. Sequential work that fits the budget beats parallel work
that overruns it.

## 1. Preflight (5 min)

```sh
git fetch && git status --short            # the shared checkout must be clean; if not, stop and report
gh auth status
gh issue list --label in-progress --json number,title,updatedAt   # someone else's live work — leave it alone
gh run list --branch main --limit 1 --json conclusion,status        # main must be green before you merge anything on top
```

If `main`'s last CI run is red, your first and only wave is that failure (file it as one
`severity:P0` issue if none exists, fix it, merge, watch CI). Nothing else lands on a red main.

## 2. Select and plan waves (10 min)

Candidates, in this order, skipping anything labelled `in-progress` or `wontfix`:

```sh
gh issue list --state open --limit 200 --json number,title,labels,body \
  --jq '[.[] | select(all(.labels[].name; . != "in-progress" and . != "wontfix"))]'
```

1. Rank: `severity:P0` → `P1` → `P2` → `P3`; within a tier `brief-ready` first, then `verified`,
   then older first. Lane items (`lane:floci`) only when nothing P0–P2 is left.
2. Take at most **6** issues for the run (fewer if they are large).
3. For each, list the files it will touch: from the brief's **May write** table when
   `brief-ready`, otherwise from the file paths named in the issue body plus the tests dir.
4. Build waves: two issues may run in the same wave **only if their file sets are disjoint**.
   Issues whose briefs say `Merges after CF-NNN` go in a later wave than CF-NNN. Everything
   touching `web-proto/js/regions/inspector.js` or `internal/emit/composition.go` is usually
   one-at-a-time — those files are the collision magnets.
5. Write the plan as a comment on each selected issue: `taking — <branch> (wave N, driver run <date>)`,
   and add the `in-progress` label. Do this **before** dispatching; it is the lock.

## 3. Dispatch a subagent per issue

One subagent per issue, isolated, with exactly this prompt (three lines when a brief exists):

```
Task CF-NNN (issue #n). Brief: docs/tasks/CF-NNN-<slug>.md
Execution contract: docs/task-execution-contract.md (read it first)
Time cap: 75 minutes wall-clock. Hand back a committed branch and the report the contract asks for.
```

When there is no brief, the subagent writes the acceptance test **first** from the issue's
repro and contract (the issue body carries both), watches it fail, and only then implements —
the contract's §3 applies unchanged. Pass the issue body verbatim into the prompt so the
subagent does not need network access to read it.

What the subagent gets from the contract and needs no reminding of: a worktree under
`.worktrees/CF-NNN` branched from `origin/main`, its own e2e port, the gates, explicit
staging, no push, no merge, no issue edits, no AI attribution.

If your platform has skills, give each subagent the two repo documents above as its skill
content; do not add rules of your own — a rule that lives only in a prompt is lost next run.

## 4. Integrate a finished branch (driver only, one at a time)

For each handover, in wave order:

1. Read the report. It must contain the **failing** and the **passing** run of the acceptance
   test. No failing run → the branch is not accepted; comment on the issue and park it.
2. `git fetch && git log main..origin/main` — `main` moves under you; rebase the branch on
   `origin/main` in its worktree, rerun `make lint && make lint-strict && make test-race`
   there, plus `make test-e2e` if `web-proto/` or `tests/` changed, plus `make test-docker`
   if `internal/emit` or `internal/adopt` changed.
3. Fast-forward or merge into `main`, referencing the issue in the merge message
   (`CF-NNN, #n`). Push `main`.
4. `gh run watch <run-id> --exit-status`. If only the `e2e` job fails, rerun it once
   (`gh run rerun <id> --failed`) before treating it as a regression. Red after that → revert
   the merge on `main`, push, comment on the issue with the CI link, keep it open, remove
   `in-progress`.
5. Green → close: `gh issue close <n> --comment "completed in <sha>; guarded by <test name>"`.
   A half-fix is **not** closed: file the residue as a new `CF-NNN` (next free id per
   `AGENTS.md` §4), comment on the original pointing at it, and close the original only if the
   part it named is genuinely done.
6. `git worktree remove .worktrees/CF-NNN`.

Never merge two branches without a CI run between them; a red run after two merges cannot
be attributed.

## 5. Parking and cleanup

A branch that missed its cap or its gates is parked: leave it pushed as
`CF-NNN-<slug>` (push the topic branch, never `main`), comment on the issue with the exact
state (which test fails, what is left), and **remove `in-progress`** so the next run can pick
it up. Never leave the shared checkout dirty, never leave a worktree the next run has to
guess about — the contract forbids `git clean`, so remove by explicit path.

## 6. Run report

At T0 + 4:30 at the latest, write `docs/comp-runs/<YYYY-MM-DD>-driver-run.md`: the plan
(waves, issues, file sets), per issue the outcome (merged sha / parked / rejected, CI link),
residues filed, anything adjacent you noticed and did not touch (file it as an issue per
`.claude/skills/backlog-authoring/SKILL.md` §2–§4, at most 3 per run). Commit it to `main`
with a plain message and push. Your final message is that report's summary plus the issue
numbers you closed.

## What you never do

- Work on more than one issue in one branch.
- Close an issue from a topic branch, or without a green CI, or without a guarding test.
- Merge past T0 + 4:00, or dispatch past T0 + 3:00.
- Tick, edit or restore `BACKLOG.md`; it is a pointer now.
- Add AI attribution to any commit or comment.
