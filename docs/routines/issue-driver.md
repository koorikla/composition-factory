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
`CF_LOCK_RETRY_SEC`, `CF_LOCKF_BIN`, `CF_GATE_SLOTS`, `CF_NOW`, `CF_LEASE_MIN`,
`CF_LAND_GATES`, `CF_CI_POLL_SEC`, `CF_LAND_WATCH_TIMEOUT_SEC`,
`CF_LAND_GATE_LOCK_TIMEOUT_SEC`, `CF_LAND_MAIN_RERUN_AFTER_SEC`, and the scripts' own
`CF_CLAIM_LOCKED` and `CF_LAND_LOCKED`): their defaults are the production values.

**`CF_LOCK_TIMEOUT_SEC=5` on the `land.sh` call of §5 is the one variable a driver sets,
and the only place it sets it.** It turns landing into a try-lock. Nowhere else.

## 0. Shift and capacity

At start, record **T0** (`date -u +%Y-%m-%dT%H:%MZ`) and your **driver id**: the scheduled
task's name with any leading `driver-` stripped, then T0 as `-<YYYYMMDD>-<HHMM>Z`. Task
`driver-06` starting `2026-09-12T03:00Z` gives the id `06-20260912-0300Z` and the worktree
`.worktrees/driver-06-20260912-0300Z` — never a doubled `driver-driver-…`. The date part
is load-bearing: a leaked driver worktree is collected by name and age
(`docs/routines/janitor.md`), so two shifts a day apart must never produce the same path.
The id must not start with `-` or contain whitespace; `claim.sh` rejects it with exit 64.

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

**The cap** is the global number of live subagents, across every driver. It is a *soft*
cap: every driver counts live subagents once per pass and then dispatches, so with D
drivers running the true peak can overshoot the cap by up to D − 1 before the next pass
sees it. That is accepted; do not compensate by dispatching below the cap. It lives on the
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

To change it, do the whole read-modify-write **under the `claim` lock**, so two drivers
tuning in the same minute cannot lose one another's edit. Inside the lock, re-read the
body; if its `cap:` line is no longer the value you tuned from, another driver changed it —
leave it this shift. Otherwise replace the `cap:` line and add one line directly under it,
`cap <old>→<new> by <driver-id>: <reason>`:

```sh
scripts/driver/lock.sh claim 1 -- /bin/bash -c '
  body="$(mktemp)"
  gh issue view 80 --json body --jq .body > "$body"
  # re-read the cap: line here; if it moved, leave the file alone and exit
  # otherwise edit "$body": the cap: line, and the new ledger line under it
  gh issue edit 80 --body-file "$body"
'
```

The `claim` pool has one slot, and `claim.sh` takes the same one, so hold it only for this
edit — never around a subagent, a gate or a `land.sh`.

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
subagent of yours still runs in it. Never touch any other worktree or branch: on
2026-09-11 a driver's cleanup force-removed five worktrees while processes were still
running in them. Anything not yours is the janitor's problem
(`docs/routines/janitor.md`), not yours.

**Nothing is removed while a process is in it.** `land.sh` moves to the main checkout at
once and can outlive the driver that started it, but a subagent, a gate or a leftover
`cf serve` in the tree being removed is real work. Check with `lsof`, and judge by its
**output**, not its exit status: `lsof` exits 1 when nobody holds the path, which is
exactly the case where removal is allowed:

```sh
cd "$ROOT"                       # never run lsof from inside the path you are testing:
                                 # your own shell's cwd would show up as a holder
wt="$ROOT/.worktrees/<name>"
if [ -n "$(lsof +d "$wt" 2>/dev/null)" ]; then
  echo "occupied — leaving $wt; name it in the shift report"
else
  git -C "$ROOT" worktree remove "$wt"
fi
```

`+d` looks one level down, which is enough for a `cf serve` or a shell sitting in the
worktree root. `+D` walks it recursively and catches a process whose only open file is
buried in `node_modules/` or a scratch dir; it is slower and strictly safer, and it is
what `docs/routines/janitor.md` uses. Prefer `+D` when the tree is small enough that the
walk is cheap.

