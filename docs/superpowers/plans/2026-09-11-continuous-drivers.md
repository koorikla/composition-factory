# Continuous Drivers Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (or
> superpowers:subagent-driven-development) to implement this plan task-by-task.

**Goal:** Let many Antigravity drivers run around the clock without colliding, by moving all
shared state into GitHub issues and serializing the three racy operations behind kernel locks.

**Architecture:** Three bash scripts — `lock.sh` (lockf descriptor-form slot pools),
`claim.sh` (lease + file-overlap claims on issues) and `land.sh` (squash, push, watch CI,
revert on red) — each a single process holding its lock for its whole critical section. The
`Makefile` routes heavy gates through the `gate` pool. The driver routine, the execution
contract and `AGENTS.md` are rewritten around them.

**Tech Stack:** `/bin/bash` 3.2 (macOS; no associative arrays, no `mapfile`), `lockf(1)`,
`jq` 1.8, `gh` 2.100, git 2.50.

**Spec:** [`docs/superpowers/specs/2026-09-11-continuous-drivers-design.md`](../specs/2026-09-11-continuous-drivers-design.md)
— read it first. Where this plan and the spec differ, this plan wins; §"Spec amendments"
lists every difference and Task 9 folds them back into the spec.

---

## How to read this plan

**Tests are verbatim. Implementation is by contract.** Every test file below was run on
2026-09-11 before this plan was written:

- **Red:** 27/27 fail against `origin/main` for the right reason — exit 127 (script
  missing), a lock holder's marker never written, or the `Makefile` not routing a gate.
  Re-run from the files as embedded in this plan, after its last edit.
- **Passable:** 27/27 pass against a throwaway reference implementation, written only to
  prove no test is impossible, then discarded. Its code is deliberately not in this plan.
- **Sharp:** four regressions were injected into that reference, and each was caught by
  the test that names it: `lockf` command form (the orphan test), landing without
  squashing, `claim.sh` ignoring `handed-back` holders, and legacy leases read from
  `updatedAt`.
- **Stable:** 5/5 full runs green with all 12 cores pinned by `yes`. Lock tests wait for
  holder markers, never for fixed sleeps.

Do not edit a test to make it pass. If a test looks wrong, stop and report — that is a
finding about this plan.

**Work in a worktree** (`docs/task-execution-contract.md` §1):

```sh
git fetch
git worktree add .worktrees/continuous-drivers -b continuous-drivers origin/main
cd .worktrees/continuous-drivers
```

Commit messages: plain and professional, no AI attribution (`AGENTS.md` §4). Stage by path.

---

## Environment seams (shared by every script)

Tests drive these; drivers leave them unset. Every script must honour exactly these names.

| Variable | Default | Used by | Meaning |
|---|---|---|---|
| `CF_LOCK_DIR` | `$(git rev-parse --git-common-dir)/cf-locks` | lock | slot files |
| `CF_LOCK_RETRY_SEC` | `10` | lock | sleep between acquisition sweeps (fractional ok) |
| `CF_LOCK_TIMEOUT_SEC` | unset (wait forever) | lock | give up after N s: stderr `lock.sh: timed out waiting for <pool>`, exit **75** |
| `CF_LOCKF_BIN` | `lockf` | lock | when not executable/found: stderr `lock.sh: lockf not found; running <pool> unlocked`, run the command directly |
| `CF_GATE_SLOTS` | unset | lock | `off` runs pool `gate` directly; **never** affects any other pool |
| `CF_NOW` | `date +%s` | claim | clock, epoch seconds |
| `CF_LEASE_MIN` | `120` | claim | lease length |
| `CF_LAND_GATES` | see Task 6 | land | shell command string run in the rebased worktree |
| `CF_CI_POLL_SEC` | `10` | land | wait between `gh run list` polls for the pushed sha |

## `gh` invocations (the fake in Task 2 honours exactly these)

Scripts parse JSON with `jq`; never `gh … --jq`. Argument order matters where noted.

| Call | Used by |
|---|---|
| `gh issue view <n> --json number,title,state,labels,comments,updatedAt` | claim, land |
| `gh issue list --state open --label <label> --limit 200 --json number,labels,comments,updatedAt` | claim |
| `gh issue edit <n> --add-label <l> --remove-label <l>` — one label per flag | claim |
| `gh issue comment <n> --body <text>` — `--body` must be the 4th argument | claim |
| `gh run list --branch main --commit <sha> --json databaseId,url` | land |
| `gh run watch <id> --exit-status` | land |
| `gh run view <id> --json jobs` | land |
| `gh run rerun <id> --failed` | land |

## Spec amendments (found while verifying the tests)

1. **`handed-back` issues hold their files regardless of lease** until landed or parked —
   a finished branch awaiting its merge still owns what it changed.
2. **A bare `in-progress` label with no `taking —` comment** is leased from the issue's
   `updatedAt` + lease, not from the label event (avoids the timeline API).
3. **`lock.sh` reports its wait on acquisition** (`lock.sh: <pool> acquired after <n>s`),
   not on exit — it `exec`s the command, so there is no exit to report from.
4. **`claim.sh`** adds exit 2 `REFUSED closed|wontfix|handed-back` and `--dry-run`
   (all reads and checks, no writes, output `CLAIMED (dry run)`).
