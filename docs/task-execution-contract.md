# Task Execution Contract

**Read this before you touch anything.** You have been handed one task from
the GitHub Issues backlog (`gh issue view <n>`), described by a brief in
`docs/tasks/CF-NNN-<slug>.md`. The brief says
*what*. This says *how you work* — and it binds you whatever tool spawned you.

You are one of several agents in this repository right now. Others hold their own
worktrees, their own branches, and their own servers, and none of you can see the
others. Everything below exists because two agents once collided in a way that cost
real work.

Nothing in this contract waits for an answer. Nobody is there to ask: where a decision
is needed, it says which conservative choice to make.

Read in this order: this file, `AGENTS.md`, then your brief.

---

## 1. You get a worktree. Always.

Never work in the shared checkout — and that binds the drivers that dispatch you as much
as it binds you. Another session commits whatever is in that tree — including your
half-finished edits, under its own commit message.

```sh
ROOT="$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")"
git fetch origin
git worktree add "$ROOT/.worktrees/CF-041" -b CF-041-preview-depth origin/main
cd "$ROOT/.worktrees/CF-041"
```

Branch from `origin/main`, not from local `main`: local is usually stale within
minutes. `ROOT` is the main checkout wherever you start, so worktrees never nest.
Everything you do happens inside that directory. If you find yourself
editing a path that does not start with your worktree, stop — you are in someone
else's tree.

**Resuming a pushed branch.** When your issue was parked or taken over, the branch in
your prompt already exists on origin (`git rev-parse --verify --quiet
refs/remotes/origin/<branch>` prints a sha after the fetch). Start from it, in a path and
local branch suffixed with your driver id, because the earlier worktree and local branch
may still exist and belong to someone else:

```sh
git worktree add "$ROOT/.worktrees/CF-041-<driver-id>" -b CF-041-preview-depth-<driver-id> origin/CF-041-preview-depth
cd "$ROOT/.worktrees/CF-041-<driver-id>"
```

Use the same suffixed form for new work when `git worktree add` reports that the path or
the branch already exists, and append `-2`, `-3`… if that exists too. Whatever your local
branch is called, you push to the branch named in your prompt (§6).

Do not create, move, or delete any other worktree. Do not run `git clean` anywhere,
in any tree, for any reason: it destroys uncommitted work belonging to agents you
cannot see. Remove files by explicit path only.

## 2. Ports

`AGENTS.md` §2 is the contract. From inside a worktree it mostly takes care of
itself, because the e2e harness hashes `git rev-parse --show-toplevel` into a port
and a scratch dir — a different worktree is automatically a different port.

| Port | Owner | You |
|---|---|---|
| 8080 | the human's `cf serve` | **never**, under any circumstance |
| 8090 | the UX tester | never |
| 18000–27999 | e2e, one per worktree, derived from your path | yours, automatically |
| 28000–37999 | demo recorder | never |

If you must pin a port, set `CF_E2E_PORT` and say so in your handover. If you need a
canvas to look at, start it on your own port with your own scratch dir — never on
8080, and never against the human's blueprint.

The kind cluster (`make cluster`) is shared. Isolation there is by namespace
`cf-<slug>` and `--group-suffix=w<hash>.cf-test`, already wired into the scripts.
Do not tear the cluster down; another agent is probably using it.

## 3. Test first, and watch it fail

**Never tick a backlog item without an automated test that fails without the change**
(`AGENTS.md` §4). The brief hands you that test verbatim.

1. Write the acceptance test exactly as the brief gives it.
2. Run it. **Watch it fail.** Keep the output — it goes in your handover.
3. Now write the implementation.
4. Run it again. Keep that output too.

A test that passes the first time means the brief is wrong, the bug is already
fixed, or you wrote the test wrong. All three are findings. Stop implementing, push
what exists, and **park** with the finding as your handover comment (§7, reason
`finding`); do not adjust the test until it fails, and do not proceed on the assumption
that passing is good news.

The test is verbatim; the implementation is yours. The brief's `Contract` section
says what must be true, deliberately not how. If satisfying it seems impossible, or
the brief contradicts what you find in the code, **park with the conflict as the
finding rather than choosing silently** — the same way, reason `finding`. An agent that
surfaces a brief's gaps is the loop working correctly; one that quietly picks an
interpretation costs a review cycle to discover.

## 4. Gates

Green before you hand back. Every task, without exception:

```sh
make lint          # gofmt over tracked files, go vet
make lint-strict   # staticcheck, at the pinned version
make test-race     # go test -short -race -count=1
```

Then whatever your change actually reaches:

```sh
make test-docker   # acceptance tests — needs Docker and the crossplane CLI
make test-e2e      # Playwright — if you touched web-proto/ or tests/
make test-cluster  # Lane C, the round-trip gate — if you touched emit or import
```

`make test-e2e` starts its own engine on your worktree's own port. Run it from your
worktree, never from the shared checkout.

`make test-race`, `make test-e2e` and `make test-docker` share a machine-wide pool of gate
slots with every other agent (`AGENTS.md` §2). When every slot is taken they print
`lock.sh: waiting for gate` and wait: that is the machine being shared, not a hang. Never
bypass it — no `CF_GATE_SLOTS=off`, no other `GATE_SLOTS`, no killing a holder. Once a slot
is yours the gate prints `lock.sh: gate acquired after <n>s`; keep that line for your
handover.

