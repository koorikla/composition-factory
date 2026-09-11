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
