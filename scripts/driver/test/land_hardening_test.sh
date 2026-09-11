#!/bin/bash
# scripts/driver/land.sh — landing safely when main moves, pushes misreport,
# runs are killed, CI hangs, and comments come from outside the project.
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
. "$(dirname "${BASH_SOURCE[0]}")/land_fixture.sh"

test_main_moving_during_gates_is_never_undone() {
  land_repo
  other_clone
  echo important > "$SANDBOX/other/other.txt"
  git -C "$SANDBOX/other" add other.txt
  git -C "$SANDBOX/other" commit -q -m "someone else's commit"
  # While the gates run, main moves and some worktree of the clone fetches.
  export CF_LAND_GATES="git -C '$SANDBOX/other' push -q origin main && git -C '$SANDBOX/work' fetch -q origin"
  printf 'green\n' > "$FAKE_GH_DIR/ci-results"
  local moved out rc
  moved="$(git -C "$SANDBOX/other" rev-parse main)"
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 6 "$rc" "a landing built on a stale base is parked" &&
    assert_eq "PARKED push-rejected" "$out" "stale base output" &&
    assert_eq "$moved" "$(origin_git rev-parse main)" "main keeps the other commit and nothing else" &&
    assert_eq important "$(origin_git show main:other.txt 2>/dev/null)" "the other commit's file is still on main"
}
