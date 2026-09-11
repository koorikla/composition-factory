#!/bin/bash
# scripts/driver/land.sh — landing safely when main moves, pushes misreport,
# runs are killed, CI hangs, and comments come from outside the project.
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
. "$(dirname "${BASH_SOURCE[0]}")/land_fixture.sh"

test_main_moving_during_gates_is_never_undone() {
  land_repo || return 1
  other_clone || return 1
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

test_push_reported_failed_but_accepted_still_lands() {
  land_repo || return 1
  local real_git
  real_git="$(command -v git)"
  # The first push to main reaches origin, then the client reports a failure.
  shim git <<EOF || return 1
#!/bin/bash
push= del=
for a in "\$@"; do
  case "\$a" in push) push=1 ;; --delete) del=1 ;; esac
done
if [ -n "\$push" ] && [ -z "\$del" ] && [ ! -e "$SANDBOX/push-misreported" ]; then
  : > "$SANDBOX/push-misreported"
  "$real_git" "\$@" || exit
  echo "fatal: the remote end hung up unexpectedly" >&2
  exit 1
fi
exec "$real_git" "\$@"
EOF
  printf 'green\n' > "$FAKE_GH_DIR/ci-results"
  local out rc
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 0 "$rc" "a push that reached origin is watched, not parked" &&
    assert_contains "$out" "LANDED $(origin_git rev-parse main) https://ci.example/runs/" "landing output" &&
    assert_eq "$BASE_SHA" "$(origin_git rev-parse main~1)" "exactly one commit lands"
}

test_landing_again_after_it_landed_resumes_the_same_sha() {
  land_repo || return 1
  printf 'green\ngreen\n' > "$FAKE_GH_DIR/ci-results"
  local first rc1 landed out rc
  first="$("$LAND" 42 2>/dev/null)"; rc1=$?
  landed="$(origin_git rev-parse main)"
  # The issue is handed back again, as when a driver acted on a stale report.
  relabel_handed_back 42 || return 1
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 0 "$rc1" "first landing" &&
    assert_eq 0 "$rc" "landing an issue that already landed" &&
    assert_eq "$first" "$out" "the second run reports the same sha and run" &&
    assert_eq "$landed" "$(origin_git rev-parse main)" "main is unchanged" &&
    assert_eq "$BASE_SHA" "$(origin_git rev-parse main~1)" "no second commit lands"
}

test_killed_after_push_resumes_watching() {
  land_repo || return 1
  # gh kills land.sh the first time it looks for the landing's run, after the push.
  shim gh <<EOF || return 1
#!/bin/bash
if [ "\$1 \${2:-}" = "run list" ] && [ ! -e "$SANDBOX/killed" ] &&
  [ "\$(git -C "$SANDBOX/origin.git" rev-parse main)" != "$BASE_SHA" ]; then
  : > "$SANDBOX/killed"
  kill -9 "\$CF_LAND_LOCKED"
  exit 1
fi
exec "$TEST_DIR/fakebin/gh" "\$@"
EOF
  printf 'green\n' > "$FAKE_GH_DIR/ci-results"
  local first rc1 landed out rc
  first="$("$LAND" 42 2>/dev/null)"; rc1=$?
  landed="$(origin_git rev-parse main)"
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 137 "$rc1" "the first run was killed" &&
    assert_eq "" "$first" "a killed run prints no result" &&
    assert_eq 0 "$rc" "the next run watches the pushed landing" &&
    assert_contains "$out" "LANDED $landed https://ci.example/runs/" "the next run reports the pushed sha" &&
    assert_eq "$landed" "$(origin_git rev-parse main)" "main is unchanged" &&
    assert_eq "$BASE_SHA" "$(origin_git rev-parse main~1)" "no second commit lands" &&
    assert_eq no "$(has_branch CF-900-thing)" "the topic branch is deleted after the green watch" &&
    assert_eq no "$(has_worktree)" "the crashed run's worktree is removed"
}

