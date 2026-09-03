# CF-NNN — <one sentence naming the wrong behaviour>

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P0 / P1 / P2 / P3 |
| **Closes** | the `BACKLOG.md` line, quoted verbatim |
| **Worktree** | `.worktrees/CF-NNN` on branch `CF-NNN-<slug>` |
| **May write** | the only files this task is allowed to change |
| **Merges after** | `CF-NNN`, or `nothing` |

## Symptom

What a user observes, in their terms. Not the mechanism - the consequence. If the
tool exits 0 while doing the wrong thing, say so here: that is the whole defect.

## Evidence

The repro, as literal commands, and the output they actually produced when the
author of this brief ran them. Verbatim, not summarised.

```sh
$ ./bin/cf ...
<real output>
```

Reproduced N times, on `<commit>`.

## Location

`path/to/file.go:NN` — what is there now and why it produces the symptom. Only files
the author actually read. Line numbers drift; confirm before you trust them.

## Acceptance test

Write this test **first**, verbatim, and watch it fail before you change any
production code. It is the definition of done; do not paraphrase it, do not weaken
an assertion to make it pass, and do not delete it if it turns out to be
inconvenient - if it is wrong, say so in the handover and stop.

```go
// path/to/file_test.go
func TestCFNNN...(t *testing.T) {
    ...
}
```

**Fails today with:**

```
<the failure output the brief author saw, or: "not run by the brief author">
```

*(For tasks with no automated oracle, replace this whole section with
`Acceptance test: none — documentation.` and fill in Verification below.)*

## Contract

What must be true when this is done. Prose, no code. State the behaviour, the error
path, and anything that must stay byte-identical. If you find the contract cannot be
satisfied as written, that is a finding - report it in the handover rather than
choosing silently.

## Verification

The exact gates to run, in order. Every task runs `make lint` and `make test-race`;
list here anything further this change reaches.

```sh
make lint && make test-race
make test-e2e      # only if this touches web-proto/ or tests/
```

## Out of scope

Named explicitly, so a wider fix is a conversation instead of a merge conflict.
Adjacent bugs you notice belong in a handover note, not in this branch.

## Handover

Branch `CF-NNN-<slug>`, committed, not pushed, not merged. In your final report:
the failing run and the passing run of the acceptance test, both pasted; every gate
you ran; every judgement call you made where the brief was silent.