Always `git -C "$ROOT" worktree remove <path>`, run from outside the worktree — never
`--force`, never `rm`. Without `--force`, git refuses a tree with modified or untracked
files, which is the protection: uncommitted work survives. If git refuses, leave it and
name it in the shift report. After removing it, delete its local branch
(`git branch -D <local branch>`) if one exists and its issue is closed.

Your own driver worktree is removed last, at the end of the shift, the same way — after
every `land.sh` of yours has exited. `cd "$ROOT"` first: you cannot remove the directory
you are standing in.

**Preflight**, then the same reads at the top of every loop pass:

```sh
git fetch --prune origin
gh auth status
command -v lockf
```

**No `lockf`, no shift.** `lock.sh` falls back to running *every* pool unlocked when
`lockf` is missing (it prints `lock.sh: lockf not found; running <pool> unlocked`), and
unlocked pools mean two drivers can claim the same issue at the same instant and two
`land.sh` calls can push `main` on top of each other. If `command -v lockf` prints
nothing, comment

```
no lockf — driver <driver-id> stopped
```

on #80 and end the shift there: claim nothing, dispatch nothing, land nothing.

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

**You never read `main`'s CI, and you never judge it.** There is no preflight for it. A
driver that looks at `gh run list --branch main` and decides sees a different run than the
commit `land.sh` will actually rebase onto — `refs/remotes/origin/main` moves under every
worktree of the clone — and two drivers reading it in the same minute reach two different
verdicts. `land.sh` is the only authority: it pins `origin/main`'s sha, reads *that* sha's
run, waits for it if it is still in flight, reruns it when GitHub's own record says a
rerun is due (§5), and only then refuses with `MAIN-RED`.

So a red `main` blocks **landing**, and nothing else. Claiming, dispatch, resumes and
takeovers carry on at full rate whatever `main`'s CI is doing: work that is in flight
while `main` is red is exactly the work that lands once it is green. The `severity:P0`
issue for a red `main` is filed from a `land.sh` exit 8 and from nowhere else (§5).

## 2. The loop

Every ~5 min (`sleep 300` after a pass that did nothing) until the checkpoints stop it:

1. **Refresh**: the preflight reads (§1).
2. **Land** at most **one** `handed-back` issue, the oldest (§5). Landing is a try-lock:
   when another driver holds the merge lock your call returns at once and you go straight
   to step 3. One landing per pass keeps a driver that is waiting on CI from starving its
   own claiming and dispatch.
3. **Resume** `parked` issues: claim (§3) and dispatch (§4) on the branch they already have.
4. **Take over** `expired` issues the same way.
5. **Dispatch** `open` issues, best candidate first.

Steps 3–5 run only while all of these hold: it is before T0 + 3:00; no quota back-off is in
force; and **live subagents** — rows in state `live`, i.e. open `in-progress` issues with an
unexpired lease, across all drivers — are fewer than the cap. Count one more after every
`CLAIMED`; the table is only re-read next pass.

**Candidate order** for steps 3–5: `severity:P0` → `P1` → `P2` → `P3` → no severity (lane
items); within a tier `brief-ready`, then `verified`, then oldest (lowest number) first.
Never claim a `skip` row. `main`'s CI does not enter into it (§1). Skip an issue whose
brief says `Merges after CF-NNN` while that CF-NNN's issue is still open — match the
phrase case-insensitively (`grep -i`): briefs write it as `Merges after`, `merges after`
and `**Merges after**`, and a case-sensitive match silently dispatches work whose test
cannot pass yet.

**Not resumed** — leave it parked and list it in the shift report:

- a `parked` issue whose newest park comment gives the reason `finding` (the subagent
  found the brief or the issue wrong; running it again gets the same answer);
- one with **three or more strikes**. A strike is a trusted `parked — ` comment *or* a
  trusted `taking — … · takeover of <driver>` claim: a takeover means a shift ran out on
  the issue and left it, which costs the same 90 minutes a park does, and counting only
  parks let an issue cycle through takeover after takeover forever.