test_new_work_after_an_earlier_landing_is_landed() {
  land_repo || return 1
  printf 'green\ngreen\n' > "$FAKE_GH_DIR/ci-results"
  local rc1 first out rc
  "$LAND" 42 >/dev/null 2>&1; rc1=$?
  first="$(origin_git rev-parse main)"
  sandbox_guard || return 1
  git fetch -q origin &&
    git checkout -q -b CF-900-more origin/main &&
    echo three >> thing.txt &&
    git commit -q -am "Teach thing a third trick" &&
    git push -q origin CF-900-more &&
    git checkout -q main || return 1
  issue_fixture 42 OPEN handed-back \
    "taking — CF-900-thing · driver d06-0300Z · lease until $(iso_at -60) · files: thing.txt" 150 \
    "taking — CF-900-more · driver d07-0500Z · lease until $(iso_at 30) · files: thing.txt" 30
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 0 "$rc1" "first landing" &&
    assert_eq 0 "$rc" "new work on the same issue lands" &&
    assert_eq "$first" "$(origin_git rev-parse main~1)" "the new work lands on top of the first landing" &&
    assert_eq "Teach thing a third trick (CF-900, #42)" "$(origin_git log -1 --format=%s main)" "new landing subject"
}

# base_run_gh VIEW...: gh ahead of the fake whose `run view <id> --json
# status,conclusion` for main's pre-landing commit answers each VIEW in turn
# (the last one repeats). Every other call goes to the fake gh.
base_run_gh() {
  local i=1 v
  sandbox_dir_guard || return 1
  for v in "$@"; do
    echo "$v" > "$SANDBOX/base-view.$i"
    i=$((i + 1))
  done
  shim gh <<EOF || return 1
#!/bin/bash
D="\$FAKE_GH_DIR"
if [ "\$1 \${2:-}" = "run view" ] && [ "\$3" = "\$(cat "\$D/runs/$BASE_SHA" 2>/dev/null)" ]; then
  case "\$*" in
    *status,conclusion*)
      echo "\$*" >> "\$D/calls.log"
      n=\$(( \$(cat "$SANDBOX/base-views" 2>/dev/null || echo 0) + 1 ))
      echo \$n > "$SANDBOX/base-views"
      [ \$n -lt $# ] || n=$#
      cat "$SANDBOX/base-view.\$n"
      exit 0
      ;;
  esac
fi
exec "$TEST_DIR/fakebin/gh" "\$@"
EOF
}

test_red_main_is_not_landed_on() {
  land_repo || return 1
  # A second attempt that failed five minutes ago: not rerun again yet.
  base_run_gh "{\"attempt\":2,\"status\":\"completed\",\"conclusion\":\"failure\",\"updatedAt\":\"$(iso_ago 5)\"}" || return 1
  printf 'green\n' > "$FAKE_GH_DIR/ci-results"
  local out rc
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 8 "$rc" "a red main stops the landing" &&
    assert_contains "$out" "MAIN-RED https://ci.example/runs/" "main-red output names main's run" &&
    assert_eq "$BASE_SHA" "$(origin_git rev-parse main)" "nothing is pushed onto a red main" &&
    assert_eq yes "$(has_branch CF-900-thing)" "the topic branch is kept" &&
    assert_eq no "$(has_worktree)" "no scratch worktree is left" &&
    assert_eq 0 "$(grep -c 'run rerun' "$FAKE_GH_DIR/calls.log")" "a recently failed second attempt is not rerun" &&
    assert_eq "handed-back severity:P2" "$(labels_of 42)" "MAIN-RED leaves the issue's labels as they are" &&
    assert_eq 0 "$(grep -c '^issue \(edit\|comment\)' "$FAKE_GH_DIR/calls.log")" "MAIN-RED neither edits nor comments"
}

test_pending_main_is_waited_for() {
  land_repo || return 1
  base_run_gh '{"status":"in_progress","conclusion":""}' '{"status":"queued","conclusion":null}' \
    '{"status":"completed","conclusion":"success"}' || return 1
  printf 'green\n' > "$FAKE_GH_DIR/ci-results"
  local out rc
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 0 "$rc" "lands once main's run completes green" &&
    assert_contains "$out" "LANDED $(origin_git rev-parse main) " "landing output" &&
    assert_eq 3 "$(cat "$SANDBOX/base-views")" "main's run is polled until it completes"
}

