# Issue driver routine (Antigravity)

**No step in this routine waits for a human.** When a decision is needed, make the
conservative choice, record it in the Driver log (issue #80), and continue.

You are one of many **drivers** for `koorikla/composition-factory`. Each driver is an
Antigravity scheduled task running one **shift** of at most 5 h; shifts overlap around the
clock, and no driver can see another. Drivers coordinate **only** through GitHub issue state
and three scripts:

| Script | What it is | Lock pool |
|---|---|---|
| `scripts/driver/claim.sh` | the only way to take an issue | `claim`, 1 slot |
| `scripts/driver/land.sh` | the only way a branch reaches `main`: rebase, gates, one squashed commit, CI watch, revert on red | `merge`, 1 slot |
| `scripts/driver/lock.sh` | the machine-wide slot pools the other two and the heavy `make` gates run under (`AGENTS.md` §2) | — |

A driver claims issues, hands each to an isolated subagent that works to
`docs/task-execution-contract.md` and hands back on the issue, lands what was handed back —
by any driver — closes what landed, and posts one shift report. The backlog is GitHub
Issues; the oracle (`docs/routines/oracle.md`) files and re-verifies, drivers resolve.

Read first, in this order: `AGENTS.md`, `docs/task-execution-contract.md`, this file.

Leave `GATE_SLOTS` and every variable the driver scripts read unset (`CF_LOCK_DIR`,
`CF_LOCK_RETRY_SEC`, `CF_LOCK_TIMEOUT_SEC`, `CF_LOCKF_BIN`, `CF_GATE_SLOTS`, `CF_NOW`,
`CF_LEASE_MIN`, `CF_LAND_GATES`, `CF_CI_POLL_SEC`, `CF_LAND_WATCH_TIMEOUT_SEC`,
`CF_LAND_GATE_LOCK_TIMEOUT_SEC`, and the scripts' own `CF_CLAIM_LOCKED` and
`CF_LAND_LOCKED`): their defaults are the production values.

## 0. Shift and capacity

At start, record **T0** (`date -u +%Y-%m-%dT%H:%MZ`) and your **driver id**,
`<scheduled task name>-<T0 as HHMMZ>` (for example `driver-06-0300Z`). The id must not
start with `-` or contain whitespace; `claim.sh` rejects it with exit 64.

| Checkpoint | Rule |
|---|---|
| T0 | preflight (§1), cap tuning (below), then the loop (§2) |
| T0 + 3:00 | last `claim.sh` call — no dispatch, resume or takeover after this |
| T0 + 3:30 | last `land.sh` **start**. `land.sh` holds the merge lock through CI and any revert, and its bounded worst case is 70 min or more |
| end | post the shift report (§6) once no `land.sh` of yours is running and every subagent you dispatched has returned, or at T0 + 4:50, whichever comes first; then remove your driver worktree (§1) and stop |

Subagents get a 90-minute cap (§4). A claim's lease is 120 min and is never renewed.
Nothing is stranded when a shift ends: any live driver lands what yours handed back, and an
expired lease is taken over (§2).

**Gate slots.** `make test-race`, `make test-e2e` and `make test-docker` each hold one of
`GATE_SLOTS` machine-wide slots (`Makefile`, `GATE_SLOTS ?= 3`). Measured 2026-09-11 on the
driver machine (12 cores, 18 GiB): alone, `test-race` took 8 s, `test-e2e` 159 s and
`test-docker` 67 s, none above 349 MiB peak RSS. With N copies of `test-e2e` running at
once beside `test-race`, three rounds each, 2 of 8 suites failed at N=4, 1 of 9 at N=3 and
1 of 6 at N=2, and free memory never fell below 32%. Four flaked most and two gained nothing over three, so
`GATE_SLOTS=3`. It is measured, not tuned per shift: never change it.

**The cap** is the global number of live subagents, across every driver. It lives on the
line `cap: <n>` in the body of #80 and nowhere else. Gates are not what bounds it — the
gate formula, 3 slots × 90 min ÷ (2.5 heavy gates × 1.3 min), gives 83 — so it started at
16; the limits that bind are model quota, landing throughput and file overlap.

Tune it once, at shift start, from the shift reports §1 prints:

1. Count only reports whose `cap:` equals the current cap, newest four at most. A report
   written before the last change says nothing about the new value.
2. **Lower by 2** (never below 4) if two or more of them report a `gate wait` max over 900 s.
3. Otherwise **raise by 2** (never above 24) if there are four and all four report a
   `gate wait` max under 120 s and a `peak live` at or above their `cap:`.
4. Otherwise leave it. A report with `gate wait: none` is neither over nor under.

To change it, re-read the body; if its `cap:` line is no longer the value you tuned from,
another driver changed it — leave it this shift. Otherwise replace the `cap:` line and add
one line directly under it, `cap <old>→<new> by <driver-id>: <reason>`:

```sh
body="$(mktemp)"
gh issue view 80 --json body --jq .body > "$body"
# edit "$body": the cap: line, and the new ledger line under it
gh issue edit 80 --body-file "$body"
```

At most one change per shift, and it goes in your shift report. If the body has no
readable `cap:` line, use 4 for this shift, change nothing, and say so in the report.

**Quota back-off.** When a subagent dies on HTTP 429 with a reset time, post on #80 a
comment whose first line is

```
quota back-off until <YYYY-MM-DDTHH:MMZ> · driver <driver-id> · <model>
```

with the reset time in UTC, rounded up to the minute. A 429 without a reset time backs off
30 min the same way. Until the newest back-off passes, **no driver calls `claim.sh`**.
Landing continues.

## 1. Worktrees and preflight

**Worktrees only.** Drivers never check out, commit, rebase or merge in the shared
checkout; its state is not your business. On 2026-09-11 a driver checked out a topic branch
there, and another session's commit made in that tree landed on `main` inside that task.
Every driver operation happens in a worktree under `.worktrees/`, and landing happens only
inside `land.sh`'s own scratch worktree. Create yours at T0 and run every command in this
file from it:

```sh
ROOT="$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")"
git fetch --prune origin
git worktree add --detach "$ROOT/.worktrees/driver-<driver-id>" origin/main
cd "$ROOT/.worktrees/driver-<driver-id>"
```

`ROOT` is the main checkout wherever you start, so worktrees never nest. The scripts you
run are the ones on `origin/main` at T0.

**Remove only worktrees created for your own claims:** your driver worktree, and a
subagent worktree named on the `Worktree:` line of a handover or park comment on an issue
you claimed — and only once that issue is closed or parked and no
subagent of yours still runs in it. Use `git worktree remove <path>`, never `--force`, never
`rm`; if git refuses, leave it and name it in the shift report. After removing it, delete
its local branch (`git branch -D <local branch>`) if one exists. Never touch any other
worktree or branch: on 2026-09-11 a driver's cleanup force-removed five worktrees while
processes were still running in them.

**Preflight**, then the same reads at the top of every loop pass:

```sh
git fetch --prune origin
gh auth status
```

*The Driver log* — prints `cap: <n>` (or `cap: MISSING`), `quota back-off: none` or
`quota back-off: until <iso>`, and the newest four shift reports:

```sh
gh issue view 80 --json body,comments | jq -r --argjson now "$(date +%s)" '
  def trusted: .authorAssociation as $a | $a == null or $a == "OWNER" or $a == "MEMBER" or $a == "COLLABORATOR";
  ([.comments[] | select(trusted) | (.body // "")]) as $bodies
  | ([.body | split("\n")[] | sub("\r$"; "") | capture("^cap: (?<n>[0-9]+)$") | .n] | first // "MISSING") as $cap
  | ([$bodies[] | (split("\n")[0] // "") | capture("^quota back-off until (?<t>[0-9T:-]+Z)") | .t
      | try (strptime("%Y-%m-%dT%H:%MZ") | mktime) catch empty] | max) as $quota
  | "cap: \($cap)",
    "quota back-off: \(if $quota != null and $now < $quota then "until " + ($quota | strftime("%Y-%m-%dT%H:%MZ")) else "none" end)",
    ([$bodies[] | select(startswith("shift report — "))] | .[-4:] | .[] | "---", .)'
```

*Issue state* — one tab-separated row per open issue, oldest first: number, state,
severity, triage, branch of the newest trusted claim, lease expiry:

```sh
gh issue list --state open --limit 500 --json number,labels,comments,updatedAt | jq -r --argjson now "$(date +%s)" '
  def trusted: .authorAssociation as $a | $a == null or $a == "OWNER" or $a == "MEMBER" or $a == "COLLABORATOR";
  def has($l): any(.labels[]; .name == $l);
  if length >= 500 then error("500 open issues: raise --limit") else . end
  | sort_by(.number) | .[]
  | ([.comments[] | select((.body // "") | startswith("taking —")) | select(trusted)] | last) as $c
  | ((($c.body // "") | split("\n")[0]) // "") as $line
  | (if $c == null then (.updatedAt | fromdateiso8601) + 7200
     else (try ([$line | capture(" · lease until (?<t>\\S+)") | .t] | first | strptime("%Y-%m-%dT%H:%MZ") | mktime) catch null)
          // (($c.createdAt | fromdateiso8601) + 7200) end) as $expiry
  | (if has("wontfix") or has("driver-log") then "skip"
     elif has("handed-back") then "handed-back"
     elif has("parked") then "parked"
     elif has("in-progress") then (if $now < $expiry then "live" else "expired" end)
     else "open" end) as $state
  | [.number, $state,
     ([.labels[].name | select(startswith("severity:")) | ltrimstr("severity:")] | first // "-"),
     (if has("brief-ready") then "brief-ready" elif has("verified") then "verified" else "-" end),
     ([$line | capture("^taking —\\s+(?<b>\\S+)") | .b] | first // "-"),
     ($expiry | strftime("%Y-%m-%dT%H:%MZ"))] | @tsv'
```

States: `handed-back`, `parked`, `live` (`in-progress`, lease not expired), `expired`
(`in-progress`, lease expired), `open` (unclaimed), `skip` (`wontfix` or `driver-log`). It
reads leases as `claim.sh` does — the newest `taking —` comment by an OWNER, MEMBER or
COLLABORATOR; its `lease until`, else its time + 120 min, else the issue's `updatedAt` +
120 min — and lists without `--label` on purpose: a labelled list goes through GitHub
search, which lags behind a claim made seconds ago. The table is for ordering and counting;
`claim.sh` has the last word (an issue with 100 or more comments can show a stale lease
here, and `claim.sh` re-reads those).

*Main's CI* — the newest completed `ci` run on `main`, then its non-passing jobs:

```sh
gh run list --workflow ci --branch main --limit 10 --json databaseId,status,conclusion,attempt,url \
  --jq '[.[] | select(.status == "completed")] | first | "\(.databaseId) \(.conclusion) attempt \(.attempt) \(.url)"'
gh run view <databaseId> --json jobs \
  --jq '[.jobs[] | select(.conclusion != "success" and .conclusion != "skipped" and .conclusion != "neutral") | .name] | join(" ")'
```

**Main is red** when that run concluded anything but `success` — except, on attempt 1, a
`cancelled` run or a `failure` whose only non-passing job is `e2e`: `land.sh` reruns those
once before it lands on them, so count them green. While main is red:

- land only `severity:P0` issues (`land.sh` refuses the rest with `MAIN-RED` and lets P0s
  through);
- claim only `severity:P0` issues;
- if no `severity:P0` issue is open, file one (§5, *Main red*).

Main is green again when this check reads green.

## 2. The loop

Every ~5 min (`sleep 300` after a pass that did nothing) until the checkpoints stop it:

1. **Refresh**: the preflight reads (§1).
2. **Land** every `handed-back` issue, oldest first, one `land.sh` at a time (§5).
3. **Resume** `parked` issues: claim (§3) and dispatch (§4) on the branch they already have.
4. **Take over** `expired` issues the same way.
5. **Dispatch** `open` issues, best candidate first.

Steps 3–5 run only while all of these hold: it is before T0 + 3:00; no quota back-off is in
force; and **live subagents** — rows in state `live`, i.e. open `in-progress` issues with an
unexpired lease, across all drivers — are fewer than the cap. Count one more after every
`CLAIMED`; the table is only re-read next pass.

**Candidate order** for steps 3–5: `severity:P0` → `P1` → `P2` → `P3` → no severity (lane
items); within a tier `brief-ready`, then `verified`, then oldest (lowest number) first.
Never claim a `skip` row. While main is red, `severity:P0` only. Skip an issue whose brief
says `Merges after CF-NNN` while that CF-NNN's issue is still open.

**Not resumed:** a `parked` issue whose newest park comment gives the reason `finding` (the
subagent found the brief or the issue wrong; running it again gets the same answer), or one
that carries three or more park comments. Leave it parked and list it in the shift report.

## 3. Claiming

Only through `claim.sh`, from your driver worktree:

```sh
scripts/driver/claim.sh <issue> <branch> <driver-id> <file>…
```

- **branch** — for a new claim, `CF-NNN-<slug>` from the issue title (`land.sh` lands only a
  branch that starts `CF-<digits>`). For a resume or takeover, the `branch` column of the
  issue-state table when `git rev-parse --verify --quiet refs/remotes/origin/<branch>` finds
  it on origin; otherwise a fresh `CF-NNN-<slug>`.
- **files** — the brief's **May write** table when the issue is `brief-ready`
  (`git show origin/main:docs/tasks/CF-NNN-<slug>.md`); otherwise the file paths named in the
  issue body plus the test file the subagent will add, named as you expect it. For a resume
  or takeover, add the previous claim's `files:` and every file the branch changed:
  `git diff --name-only origin/main...origin/<branch>`. Paths are relative to the repository
  root and compared as exact strings; none may start with `-` or contain whitespace.

Decide by the **stdout line**, not the exit code alone: `lock.sh` can end the run with exit
2 and an empty stdout, which is not a `REFUSED`.

| stdout | exit | Do |
|---|---|---|
| `CLAIMED` | 0 | dispatch (§4) |
| `TAKEN <driver> until <iso>` | 3 | next candidate |
| `OVERLAP #<holder> <file>` | 4 | next candidate |
| `REFUSED closed`, `REFUSED wontfix`, `REFUSED handed-back` | 2 | skip the issue this pass |
| empty | 64 | your arguments are malformed (see its stderr): fix them and call once more; 64 again → skip and note it in the shift report |
| empty | 70 | not claimed: do not dispatch, do not touch the labels (one may remain), note it in the shift report |
| empty | 2, 73 or 75 | `lock.sh` did not get the `claim` lock: not claimed, do not dispatch, note it in the shift report |

`CLAIMED (dry run)` comes only from `claim.sh --dry-run`, which does every read and check
and writes nothing; never dispatch on it. Use it to rehearse arguments.

**Trust.** The repository is public, so both scripts ignore `taking —` comments from anyone
who is not an OWNER, MEMBER or COLLABORATOR. When `claim.sh` does, it prints
`claim.sh: ignored <k> claim comment(s) from outside the project on #<n>` on stderr; copy
that line into your shift report. Never write a `taking —` comment yourself.

**Bare labels.** An `in-progress` label with no trusted `taking —` comment is leased from the
issue's `updatedAt` + 120 min, and any comment — an outsider's included — moves `updatedAt`.
Such an issue can answer `TAKEN legacy until …` indefinitely. Leave it, and list it in the
shift report.

## 4. Dispatch

One subagent per claimed issue, in its own worktree (the contract's §1), with exactly these
four lines:

```
Task CF-NNN (issue #n), branch <branch>, driver <driver-id>. Brief: docs/tasks/CF-NNN-<slug>.md
Execution contract: docs/task-execution-contract.md (read it first)
Time cap: 90 minutes wall-clock.
Push your topic branch; hand back on the issue.
```

Below them, paste the issue body verbatim, so the subagent does not need to fetch it; for a
resume or takeover, also the newest handover or park comment, verbatim. Without a brief,
line 1 ends `Brief: none — the issue body below is the task.`, and the subagent writes the
acceptance test **first** from the issue's repro and contract, watches it fail, and only
then implements; the contract's §3 applies unchanged.

If your platform has skills, give each subagent the two repository documents above as its
skill content. Add no rules of your own: a rule that lives only in a prompt is lost next
run.

**When a subagent returns**, re-read its issue:

- `handed-back` or `parked` → done; landing picks it up.
- still `in-progress`, and the newest trusted `taking —` comment is your claim → it ended
  without handing back. Comment `parked — <branch> · subagent-ended: <its last error, one
  line>`, then `gh issue edit <n> --add-label parked --remove-label in-progress`. An HTTP
  429 also triggers the quota back-off (§0).
- the newest trusted claim is not yours → it was superseded; change nothing.

## 5. Landing

Only through `land.sh`, one call at a time, oldest `handed-back` first — any driver's, not
only yours. The merge lock serializes landings across drivers, so a call may first print
`lock.sh: waiting for merge`. No `land.sh` starts after T0 + 3:30; while main is red,
`severity:P0` issues only.

**Before calling it**, read the newest trusted comment on the issue whose first line starts
`handover — `. It must contain a failing and a passing run of the acceptance test (for a
brief whose test is `Acceptance test: none - documentation`, the output of its verification
commands instead). If it does not, do not land: comment
`parked — <branch> · no failing run in handover`, then
`gh issue edit <n> --add-label parked --remove-label handed-back`.

```sh
scripts/driver/land.sh <n>
```

Decide by the **stdout line** (exactly one; everything else is stderr):

| stdout | exit | Do |
|---|---|---|
| `LANDED <sha> <run-url>` | 0 | `gh issue close <n> --comment "completed in <sha>; guarded by <test>"`, naming the test from the handover; then `gh issue edit <n> --remove-label handed-back`, plus `--remove-label in-progress` and `--remove-label parked` for each the issue also carries. Remove its worktree if it is yours (§1). |
| `NOT-HANDED-BACK` | 5 | skip: its labels changed since you listed them |
| `PARKED rebase-conflict`, `PARKED gates-red`, `PARKED push-rejected` | 6 | comment `parked — <branch> · land.sh PARKED <reason>`, then `gh issue edit <n> --add-label parked --remove-label handed-back`. `main` is untouched. |
| `REVERTED <run-url>` | 7 | the same, the comment naming the CI link: `parked — <branch> · land.sh REVERTED <run-url>`. The topic branch is kept. |
| `REVERTED-RED <run-url>` | 8 | park as for exit 7, then *Main red* |
| `REVERTED-RED push-unknown` | 8 | the push failed and origin could not be read: leave the labels (the next `land.sh` on this issue finds the landing if it went through and watches it), comment the line on the issue, then *Main red* |
| `MAIN-RED <run-url>` | 8 | main's CI is red and the issue is not `severity:P0`; nothing was pushed. Leave the issue `handed-back`; *Main red* |
| empty | 64 | not landed (its stderr says why, e.g. no trusted claim naming a `CF-<digits>` branch): record it and move on |
| empty | 70 | not landed (`gh`, `git` or `jq` failed, or the branch is missing or has nothing to land): record it and move on |
| empty | 2, 73 or 75 | `lock.sh` did not get the `merge` lock: not landed; record it and move on |

A `<run-url>` reads `no-ci-run` when no CI run appeared for a pushed sha. An issue that
returns 64 or 70 twice in your shift is parked with
`parked — <branch> · land.sh exit <code>: <its last stderr line>`, so the next resume can
fix it instead of every driver retrying it.

A half-fix is not closed as done: when the handover names a part left undone, file the
residue as a new `CF-NNN` (next free id per `AGENTS.md` §4), comment on the original
pointing at it, and close the original only if the part it named is genuinely done.

**Main red** — any exit 8:

1. Stop landing and claiming non-P0 issues until main's CI check (§1) reads green.
   `severity:P0` work continues: an exit 8 from a P0's own landing does not stop P0 work.
2. Comment on #80: `main red — <stdout line> · driver <driver-id> · issue #<n>`.
3. List open `severity:P0` issues again; if there is none, file one per
   `.claude/skills/backlog-authoring/SKILL.md` §4: the next free `CF-NNN`, title
   `CF-NNN — main is red: <failing job> fails on <sha>`, labels `severity:P0` and
   `scale:engine` (`scale:ux` when only `e2e` fails), and a body quoting the `land.sh` line,
   the run URL and its failing jobs (`gh run view <id> --json jobs`).

## 6. Shift report

One comment on #80 and nothing else — no commit; `docs/comp-runs/` is history now:

```sh
gh issue comment 80 --body-file - <<'EOF'
shift report — <driver-id>
T0: <YYYY-MM-DDTHH:MMZ> · ended: <YYYY-MM-DDTHH:MMZ> · cap: <n> · peak live: <n>
landed: #<n> <sha> <run-url>, … | none
parked: #<n> <reason>, … | none
reverted: #<n> <run-url>, … | none
takeovers: #<n> of <old driver>, … | none
not claimed: #<n> <exit code>, … | none
gate wait: p50 <n>s, max <n>s over <k> samples | none
merge wait: max <n>s | none
quota: <back-off posted, until when> | none
main red: <land.sh line> | none
residues filed: #<n>, … | none
cap change: <old>→<new>: <reason> | none
notes: <ignored outside claims, bare labels, parked issues not resumed, worktrees git would not remove>
EOF
```

- **gate wait** — from the `lock.sh: gate acquired after <n>s` lines that subagents paste
  into their handover and park comments (contract §7), on every issue you landed or parked
  this shift.
- **merge wait** — from the `lock.sh: merge acquired after <n>s` line of each of your
  `land.sh` calls.
- **peak live** — the highest live-subagent count you saw in one pass.

File what handovers noticed and did not fix as issues per
`.claude/skills/backlog-authoring/SKILL.md` §2–§4, at most 3 per shift, and list them under
`residues filed`. Your final message is the shift report.

## What you never do

- Claim or land by hand: no `taking —` comment, no `in-progress` label outside `claim.sh`, no
  push to `main` outside `land.sh`.
- Set `CF_GATE_SLOTS=off`, change `GATE_SLOTS`, or set any other variable the driver scripts
  read.
- Edit or delete another driver's worktree, branch or claim.
- Check out, commit, rebase or merge in the shared checkout.
- Work on more than one issue in one branch.
- Close an issue without `LANDED` from `land.sh`, or without a guarding test.
- Tick, edit or restore `BACKLOG.md`; it is a pointer now.
- Add AI attribution to any commit or comment.
