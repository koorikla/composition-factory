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