test_watch_that_exits_before_the_run_completes_is_rewatched() {
  land_repo || return 1
  # The first watch exits non-zero (a dropped connection) while the run is still
  # in progress; the second watch goes to the fake and reads green.
  shim gh <<EOF || return 1
#!/bin/bash
D="\$FAKE_GH_DIR"
case "\$1 \${2:-}" in
"run watch")
  if [ ! -e "\$D/watch-1" ]; then
    : > "\$D/watch-1"
    echo "\$*" >> "\$D/calls.log"
    exit 1
  fi
  ;;
"run view")
  case "\$*" in *status,conclusion*)
    if [ -e "\$D/watch-1" ]; then
      echo "\$*" >> "\$D/calls.log"
      echo '{"status":"in_progress","conclusion":""}'
      exit 0
    fi
    ;;
  esac
  ;;
esac
exec "$TEST_DIR/fakebin/gh" "\$@"
EOF
  printf 'green\n' > "$FAKE_GH_DIR/ci-results"
  local out rc
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 0 "$rc" "a watch that ended early is not a red run" &&
    assert_contains "$out" "LANDED $(origin_git rev-parse main) " "landing output" &&
    assert_eq 2 "$(grep -c 'run watch' "$FAKE_GH_DIR/calls.log")" "the run is watched again" &&
    assert_eq 0 "$(grep -c 'run rerun' "$FAKE_GH_DIR/calls.log")" "nothing is rerun"
}

test_rerun_wait_ends_when_the_attempt_moves() {
  land_repo || return 1
  # The landing's run fails only in e2e (attempt 1). After the rerun, run view
  # reports attempt 2 while its job list still lags on the old failure; the
  # watch goes green once a view has seen attempt 2.
  shim gh <<EOF || return 1
#!/bin/bash
D="\$FAKE_GH_DIR"
jobs_failed='[{"name":"test","status":"completed","conclusion":"success"},{"name":"e2e","status":"completed","conclusion":"failure"}]'
case "\$1 \${2:-}" in
"run rerun")
  echo "\$*" >> "\$D/calls.log"
  : > "\$D/reran"
  exit 0
  ;;
"run view")
  case "\$*" in *jobs*)
    echo "\$*" >> "\$D/calls.log"
    if [ -e "\$D/reran" ]; then
      echo x >> "\$D/views-after-rerun"
      echo "{\"attempt\":2,\"jobs\":\$jobs_failed}"
    else
      echo "{\"attempt\":1,\"jobs\":\$jobs_failed}"
    fi
    exit 0
    ;;
  esac
  ;;
"run watch")
  echo "\$*" >> "\$D/calls.log"
  [ -s "\$D/views-after-rerun" ]
  exit
  ;;
esac
exec "$TEST_DIR/fakebin/gh" "\$@"
EOF
  : > "$FAKE_GH_DIR/ci-results"
  local out rc
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 0 "$rc" "the rerun lands" &&
    assert_eq 1 "$(grep -c 'run rerun' "$FAKE_GH_DIR/calls.log")" "e2e is rerun once" &&
    assert_eq 1 "$(($(wc -l < "$FAKE_GH_DIR/views-after-rerun")))" "one view showing attempt 2 ends the wait"
}

test_ai_attribution_never_reaches_main() {
  land_repo || return 1
  sandbox_guard || return 1
  git checkout -q CF-900-thing &&
    echo three >> thing.txt || return 1
  printf '%s\n' "Teach thing a third trick" "" "Third body, generated by the maintainer's script." \
    "CRD schemas are generated by controller-gen for the Azure AI provider." "" \
    "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>" \
    "🤖 Generated with [Claude Code](https://claude.com/claude-code)" \
    "Co-authored-by: Jane Human <jane@example.com>" > "$SANDBOX/msg"
  sandbox_guard || return 1
  git commit -q -a -F "$SANDBOX/msg" &&
    git push -q origin CF-900-thing &&
    git checkout -q main || return 1
  printf 'green\n' > "$FAKE_GH_DIR/ci-results"
  local rc msg
  "$LAND" 42 >/dev/null 2>&1; rc=$?
  msg="$(origin_git log -1 --format=%B main)"
  assert_eq 0 "$rc" "landing" &&
    assert_contains "$msg" "Co-authored-by: Jane Human <jane@example.com>" "human co-authors are kept" &&
    assert_contains "$msg" "Third body, generated by the maintainer's script." "ordinary body lines are kept" &&
    assert_contains "$msg" "CRD schemas are generated by controller-gen for the Azure AI provider." "prose about generated code is kept" &&
    assert_not_contains "$msg" "noreply@anthropic.com" "AI co-author trailers are dropped" &&
    assert_not_contains "$msg" "Generated with" "generated-with lines are dropped"
}