```sh
gh issue view <n> --json comments --jq '
  def trusted: (.authorAssociation // "OWNER") | IN("OWNER", "MEMBER", "COLLABORATOR");
  [.comments[] | select(trusted) | (.body // "") | (split("\n")[0] // "")
   | select(startswith("parked — ") or (startswith("taking —") and contains(" · takeover of ")))] | length'
```

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

  **Expand anything that is not one exact path before you pass it.** `claim.sh` compares
  strings: it does not know that `internal/emit/` contains `composition.go`, and it does
  not glob. A **May write** entry that is a directory or a pattern therefore overlaps with
  nothing, and two subagents get dispatched onto the same file. Expand it against
  `origin/main` first, and pass the result:

  ```sh
  git ls-files --with-tree=origin/main -- 'internal/emit/*' 'web-proto/src/**/*.ts'
  ```

  A file the brief allows but that does not exist yet (the acceptance test) is not in
  `git ls-files`: add it by hand, spelled exactly as the subagent will create it.

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

One subagent per claimed issue. **The subagent creates its own worktree** — the contract's
§1 tells it how, including the suffixed path and local branch a resume needs. Never create
it for them, never hand them yours, and never dispatch a subagent into your driver
worktree. Give it exactly these five lines:

```
Task CF-NNN (issue #n), branch <branch>, driver <driver-id>. Brief: docs/tasks/CF-NNN-<slug>.md
Execution contract: docs/task-execution-contract.md (read it first)
Time cap: 90 minutes wall-clock.
Push your topic branch; hand back on the issue.
Your claim: <the `taking — …` line claim.sh just posted, copied byte for byte>
```

The fifth line is what the subagent compares against before **every** push and again
before it hands back (contract §7). It must be the first line of the comment `claim.sh`
posted, exactly as posted — same branch, same driver id, same `lease until`, same
`files:` — because a subagent that cannot tell its own claim from the claim that took it
over will push over work it never saw. Read it back rather than reconstructing it:

```sh
gh issue view <n> --json comments --jq '[.comments[] | (.body // "") | (split("\n")[0] // "") | select(startswith("taking —"))] | last'
```

Below the five lines, paste the issue body verbatim, so the subagent does not need to
fetch it; for a resume or takeover, also the newest handover or park comment, verbatim.
Without a brief,
line 1 ends `Brief: none — the issue body below is the task.`, and the subagent writes the
acceptance test **first** from the issue's repro and contract, watches it fail, and only
then implements; the contract's §3 applies unchanged.

If your platform has skills, give each subagent the two repository documents above as its
skill content. Add no rules of your own: a rule that lives only in a prompt is lost next
run.

**When a subagent returns**, re-read its issue:

- `handed-back` or `parked` → done; landing picks it up (a `parked` issue is picked up by
  the resume step, §2 step 3).
- still `in-progress`, and the newest trusted `taking —` comment is your claim → **check
  for a `handover — ` comment newer than your claim before you park it.** The label edit
  is a separate call from the comment, and a subagent that died between the two left a
  complete, landable handover behind. If one exists, treat the issue as handed back —
  `gh issue edit <n> --add-label handed-back --remove-label in-progress` — and let §5
  land it. Only with no such comment: comment `parked — <branch> · subagent-ended: <its
  last error, one line>`, then
  `gh issue edit <n> --add-label parked --remove-label in-progress`. An HTTP 429 also
  triggers the quota back-off (§0).

  ```sh
  gh issue view <n> --json comments | jq -r --arg claim '<your taking — line>' '
    def trusted: (.authorAssociation // "OWNER") | IN("OWNER", "MEMBER", "COLLABORATOR");
    (([.comments[] | select(trusted) | select(((.body // "") | split("\n")[0]) == $claim) | .createdAt] | last) // "") as $at
    | [.comments[] | select(trusted) | select(.createdAt > $at)
       | select((.body // "") | startswith("handover — "))] | length'
  ```

  (`gh --jq` takes no `--arg`; pipe into `jq` whenever you need to pass a value in.)
