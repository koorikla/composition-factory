# Janitor routine (Antigravity, local)

**No step in this routine waits for a human.** Where a decision is needed, make the
conservative choice — which here is always "leave it, and say why" — and continue.

You run **on the driver machine**, in the same clone the drivers use, as a local
Antigravity scheduled task. You reclaim things that are provably dead: worktrees whose
work landed, branches whose remote is gone, state labels on closed issues, a gate slot
held by an orphan, and scratch directories under worktrees that no longer exist.

You are the only agent allowed to remove something that is not yours — which is exactly
why every rule below is a conjunction of conditions, and why "I am fairly sure nobody is
using it" is not one of them. On 2026-09-11 a driver's cleanup force-removed five
worktrees while processes were still running in them; that is the failure this routine
exists to make impossible.

**You never claim, never dispatch, never land.** You never call `scripts/driver/claim.sh`
or `scripts/driver/land.sh`, never push anything, and **never touch an issue labelled
`in-progress` or `handed-back`, or anything belonging to one** — those are live.

## 0. Budget

**20 minutes** from T0. Work the five sweeps of §2–§6 in order and stop when the budget
runs out; an unfinished sweep costs nothing, because the next run redoes it from scratch.
Everything you skip goes in the report with its reason.

Start from the main checkout, not a worktree — you may be removing worktrees, and you
cannot remove the one you are standing in:

```sh
ROOT="$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")"
cd "$ROOT"
git fetch --prune origin
```

`--prune` matters: every "is this branch gone from origin?" test below reads
`refs/remotes/origin/…`, and without the prune a deleted branch still has a stale
remote-tracking ref and nothing is ever collected.

## 1. `lsof`, and how to read it

Every removal below is gated on `lsof` printing nothing for the path.

**Judge by the output, not the exit status.** `lsof` exits 1 when it finds no holder —
that is the *success* case for you, and treating a non-zero exit as an error inverts the
whole rule:

```sh
if [ -n "$(lsof +D "$ROOT/.worktrees/<name>" 2>/dev/null)" ]; then
  echo "occupied — leave it"
fi
```

`+D` walks the directory recursively (`+d` is one level only): a `cf serve` whose cwd is
the worktree root and a `go test` whose open file is six levels down both count, and both
mean somebody is working there. Run it from `$ROOT`, never from inside the path being
tested — your own shell's cwd would show up as a holder and nothing would ever be
collectable.

## 2. Worktree GC

Remove `$ROOT/.worktrees/<name>` only when **every one** of these holds:

1. **The name matches `CF-<digits>` or `CF-<digits>-<suffix>`.** Nothing else, ever.
   Never a `land-*` worktree: that is `land.sh`'s own scratch tree, `land.sh` removes it
   itself, and one that exists right now almost certainly belongs to a landing in flight.
   A `driver-*` worktree only when it is **older than 6 hours** — a shift is at most 5 h,
   so six hours means the driver that made it is gone. Read the age out of the **name**,
   not the filesystem: a driver id ends `-<YYYYMMDD>-<HHMM>Z`
   (`docs/routines/issue-driver.md` §0) precisely so that this is decidable, and an mtime
   is whatever the last write happened to be. A `driver-*` name with no parseable
   timestamp is left alone and reported. Anything else (`qa-*`, a human's tree, an
   unrecognised name) is left alone without further checks.
2. **The issue that name refers to is CLOSED.** The worktree name carries a `CF-NNN`, not
   an issue number — they are different sequences — so resolve it through the title:

   ```sh
   gh issue list --state all --limit 500 --json number,title,state \
     --jq '.[] | select(.title | test("^CF-411\\b")) | "\(.number) \(.state)"'
   ```

   `CLOSED` and nothing else. No match, or more than one, means you cannot resolve it:
   leave the worktree and report it. A `parked` issue is *not* closed — its branch and its
   worktree are exactly what a resume picks up, and removing them turns a resume into a
   restart.
3. **`origin/<its branch>` is gone**, after the fetch of §0. The branch is the one the
   worktree has checked out — `git -C "$ROOT" worktree list --porcelain` prints it as
   `branch refs/heads/<name>` under that worktree, and a detached worktree has no branch
   and passes this condition trivially:

   ```sh
   git -C "$ROOT" rev-parse --verify --quiet "refs/remotes/origin/<branch>"   # must print nothing
   ```

   `land.sh` deletes the topic branch on a green landing, so a branch that is gone from
   origin is a branch that landed.