test_claims_from_outside_the_project_are_ignored() {
  land_repo || return 1
  sandbox_guard || return 1
  git checkout -q -b CF-901-evil main &&
    echo evil > evil.txt &&
    git add evil.txt &&
    git commit -q -m "Something else" &&
    git push -q origin CF-901-evil &&
    git checkout -q main || return 1
  local f="$FAKE_GH_DIR/issues/42.json"
  jq --arg b "taking — CF-901-evil · driver x · lease until $(iso_at 60) · files: evil.txt" \
    '.comments = ([.comments[] | . + {authorAssociation: "MEMBER"}]
                  + [{body: $b, createdAt: "2026-09-11T05:59:00Z", authorAssociation: "NONE"}])' \
    "$f" > "$f.tmp" && mv "$f.tmp" "$f"
  printf 'green\n' > "$FAKE_GH_DIR/ci-results"
  local rc
  "$LAND" 42 >/dev/null 2>&1; rc=$?
  assert_eq 0 "$rc" "landing" &&
    assert_eq "Teach thing a second trick (CF-900, #42)" "$(origin_git log -1 --format=%s main)" "the member's claim decides the branch" &&
    assert_eq "" "$(origin_git ls-tree --name-only main evil.txt)" "the outsider's branch is not landed" &&
    assert_eq yes "$(has_branch CF-901-evil)" "the outsider's branch is untouched"
}

# hung_watch_gh N: gh ahead of the fake whose first N `run watch` calls hang.
hung_watch_gh() {
  shim gh <<EOF || return 1
#!/bin/bash
D="\$FAKE_GH_DIR"
if [ "\$1 \${2:-}" = "run watch" ]; then
  n=\$(( \$(cat "\$D/watches" 2>/dev/null || echo 0) + 1 ))
  echo \$n > "\$D/watches"
  if [ \$n -le $1 ]; then
    echo "\$*" >> "\$D/calls.log"
    sleep 30
    exit 1
  fi
fi
exec "$TEST_DIR/fakebin/gh" "\$@"
EOF
}

test_hung_watch_times_out_and_reverts() {
  land_repo || return 1
  hung_watch_gh 1 || return 1
  export CF_LAND_WATCH_TIMEOUT_SEC=2
  printf 'green\n' > "$FAKE_GH_DIR/ci-results"
  local out rc
  SECONDS=0
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 7 "$rc" "a landing whose watch hangs is reverted" &&
    assert_contains "$out" "REVERTED https://ci.example/runs/" "reverted output" &&
    assert_eq "$(origin_git rev-parse "$BASE_SHA^{tree}")" "$(origin_git rev-parse 'main^{tree}')" "main's tree is restored" &&
    { [ "$SECONDS" -lt 20 ] || fail "the hung watch was not cut off: ${SECONDS}s"; }
}

test_hung_revert_watch_is_reverted_red() {
  land_repo || return 1
  hung_watch_gh 99 || return 1
  export CF_LAND_WATCH_TIMEOUT_SEC=2
  : > "$FAKE_GH_DIR/ci-results"
  local out rc
  SECONDS=0
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 8 "$rc" "a revert whose watch hangs leaves main red" &&
    assert_contains "$out" "REVERTED-RED https://ci.example/runs/" "reverted-red output" &&
    { [ "$SECONDS" -lt 20 ] || fail "the hung watches were not cut off: ${SECONDS}s"; }
}

