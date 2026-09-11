#!/bin/bash
# scripts/driver/land.sh — the issue state it leaves behind under the merge lock,
# and branches whose landing main has already reverted.
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
. "$(dirname "${BASH_SOURCE[0]}")/land_fixture.sh"

test_landed_clears_the_issue_state_labels() {
  land_repo || return 1
  issue_fixture 42 OPEN "handed-back,in-progress,parked,severity:P2" \
    "taking — CF-900-thing · driver d06-0300Z · lease until $(iso_at 30) · files: thing.txt" 90
  printf 'green\n' > "$FAKE_GH_DIR/ci-results"
  local out rc
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 0 "$rc" "landing" &&
    assert_contains "$out" "LANDED " "landing output" &&
    assert_eq "severity:P2" "$(labels_of 42)" "handed-back, in-progress and parked are removed" &&
    assert_eq OPEN "$(jq -r .state "$FAKE_GH_DIR/issues/42.json")" "the issue stays open for the driver to close" &&
    assert_eq 0 "$(grep -c '^issue comment' "$FAKE_GH_DIR/calls.log")" "a landing posts no comment"
}

test_landed_removes_only_labels_the_issue_has() {
  land_repo || return 1
  printf 'green\n' > "$FAKE_GH_DIR/ci-results"
  local rc
  "$LAND" 42 >/dev/null 2>&1; rc=$?
  assert_eq 0 "$rc" "landing" &&
    assert_eq "issue edit 42 --remove-label handed-back" "$(grep '^issue edit' "$FAKE_GH_DIR/calls.log")" \
      "labels the issue lacks are not removed" &&
    assert_eq "severity:P2" "$(labels_of 42)" "handed-back is removed"
}

test_gates_red_parks_the_issue_with_a_comment() {
  land_repo || return 1
  export CF_LAND_GATES=false
  local out rc
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 6 "$rc" "red gates exit" &&
    assert_eq "PARKED gates-red" "$out" "red gates output" &&
    assert_eq "parked severity:P2" "$(labels_of 42)" "parked replaces handed-back" &&
    assert_eq "parked — CF-900-thing · gates-red" "$(last_comment 42)" "the parking comment names branch and reason"
}

test_reverted_parks_the_issue_with_the_failed_run() {
  land_repo || return 1
  printf 'red\ngreen\n' > "$FAKE_GH_DIR/ci-results"
  local out rc
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 7 "$rc" "red CI exit" &&
    assert_eq "parked severity:P2" "$(labels_of 42)" "a reverted landing parks the issue" &&
    assert_eq "parked — CF-900-thing · reverted ${out#REVERTED }" "$(last_comment 42)" "the comment names the failed run"
}

test_reverted_red_parks_the_issue() {
  land_repo || return 1
  printf 'red\nred\n' > "$FAKE_GH_DIR/ci-results"
  local out rc
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 8 "$rc" "red revert exit" &&
    assert_contains "$out" "REVERTED-RED https://ci.example/runs/" "reverted-red output" &&
    assert_eq "parked severity:P2" "$(labels_of 42)" "a reverted landing parks the issue even when main stays red" &&
    assert_eq "parked — CF-900-thing · reverted-red ${out#REVERTED-RED }" "$(last_comment 42)" "the comment names the failed run"
}

test_push_unknown_leaves_the_issue_labels() {
  land_repo || return 1
  local real_git
  real_git="$(command -v git)"
  # Every push to main fails without reaching origin, and origin cannot be read.
  shim git <<EOF || return 1
#!/bin/bash
for a in "\$@"; do
  case "\$a" in push | ls-remote) echo "fatal: unable to access origin" >&2; exit 1 ;; esac
done
exec "$real_git" "\$@"
EOF
  local out rc
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 8 "$rc" "unknown push exit" &&
    assert_eq "REVERTED-RED push-unknown" "$out" "unknown push output" &&
    assert_eq "handed-back severity:P2" "$(labels_of 42)" "labels are left for the driver" &&
    assert_eq 0 "$(grep -c '^issue \(edit\|comment\)' "$FAKE_GH_DIR/calls.log")" "no edit and no comment"
}

test_second_land_after_landing_is_not_handed_back_and_fetches_nothing() {
  land_repo || return 1
  printf 'green\n' > "$FAKE_GH_DIR/ci-results"
  local first rc1 out rc real_git
  first="$("$LAND" 42 2>/dev/null)"; rc1=$?
  real_git="$(command -v git)"
  shim git <<EOF || return 1
#!/bin/bash
for a in "\$@"; do
  [ "\$a" != fetch ] || echo "\$*" >> "$SANDBOX/fetches"
done
exec "$real_git" "\$@"
EOF
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 0 "$rc1" "first landing" &&
    assert_contains "$first" "LANDED " "first landing output" &&
    assert_eq 5 "$rc" "the next land.sh right after sees the issue is no longer handed back" &&
    assert_eq "NOT-HANDED-BACK" "$out" "second output" &&
    assert_eq no "$([ -e "$SANDBOX/fetches" ] && echo yes || echo no)" "the second run fetches nothing"
}

test_branch_whose_landing_was_reverted_is_parked() {
  land_repo || return 1
  printf 'red\ngreen\n' > "$FAKE_GH_DIR/ci-results"
  local rc1 after_revert out rc
  "$LAND" 42 >/dev/null 2>&1; rc1=$?
  after_revert="$(origin_git rev-parse main)"
  relabel_handed_back 42 || return 1
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 7 "$rc1" "the first landing was reverted" &&
    assert_eq 6 "$rc" "the same branch is not landed again" &&
    assert_eq "PARKED already-reverted" "$out" "already-reverted output" &&
    assert_eq "$after_revert" "$(origin_git rev-parse main)" "nothing is pushed" &&
    assert_eq "parked severity:P2" "$(labels_of 42)" "the issue is parked" &&
    assert_eq "parked — CF-900-thing · already-reverted" "$(last_comment 42)" "the comment says why"
}

test_new_commits_after_a_revert_land() {
  land_repo || return 1
  printf 'red\ngreen\ngreen\n' > "$FAKE_GH_DIR/ci-results"
  local rc1 after_revert out rc
  "$LAND" 42 >/dev/null 2>&1; rc1=$?
  after_revert="$(origin_git rev-parse main)"
  sandbox_guard || return 1
  git checkout -q CF-900-thing &&
    echo three >> thing.txt &&
    git commit -q -am "Fix the trick that broke CI" &&
    git push -q origin CF-900-thing &&
    git checkout -q main || return 1
  relabel_handed_back 42 || return 1
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 7 "$rc1" "the first landing was reverted" &&
    assert_eq 0 "$rc" "the fixed branch lands" &&
    assert_contains "$out" "LANDED $(origin_git rev-parse main) " "landing output" &&
    assert_eq "$after_revert" "$(origin_git rev-parse main~1)" "one commit lands on top of the revert" &&
    assert_eq "Fix the trick that broke CI (CF-900, #42)" "$(origin_git log -1 --format=%s main)" "subject of the new landing"
}
