#!/bin/bash
# scripts/driver/land.sh — waiting for an e2e rerun to start, and scratch
# worktrees left registered after their directory was deleted.
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
LAND="$DRIVER_DIR/land.sh"

# land_repo: the same shape as land_test.sh. A bare origin and a working clone;
# main has one commit; topic branch CF-900-thing has two commits on thing.txt;
# both are pushed. Issue #42 is handed back with a claim naming that branch.
# Leaves the shell in the clone.
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
}

origin_git() { git -C "$SANDBOX/origin.git" "$@"; }
has_branch() { origin_git show-ref --verify --quiet "refs/heads/$1" && echo yes || echo no; }
has_worktree() { [ -d "$SANDBOX/work/.worktrees/land-CF-900" ] && echo yes || echo no; }

# slow_rerun_gh STALE: a gh ahead of the fake whose rerun starts late, like
# GitHub's asynchronous rerun endpoint. After `run rerun <id>`, `run view <id>`
# still reads the old attempt (e2e completed, failure) for STALE calls, then
# the new attempt (success); `run watch <id>` exits non-zero, as real gh does
# for a run that has already completed, until a view has read the new attempt.
# Every other call goes to the fake gh.
slow_rerun_gh() {
  mkdir -p "$SANDBOX/bin"
  cat > "$SANDBOX/bin/gh" <<EOF
#!/bin/bash
set -u
D="\$FAKE_GH_DIR"
STALE=$1
views() { [ -f "\$D/rerun-views" ] && echo \$((\$(wc -l < "\$D/rerun-views"))) || echo 0; }
case "\$1 \${2:-}" in
"run rerun")
  echo "\$*" >> "\$D/calls.log"
  echo "\$3" > "\$D/rerun-id"
  : > "\$D/rerun-views"
  exit 0
  ;;
"run view" | "run watch")
  if [ "\$3" = "\$(cat "\$D/rerun-id" 2>/dev/null)" ]; then
    echo "\$*" >> "\$D/calls.log"
    if [ "\$2" = view ]; then
      echo x >> "\$D/rerun-views"
      if [ "\$(views)" -le "\$STALE" ]; then
        echo '{"jobs":[{"name":"test","status":"completed","conclusion":"success"},{"name":"e2e","status":"completed","conclusion":"failure"}]}'
      else
        echo '{"jobs":[{"name":"test","status":"completed","conclusion":"success"},{"name":"e2e","status":"completed","conclusion":"success"}]}'
      fi
      exit 0
    fi
    if [ "\$(views)" -le "\$STALE" ]; then
      echo "Run \$3 has already completed with 'failure'" >&2
      exit 1
    fi
    exit 0
  fi
  ;;
esac
exec "$TEST_DIR/fakebin/gh" "\$@"
EOF
  chmod +x "$SANDBOX/bin/gh"
  export PATH="$SANDBOX/bin:$PATH"
}

test_e2e_rerun_is_watched_only_after_it_starts() {
  land_repo
  slow_rerun_gh 3
  printf 'e2e-red\ngreen\n' > "$FAKE_GH_DIR/ci-results"
  local out rc
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 0 "$rc" "a rerun that starts late still lands" &&
    assert_contains "$out" "LANDED $(origin_git rev-parse main) https://ci.example/runs/" "output names sha and run" &&
    assert_eq 1 "$(grep -c 'run rerun' "$FAKE_GH_DIR/calls.log")" "e2e is rerun once" &&
    assert_eq no "$(has_branch CF-900-thing)" "the topic branch is deleted after landing"
}

test_registered_worktree_with_deleted_directory_is_pruned() {
  land_repo
  git worktree add -q --detach .worktrees/land-CF-900 main
  rm -rf .worktrees/land-CF-900
  printf 'green\n' > "$FAKE_GH_DIR/ci-results"
  local out rc
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 0 "$rc" "a stale worktree registration does not block landing" &&
    assert_contains "$out" "LANDED $(origin_git rev-parse main) " "landing output" &&
    assert_eq no "$(has_worktree)" "the scratch worktree is removed" &&
    assert_not_contains "$(git worktree list)" "land-CF-900" "no land-CF-900 registration is left"
}