# red_main_gh CONCLUSION TEST E2E RERUN: gh ahead of the fake for main's
# pre-landing run (attempt 1): status completed with CONCLUSION, its test and e2e
# jobs concluding TEST and E2E. After `run rerun` of that run it reads attempt 2
# with e2e in progress, and watching it exits 0 when RERUN is green. Every other
# call goes to the fake gh.
red_main_gh() {
  local attempt1="{\"attempt\":1,\"jobs\":[{\"name\":\"test\",\"status\":\"completed\",\"conclusion\":\"$2\"},{\"name\":\"e2e\",\"status\":\"completed\",\"conclusion\":\"$3\"}]}"
  local attempt2='{"attempt":2,"jobs":[{"name":"test","status":"completed","conclusion":"success"},{"name":"e2e","status":"in_progress","conclusion":""}]}'
  shim gh <<EOF || return 1
#!/bin/bash
D="\$FAKE_GH_DIR"
main_id="\$(cat "\$D/runs/$BASE_SHA" 2>/dev/null)"
if [ -n "\$main_id" ] && [ "\${3:-}" = "\$main_id" ]; then
  case "\$1 \$2" in
  "run view")
    echo "\$*" >> "\$D/calls.log"
    case "\$*" in
      *status,conclusion*) echo '{"status":"completed","conclusion":"$1"}' ;;
      *) if [ -e "\$D/main-reran" ]; then echo '$attempt2'; else echo '$attempt1'; fi ;;
    esac
    exit 0
    ;;
  "run rerun")
    echo "\$*" >> "\$D/calls.log"
    : > "\$D/main-reran"
    exit 0
    ;;
  "run watch")
    echo "\$*" >> "\$D/calls.log"
    [ -e "\$D/main-reran" ] && [ "$4" = green ]
    exit
    ;;
  esac
fi
exec "$TEST_DIR/fakebin/gh" "\$@"
EOF
}

test_p0_lands_on_red_main() {
  land_repo || return 1
  issue_fixture 42 OPEN "handed-back,severity:P0" \
    "taking — CF-900-thing · driver d06-0300Z · lease until $(iso_at 30) · files: thing.txt" 90
  red_main_gh failure failure success red || return 1
  printf 'green\n' > "$FAKE_GH_DIR/ci-results"
  local out rc
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 0 "$rc" "the P0 that fixes main lands on a red main" &&
    assert_contains "$out" "LANDED $(origin_git rev-parse main) " "landing output" &&
    assert_eq 0 "$(grep -c 'run rerun' "$FAKE_GH_DIR/calls.log")" "main is not rerun for a P0"
}

test_main_red_only_in_e2e_is_rerun_once() {
  land_repo || return 1
  red_main_gh failure success failure green || return 1
  printf 'green\n' > "$FAKE_GH_DIR/ci-results"
  local out rc main_id
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  main_id="$(cat "$FAKE_GH_DIR/runs/$BASE_SHA")"
  assert_eq 0 "$rc" "main green after one e2e rerun takes the landing" &&
    assert_contains "$out" "LANDED $(origin_git rev-parse main) " "landing output" &&
    assert_eq 1 "$(grep -c "run rerun $main_id --failed" "$FAKE_GH_DIR/calls.log")" "main's failed e2e job is rerun once" &&
    assert_eq 1 "$(grep -c 'run rerun' "$FAKE_GH_DIR/calls.log")" "nothing else is rerun"
}

test_main_red_in_e2e_after_its_rerun_stays_main_red() {
  land_repo || return 1
  red_main_gh failure success failure red || return 1
  printf 'green\n' > "$FAKE_GH_DIR/ci-results"
  local out rc
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 8 "$rc" "main still red after its one rerun" &&
    assert_contains "$out" "MAIN-RED https://ci.example/runs/" "main-red output" &&
    assert_eq "$BASE_SHA" "$(origin_git rev-parse main)" "nothing is pushed" &&
    assert_eq 1 "$(grep -c 'run rerun' "$FAKE_GH_DIR/calls.log")" "main is rerun at most once"
}

test_cancelled_main_is_rerun_in_full_once() {
  land_repo || return 1
  red_main_gh cancelled cancelled cancelled green || return 1
  printf 'green\n' > "$FAKE_GH_DIR/ci-results"
  local out rc main_id
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  main_id="$(cat "$FAKE_GH_DIR/runs/$BASE_SHA")"
  assert_eq 0 "$rc" "a cancelled main run is rerun and then taken as green" &&
    assert_contains "$out" "LANDED $(origin_git rev-parse main) " "landing output" &&
    assert_eq 1 "$(grep -c "run rerun $main_id\$" "$FAKE_GH_DIR/calls.log")" "the whole cancelled run is rerun once"
}