4. **`lsof +D <path>` prints nothing** (§1).

Then, and only then:

```sh
git -C "$ROOT" worktree remove "$ROOT/.worktrees/<name>"
```

**Never `--force`.** Without it, git refuses a tree holding modified or untracked files —
`fatal: '<path>' contains modified or untracked files, use --force to delete it` — and
that refusal is a feature: it is uncommitted work that no `lsof` check can see, because
the agent that wrote it has already exited. A refusal is a result, not an error. Leave the
tree and name it in the report.

```sh
# candidate names only; conditions 2-4 are checked per candidate afterwards.
now="$(date -u +%s)"
git -C "$ROOT" worktree list --porcelain | sed -n 's/^worktree //p' | while read -r wt; do
  name="${wt##*/}"
  case "$name" in
    CF-[0-9]*) ;;
    driver-*)
      ts="$(printf '%s\n' "$name" | sed -n 's/.*-\([0-9]\{8\}\)-\([0-9]\{4\}\)Z$/\1\2/p')"
      [ -n "$ts" ] || continue                       # no timestamp in the name: leave it
      started="$(date -u -j -f %Y%m%d%H%M "$ts" +%s 2>/dev/null)" || continue
      [ $((now - started)) -gt 21600 ] || continue   # younger than 6 h
      ;;
    *) continue ;;
  esac
  echo "$name"
done
```

(`date -u -j -f` is BSD `date`, which is what the driver machine has.)

After removing a worktree, `git -C "$ROOT" worktree prune` so a registration whose
directory is already gone cannot block a later `worktree add`.

## 3. Local branches whose remote is gone

A local branch is deletable only when **its issue is closed** and `origin/<branch>` no
longer exists. The branch name starts with the issue's `CF-NNN`; resolve that to an issue
number and state with the lookup in §2, condition 2. If it does not resolve, leave the
branch — an unresolvable name is not evidence of anything.

```sh
git -C "$ROOT" for-each-ref --format='%(refname:short)' refs/heads | while read -r b; do
  git -C "$ROOT" rev-parse --verify --quiet "refs/remotes/origin/$b" >/dev/null || echo "$b"
done
```

Delete with `git -C "$ROOT" branch -d "$b"` first; `-d` refuses a branch whose commits are
not merged, which is the same protection `worktree remove` gives. Use `-D` only when the
branch's issue is closed *and* `-d`'s refusal is because the commits landed squashed —
`land.sh` squashes, so a landed branch is genuinely unmerged by ancestry. Check it before
you force:

```sh
git -C "$ROOT" log --first-parent -n 200 --format=%s origin/main | grep -F "(CF-NNN, #<n>)"
```

A hit means the work is on `main` under a different sha and `-D` is safe. No hit: leave
the branch.

## 4. State labels on closed issues

`in-progress`, `handed-back` and `parked` on a **closed** issue are dead state: `land.sh`
removes them when it lands, but a landing that was interrupted between the label edit and
the close, or an issue a human closed, leaves them behind. They cost every driver that
lists the backlog, because the issue-state table reads labels before state.

```sh
gh issue list --state closed --limit 200 --json number,labels --jq '
  [.[] | select(any(.labels[]?; .name == "in-progress" or .name == "handed-back" or .name == "parked"))
       | {number, stale: [.labels[].name | select(. == "in-progress" or . == "handed-back" or . == "parked")]}]'
```

For each, remove only those three labels, and only while the issue is still closed when
you act (re-read it; a closed issue can be reopened between the list and the edit):

```sh
gh issue edit <n> --remove-label in-progress --remove-label handed-back --remove-label parked
```

Pass only the labels the issue actually has — `gh` fails the whole call on a label that is
not there. Nothing else about the issue changes: no comment, no reopen, no close.

## 5. Leaked gate slots

A gate slot is a `flock` on `<git common dir>/cf-locks/gate.<i>`, held for exactly as long
as some process keeps that descriptor open. An e2e run that was interrupted can leave a
`cf serve` behind, detached, holding the slot for the rest of the machine's uptime — and
with three slots, three of those stop every gate on the machine forever.

Reclaim one **only** when all four hold:

