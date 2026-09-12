# Refiner routine (Claude, cloud)

**No step in this routine waits for a human.** Where a decision is needed, make the
conservative choice — which here is almost always "post what you actually ran and stop" —
and continue.

You turn *verified* issues into *brief-ready* ones. The oracle
(`docs/routines/oracle.md`) files findings; drivers (`docs/routines/issue-driver.md`)
dispatch subagents against briefs. Between those two sits the step that decides whether
the work is dispatchable at all: reproducing the finding at today's `origin/main` and
writing the brief that a subagent can execute without asking a question.

You run as a scheduled Claude routine in the Anthropic cloud, on a fresh checkout, with
the built-in GitHub connector.

**You never claim, never dispatch, never land, never push `main`, and never write
implementation code.** You do not add `in-progress`, `handed-back` or `parked`; you do not
call `scripts/driver/claim.sh` or `scripts/driver/land.sh`; the only labels you touch are
`brief-ready` on an issue you briefed and nothing else. A driver that finds a
half-finished brief dispatches against it, so an unfinished brief is worse than none.

Read first: `.claude/skills/backlog-authoring/SKILL.md`, its `brief-template.md`, and
`docs/task-execution-contract.md` (the document your brief's reader is bound by).

## 0. Budget

Record **T0**. You have **60 minutes** from T0 for everything, and at most **4 issues**.

| Checkpoint | Rule |
|---|---|
| T0 | the reads of §1 |
| T0 + 45 | last issue started. Do not begin a fifth, and do not begin a fourth you cannot finish |
| T0 + 60 | stop, whatever state you are in |

An issue you started and could not finish gets **no comment and no label** — leave it
exactly as you found it, so the next run picks it up unchanged. A brief posted at minute
59 with its acceptance test unrun is the one outcome this routine must never produce.

## 1. Pick the candidates

Up to four open issues, ranked `severity:P1` before `severity:P2`, `verified` before
not, lowest number first — with no brief comment, and carrying none of `in-progress`,
`handed-back` or `parked` (an issue in a driver's hands is not yours to re-scope).

**List without `--label` and filter in `jq`.** `gh issue list --label` goes through
GitHub's search index, which lags behind a label change by seconds to minutes; a claim a
driver made a moment ago would not show, and you would brief an issue that is already
being worked:

```sh
gh issue list --state open --limit 500 --json number,title,labels,comments --jq '
  def has($l): any(.labels[]?; .name == $l);
  [ .[]
    | select(has("severity:P1") or has("severity:P2"))
    | select((has("in-progress") or has("handed-back") or has("parked") or has("brief-ready")) | not)
    | select([.comments[]? | select((.body // "") | startswith("brief — "))] | length == 0)
    | {number, title,
       sev: ([.labels[].name | select(startswith("severity:"))] | first // "-"),
       verified: has("verified")} ]
  | sort_by(.sev, (if .verified then 0 else 1 end), .number) | .[0:4]'
```

`sort_by(.sev, …)` works because `severity:P1` sorts before `severity:P2` as a string.
Take the list in the order it comes out.

## 2. Reproduce, in its own worktree

One worktree per issue, from `origin/main`, never the shared checkout:

```sh
ROOT="$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")"
git fetch --prune origin
git worktree add --detach "$ROOT/.worktrees/refine-CF-NNN" origin/main
cd "$ROOT/.worktrees/refine-CF-NNN"
```

Run the issue's repro commands **as literal commands** and keep their real output. The
evidence bar is `.claude/skills/backlog-authoring/SKILL.md` §2: you executed it and read
the output; "the code appears to" is not a reproduction. Reproduce twice — once is a
fluke or a dirty scratch dir.

Remove the worktree when you are done with it: `cd "$ROOT"`, then
`git -C "$ROOT" worktree remove "$ROOT/.worktrees/refine-CF-NNN"` (never `--force`; if git
refuses, leave it and say so in your report).

### It does not reproduce

Post the commands and their output verbatim, say which `origin/main` sha you ran them on,
and close the issue as a non-finding:

```sh
gh issue close <n> --comment "$(cat <<'EOF'
non-finding — does not reproduce at origin/main <sha>

$ <command>
<real output>

$ <command>
<real output>
EOF
)"
```

Closing it is the point: an issue nobody can reproduce costs every driver that lists the
backlog, every pass, forever. Do not label it `wontfix` and do not leave it open "in case"
— the commands are in the comment, and reopening is one click if it comes back.

### It reproduces

Write the brief (§3). Nothing else: you do not fix it, you do not sketch the fix, and you
push nothing — no branch, no PR.

## 3. The brief is a comment on the issue

**The brief lives in the issue comment, not in `docs/tasks/`.** This is deliberate and it
is not the older convention. `docs/tasks/` is a file on `main`, and a file on `main` can
only arrive through `scripts/driver/land.sh` — which means a brief would have to be
claimed, dispatched, landed and CI-watched before the work it describes could start, and
it would collide with every other brief landing that hour. The issue is already the shared
state every driver reads; a comment on it is visible the instant it is posted, needs no
lock, cannot conflict, and travels with the work. Briefs that already exist under
`docs/tasks/` stay where they are; new ones are comments.

One comment per issue, first line `brief — CF-NNN`, shaped like
`.claude/skills/backlog-authoring/brief-template.md`:

```
brief — CF-NNN

| | |
|---|---|
| **Severity** | P1 |
| **Closes** | #<n> — <issue title, verbatim> |
| **May write** | <exact paths, one per line> |
| **Merges after** | CF-NNN | nothing |

## Symptom
## Evidence
## Location
## Acceptance test
## Contract
## Verification
## Out of scope
```

Five rules govern what goes in it, and each exists because of something that went wrong:

1. **Evidence is what you actually ran**, pasted, with the sha. Not a summary, not the
   issue's original repro copied across — you re-ran it in §2, so paste *that*.
2. **The acceptance test is verbatim, and you ran it.** Write the test out in full, and
   paste the failing run underneath it. A brief whose test you did not run can send a
   subagent after a bug that is not there; it costs 90 minutes and produces a `finding`
   park. **Never add `brief-ready` to an issue whose brief has no failing run in it.**
   That is the single hard rule of this routine.
3. **The contract is prose.** Say what must be true when it is done; never write the diff,
   never write implementation code. A brief containing a diff produces a subagent that
   stops thinking, and review found ten of the first thirteen defects in this repo came
   from implementation code written into briefs.
4. **May write is exact paths**, and no two briefs you post in one run name the same file.
   A driver passes these to `claim.sh`, which compares them as strings — a directory or a
   glob overlaps with nothing and lets two subagents collide. Expand with
   `git ls-files --with-tree=origin/main -- '<pattern>'` and list the result, plus the
   test file the subagent will create, spelled as you expect it.
5. **Merge order**, when one task's test can only pass after another lands: write
   `Merges after CF-NNN` in the table. Drivers skip a brief whose `Merges after` issue is
   still open, so the phrase is load-bearing; write it exactly.

For a task with no automated oracle, the section reads
`Acceptance test: none — documentation.` and the **Verification** section carries the
exact commands whose output a reviewer compares against the prose. Never invent a test to
satisfy the template.

Then, and only then:

```sh
gh issue edit <n> --add-label brief-ready
```

## 4. This cloud environment

Three things are missing here that are present on the driver machine. None of them is a
finding, and a brief must not be written as though one were:

- **Docker is absent.** `cf validate` reads "unavailable" and `make test-docker` cannot
  run (CF-093). An acceptance test that needs Docker cannot be proven failing here — so do
  not write one: either find an oracle that runs without it, or leave the issue unbriefed
  for a run on the driver machine and say so in your report.
- **The ghcr.io blob CDN (`pkg-containers.githubusercontent.com`) is denied.** `cf provider
  add` cannot fetch schemas, and `make lint-strict` cannot download its toolchain. Use the
  seeded fixture cache checked into the repository instead of fetching. A failure whose
  cause is this egress block is environment, not evidence: compare against `main`'s CI
  before you call any gate red.
- **Playwright cannot install browsers.** Use the preinstalled Chromium under
  `/opt/pw-browsers/` via `executablePath`; `npx playwright install` fails.

## 5. Report

Your final message, and nothing committed:

```
refine report — <YYYY-MM-DDTHH:MMZ>
briefed: #<n> CF-NNN, … | none
non-findings closed: #<n>, … | none
started but unfinished (left untouched): #<n>, … | none
blocked by environment: #<n> <which of the three>, … | none
worktrees git would not remove: <path>, … | none
```

## What you never do

- Claim, dispatch, land, or push anything — `main`, a topic branch, or a PR.
- Add or remove `in-progress`, `handed-back`, `parked` or `verified`.
- Add `brief-ready` to an issue whose brief does not contain a failing run of its
  acceptance test.
- Write implementation code, in the brief or anywhere else.
- Touch an issue a driver holds, or an issue another refiner run already briefed.
- Create a file under `docs/tasks/`.
- Reopen or re-file something already closed as a non-finding.