test_new_claim_after_landing_with_missing_branch_is_not_resumed() {
  land_repo || return 1
  printf 'green\ngreen\n' > "$FAKE_GH_DIR/ci-results"
  local rc1 landed after out rc f="$FAKE_GH_DIR/issues/42.json"
  "$LAND" 42 >/dev/null 2>&1; rc1=$?
  landed="$(origin_git rev-parse main)"
  # A newer claim, made after that landing, names a branch never pushed.
  after="$(jq -nr --argjson t "$(($(origin_git log -1 --format=%ct main) + 60))" '$t | strftime("%Y-%m-%dT%H:%M:%SZ")')"
  jq --arg b "taking — CF-900-again · driver d08-0600Z · lease until $(iso_at 30) · files: thing.txt" --arg at "$after" \
    '.comments += [{body: $b, createdAt: $at}]' "$f" > "$f.tmp" && mv "$f.tmp" "$f"
  relabel_handed_back 42 || return 1
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 0 "$rc1" "first landing" &&
    assert_eq 70 "$rc" "a claim newer than the landing is new work whose branch is missing" &&
    assert_eq "" "$out" "no result line" &&
    assert_eq "$landed" "$(origin_git rev-parse main)" "main is unchanged"
}

test_rerun_wait_needs_the_attempt_to_move_when_gh_reports_one() {
  land_repo || return 1
  # After the rerun, the first two views still read attempt 1 but with an empty
  # job list; the watch goes green only once a view has read attempt 2.
  shim gh <<EOF || return 1
#!/bin/bash
D="\$FAKE_GH_DIR"
jobs_failed='[{"name":"test","status":"completed","conclusion":"success"},{"name":"e2e","status":"completed","conclusion":"failure"}]'
case "\$1 \${2:-}" in
"run rerun")
  echo "\$*" >> "\$D/calls.log"
  : > "\$D/reran"
  exit 0
  ;;
"run view")
  case "\$*" in *jobs*)
    echo "\$*" >> "\$D/calls.log"
    if [ -e "\$D/reran" ]; then
      echo x >> "\$D/views-after-rerun"
      if [ "\$((\$(wc -l < "\$D/views-after-rerun")))" -le 2 ]; then
        echo '{"attempt":1,"jobs":[]}'
      else
        : > "\$D/saw-attempt-2"
        echo '{"attempt":2,"jobs":[]}'
      fi
    else
      echo "{\"attempt\":1,\"jobs\":\$jobs_failed}"
    fi
    exit 0
    ;;
  esac
  ;;
"run watch")
  echo "\$*" >> "\$D/calls.log"
  [ -e "\$D/saw-attempt-2" ]
  exit
  ;;
esac
exec "$TEST_DIR/fakebin/gh" "\$@"
EOF
  : > "$FAKE_GH_DIR/ci-results"
  local out rc
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 0 "$rc" "the rerun lands once its attempt moves" &&
    assert_eq 3 "$(($(wc -l < "$FAKE_GH_DIR/views-after-rerun")))" "an unchanged attempt keeps the wait going"
}

test_watch_ignoring_term_is_killed() {
  land_repo || return 1
  # The first watch ignores SIGTERM and outlives the deadline by far.
  shim gh <<EOF || return 1
#!/bin/bash
D="\$FAKE_GH_DIR"
if [ "\$1 \${2:-}" = "run watch" ] && [ ! -e "\$D/stubborn" ]; then
  : > "\$D/stubborn"
  echo "\$*" >> "\$D/calls.log"
  trap '' TERM
  for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25; do sleep 1; done
  exit 1
fi
exec "$TEST_DIR/fakebin/gh" "\$@"
EOF
  export CF_LAND_WATCH_TIMEOUT_SEC=2
  printf 'green\n' > "$FAKE_GH_DIR/ci-results"
  local out rc
  SECONDS=0
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 7 "$rc" "a watch that ignores SIGTERM is killed and the landing reverted" &&
    assert_contains "$out" "REVERTED https://ci.example/runs/" "reverted output" &&
    { [ "$SECONDS" -lt 20 ] || fail "the stubborn watch was not killed: ${SECONDS}s"; }
}