1. The holder is listed by `lsof` on a `gate.*` slot file. Derive the lock directory the
   way `lock.sh` does rather than assuming `.git`:

   ```sh
   LOCKS="$(git -C "$ROOT" rev-parse --path-format=absolute --git-common-dir)/cf-locks"
   ls "$LOCKS"/gate.* >/dev/null 2>&1 && lsof "$LOCKS"/gate.*
   ```

   The slot files are created on first use, so no files at all means no holders and
   nothing to do.
2. Its command is **`cf serve`** — `ps -o command= -p <pid>`. Not `go`, not `node`, not
   `make`, not a shell: those are live gates.
3. Its **parent is pid 1** — `ps -o ppid= -p <pid>` prints `1`. A reparented process is
   one whose agent has exited; a process with a live parent still belongs to someone.
4. Its **cwd is a `.worktrees/CF-*` of an issue that is not live** —
   `lsof -a -p <pid> -d cwd -Fn | sed -n 's/^n//p'` gives the path. Resolve its `CF-NNN`
   to an issue with the lookup in §2, condition 2; that issue must carry neither
   `in-progress` nor `handed-back`. A cwd anywhere else — a `land-*` tree, the shared
   checkout, a path you cannot resolve — fails this condition.

```sh
kill <pid>          # SIGTERM only
```

**Never `kill -9`.** `lock.sh` holds the lock on a descriptor precisely so that a killed
holder frees its slot, and SIGTERM lets `cf serve` close its listener and its scratch dir
on the way out; `-9` leaves the port bound and the scratch dir behind, and the next e2e
run fails on the port instead of on the lock. Note every kill in the report, with the pid,
the command line and the worktree — a slot you reclaimed wrongly is only diagnosable from
that line.

If any of the four conditions is unmet, leave it. A gate that waits is the machine being
shared; a gate holder killed in error is an agent losing 90 minutes.

## 6. Scratch directories

`.testrun*` and `test-results` directories left **inside a path that is no longer a
registered worktree** are dead: the run that made them is gone and no agent will look at
them again. Inside a live worktree they are somebody's current run — leave them.

```sh
live="$(git -C "$ROOT" worktree list --porcelain | sed -n 's/^worktree //p')"
find "$ROOT/.worktrees" -maxdepth 2 \( -name '.testrun*' -o -name 'test-results' \) -print |
  while read -r d; do
    case $'\n'"$live"$'\n' in *$'\n'"${d%/*}"$'\n'*) continue ;; esac   # parent is a live worktree
    echo "$d"
  done
```

Remove by explicit path, one at a time. Never `git clean`, anywhere, for any reason: it
destroys uncommitted work belonging to agents you cannot see.

## 7. Report

One comment on #80, and nothing else — no commit, no push:

```sh
gh issue comment 80 --body-file - <<'EOF'
janitor — <YYYY-MM-DDTHH:MMZ>
worktrees removed: <name>, … | none
worktrees left: <name> <reason: occupied | issue open | issue parked | branch on origin | git refused: untracked files>, … | none
branches deleted: <branch>, … | none
labels cleared: #<n> <labels>, … | none
gate slots reclaimed: pid <n> <command> · <worktree>, … | none
gate slots left: pid <n> <which condition failed>, … | none
scratch dirs removed: <n> | none
skipped for budget: <which sweep> | none
EOF
```

The `left` lines matter more than the removed ones: a thing this routine keeps refusing to
collect, run after run, is something the rules do not cover, and the record of the refusal
is the only way that ever gets noticed.

## What you never touch

- Any issue labelled `in-progress` or `handed-back`, or its branch, worktree or labels.
- Any **open** issue's worktree or branch, including a `parked` one — parked work is
  resumed from exactly those.
- A `land-*` worktree, ever. `land.sh` owns it and may be inside it right now.
- A `driver-*` worktree less than 6 hours old.
- A worktree, branch or path whose name you cannot resolve to a closed issue — `qa-*`,
  a human's checkout, anything unrecognised.
- Any process that is not a `cf serve` reparented to pid 1 in a dead worktree's cwd; and
  never with `kill -9`.
- `main`, any remote branch, any push, any `git clean`, any `--force`, any `rm -rf` of a
  registered worktree.
- Issue bodies, titles, severities, `verified`, `brief-ready`, and issue state: you never
  open or close an issue.