5. **`land.sh`** adds exit 6 `PARKED push-rejected` and exit 8 `REVERTED-RED <run-url>`
   (the revert's own CI run failed — `main` is red and needs a human).
6. **Resuming a `parked` issue is not a takeover**; `takeover of <driver>` appears only
   when the claimed issue carried `in-progress` with an expired lease (`legacy` when that
   lease came from an old-style comment or a bare label).

---

## Task 1: Measure gate capacity

No code. The slot count and global cap come from these numbers (spec §6).

**Files:** none yet — results go into `docs/routines/issue-driver.md` §0 in Task 8.

**Step 1: Get an idle machine.** Ask Kaur to pause the Antigravity scheduled tasks for ~40
minutes. Confirm nothing heavy runs:

```sh
pgrep -fl 'go test|playwright|crossplane' || echo idle
```

Expected: `idle`. Do not measure on a busy machine — the numbers would bake contention in.

**Step 2: Time each heavy gate alone**, from the worktree:

```sh
/usr/bin/time -l make test-race   2> /tmp/gate-race.txt
/usr/bin/time -l make test-e2e    2> /tmp/gate-e2e.txt
/usr/bin/time -l make test-docker 2> /tmp/gate-docker.txt
grep -E 'real|maximum resident' /tmp/gate-*.txt
```

Record wall-clock and peak RSS per gate. Note `/usr/bin/time -l` reports the peak RSS of the
largest single process, not the tree; also watch Activity Monitor's memory pressure.

**Step 3: Find the slot count.** For N = 2, 3, 4: run N copies of `make test-e2e` and
`make test-race` interleaved (e.g. `test-e2e` in N worktrees at once — each hashes its own
port) and record: memory pressure stays green, e2e passes three runs in a row. `GATE_SLOTS`
= the largest N that holds.

**Step 4: Compute the global cap.** `cap = floor(GATE_SLOTS × 90 ÷ (2.5 × mean heavy-gate
minutes))`. Write down the inputs and the result.

**Step 5: Resume the schedule** and write the numbers into your handover. No commit.

---

## Task 2: Test harness and `make test-driver`

**Files:**
- Create: `scripts/driver/test/lib.sh`
- Create: `scripts/driver/test/fakebin/gh` (executable)
- Create: `scripts/driver/test/run.sh` (executable)
- Modify: `Makefile` — add `test-driver` to `.PHONY` and a target

**Step 1: Create the harness files verbatim.**

`scripts/driver/test/lib.sh`:

```bash
# shellcheck shell=bash
# Helpers for scripts/driver tests. Sourced by run.sh and every *_test.sh.
# Written for /bin/bash 3.2: no associative arrays, no mapfile.
set -u

DRIVER_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_DIR="$DRIVER_DIR/test"

fail() { echo "    FAIL: $*" >&2; return 1; }

assert_eq() { # expected actual message
  [ "$1" = "$2" ] || fail "$3: expected [$1], got [$2]"
}

assert_contains() { # haystack needle message
  case "$1" in *"$2"*) return 0 ;; esac
  fail "$3: [$2] not found in [$1]"
}

assert_not_contains() { # haystack needle message
  case "$1" in *"$2"*) fail "$3: [$2] unexpectedly present"; return 1 ;; esac
  return 0
}

# new_sandbox: a fresh temp dir, the fake gh first on PATH, a private lock dir,
# short lock retries, and a fixed clock (CF_NOW = 2026-09-11T06:00Z).
new_sandbox() {
  SANDBOX="$(mktemp -d "${TMPDIR:-/tmp}/cf-driver-test.XXXXXX")"
  export FAKE_GH_DIR="$SANDBOX/gh"
  mkdir -p "$FAKE_GH_DIR/issues" "$FAKE_GH_DIR/runs"
  : > "$FAKE_GH_DIR/calls.log"
  export CF_LOCK_DIR="$SANDBOX/locks"
  export CF_LOCK_RETRY_SEC=0.2
  export CF_NOW=1789106400
  export PATH="$TEST_DIR/fakebin:$PATH"
}

# iso_at MINUTES -> CF_NOW + MINUTES as YYYY-MM-DDTHH:MMZ (the lease format).
iso_at() {
  jq -nr --argjson t "$((CF_NOW + $1 * 60))" '$t | strftime("%Y-%m-%dT%H:%MZ")'
}

# issue_fixture NUMBER STATE LABELS [BODY MINUTES_AGO]...
# Writes the full issue object the fake gh serves. LABELS is comma-separated.
issue_fixture() {
  local n="$1" state="$2" labels="$3" comments='[]' at
  shift 3
  while [ $# -ge 2 ]; do
    at="$(jq -nr --argjson t "$((CF_NOW - $2 * 60))" '$t | strftime("%Y-%m-%dT%H:%M:%SZ")')"
    comments="$(jq -c --arg b "$1" --arg at "$at" '. + [{body: $b, createdAt: $at}]' <<<"$comments")"
    shift 2
  done
  jq -n --argjson n "$n" --arg s "$state" --arg l "$labels" --argjson c "$comments" \
    --arg u "$(jq -nr --argjson t "$((CF_NOW - 300 * 60))" '$t | strftime("%Y-%m-%dT%H:%M:%SZ")')" \
    '{number: $n, title: ("issue " + ($n | tostring)), state: $s, updatedAt: $u,
      labels: [$l | split(",")[] | select(. != "") | {name: .}], comments: $c}' \
    > "$FAKE_GH_DIR/issues/$n.json"
}

# wait_for FILE: poll until FILE exists (a holder's "I have the lock" marker).
# Fixed sleeps race on a loaded machine; markers do not.
wait_for() {
  local i=0
  while [ ! -s "$1" ]; do
    [ $i -ge 200 ] && { fail "timed out waiting for $1"; return 1; }
    sleep 0.05
    i=$((i + 1))
  done
}
```

`scripts/driver/test/fakebin/gh`:

```bash
#!/bin/bash
# Fake gh for scripts/driver tests. State lives in $FAKE_GH_DIR:
#   issues/<n>.json  full issue objects: number, title, state, updatedAt, labels, comments
#   ci-results       one line per `run watch`, consumed in order: green | red | e2e-red
#   calls.log        every invocation, one line each
# It honours only the invocations the driver scripts are contracted to make,
# ignores --json field lists (it always returns the whole object), and does not
# implement --jq: the scripts parse with jq.
set -u
D="${FAKE_GH_DIR:?FAKE_GH_DIR is not set}"
echo "$*" >> "$D/calls.log"

now_iso() { jq -nr --argjson t "${CF_NOW:-0}" '$t | strftime("%Y-%m-%dT%H:%M:%SZ")'; }
rewrite() { jq "$@" > "$f.tmp" && mv "$f.tmp" "$f"; }

case "$1 ${2:-}" in
"issue view")
  cat "$D/issues/$3.json"
  ;;
"issue list")
  shift 2
  state=open
  labels=""
  while [ $# -gt 0 ]; do
    case "$1" in
      --state) state="$2"; shift 2 ;;
      --label) labels="$labels,$2"; shift 2 ;;
      *) shift ;;
    esac
  done
  cat "$D"/issues/*.json | jq -s --arg state "$state" --arg labels "$labels" '
    ($labels | split(",") | map(select(. != ""))) as $want
    | [ .[]
        | select($state == "all" or (.state | ascii_downcase) == $state)
        | select([.labels[].name] as $have | all($want[]; . as $w | any($have[]; . == $w))) ]'
  ;;
"issue edit")
  f="$D/issues/$3.json"
  shift 3
  while [ $# -gt 0 ]; do
    case "$1" in
      --add-label)    rewrite --arg l "$2" '.labels = (.labels + [{name: $l}] | unique_by(.name))' "$f"; shift 2 ;;
      --remove-label) rewrite --arg l "$2" '.labels = [.labels[] | select(.name != $l)]' "$f"; shift 2 ;;
      *) shift ;;
    esac
  done
  ;;
"issue comment")
  f="$D/issues/$3.json"
  [ "${4:-}" = "--body" ] || { echo "fake gh: issue comment wants --body as its 4th argument" >&2; exit 2; }
  rewrite --arg b "$5" --arg at "$(now_iso)" '.comments += [{body: $b, createdAt: $at}]' "$f"
  ;;
"run list")
  shift 2
  sha=""
  while [ $# -gt 0 ]; do
    case "$1" in --commit) sha="$2"; shift 2 ;; *) shift ;; esac
  done
  [ -f "$D/runs/$sha" ] || echo $(( $(ls "$D/runs" | wc -l) + 1000 )) > "$D/runs/$sha"
  id="$(cat "$D/runs/$sha")"
  printf '[{"databaseId":%s,"url":"https://ci.example/runs/%s"}]\n' "$id" "$id"
  ;;
"run watch")
  result="$(head -n 1 "$D/ci-results")"
  tail -n +2 "$D/ci-results" > "$D/ci-results.tmp" && mv "$D/ci-results.tmp" "$D/ci-results"
  echo "$result" > "$D/last-result-$3"
  [ "$result" = green ]
  ;;
"run view")
  case "$(cat "$D/last-result-$3" 2>/dev/null)" in
    green)   echo '{"jobs":[{"name":"test","conclusion":"success"},{"name":"e2e","conclusion":"success"}]}' ;;
    e2e-red) echo '{"jobs":[{"name":"test","conclusion":"success"},{"name":"e2e","conclusion":"failure"}]}' ;;
    *)       echo '{"jobs":[{"name":"test","conclusion":"failure"},{"name":"e2e","conclusion":"success"}]}' ;;
  esac
  ;;
"run rerun")
  :
  ;;
*)
  echo "fake gh: unsupported invocation: $*" >&2
  exit 2
  ;;
esac
```

`scripts/driver/test/run.sh`:

```bash
#!/bin/bash
# Runs every test_* function in scripts/driver/test/*_test.sh, each in its own
# subshell with its own sandbox. lock_test.sh is skipped where lockf is absent.
set -u
cd "$(dirname "$0")" || exit 1
command -v jq >/dev/null || { echo "test-driver: jq is required" >&2; exit 1; }

pass=0 failed=0 skipped=0
for file in *_test.sh; do
  for t in $(bash -c ". ./$file >/dev/null 2>&1; declare -F" | awk '$3 ~ /^test_/ { print $3 }'); do
    if [ "$file" = lock_test.sh ] && ! command -v lockf >/dev/null; then
      echo "SKIP $file $t (no lockf)"
      skipped=$((skipped + 1))
      continue
    fi
    if ( . "./$file"; "$t"; rc=$?; cd /; rm -rf "${SANDBOX:-/nonexistent}"; exit $rc ); then
      echo "PASS $file $t"
      pass=$((pass + 1))
    else
      echo "FAIL $file $t"
      failed=$((failed + 1))
    fi
  done
done
echo "test-driver: $pass passed, $failed failed, $skipped skipped"
[ "$failed" -eq 0 ]
```

```sh
chmod +x scripts/driver/test/fakebin/gh scripts/driver/test/run.sh
```

**Step 2: Add the target.** In `Makefile`, add `test-driver` to the `.PHONY` line and:

```make
# Driver coordination scripts (scripts/driver). Lock tests need lockf, so they skip on Linux.
test-driver:
	./scripts/driver/test/run.sh
```

**Step 3: Run it.**

Run: `make test-driver`
Expected: `test-driver: 0 passed, 0 failed, 0 skipped` (no `*_test.sh` yet), exit 0.

**Step 4: Commit.**

```sh
git add scripts/driver/test/lib.sh scripts/driver/test/fakebin/gh scripts/driver/test/run.sh Makefile
git commit -m "test(driver): harness for the driver coordination scripts"
```

---

## Task 3: `scripts/driver/lock.sh`

**Files:**
- Create: `scripts/driver/test/lock_test.sh`
- Create: `scripts/driver/lock.sh` (executable)

**Step 1: Write the failing tests verbatim.**

```bash
#!/bin/bash
# scripts/driver/lock.sh — kernel-held slot pools shared by every worktree.
# Holders write a marker once they run, and callers wait_for it: fixed sleeps
# race on the loaded machine these locks exist to protect.
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
LOCK="$DRIVER_DIR/lock.sh"

test_one_slot_serializes() {
  new_sandbox
  local log="$SANDBOX/order.log"
  "$LOCK" p 1 -- sh -c "echo A-start >> '$log'; sleep 1; echo A-end >> '$log'" 2>/dev/null &
  wait_for "$log" || return 1
  "$LOCK" p 1 -- sh -c "echo B >> '$log'" 2>/dev/null
  wait
  assert_eq "A-start A-end B" "$(tr '\n' ' ' < "$log" | sed 's/ $//')" "a 1-slot pool must not run B inside A"
}

test_two_slots_admit_two() {
  new_sandbox
  local log="$SANDBOX/order.log"
  "$LOCK" p 2 -- sh -c "echo A-start >> '$log'; sleep 2; echo A-end >> '$log'" 2>/dev/null &
  wait_for "$log" || return 1
  "$LOCK" p 2 -- sh -c "echo B-start >> '$log'; echo B-end >> '$log'" 2>/dev/null
  wait
  assert_eq "A-start B-start B-end A-end" "$(tr '\n' ' ' < "$log" | sed 's/ $//')" "a 2-slot pool must run B while A holds a slot"
}

test_exit_status_propagates() {
  new_sandbox
  "$LOCK" p 1 -- sh -c 'exit 42' 2>/dev/null
  local rc=$?
  assert_eq 42 "$rc" "lock.sh must return the command's exit status"
}

test_kill9_holder_frees_slot() {
  new_sandbox
  "$LOCK" p 1 -- sh -c "echo held > '$SANDBOX/ready'; exec sleep 30" 2>/dev/null &
  local holder=$!
  wait_for "$SANDBOX/ready" || return 1
  kill -9 "$holder"
  wait "$holder" 2>/dev/null
  CF_LOCK_TIMEOUT_SEC=3 "$LOCK" p 1 -- true 2>/dev/null
  local rc=$?
  assert_eq 0 "$rc" "a slot must be free once its holder is killed with -9"
}

# The reason lock.sh uses lockf's descriptor form: with `lockf file cmd`, killing
# lockf releases the lock while its child runs on. A landing must never continue
# pushing main with the merge lock free.
test_orphaned_descendant_keeps_slot() {
  new_sandbox
  "$LOCK" p 1 -- sh -c "sleep 30 & echo \$! > '$SANDBOX/orphan.pid'; wait" 2>/dev/null &
  local holder=$!
  wait_for "$SANDBOX/orphan.pid" || return 1
  kill -9 "$holder"
  wait "$holder" 2>/dev/null
  CF_LOCK_TIMEOUT_SEC=1 "$LOCK" p 1 -- true 2>/dev/null
  local rc=$?
  kill "$(cat "$SANDBOX/orphan.pid")" 2>/dev/null
  assert_eq 75 "$rc" "a descendant that outlives its holder must keep the slot"
}

test_gate_bypass_applies_to_gate_pool_only() {
  new_sandbox
  "$LOCK" gate 1 -- sh -c "echo held > '$SANDBOX/gate'; exec sleep 30" 2>/dev/null &
  local g=$!
  "$LOCK" merge 1 -- sh -c "echo held > '$SANDBOX/merge'; exec sleep 30" 2>/dev/null &
  local m=$!
  wait_for "$SANDBOX/gate" && wait_for "$SANDBOX/merge" || return 1
  CF_GATE_SLOTS=off CF_LOCK_TIMEOUT_SEC=1 "$LOCK" gate 1 -- true 2>/dev/null
  local gate_rc=$?
  CF_GATE_SLOTS=off CF_LOCK_TIMEOUT_SEC=1 "$LOCK" merge 1 -- true 2>/dev/null
  local merge_rc=$?
  kill "$g" "$m" 2>/dev/null
  wait 2>/dev/null
  assert_eq 0 "$gate_rc" "CF_GATE_SLOTS=off must bypass a held gate pool" &&
    assert_eq 75 "$merge_rc" "CF_GATE_SLOTS=off must never bypass the merge pool"
}

test_missing_lockf_runs_command_unlocked() {
  new_sandbox
  local out
  out="$(CF_LOCKF_BIN=/nonexistent/lockf "$LOCK" p 1 -- echo ran 2>"$SANDBOX/err")"
  assert_eq "ran" "$out" "without lockf the command must still run" &&
    assert_contains "$(cat "$SANDBOX/err")" "running p unlocked" "without lockf lock.sh must say so"
}

test_reports_wait() {
  new_sandbox
  "$LOCK" p 1 -- sh -c "echo held > '$SANDBOX/ready'; sleep 1" 2>/dev/null &
  wait_for "$SANDBOX/ready" || return 1
  "$LOCK" p 1 -- true 2>"$SANDBOX/err"
  wait
  assert_eq 1 "$(grep -c 'waiting for p' "$SANDBOX/err")" "a blocked caller says what it waits for, once" &&
    assert_contains "$(cat "$SANDBOX/err")" "p acquired after" "the wait is reported for shift reports"
}
```

**Step 2: Watch them fail.**

Run: `make test-driver`
Expected: 8 `FAIL lock_test.sh …`. Two fail on output
(`lock.sh must return the command's exit status: expected [42], got [127]`,
`without lockf the command must still run: expected [ran], got []`); the other six fail
`timed out waiting for …/<marker>` after 10 s each — the holder never ran, so it never
wrote its marker. The red run of the whole suite takes about 75 s. Paste this output into
your handover.

**Step 3: Implement `lock.sh` to this contract.**

Usage: `scripts/driver/lock.sh <pool> <slots> -- <command> [args…]`

- If `<pool>` is `gate` and `CF_GATE_SLOTS=off`: `exec` the command. No other pool bypasses.
- If `CF_LOCKF_BIN` (default `lockf`) is not found: print
  `lock.sh: lockf not found; running <pool> unlocked` to stderr and `exec` the command.
- Slot files are `<lock dir>/<pool>.<i>` for `i` in `1..slots`; create the directory.
- **Descriptor form, and nothing else.** For each slot: open the file on fd 9 for append,
  run `lockf -s -t 0 9`; on success print `lock.sh: <pool> acquired after <n>s` to stderr
  and `exec` the command (it inherits fd 9 and with it the lock); on failure close fd 9 and
  try the next slot. **Never** use `lockf <file> <command>`: measured 2026-09-11, killing
  `lockf` with `-9` releases the lock while its child keeps running — the orphan test
  exists to catch exactly that.
- When every slot is busy: print `lock.sh: waiting for <pool>` to stderr **once**, sleep
  `CF_LOCK_RETRY_SEC`, sweep again. With `CF_LOCK_TIMEOUT_SEC` set and exceeded: print
  `lock.sh: timed out waiting for <pool>`, exit 75.
- `set -u`. Bash 3.2 only.

**Step 4: Watch them pass.**

Run: `make test-driver`
Expected: 8 `PASS lock_test.sh …`, `0 failed`.

**Step 5: Commit.**

```sh
git add scripts/driver/lock.sh scripts/driver/test/lock_test.sh
git commit -m "driver: lock.sh — kernel-held slot pools shared by every worktree"
```

---

## Task 4: Heavy gates through the `gate` pool

**Files:**
- Create: `scripts/driver/test/makefile_test.sh`
- Modify: `Makefile` — `test-race`, `test-e2e`, `test-docker`

**Step 1: Write the failing test verbatim.**

```bash
#!/bin/bash
# The gate pool is enforced by the Makefile, not by instruction.
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

test_heavy_gates_go_through_the_gate_pool() {
  local repo t
  repo="$(cd "$DRIVER_DIR/../.." && pwd)"
  for t in test-race test-e2e test-docker; do
    assert_contains "$(make -s -n -C "$repo" "$t")" "scripts/driver/lock.sh gate" "make $t must run through the gate pool" || return 1
  done
  assert_not_contains "$(make -s -n -C "$repo" lint)" "lock.sh" "make lint must never queue"
}
```

**Step 2: Watch it fail.**

Run: `make test-driver`
Expected: `FAIL makefile_test.sh test_heavy_gates_go_through_the_gate_pool` with
`[scripts/driver/lock.sh gate] not found in [go test $(go list ./... | grep -v /node_modules/) -short -race -count=1]`.

**Step 3: Implement to this contract.**

- A `GATE_SLOTS ?= <N from Task 1>` variable, overridable per invocation, with a comment
  naming the measurement it came from.
- `test-race`, `test-e2e` and `test-docker` recipes run their existing commands prefixed
  by `./scripts/driver/lock.sh gate $(GATE_SLOTS) --`. The commands themselves are
  unchanged.
- Nothing else is wrapped — not `test`, not `lint`, not `lint-strict`, not `test-cluster`.
- The `AGENTS.md` §3 descriptions of those three targets mention the pool (Task 8).

**Step 4: Watch it pass, then prove the no-op path** — the same recipes must still work in
CI, where `lockf` does not exist:

Run: `make test-driver` → `0 failed`.
Run: `CF_LOCKF_BIN=/nonexistent make test-race 2>&1 | head -3`
Expected: `lock.sh: lockf not found; running gate unlocked`, then `go test` output.

**Step 5: Commit.**

```sh
git add Makefile scripts/driver/test/makefile_test.sh
git commit -m "make: heavy gates queue for a machine-wide gate slot"
```

---

## Task 5: `scripts/driver/claim.sh`

**Files:**
- Create: `scripts/driver/test/claim_test.sh`
- Create: `scripts/driver/claim.sh` (executable)

**Step 1: Write the failing tests verbatim.**

```bash
#!/bin/bash
# scripts/driver/claim.sh — the only way a driver takes an issue.
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CLAIM="$DRIVER_DIR/claim.sh"

claim_line() { jq -r '.comments[-1].body | split("\n")[0]' "$FAKE_GH_DIR/issues/$1.json"; }
labels_of() { jq -r '[.labels[].name] | sort | join(",")' "$FAKE_GH_DIR/issues/$1.json"; }
comment_count() { jq '.comments | length' "$FAKE_GH_DIR/issues/$1.json"; }

test_clean_claim_labels_and_comments() {
  new_sandbox
  issue_fixture 10 OPEN "severity:P2"
  local out rc
  out="$("$CLAIM" 10 CF-200-fix d06-0300Z internal/a.go internal/a_test.go)"; rc=$?
  assert_eq 0 "$rc" "clean claim exit" &&
    assert_eq "CLAIMED" "$out" "clean claim output" &&
    assert_eq "in-progress,severity:P2" "$(labels_of 10)" "clean claim labels" &&
    assert_eq "taking — CF-200-fix · driver d06-0300Z · lease until $(iso_at 120) · files: internal/a.go internal/a_test.go" \
      "$(claim_line 10)" "claim comment format"
}

test_unexpired_lease_is_taken() {
  new_sandbox
  issue_fixture 11 OPEN "in-progress" \
    "taking — CF-201-x · driver d05-0200Z · lease until $(iso_at 30) · files: internal/b.go" 90
  local out rc
  out="$("$CLAIM" 11 CF-201-x d06-0300Z internal/b.go)"; rc=$?
  assert_eq 3 "$rc" "live lease exit" &&
    assert_eq "TAKEN d05-0200Z until $(iso_at 30)" "$out" "live lease output" &&
    assert_eq 1 "$(comment_count 11)" "a refused claim must not comment"
}

test_expired_lease_is_taken_over() {
  new_sandbox
  issue_fixture 12 OPEN "in-progress" \
    "taking — CF-202-y · driver d04-0100Z · lease until $(iso_at -10) · files: internal/c.go" 130
  local out rc
  out="$("$CLAIM" 12 CF-202-y d06-0300Z internal/c.go)"; rc=$?
  assert_eq 0 "$rc" "takeover exit" &&
    assert_eq "CLAIMED" "$out" "takeover output" &&
    assert_eq "taking — CF-202-y · driver d06-0300Z · lease until $(iso_at 120) · takeover of d04-0100Z · files: internal/c.go" \
      "$(claim_line 12)" "takeover comment format"
}

test_legacy_lock_leased_from_comment_time() {
  new_sandbox
  issue_fixture 13 OPEN "in-progress" "taking — CF-203-z (wave 1, driver run 2026-09-11)" 60
  local out rc
  out="$("$CLAIM" 13 CF-203-z d06-0300Z internal/d.go)"; rc=$?
  assert_eq 3 "$rc" "a 60-minute-old legacy lock is still leased" &&
    assert_eq "TAKEN legacy until $(iso_at 60)" "$out" "legacy lease = comment time + 120 min"
}

test_label_without_claim_comment_uses_updated_at() {
  new_sandbox
  issue_fixture 26 OPEN "in-progress"
  local out rc
  out="$("$CLAIM" 26 CF-216-e d06-0300Z internal/j.go)"; rc=$?
  assert_eq 0 "$rc" "a bare label untouched for 300 min has expired" &&
    assert_contains "$(claim_line 26)" "· takeover of legacy ·" "taking over a bare label is recorded"
}

test_overlapping_files_refused() {
  new_sandbox
  issue_fixture 14 OPEN "in-progress" \
    "taking — CF-204-p · driver d05-0200Z · lease until $(iso_at 45) · files: web-proto/js/regions/inspector.js tests/cf204.spec.js" 75
  issue_fixture 15 OPEN "severity:P2"
  local out rc
  out="$("$CLAIM" 15 CF-205-q d06-0300Z internal/e.go web-proto/js/regions/inspector.js)"; rc=$?
  assert_eq 4 "$rc" "overlap exit" &&
    assert_eq "OVERLAP #14 web-proto/js/regions/inspector.js" "$out" "overlap names the holder and the file" &&
    assert_eq "severity:P2" "$(labels_of 15)" "a refused claim must not label"
}

test_expired_and_closed_claims_do_not_block() {
  new_sandbox
  issue_fixture 16 OPEN "in-progress" \
    "taking — CF-206-r · driver d04-0100Z · lease until $(iso_at -1) · files: internal/f.go" 121
  issue_fixture 17 CLOSED "in-progress" \
    "taking — CF-207-s · driver d05-0200Z · lease until $(iso_at 60) · files: internal/f.go" 60
  issue_fixture 18 OPEN ""
  local out rc
  out="$("$CLAIM" 18 CF-208-t d06-0300Z internal/f.go)"; rc=$?
  assert_eq 0 "$rc" "expired leases and closed issues must not hold files" &&
    assert_eq "CLAIMED" "$out" "claim succeeds"
}

test_handed_back_holds_files_until_landed() {
  new_sandbox
  issue_fixture 19 OPEN "handed-back" \
    "taking — CF-209-u · driver d04-0100Z · lease until $(iso_at -200) · files: internal/g.go" 320
  issue_fixture 20 OPEN ""
  local out rc
  out="$("$CLAIM" 20 CF-210-v d06-0300Z internal/g.go)"; rc=$?
  assert_eq 4 "$rc" "a handed-back branch still owns its files" &&
    assert_eq "OVERLAP #19 internal/g.go" "$out" "overlap names the handed-back holder"
}

test_parked_issue_is_resumable() {
  new_sandbox
  issue_fixture 21 OPEN "parked,severity:P1" \
    "taking — CF-211-w · driver d04-0100Z · lease until $(iso_at -60) · files: internal/h.go" 180
  local out rc
  out="$("$CLAIM" 21 CF-211-w d06-0300Z internal/h.go)"; rc=$?
  assert_eq 0 "$rc" "parked issues are claimable" &&
    assert_eq "in-progress,severity:P1" "$(labels_of 21)" "claiming a parked issue removes parked" &&
    assert_not_contains "$(claim_line 21)" "takeover of" "resuming a parked issue is not a takeover"
}

test_refuses_closed_wontfix_and_handed_back() {
  new_sandbox
  issue_fixture 22 CLOSED ""
  issue_fixture 23 OPEN "wontfix"
  issue_fixture 24 OPEN "handed-back"
  local a b c ra rb rc
  a="$("$CLAIM" 22 CF-212-a d06-0300Z x.go)"; ra=$?
  b="$("$CLAIM" 23 CF-213-b d06-0300Z y.go)"; rb=$?
  c="$("$CLAIM" 24 CF-214-c d06-0300Z z.go)"; rc=$?
  assert_eq "2 REFUSED closed" "$ra $a" "closed" &&
    assert_eq "2 REFUSED wontfix" "$rb $b" "wontfix" &&
    assert_eq "2 REFUSED handed-back" "$rc $c" "handed-back is for landing, not claiming"
}

test_dry_run_writes_nothing() {
  new_sandbox
  issue_fixture 25 OPEN ""
  local out rc
  out="$("$CLAIM" --dry-run 25 CF-215-d d06-0300Z internal/i.go)"; rc=$?
  assert_eq 0 "$rc" "dry run exit" &&
    assert_eq "CLAIMED (dry run)" "$out" "dry run output" &&
    assert_not_contains "$(cat "$FAKE_GH_DIR/calls.log")" "issue edit" "dry run must not label" &&
    assert_not_contains "$(cat "$FAKE_GH_DIR/calls.log")" "issue comment" "dry run must not comment"
}
```

**Step 2: Watch them fail.**

Run: `make test-driver`
Expected: 11 `FAIL claim_test.sh …`, each `got [127]`. Paste into your handover.

**Step 3: Implement `claim.sh` to this contract.**

Usage: `scripts/driver/claim.sh [--dry-run] <issue> <branch> <driver-id> <file>…`

**Serialization.** The whole script runs under pool `claim`, 1 slot: when not already
inside the lock (use an environment marker), re-`exec` itself through
`lock.sh claim 1 -- "$0" "$@"`.

**The claim line** — first line of a comment, exactly:

```
taking — <branch> · driver <driver-id> · lease until <YYYY-MM-DDTHH:MMZ> · files: <f1> <f2> …
```

With a takeover, `· takeover of <old-driver> ` sits between the lease and `· files:`.
Separators are ` · ` (U+00B7 with spaces); the dash after `taking` is U+2014.

**Reading a lease** from an issue — the newest comment whose body starts with `taking —`:
- It has `lease until <iso>` → driver from `driver <id>`, expiry from the iso, files from
  everything after `· files: `.
- It has no `lease until` (old routine) → driver `legacy`, expiry = comment `createdAt` +
  `CF_LEASE_MIN`, no files.
- No such comment → driver `legacy`, expiry = issue `updatedAt` + `CF_LEASE_MIN`, no files.
- A lease is live while `CF_NOW < expiry`.

**Decision, in this order:**
1. Issue not open → stdout `REFUSED closed`, exit 2. Labelled `wontfix` → `REFUSED wontfix`,
   exit 2. Labelled `handed-back` → `REFUSED handed-back`, exit 2.
2. Labelled `in-progress` with a live lease → `TAKEN <driver> until <expiry iso>`, exit 3.
   With an expired lease → remember `<driver>` for the takeover note.
3. **Held files** = the file sets of every *other* open issue that is labelled
   `handed-back` (any lease), or labelled `in-progress` with a live lease. Compare paths as
   exact strings. First requested file that is held → `OVERLAP #<holder> <file>`, exit 4.
4. `--dry-run` → `CLAIMED (dry run)`, exit 0, and no `gh issue edit`/`gh issue comment`.
5. Otherwise: `gh issue edit <n> --add-label in-progress --remove-label parked`, then post the
   claim line, then stdout `CLAIMED`, exit 0.

stdout carries exactly the one result line; anything else goes to stderr. `set -u`, bash 3.2,
`jq` for all JSON and all date arithmetic (`strptime`/`mktime`/`strftime`/`fromdateiso8601`
— BSD and GNU `date` disagree).

**Step 4: Watch them pass.**

Run: `make test-driver` → 11 `PASS claim_test.sh …`, `0 failed`.

**Step 5: Rehearse against real GitHub, read-only:**

```sh
scripts/driver/claim.sh --dry-run <any open unlabelled issue> CF-000-rehearsal rehearsal internal/nonexistent.go
```

Expected: `CLAIMED (dry run)`, and `gh issue view <n>` unchanged. Paste both.

**Step 6: Commit.**

```sh
git add scripts/driver/claim.sh scripts/driver/test/claim_test.sh
git commit -m "driver: claim.sh — leased, overlap-checked issue claims"
```

---

## Task 6: `scripts/driver/land.sh`

**Files:**
- Create: `scripts/driver/test/land_test.sh`
- Create: `scripts/driver/land.sh` (executable)

**Step 1: Write the failing tests verbatim.**

```bash
#!/bin/bash
# scripts/driver/land.sh — the only way a branch reaches main.
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
LAND="$DRIVER_DIR/land.sh"

# land_repo: a bare origin and a working clone. main has one commit; topic branch
# CF-900-thing has two commits on thing.txt; both are pushed. Issue #42 is handed
# back with a claim naming that branch. Leaves the shell in the clone.
land_repo() {
  new_sandbox
  export CF_LAND_GATES=true CF_CI_POLL_SEC=0
  git init -q --bare -b main "$SANDBOX/origin.git"
  git clone -q "$SANDBOX/origin.git" "$SANDBOX/work" 2>/dev/null
  cd "$SANDBOX/work" || return 1
  git config user.email tester@example.com
  git config user.name tester
  git checkout -q -b main
  echo base > thing.txt
  git add thing.txt
  git commit -q -m "base"
  git push -q origin main
  git checkout -q -b CF-900-thing
  echo one >> thing.txt
  git commit -q -am "Teach thing a first trick" -m "First body."
  echo two >> thing.txt
  git commit -q -am "Teach thing a second trick" -m "Second body."
  git push -q origin CF-900-thing
  git checkout -q main
  issue_fixture 42 OPEN "handed-back,severity:P2" \
    "taking — CF-900-thing · driver d06-0300Z · lease until $(iso_at 30) · files: thing.txt" 90
  BASE_SHA="$(git rev-parse main)"
}

origin_git() { git -C "$SANDBOX/origin.git" "$@"; }
has_branch() { origin_git show-ref --verify --quiet "refs/heads/$1" && echo yes || echo no; }
has_worktree() { [ -d "$SANDBOX/work/.worktrees/land-CF-900" ] && echo yes || echo no; }

test_lands_one_squashed_commit_with_issue_suffix() {
  land_repo
  printf 'green\n' > "$FAKE_GH_DIR/ci-results"
  local out rc
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 0 "$rc" "clean landing exit" &&
    assert_contains "$out" "LANDED $(origin_git rev-parse main) https://ci.example/runs/" "output names sha and run" &&
    assert_eq "$BASE_SHA" "$(origin_git rev-parse main~1)" "exactly one commit lands per issue" &&
    assert_eq "Teach thing a second trick (CF-900, #42)" "$(origin_git log -1 --format=%s main)" "subject = newest subject + suffix" &&
    assert_contains "$(origin_git log -1 --format=%b main)" "First body." "commit bodies are kept" &&
    assert_eq no "$(has_branch CF-900-thing)" "the topic branch is deleted after landing" &&
    assert_eq no "$(has_worktree)" "the scratch worktree is removed"
}

test_rebase_conflict_parks_without_touching_main() {
  land_repo
  echo conflicting > thing.txt
  git commit -q -am "main moved"
  git push -q origin main
  local before out rc
  before="$(origin_git rev-parse main)"
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 6 "$rc" "conflict exit" &&
    assert_eq "PARKED rebase-conflict" "$out" "conflict output" &&
    assert_eq "$before" "$(origin_git rev-parse main)" "a conflict leaves main untouched" &&
    assert_eq no "$(has_worktree)" "the scratch worktree is removed"
}

test_red_gates_park_without_touching_main() {
  land_repo
  export CF_LAND_GATES=false
  local out rc
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 6 "$rc" "red gates exit" &&
    assert_eq "PARKED gates-red" "$out" "red gates output" &&
    assert_eq "$BASE_SHA" "$(origin_git rev-parse main)" "red gates leave main untouched"
}

test_red_ci_reverts_and_keeps_branch() {
  land_repo
  printf 'red\ngreen\n' > "$FAKE_GH_DIR/ci-results"
  local out rc
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 7 "$rc" "red CI exit" &&
    assert_contains "$out" "REVERTED https://ci.example/runs/" "output names the failed run" &&
    assert_eq "$(origin_git rev-parse "$BASE_SHA^{tree}")" "$(origin_git rev-parse 'main^{tree}')" "main's tree returns to its pre-landing state" &&
    assert_eq yes "$(has_branch CF-900-thing)" "the topic branch survives a revert"
}

test_e2e_only_failure_is_rerun_once() {
  land_repo
  printf 'e2e-red\ngreen\n' > "$FAKE_GH_DIR/ci-results"
  local rc
  "$LAND" 42 >/dev/null 2>&1; rc=$?
  assert_eq 0 "$rc" "an e2e flake that passes on rerun lands" &&
    assert_eq 1 "$(grep -c 'run rerun' "$FAKE_GH_DIR/calls.log")" "the failed e2e job is rerun once"
}

test_e2e_failing_twice_reverts() {
  land_repo
  printf 'e2e-red\ne2e-red\ngreen\n' > "$FAKE_GH_DIR/ci-results"
  local rc
  "$LAND" 42 >/dev/null 2>&1; rc=$?
  assert_eq 7 "$rc" "e2e red after one rerun is a regression" &&
    assert_eq 1 "$(grep -c 'run rerun' "$FAKE_GH_DIR/calls.log")" "e2e is never rerun twice"
}

test_refuses_issue_not_handed_back() {
  land_repo
  issue_fixture 42 OPEN "in-progress" \
    "taking — CF-900-thing · driver d06-0300Z · lease until $(iso_at 30) · files: thing.txt" 90
  local out rc
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 5 "$rc" "not-handed-back exit" &&
    assert_eq "NOT-HANDED-BACK" "$out" "not-handed-back output" &&
    assert_eq "$BASE_SHA" "$(origin_git rev-parse main)" "main untouched" &&
    assert_eq no "$(has_worktree)" "no worktree is created"
}
```

**Step 2: Watch them fail.**

Run: `make test-driver`
Expected: 7 `FAIL land_test.sh …`, each `got [127]`. Paste into your handover.

**Step 3: Implement `land.sh` to this contract.**

Usage: `scripts/driver/land.sh <issue>`, run from inside any worktree of the clone.

**Serialization.** The whole script runs under pool `merge`, 1 slot, by re-`exec`ing
through `lock.sh merge 1 -- "$0" "$@"` — including the CI watch and any revert. Nobody
merges on top of an unwatched push.

**Steps and results** (stdout carries exactly the one result line):

1. Read the issue. Not labelled `handed-back` → `NOT-HANDED-BACK`, exit 5, **before any
   `git fetch` or worktree**.
2. Branch = the `taking — <branch>` of the newest claim comment; `CF-NNN` = its prefix.
3. `git fetch origin`; add a detached worktree at `<toplevel>/.worktrees/land-CF-NNN` on
   `origin/<branch>`; rebase onto `origin/main`. Conflict → abort the rebase, remove the
   worktree, `PARKED rebase-conflict`, exit 6.
4. Run `CF_LAND_GATES` in the worktree (default:
   `make lint && make lint-strict && go test -short <packages containing changed .go files>`,
   the `go test` omitted when no Go file changed). Non-zero → remove the worktree,
   `PARKED gates-red`, exit 6.
5. Squash onto `origin/main` into **one** commit. Subject = the branch's newest commit
   subject, with ` (CF-NNN, #<issue>)` appended unless that exact text is already present.
   Body = the branch's non-empty commit bodies, oldest first. `git push origin HEAD:main`
   without force; rejected → remove the worktree, `PARKED push-rejected`, exit 6.
6. Find the run with `gh run list --branch main --commit <sha> --json databaseId,url`,
   polling every `CF_CI_POLL_SEC` until it appears. `gh run watch <id> --exit-status`.
   On failure, `gh run view <id> --json jobs`: if the **only** failed job is `e2e` and it
   has not been rerun, `gh run rerun <id> --failed` and watch once more. Never rerun twice.
7. Green → `git push origin --delete <branch>`, remove the worktree,
   `LANDED <sha> <run-url>`, exit 0.
8. Red → `git revert --no-edit <sha>`, push, find and watch the revert's run (no rerun).
   Revert green → remove the worktree, `REVERTED <failed run-url>`, exit 7; the topic branch
   is kept. Revert red → `REVERTED-RED <failed run-url>`, exit 8.

`land.sh` never touches issues. The driver acts on its exit code (Task 8).

**Step 4: Watch them pass.**

Run: `make test-driver` → 7 `PASS land_test.sh …`, `test-driver: 27 passed, 0 failed, 0 skipped`.

**Step 5: Commit.**

```sh
git add scripts/driver/land.sh scripts/driver/test/land_test.sh
git commit -m "driver: land.sh — one squashed, CI-watched landing at a time"
```

---

## Task 7: Run the driver tests in CI

**Files:** Modify: `.github/workflows/ci.yml` — the `test` job.

**Step 1:** Add a step after the existing unit-test step: `make test-driver`. Ubuntu runners
ship `jq`; there is no `lockf`, so `lock_test.sh` skips and the other 19 tests run.

**Step 2:** Push the branch (topic branch only) and confirm the step's log ends
`test-driver: 19 passed, 0 failed, 8 skipped`.

**Step 3: Commit** (before the push in Step 2):

```sh
git add .github/workflows/ci.yml
git commit -m "ci: run the driver coordination tests"
```

---

## Task 8: Rewrite the routine, the contract and `AGENTS.md`

No automated oracle — this is prose. Every command it tells an agent to run must be one you
ran in Tasks 3–6.

**Files:**
- Modify: `docs/routines/issue-driver.md` (rewrite)
- Modify: `docs/task-execution-contract.md` §6, §7
- Modify: `AGENTS.md` §2, §3, §4

**Step 1: `docs/routines/issue-driver.md`.** Rewrite to spec §5 plus the amendments above.
It must contain:

- §0 **Shift and capacity.** A shift is one scheduled run of at most 5 h; checkpoints
  T0+3:00 last dispatch, T0+4:30 last `land.sh`, T0+4:45 post report and exit. The global
  cap and `GATE_SLOTS` with the Task 1 measurements that produced them. The quota back-off
  rule. No "5-hour quota is a hard wall" — drivers run on Gemini.
- §1 **Preflight** as today, plus: `gh issue list --label handed-back`, and the Driver log
  issue for any posted quota reset time.
- §2 **The loop**, every ~5 min: land → resume parked → take over expired → dispatch, with
  live subagents counted as open `in-progress` issues with a live lease.
- §3 **Claiming** only via `scripts/driver/claim.sh`, with every result and what to do:
  `CLAIMED` dispatch; `TAKEN`/`OVERLAP` pick the next candidate; `REFUSED` skip.
- §4 **Dispatch** prompt (four lines — adds: `Push your topic branch; hand back on the issue.`).
- §5 **Landing** only via `scripts/driver/land.sh`, one call at a time, oldest
  `handed-back` first, and the driver's action per exit code: 0 close the issue with
  `completed in <sha>; guarded by <test>` and remove `in-progress`/`handed-back`/`parked`;
  5 skip; 6 swap `handed-back`→`parked` with a comment naming the reason; 7 the same plus the
  CI link; 8 stop landing, post on the Driver log issue, file `severity:P0` if none exists.
  Before landing, the report on the issue must contain a failing and a passing run, or the
  driver swaps to `parked` with `no failing run in handover`.
- §6 **Report** as one comment on the pinned Driver log issue (fields per spec §7).
- **What you never do:** unchanged list, minus the T0 merge rule, plus "claim or land by
  hand" and "set `CF_GATE_SLOTS=off`".

**Step 2: `docs/task-execution-contract.md`.**
- §6: replace **Do not push** with: push your own topic branch after every green commit;
  `--force-with-lease` to that branch only after a rebase; never `main`, never another branch.
- §7: the handover is a comment on your own issue, followed by
  `gh issue edit <n> --add-label handed-back --remove-label in-progress`. Parked is the same
  with `parked` and the exact state. No other issue edits.
- §4: heavy gates may print `lock.sh: waiting for gate` — that is the machine being shared,
  not a hang; never bypass it.

**Step 3: `AGENTS.md`.**
- §4: **One-Driver Rule** becomes **One Merge at a Time** — many drivers may run; only
  `scripts/driver/land.sh` pushes `main`, and it holds the merge lock through CI.
  **Post-Merge CI Check** points at `land.sh`. Everything else in §4 stands.
- §2: a bullet for the lock pools (`claim`, `merge`, `gate`), where the slot files live, and
  `lsof <slot file>` to see a holder.
- §3: `make test-driver`; the gate note on `test-race`/`test-e2e`/`test-docker`.

**Step 4: Verify** by reading each rewritten file end to end against the scripts' actual
outputs, and grep for leftovers:

```sh
grep -nE "6 subagents|at most \*\*6\*\*|One-Driver|hard wall|Do not push" AGENTS.md docs/routines/issue-driver.md docs/task-execution-contract.md
```

Expected: no output.

**Step 5: Commit.**

```sh
git add docs/routines/issue-driver.md docs/task-execution-contract.md AGENTS.md
git commit -m "docs: continuous drivers — claim, land and gate through scripts/driver"
```

---

## Task 9: Fold the amendments into the spec

**Files:** Modify: `docs/superpowers/specs/2026-09-11-continuous-drivers-design.md`

Apply §"Spec amendments" 1–6 at their places in spec §1, §3 and §4. No new decisions.

```sh
git add docs/superpowers/specs/2026-09-11-continuous-drivers-design.md
git commit -m "docs(spec): continuous drivers — amendments found while writing the tests"
```

---

## Task 10: GitHub setup and rollout

**Outward-facing: confirm with Kaur before each command.**

**Step 1: Labels and the log issue.**

```sh
gh label create handed-back --color 0E8A16 --description "Subagent finished; branch pushed, report on the issue, awaiting land.sh"
gh label create parked      --color FBCA04 --description "Branch pushed but not landable yet; the issue says why; claimable"
gh issue create --title "Driver log" --body "One comment per driver shift. See docs/routines/issue-driver.md §6."
gh issue pin <number>
```

**Step 2: Land this branch — the last merge done the old way.** The merge lock does not
exist on `main` until this lands, so choose a quiet moment: no driver mid-merge
(`gh run list --branch main --limit 3` shows nothing in progress), rebase on `origin/main`,
`make lint && make lint-strict && make test-race && make test-driver`, push `main`, and
`gh run watch <id> --exit-status`.

**Step 3: Tell Kaur the two manual steps:** add a 23:00 scheduled task, and nothing else —
the existing tasks' prompts already point at `docs/routines/issue-driver.md`, so each new
shift picks up the new rules.

**Step 4: Watch the first two shifts' Driver log comments** for `REVERTED`, `OVERLAP` churn
and gate wait; lower `GATE_SLOTS` or the cap if either climbs.