- the newest trusted claim is not yours → it was superseded; change nothing.

## 5. Landing

Only through `land.sh`, and **at most one issue per pass** — the oldest `handed-back`,
any driver's, not only yours. No `land.sh` starts after T0 + 3:30.

**Landing is a try-lock.** `land.sh` holds the `merge` slot for its whole run, through the
landing's CI and any revert: that can be over an hour. Waiting on it would stop you
claiming, dispatching and collecting subagents for that hour, while the driver that holds
the lock is already doing the work. So call it with a five-second lock timeout and give up
when another driver has it:

```sh
CF_LOCK_TIMEOUT_SEC=5 scripts/driver/land.sh <n>
```

`CF_LOCK_TIMEOUT_SEC` is the one variable you set, and this is the only line you set it
on. **Exit 75 with an empty stdout means another driver is landing**: not a failure, not
something to report as one. Skip to step 3 of the loop and try again next pass — their
`land.sh` will land this issue if you do not.

You may call it from your driver worktree, but it does not stay there: `land.sh` moves to
the clone's main checkout immediately and exports `GH_REPO` from origin's URL, so neither
`git` nor `gh` depends on the directory you launched it from. Removing a worktree cannot
pull the ground from under a landing — which is why §1's `lsof` guard is about subagents
and stray servers, not about `land.sh`.

**Before calling it**, read the newest trusted comment on the issue whose first line starts
`handover — `. It must contain a failing and a passing run of the acceptance test (for a
brief whose Acceptance test is `none …` — the template writes
`Acceptance test: none — documentation.`, older briefs a hyphen, so match on `none` and
not on the rest of the line — the output of its Verification commands instead). If it does
not, do not land: comment `parked — <branch> · no failing run in handover`, then
`gh issue edit <n> --add-label parked --remove-label handed-back`.

**`land.sh` owns the issue's state labels.** It sets them under the merge lock, before it
releases it and before it prints its result line, so the next `land.sh` never sees labels
a driver has not got round to updating. Most rows therefore leave you nothing to do but
record the result: make no label edit except where the table says so.

| stdout | exit | Do |
|---|---|---|
| `LANDED <sha> <run-url>` | 0 | `gh issue close <n> --comment "completed in <sha>; guarded by <test>"`, naming the test from the handover. `land.sh` has already removed `handed-back`, `in-progress` and `parked`; do not touch labels. Then clear the red-main P0, below. Remove the worktree if it is yours (§1). |
| `NOT-HANDED-BACK` | 5 | skip: its labels changed since you listed them |
| `PARKED rebase-conflict`, `PARKED gates-red`, `PARKED already-reverted` | 6 | **nothing.** `land.sh` added `parked`, removed `handed-back` and posted `parked — <branch> · <reason>`. Record it in the shift report; §2 step 3 picks it up as a resume. |
| `PARKED push-rejected` | 6 | `main` moved under the push; the branch itself is sound and needs no subagent. **Once per issue per shift**, put it straight back: `gh issue edit <n> --add-label handed-back --remove-label parked`, and comment `re-queued — <branch> · push-rejected, main moved`. The next pass lands it against the new `main`. A second `push-rejected` on the same issue: leave it parked for a resume. |
| `REVERTED <run-url>` | 7 | **nothing.** `land.sh` parked it and posted `parked — <branch> · reverted <run-url>`. The topic branch is kept. Record it. |
| `REVERTED-RED <run-url>` | 8 | `land.sh` parked it (`reverted-red` or `revert-failed`). Then *Main is red*, below. |
| `REVERTED-RED push-unknown` | 8 | the push failed and origin could not be read; `land.sh` left the labels alone on purpose — the next `land.sh` on this issue finds the landing if it went through and watches it. Change nothing. Then *Main is red*. |
| `MAIN-RED <run-url>` | 8 | `main`'s CI is red and this issue is not `severity:P0`; nothing was pushed and the issue stays `handed-back`. Then *Main is red*. |
| empty | 64 | not landed (its stderr says why, e.g. no trusted claim naming a `CF-<digits>` branch): record it and move on |
| empty | 70 | not landed (`gh`, `git` or `jq` failed, or the branch is missing or has nothing to land): record it and move on |
| empty | 75 | another driver holds the `merge` lock: skip to the next loop step, and do **not** record it as a failure |
| empty | 2 or 73 | `lock.sh` could not take the lock at all (bad arguments, or no slot could be opened): not landed; record it and move on |