# main_run_gh ATTEMPT CONCLUSION FAILED_JOB UPDATED_MIN_AGO RERUN: gh ahead of the
# fake for main's pre-landing run, as GitHub reports it: attempt ATTEMPT,
# completed with CONCLUSION, job FAILED_JOB (test, acceptance or e2e) failed, last
# updated UPDATED_MIN_AGO minutes before CF_NOW. After `run rerun` of that run it
# reads the next attempt, and watching it exits 0 when RERUN is green. Every
# other call goes to the fake gh.
main_run_gh() {
  sandbox_dir_guard || return 1
  local jobs rerun_conclusion=failure
  [ "$5" != green ] || rerun_conclusion=success
  jobs="$(jq -cn --arg f "$3" '[("test", "acceptance", "e2e") | {name: ., status: "completed",
    conclusion: (if . == $f then "failure" else "success" end)}]')" || return 1
  jq -cn --argjson a "$1" --arg c "$2" --arg u "$(iso_ago "$4")" --argjson j "$jobs" \
    '{attempt: $a, status: "completed", conclusion: $c, updatedAt: $u, jobs: $j}' > "$SANDBOX/main-run.json" &&
    jq -cn --argjson a "$(($1 + 1))" --arg c "$rerun_conclusion" --arg u "$(iso_ago 0)" \
      '{attempt: $a, status: "completed", conclusion: $c, updatedAt: $u, jobs: []}' > "$SANDBOX/main-rerun.json" ||
    return 1
  shim gh <<EOF
#!/bin/bash
D="\$FAKE_GH_DIR"
main_id="\$(cat "\$D/runs/$BASE_SHA" 2>/dev/null)"
if [ -n "\$main_id" ] && [ "\${3:-}" = "\$main_id" ]; then
  case "\$1 \$2" in
  "run view")
    echo "\$*" >> "\$D/calls.log"
    if [ -e "\$D/main-reran" ]; then cat "$SANDBOX/main-rerun.json"; else cat "$SANDBOX/main-run.json"; fi
    exit 0
    ;;
  "run rerun")
    echo "\$*" >> "\$D/calls.log"
    : > "\$D/main-reran"
    exit 0
    ;;
  "run watch")
    echo "\$*" >> "\$D/calls.log"
    [ -e "\$D/main-reran" ] && [ "$5" = green ]
    exit
    ;;
  esac
fi
exec "$TEST_DIR/fakebin/gh" "\$@"
EOF
}

test_main_first_attempt_red_outside_e2e_is_rerun() {
  land_repo || return 1
  main_run_gh 1 failure acceptance 5 green || return 1
  printf 'green\n' > "$FAKE_GH_DIR/ci-results"
  local out rc main_id
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  main_id="$(cat "$FAKE_GH_DIR/runs/$BASE_SHA")"
  assert_eq 0 "$rc" "a first attempt red in acceptance is rerun, and its green rerun takes the landing" &&
    assert_contains "$out" "LANDED $(origin_git rev-parse main) " "landing output" &&
    assert_eq 1 "$(grep -c "run rerun $main_id --failed" "$FAKE_GH_DIR/calls.log")" "main's failed jobs are rerun once" &&
    assert_eq 1 "$(grep -c 'run rerun' "$FAKE_GH_DIR/calls.log")" "nothing else is rerun"
}

test_main_second_attempt_red_recently_is_not_rerun() {
  land_repo || return 1
  main_run_gh 2 failure e2e 5 green || return 1
  printf 'green\n' > "$FAKE_GH_DIR/ci-results"
  local out rc
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 8 "$rc" "a second attempt red five minutes ago stops the landing" &&
    assert_contains "$out" "MAIN-RED https://ci.example/runs/" "main-red output" &&
    assert_eq "$BASE_SHA" "$(origin_git rev-parse main)" "nothing is pushed" &&
    assert_eq 0 "$(grep -c 'run rerun' "$FAKE_GH_DIR/calls.log")" "main is not rerun again, not even for e2e"
}

test_main_second_attempt_red_long_ago_is_rerun() {
  land_repo || return 1
  main_run_gh 2 failure acceptance 120 green || return 1
  printf 'green\n' > "$FAKE_GH_DIR/ci-results"
  local out rc main_id
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  main_id="$(cat "$FAKE_GH_DIR/runs/$BASE_SHA")"
  assert_eq 0 "$rc" "a second attempt red two hours ago is rerun and lands when green" &&
    assert_contains "$out" "LANDED $(origin_git rev-parse main) " "landing output" &&
    assert_eq 1 "$(grep -c "run rerun $main_id --failed" "$FAKE_GH_DIR/calls.log")" "main is rerun once"
}