If a gate fails for a reason unrelated to your change, say so in the handover with
the output. Do not fix it in this branch — that is a second task, and merging two
tasks in one branch is how a revert becomes impossible.

## 5. Git hygiene

- **Stage explicitly.** Never `git add -A`, never `git add .`. Name every path. This
  rule was written twice, after a sweep committed another agent's files and after
  one committed worktree gitlinks.
- **`gofmt -w`** every Go file you touched, before you commit.
- **No AI attribution.** No `Co-authored-by:` trailer, no "generated by" line, no AI
  mention in a commit message or a code comment. Commit messages are ordinary,
  professional, and describe the change.
- **Commit the moment your tests are green.** Uncommitted work in this repository
  gets absorbed by someone else's sweep.
- **Preserve comments and docstrings** you did not come to change.

## 6. What you do and do not do

- **Push your own topic branch after every green commit** — the branch named in your
  prompt, and nothing else:

  ```sh
  git push origin HEAD:refs/heads/<branch>
  git rev-parse HEAD        # note it: the sha you last pushed
  ```

  For a resumed branch, the sha you started from counts as the sha you last pushed. After a
  rebase — and only then — force with an explicit lease on that sha:

  ```sh
  git push --force-with-lease=refs/heads/<branch>:<sha you last pushed> origin HEAD:refs/heads/<branch>
  ```

  The explicit sha matters: remote-tracking refs are shared by every worktree of the clone,
  so a bare `--force-with-lease` can overwrite a push you never saw. Never push `main`,
  never another branch. A rejected push means someone else pushed your branch: never force
  past it. Re-read your claim (§7); if it is still yours, `git fetch origin`,
  `git rebase origin/<branch>`, rerun your tests and push again; if the rebase conflicts,
  `git rebase --abort` and park (reason `push-rejected`).
  An older brief's Handover section may say "not pushed": this contract supersedes it.
- **Do not merge, and do not touch `main`.** Only `scripts/driver/land.sh`, run by a
  driver, puts a commit on `main` (`AGENTS.md` §4, One Merge at a Time). You hand back a
  pushed branch; the driver lands it and owns its CI.
- **Do not claim, close, label or comment on any issue** except the handover or park
  comment and the one label change on your own issue (§7). The driver claimed your issue
  through `scripts/driver/claim.sh` before dispatching you, and closes it after `land.sh`
  reports `LANDED`. A close from you claims a landing that has not happened.
- **Do not fix adjacent bugs.** Note them in the handover. A brief's `Out of scope`
  section is a boundary, not a suggestion.
- **Do not edit another task's brief**, another agent's files, or any file outside
  your brief's `May write` list without saying so in the handover.

## 7. Handover — on the issue

You hand back when you are green, and park when you are not: at your prompt's time cap,
with gates red, with a finding (§3), or with a push you could not complete (§6). Both
happen on your own issue and nowhere else.

**First, confirm the issue is still yours.** Leases are never renewed: after 120 minutes
another driver may take your issue over, onto the same branch name. Print the newest claim
by a project member — once when you start (keep the line), and again right before you hand
back or park:

```sh
gh issue view <n> --json comments --jq '[.comments[] | select((.authorAssociation // "OWNER") | IN("OWNER", "MEMBER", "COLLABORATOR")) | (.body // "") | (split("\n")[0] // "") | select(startswith("taking —"))] | last'
```

It must still be the line you started under, naming your branch and your driver id. If it
is not, you were superseded: push your branch (§6; if the push is rejected, do not force
it), comment `superseded — <branch> · driver <driver-id> · <sha> <pushed|not pushed> ·
worktree <absolute path>` on the issue, and change no labels.

**Otherwise, hand back.** Post one comment, then change the label:

```sh
gh issue comment <n> --body-file - <<'EOF'
handover — <branch>
Worktree: <absolute path of your worktree>
...
EOF
gh issue edit <n> --add-label handed-back --remove-label in-progress
```

The comment, after those two lines, contains:

- **The failing run and the passing run** of the acceptance test, both pasted in
  full. This is the load-bearing part; a claim that a test "now fails without the
  fix" is not accepted untested, and a driver will not land a handover without it.
- **Every gate you ran**, with its result and its `lock.sh: gate acquired after <n>s`
  line where it printed one. Name what you did not run, and why.
- **Every judgement call** where the brief was silent or wrong, and what you chose.
- **Anything you noticed and did not fix**, so it can be filed.
- **Anything left undone** of what the issue names, or `none`.

**Parked** is the same, with the first line `parked — <branch> · <reason>` — `time-cap`,
`gates-red`, `finding` or `push-rejected` — the exact state in the comment (which test
fails, what is left, the finding in full), and
`gh issue edit <n> --add-label parked --remove-label in-progress`.

No other issue edits. Leave the worktree in place; the driver that claimed your issue
removes it once the issue is closed or parked.

---

*Producing tasks rather than executing one? That is
`.claude/skills/backlog-authoring/` — the other half of this contract.*