A `<run-url>` reads `no-ci-run` when no CI run appeared for a pushed sha.

**Exits 64 and 70 twice: park — but only when the cause is deterministic.** Re-running
`land.sh` against a transient `gh` or network failure is exactly the right thing to do,
and parking the issue for it strands work that would have landed on the next pass. So:

- Park at once, on the **first** occurrence, when `land.sh`'s last stderr line is
  `land.sh: branch <branch> is not on origin` or
  `land.sh: branch <branch> has nothing to land against origin/main`. Both are statements
  about the branch, and no number of retries changes either.
- Otherwise count it. A *second* 64 or 70 on the same issue in your shift whose stderr is
  not a `gh`/`git` transport failure (`HTTP 5xx`, `could not resolve host`, `connection
  reset`, `timeout`, `API rate limit`) parks it. Transport failures never count, however
  many of them you see; note the count in the shift report instead.

Park with `parked — <branch> · land.sh exit <code>: <its last stderr line>`, then
`gh issue edit <n> --add-label parked --remove-label handed-back`, so the next resume
fixes it instead of every driver retrying it forever.

A half-fix is not closed as done: when the handover names a part left undone, file the
residue as a new `CF-NNN` (next free id per `AGENTS.md` §4), comment on the original
pointing at it, and close the original only if the part it named is genuinely done.

### Main is red

Reached only from a `land.sh` exit 8 — never from reading CI yourself (§1). `land.sh` has
already decided, against the sha it pinned, that `main` is red.

Every exit 8 carries an identifier that is stable across drivers, and that identifier is
what both the comment and the P0 are deduplicated on. For `MAIN-RED <run-url>` and
`REVERTED-RED <run-url>` it is the run URL's last path segment, the run id. For
`REVERTED-RED push-unknown` there is no run URL: use the landing sha `land.sh` printed on
stderr (`land.sh: the push of <sha> failed …`) in its place, and read `run-id` as `sha`
everywhere below.

1. **Stop landing non-`severity:P0` work for the rest of this pass**, and keep stopping at
   the top of each later pass while `land.sh` keeps answering `MAIN-RED`. `land.sh` itself
   enforces this — it refuses a non-P0 landing on a red `main` and lets a P0 through — so
   you never have to guess when `main` is green again: the pass where a non-P0 landing
   returns `LANDED` is that moment. **Claiming and dispatch do not stop.** An exit 8 from a
   P0's own landing does not stop P0 work.
2. **Post on #80 once per run URL, not once per exit 8.** Every driver that tries to land
   while `main` is red gets the same `MAIN-RED <run-url>`, and one broken run otherwise
   produces a comment per driver per pass. Before commenting, look for that run URL in the
   existing comments, and stay quiet if it is there:

   ```sh
   gh issue view 80 --json comments | jq -r --arg url '<run-url>' \
     '[.comments[] | select((.body // "") | contains($url))] | length'
   ```

   The comment, when it is the first: `main red — <run-url> · driver <driver-id> · issue #<n>`.
