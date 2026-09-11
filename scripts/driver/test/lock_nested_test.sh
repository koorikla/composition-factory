#!/bin/bash
# scripts/driver/lock.sh nested in the same process: an inner call must not
# drop the outer call's lock. run.sh only auto-skips lock_test.sh, so each test
# here skips itself where lockf is absent.
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
LOCK="$DRIVER_DIR/lock.sh"

test_exec_chain_holds_both_locks() {
  command -v lockf >/dev/null || return 0
  new_sandbox
  "$LOCK" a 1 -- "$LOCK" b 1 -- sh -c "echo held > '$SANDBOX/ready'; exec sleep 30" 2>/dev/null &
  local holder=$!
  wait_for "$SANDBOX/ready" || { kill "$holder" 2>/dev/null; return 1; }
  CF_LOCK_TIMEOUT_SEC=0 "$LOCK" a 1 -- true 2>/dev/null
  local a_rc=$?
  CF_LOCK_TIMEOUT_SEC=0 "$LOCK" b 1 -- true 2>/dev/null
  local b_rc=$?
  kill "$holder" 2>/dev/null
  wait 2>/dev/null
  assert_eq 75 "$a_rc" "the outer pool must stay held when lock.sh nests in the same process" &&
    assert_eq 75 "$b_rc" "the inner pool must be held when lock.sh nests in the same process"
}
