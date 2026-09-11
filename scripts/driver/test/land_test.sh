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