test_main_rerun_after_is_configurable() {
  land_repo || return 1
  main_run_gh 3 timed_out acceptance 5 green || return 1
  export CF_LAND_MAIN_RERUN_AFTER_SEC=60
  printf 'green\n' > "$FAKE_GH_DIR/ci-results"
  local out rc
  out="$("$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 0 "$rc" "a run idle longer than CF_LAND_MAIN_RERUN_AFTER_SEC is rerun" &&
    assert_contains "$out" "LANDED " "landing output" &&
    assert_eq 1 "$(grep -c 'run rerun' "$FAKE_GH_DIR/calls.log")" "main is rerun once"
}

# cwd_gh: gh ahead of the fake that, like the real gh left to infer its
# repository, fails in a working directory that no longer exists. It logs the
# GH_REPO each call saw.
cwd_gh() {
  shim gh <<EOF
#!/bin/bash
if ! pwd -P >/dev/null 2>&1; then
  echo "\$*" >> "$SANDBOX/gh-in-deleted-cwd"
  exit 1
fi
echo "\${GH_REPO-unset}" >> "$SANDBOX/gh-repo"
exec "$TEST_DIR/fakebin/gh" "\$@"
EOF
}

test_caller_worktree_deleted_mid_run_still_lands() {
  land_repo || return 1
  sandbox_guard || return 1
  git worktree add -q --detach "$SANDBOX/work/.worktrees/caller" main || return 1
  cwd_gh || return 1
  unset GH_REPO
  # The gates remove the worktree land.sh was started from.
  export CF_LAND_GATES="rm -rf '$SANDBOX/work/.worktrees/caller'"
  printf 'green\n' > "$FAKE_GH_DIR/ci-results"
  local out rc
  out="$(cd "$SANDBOX/work/.worktrees/caller" && "$LAND" 42 2>/dev/null)"; rc=$?
  assert_eq 0 "$rc" "land.sh keeps working after its caller's directory is gone" &&
    assert_contains "$out" "LANDED $(origin_git rev-parse main) " "landing output" &&
    assert_eq no "$([ -d "$SANDBOX/work/.worktrees/caller" ] && echo yes || echo no)" "the caller's worktree was removed" &&
    assert_eq "" "$(cat "$SANDBOX/gh-in-deleted-cwd" 2>/dev/null)" "no gh call ran in the deleted directory" &&
    assert_eq unset "$(sort -u "$SANDBOX/gh-repo")" "a local origin path sets no GH_REPO"
}

# gh_repo_from URL: land from a clone whose origin is configured as URL, with git
# rewriting URL to the sandbox's bare repo (so nothing leaves the sandbox), and
# print the distinct GH_REPO values land.sh's gh calls saw.
gh_repo_from() {
  sandbox_guard || return 1
  git config remote.origin.url "$1" &&
    git config "url.$SANDBOX/origin.git.insteadOf" "$1" || return 1
  # get-url applies insteadOf: the guard still sees the sandbox's bare repo.
  sandbox_guard || return 1
  cwd_gh || return 1
  unset GH_REPO
  printf 'green\n' > "$FAKE_GH_DIR/ci-results"
  "$LAND" 42 > "$SANDBOX/out" 2>/dev/null || return 1
  sort -u "$SANDBOX/gh-repo"
}

test_gh_repo_comes_from_a_github_ssh_origin() {
  land_repo || return 1
  local seen
  seen="$(gh_repo_from "git@github.com:acme/widget.git")" || { fail "landing through a rewritten origin failed"; return 1; }
  assert_eq "acme/widget" "$seen" "GH_REPO for git@github.com:acme/widget.git" &&
    assert_contains "$(cat "$SANDBOX/out")" "LANDED " "landing output"
}

test_gh_repo_keeps_a_non_github_host() {
  land_repo || return 1
  local seen
  seen="$(gh_repo_from "https://git.example.invalid:8443/acme/widget/")" || { fail "landing through a rewritten origin failed"; return 1; }
  assert_eq "git.example.invalid/acme/widget" "$seen" "GH_REPO for an https origin on another host"
}