3. **File one `severity:P0`, serialized and deduplicated by run URL.** Two drivers hitting
   the same red `main` in the same minute will otherwise file two P0s for it. Do the
   re-read and the create inside the `claim` lock, and key the dedup on the run's **id** —
   the last path segment of the run URL — because every driver sees the same id and no two
   red runs share one:

   ```sh
   export CF_RUN_ID="${run_url##*/}"
   scripts/driver/lock.sh claim 1 -- /bin/bash -c '
     n="$(gh issue list --state open --limit 500 --json title --jq ".[].title" | grep -c -F "$CF_RUN_ID")"
     [ "$n" = 0 ] || exit 0     # another driver already filed this run; say nothing
     gh issue create --title "CF-NNN — main is red: <job> fails on <short sha> (run $CF_RUN_ID)" …
   '
   ```

   List **without** `--label`: a labelled list goes through GitHub search, which lags
   behind an issue created seconds ago, and the lag is precisely the window the lock
   exists to close.

   When the count is 0, file it per `.claude/skills/backlog-authoring/SKILL.md` §4: the
   next free `CF-NNN`, title
   `CF-NNN — main is red: <failing job> fails on <short sha> (run <run-id>)` — the run id
   must be in the **title**, that is what the next driver matches on — labels
   `severity:P0` and `scale:engine` (`scale:ux` when only `e2e` fails), and a body quoting
   the `land.sh` line, the run URL and its failing jobs
   (`gh run view <run-id> --json jobs`).
4. **Close it when a non-P0 landing gets through.** `land.sh` refuses a non-P0 landing on
   a `main` it found red, so a non-P0 that reaches `LANDED` was rebased onto a `main`
   `land.sh` did not find red — and its own CI then went green on top of it. On any such
   `LANDED`, close every open `main is red` P0:

   ```sh
   gh issue close <n> --comment "main green: CF-<the landed id> landed <sha>"
   ```

   Never close one off your own `LANDED` on a **P0**: a P0 lands on a red `main` by
   design, and proves nothing about it. If the red `main` was in fact never fixed, the
   next non-P0 landing returns `MAIN-RED` again and the P0 is refiled against the same
   run id — which is the whole reason the dedup key is the run and not the issue.

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
e2e flakes: <spec name> #<n> <run-url>, … | none
quota: <back-off posted, until when> | none
main red: <run-url> (filed #<n> | already filed) | none
residues filed: #<n>, … | none
cap change: <old>→<new>: <reason> | none
notes: <ignored outside claims, bare labels, parked issues not resumed, worktrees git would not remove>
EOF
```

- **gate wait** — from the `lock.sh: gate acquired after <n>s` lines that subagents paste
  into their handover and park comments (contract §7), on every issue you landed or parked
  this shift.
- **merge wait** — from the `lock.sh: merge acquired after <n>s` line of each of your
  `land.sh` calls. A call that returned exit 75 gave up at 5 s and is not a sample.
- **e2e flakes** — every `e2e` failure `land.sh` reran (its stderr says
  `only e2e failed in <run-url>; rerunning it once`) and every `e2e` failure a subagent
  reported in a handover. Name the spec, not just the job. These are the only evidence the
  flaky-spec backlog is built from, and a rerun that goes green leaves no other trace.
- **peak live** — the highest live-subagent count you saw in one pass. It can exceed
  `cap:` by up to (drivers − 1); say so rather than treating it as a fault.

File what handovers noticed and did not fix as issues per
`.claude/skills/backlog-authoring/SKILL.md` §2–§4, at most 3 per shift, and list them under
`residues filed`. Your final message is the shift report.

## What you never do

- Claim or land by hand: no `taking —` comment, no `in-progress` label outside `claim.sh`, no
  push to `main` outside `land.sh`.
- Set `CF_GATE_SLOTS=off`, change `GATE_SLOTS`, or set any variable the driver scripts read
  other than `CF_LOCK_TIMEOUT_SEC=5` on the `land.sh` call of §5.
- Read `main`'s CI to decide anything. `land.sh` decides; you act on its result line.
- Edit or delete another driver's worktree, branch or claim, or kill a process holding a
  gate slot. Reclaiming dead ones is `docs/routines/janitor.md`'s job.
- Check out, commit, rebase or merge in the shared checkout.
- Work on more than one issue in one branch.
- Close an issue without `LANDED` from `land.sh`, or without a guarding test.
- Tick, edit or restore `BACKLOG.md`; it is a pointer now.
- Add AI attribution to any commit or comment.
