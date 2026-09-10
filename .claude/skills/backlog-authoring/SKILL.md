---
name: backlog-authoring
description: Use when scanning or auditing the codebase to file work - turning what you found into ID'd GitHub issues and, when a task is dispatched, a self-contained brief a subagent can execute without asking a single question. Not for executing a task (that is docs/task-execution-contract.md), and not for judging the canvas as a user (that is canvas-ux-tester).
---

# Backlog Authoring

You are the oracle, not the driver. Your output is not a fix - it is a claim about
this codebase that is true, reproducible by someone who was not here, and small
enough to hand to one agent.

Two artifacts, written at different times:

| | Written | Cost | Read by |
|---|---|---|---|
| **An issue** | when you find it | every agent that lists the backlog | humans choosing what is next |
| **A task brief** | when it is dispatched | only the subagent that gets it | one subagent, once |

The backlog is GitHub Issues in `koorikla/composition-factory` (`AGENTS.md` §4). Every
agent lists it, so an issue body is read many times: keep it short and put the depth
in the brief - and write the brief only when the work actually goes out. Most issues
never become briefs, and that is the point.

## 1. Before you file anything

This checkout is shared. Another session commits whatever is in the working tree and
edits files between your tool calls.

```sh
git fetch && git log --oneline HEAD..origin/main    # local state goes stale in minutes
git status --short                                  # someone else's live WIP
git diff --stat                                     # what they are already fixing
```

**A defect already being fixed in the dirty tree is not a finding.** Check the diff
before you write the line, not after. Then search closed issues (`gh issue list --state all --search "<keyword>"`),
`docs/backlog-archive.md` (pre-migration history) and `docs/non-findings.md` - re-raising
a settled item costs a reviewer the whole round trip to work out it is settled.

## 2. The evidence bar

You ran it. Not "the code appears to", not "this would presumably" - you executed
something and read the output.

- **Reproduce twice.** Once is a fluke or a dirty `.testrun` dir.
- **Name the mechanism.** `internal/emit/composition.go` discards the `warnings`
  slice is a finding. "warnings seem unreliable" is a complaint.
- **Quote the observed output**, not your summary of it.
- Mark **[V]** only what you re-verified by hand, independently of the run that
  first surfaced it. `[V]` is a promise to the reader that they can skip re-checking.
- If a static gate already catches it (`gofmt`, `vet`, `staticcheck`, race, the Go
  suite, the Playwright suite), it is not a backlog item - it is a broken build.
  Every item you file must live in behaviour those gates do not reach.

**Not findings, and do not file them:** feature wishes ("it should also do X"),
anything you could not reproduce, aggregate verdicts ("the API is inconsistent"),
style preferences, and refactors nothing in the current goal touches. A finding names
one moment where the tool did the wrong thing.

## 3. Severity

| | Meaning |
|---|---|
| **P0** | Emits wrong output, or dies, and says nothing. The user cannot tell they were harmed. |
| **P1** | Loss that survives to the cluster: a round-trip drop, a silent downgrade, an inert declared field. |
| **P2** | Works, but only with knowledge that exists solely in the source - or an API contract an agent cannot use safely. |
| **P3** | Documentation and polish. The behaviour is right; the record of it is not. |

Silence is what moves severity up. An emitter that refuses loudly is P2; the same
emitter exiting 0 on the same input is P0.

## 4. The issue

```sh
gh issue create \
  --title "CF-041 — One sentence naming the wrong behaviour." \
  --label "severity:P1,scale:engine,verified" \
  --body "$(cat <<'EOF'
The mechanism, in two or three sentences, with `file.go:NN` and a literal repro.
What it costs the user. What the fix must achieve - never how to write it.
EOF
)"
```

Labels are the triage: exactly one `severity:P0..P3` (or `lane:<name>` for a lane item),
exactly one of `scale:engine` / `scale:ux`, `verified` only when §2's bar is met, and
`brief-ready` once a brief exists. Keep the title under 110 characters; the full
sentence goes first in the body.

**IDs are permanent.** The next free one is one more than the highest `CF-NNN` across
open and closed issue titles, `docs/tasks/`, and `docs/backlog-archive.md`:

```sh
{ gh issue list --state all --limit 500 --json title --jq '.[].title'; ls docs/tasks; grep -oE 'CF-[0-9]{3}' docs/backlog-archive.md; } | grep -oE 'CF-[0-9]{3}' | sort -u | tail -1
```

Never reuse a number, including one whose item was closed or reclassified as a
non-finding - a stale link that resolves to the wrong task is worse than one that
resolves to nothing.

## 5. Promoting a line to a brief

Only when it is going out. Copy `brief-template.md` to
`docs/tasks/CF-041-<slug>.md` and fill every field; a field you cannot fill is a
finding about your own evidence, not a field to delete.

Two rules govern what you put in it, and they exist because of what went wrong before:

**Acceptance test verbatim.** Write the test code out in full. It pins the behaviour,
and it is the only part of the brief the implementer must not paraphrase. Of the
first ~13 defects review found in this repo, 10 came from implementation code written
into briefs; test code was largely sound.

**Implementation by contract only.** Say what must be true when it is done - never
the diff. You are not in the file, you have not seen what the implementer will see,
and a brief that dictates code produces an agent that stops thinking.

**Then prove your own test fails.** Run it before you ship the brief and paste the
failure into the brief. A test asserting behaviour that already works turns a
subagent loose on a bug that is not there. If you cannot run it, say so in the brief
in those words - do not imply a run you did not do.

Some tasks have no automated oracle - documentation, a doc/code mismatch, a fixture
you can only judge by reading. Those say `Acceptance test: none - documentation` and
carry a **verification** section instead: the exact commands whose output the
reviewer compares against the prose. Never invent a test to satisfy the template.

## 6. Fan-out: overlap and merge order

When you dispatch more than one brief at a time, you own the collision they cannot
see. Each subagent works alone in a worktree and cannot know what the others touch.

Before dispatching a set, write in each brief:

- **Files this task may write.** Two dispatched briefs must not name the same file.
  If they do, either merge them into one task or dispatch them in sequence - never
  concurrently, and never with a note asking the agents to "coordinate".
- **Merge order**, when one task's test only passes after another lands. State it as
  `merges after CF-039`, in both briefs.

Order the set so the task that turns prose into a failing test goes first. A gate
widened before the bugs it catches are fixed converts eight paragraphs of backlog
into eight red tests, and red tests do not go stale.

## 7. Handing off

Antigravity does not read `.claude/skills/`. What reaches a subagent is the task
prompt and the files in the repo, so give it exactly three things:

```
Task CF-041. Brief: docs/tasks/CF-041-<slug>.md
Execution contract: docs/task-execution-contract.md (read it first)
```

Everything else it needs - isolation, ports, gates, handover - is in the contract,
which `AGENTS.md` §4 also points at. If you find yourself adding a fourth line of
instruction, that instruction belongs in the brief or in the contract, not in a
prompt that vanishes when the session ends.

## 8. After it lands

You do not close issues and you do not merge. When the driver merges a task they close
the issue with the merge commit and the guarding test named in the closing comment
(`AGENTS.md` §4). The brief in `docs/tasks/` stays: it is the record of what was asked,
and it is the only thing that makes the merge reviewable a month later. A half-fix
does not close the issue: file the residue under a new id and close the original
pointing at it.

## Traps

- **Filing what another session is already fixing.** The tree is dirty with someone
  else's work most of the time. `git diff --stat` first, every time.
- **A brief written at find-time.** By dispatch, the file has moved, the line
  numbers are wrong, and the bug may be fixed. Write briefs when work goes out.
- **Writing the fix into the brief.** The strongest signal you have overstepped is
  that your brief contains a diff. Delete it and state the contract.
- **A verbatim test you never ran.** It is the load-bearing part of the brief. An
  unrun test that passes today sends an agent hunting a bug that does not exist.
- **`[V]` on the run that found it.** `[V]` means a second, independent check.
  Applying it to your own first run makes the marker worthless for everyone.
- **Long issue bodies.** Every agent that lists the backlog pays for them. Depth
  belongs in the brief.
- **Growing the backlog with items no one will ever pick.** A backlog nobody
  triages is a document, not a queue. If it does not deserve a brief within the
  month, close it as `wontfix` with one line saying why.
