#!/bin/bash
# land.sh <issue>
#
# The only way a branch reaches main. Run from inside any worktree of the clone.
# It lands the branch named by the issue's newest claim comment from a project
# member (`taking — <branch> · driver …`, or the legacy `taking — <branch> (wave …`):
# rebase onto origin/main (pinned when fetched) in a scratch worktree at
# <main repo>/.worktrees/land-CF-NNN, run the gates, squash to ONE commit
# ("<newest subject> (CF-NNN, #<issue>)", the branch's commit bodies oldest
# first, AI attribution lines dropped), push it to main, watch its CI run, and
# revert it if CI is red. The rebase linearizes the branch: merge commits are
# dropped and the commits they brought in are replayed.
#
# NEVER run this by hand against the real repository casually: it pushes main.
# There is no dry run. Tests drive it against a local bare origin and a fake gh.
#
# The whole script, including the CI watch and any revert, runs under lock pool
# `merge` (1 slot): it re-execs itself through lock.sh, which leaves the lock on
# an inherited descriptor (the highest free one in 9..3). One landing at a time,
# and nobody lands on top of an unwatched push. Every git call disables auto gc,
# auto maintenance and fsmonitor so no detached git process inherits the lock;
# the gates and `gh run watch` run with descriptors 3-9 closed. Every wait while
# the lock is held is bounded: ssh keepalives, the gate pool's lock timeout, and
# a deadline on each watched run.
#
# Resuming: when main's last 200 first-parent commits hold this issue's landing
# `… (CF-NNN, #<issue>)` with no newer `Revert "…"` of it, and the claimed branch
# is gone or its content is exactly that landing, land.sh does not land again: it
# watches that commit (a run killed after its push, or a lost report).
# Before landing new work it checks main's own CI run and refuses a red main.
#
# Callers decide by the stdout line (exactly one; everything else is stderr):
#   0  LANDED <sha> <run-url>            CI green; topic branch deleted
#   5  NOT-HANDED-BACK                   issue lacks `handed-back`; nothing fetched
#   6  PARKED rebase-conflict | PARKED gates-red | PARKED push-rejected
#                                        main untouched
#   7  REVERTED <failed run-url>         CI red; revert pushed and its CI green;
#                                        topic branch kept
#   8  REVERTED-RED <failed run-url>     CI red and the revert's CI red too, or the
#                                        revert could not be pushed: main is red
#   8  REVERTED-RED push-unknown         the push failed and origin could not be
#                                        read: main's state is unknown
#   8  MAIN-RED <run-url>                main's CI was already red; nothing pushed
#  64  usage error, a bad environment value, or no member's claim naming a
#      CF-<digits> branch (stderr only, empty stdout)
#  70  gh, git or jq failed, the branch is missing, or it has nothing to land,
#      before anything was pushed (stderr only, empty stdout)
# A run url reads `no-ci-run` when no CI run appeared for a pushed sha.
# lock.sh may also end the run with 2, 73 or 75 and an empty stdout (see its header).
# The only e2e rerun: when a run's single non-passing job is `e2e` with conclusion
# failure, it is rerun once (`gh run rerun <id> --failed`) and watched again once
# `gh run view` shows a new attempt (at most 30 polls, every CF_CI_POLL_SEC).
#
# Environment:
#   CF_LAND_GATES   shell command run in the rebased worktree. Default:
#                   make lint && make lint-strict && go test -short <packages with
#                   changed .go files> (no go test when no Go file changed)
#   CF_CI_POLL_SEC  wait between gh polls (default 10); a pushed sha with no run
#                   after 60 polls is treated as red
#   CF_LAND_WATCH_TIMEOUT_SEC       deadline per watched run (default 2700); a
#                   landing past it is reverted, a revert past it is REVERTED-RED
#   CF_LAND_GATE_LOCK_TIMEOUT_SEC   CF_LOCK_TIMEOUT_SEC for the gates (default 1800)
#   plus lock.sh's.
# land.sh never edits, comments on or closes issues.
# Written for /bin/bash 3.2; all JSON is jq.
set -u

MAX_POLLS=60     # `gh run list` polls for a pushed sha
RERUN_POLLS=30   # `gh run view` polls for a rerun to start
MAIN_POLLS=90    # `gh run view` polls for main's pending run
WATCH_TRIES=5    # `gh run watch` calls per run while it still reads in progress
HISTORY=200      # first-parent commits of main searched for an earlier landing

usage() {
  echo "usage: land.sh <issue>" >&2
  echo "       <issue> is a positive integer" >&2
  exit 64
}

bad_env() {
  echo "land.sh: $1" >&2
  exit 64
}

[ $# -eq 1 ] || usage
case "$1" in '' | 0* | *[!0-9]*) usage ;; esac

poll="${CF_CI_POLL_SEC:-10}"
case "$poll" in '' | . | *[!0-9.]* | *.*.*) bad_env "CF_CI_POLL_SEC must be a number of seconds" ;; esac
watch_timeout="${CF_LAND_WATCH_TIMEOUT_SEC:-2700}"
case "$watch_timeout" in '' | *[!0-9]*) bad_env "CF_LAND_WATCH_TIMEOUT_SEC must be whole seconds" ;; esac
watch_timeout=$((10#$watch_timeout))
[ "$watch_timeout" -ge 1 ] || bad_env "CF_LAND_WATCH_TIMEOUT_SEC must be at least 1"
gate_lock_timeout="${CF_LAND_GATE_LOCK_TIMEOUT_SEC:-1800}"
case "$gate_lock_timeout" in '' | *[!0-9]*) bad_env "CF_LAND_GATE_LOCK_TIMEOUT_SEC must be whole seconds" ;; esac

# Serialize: the pid survives both execs, so a marker inherited from some
# other process never matches and cannot skip the lock.
if [ "${CF_LAND_LOCKED:-}" != "$$" ]; then
  CF_LAND_LOCKED=$$
  export CF_LAND_LOCKED
  exec "$(dirname "$0")/lock.sh" merge 1 -- "$0" "$@"
fi

issue="$1"

say() { echo "land.sh: $*" >&2; }
die() {
  say "$*"
  exit 70
}

# No prompt and no silent stall on the network while the merge lock is held.
GIT_SSH_COMMAND="${GIT_SSH_COMMAND:-ssh -o BatchMode=yes -o ServerAliveInterval=15 -o ServerAliveCountMax=4}"
GIT_TERMINAL_PROMPT=0
export GIT_SSH_COMMAND GIT_TERMINAL_PROMPT

# Every git call: nothing that detaches (auto gc, auto maintenance, fsmonitor
# daemon) and would keep the merge lock's descriptor open after land.sh exits,
# and no user config that would sign, stash or move other refs.
g() {
  git -c gc.auto=0 -c maintenance.auto=false -c core.fsmonitor=false \
    -c commit.gpgsign=false -c rebase.updateRefs=false -c rebase.autoStash=false "$@"
}

# Step 1: the issue must be handed back, before anything is fetched.
issue_json="$(gh issue view "$issue" --json number,title,state,labels,comments,updatedAt)" ||
  die "gh issue view $issue failed"
handed="$(printf '%s\n' "$issue_json" | jq -r 'if any(.labels[]?; .name == "handed-back") then 1 else 0 end')" ||
  die "could not read issue $issue"
if [ "$handed" != 1 ]; then
  echo "NOT-HANDED-BACK"
  exit 5
fi

# Step 2: branch from the newest `taking —` comment by a project member (the repo
# is public: anyone can comment); CF-NNN from its prefix. A comment without
# authorAssociation is trusted.
claim="$(printf '%s\n' "$issue_json" | jq -r '
  [.comments[]? | select((.body // "") | startswith("taking —"))] as $all
  | [$all[] | select(.authorAssociation == null or .authorAssociation == "OWNER"
                     or .authorAssociation == "MEMBER" or .authorAssociation == "COLLABORATOR")] as $trusted
  | ($trusted | last) as $c
  | "\(($all | length) - ($trusted | length))\t" +
    (if $c == null then "\t"
     else ([$c.body | split("\n")[0] | capture("^taking —\\s+(?<b>\\S+)") | .b] | first // "") as $b
       | ([$b | capture("^(?<cf>CF-[0-9]+)") | .cf] | first // "") as $cf
       | "\($b)\t\($cf)"
     end)
')" || die "could not read the claim on issue $issue"
ignored="${claim%%$'\t'*}"
claim="${claim#*$'\t'}"
branch="${claim%%$'\t'*}"
cf="${claim#*$'\t'}"
[ "$ignored" = 0 ] || say "ignoring $ignored claim comment(s) on issue $issue from outside the project"
if [ -z "$branch" ]; then
  say "issue $issue has no \`taking — <branch>\` claim comment from a project member"
  exit 64
fi
if [ -z "$cf" ] || ! g check-ref-format "refs/heads/$branch"; then
  say "claimed branch '$branch' on issue $issue is not a valid CF-<digits> branch"
  exit 64
fi
suffix="($cf, #$issue)"
say "issue #$issue: landing $branch as $cf"

# Step 3: fetch, pin the base, scratch worktree, rebase.
common="$(g rev-parse --path-format=absolute --git-common-dir)" || die "not inside a git repository"
case "$common" in
  */.git) top="${common%/.git}" ;;
  *) die "the clone at $common has no main worktree to hold .worktrees/" ;;
esac
wt="$top/.worktrees/land-$cf"

# remove_worktree: remove the scratch worktree if present, then prune, so a
# registration whose directory was deleted cannot block the next `worktree add`.
remove_worktree() {
  if [ -e "$wt" ]; then
    g -C "$top" worktree remove --force "$wt" >/dev/null 2>&1 || rm -rf "$wt"
  fi
  g -C "$top" worktree prune >&2
}

g -C "$top" fetch --prune origin >&2 || die "git fetch origin failed"
# Pin the base: refs/remotes/origin/main is shared by every worktree of the
# clone, and another session's fetch can move it while the gates run.
base="$(g -C "$top" rev-parse --verify --quiet "refs/remotes/origin/main^{commit}")" || die "origin/main is missing"
branch_sha="$(g -C "$top" rev-parse --verify --quiet "refs/remotes/origin/$branch^{commit}")" || branch_sha=""

remove_worktree # a leftover from a crashed run
trap 'remove_worktree' EXIT

# find_run SHA: sets run_id and run_url, polling until gh lists a ci run for SHA.
find_run() {
  local i=0 json row
  run_id=""
  run_url="no-ci-run"
  while [ "$i" -lt "$MAX_POLLS" ]; do
    [ "$i" -eq 0 ] || sleep "$poll"
    i=$((i + 1))
    json="$(gh run list --workflow ci --branch main --commit "$1" --json databaseId,url)" || continue
    row="$(printf '%s\n' "$json" | jq -r '[.[] | "\(.databaseId)\t\(.url)"] | first // empty')" || continue
    if [ -n "$row" ]; then
      run_id="${row%%$'\t'*}"
      run_url="${row#*$'\t'}"
      return 0
    fi
  done
  say "no CI run appeared for $1 after $MAX_POLLS polls; treating it as red"
  return 1
}

# run_state ID: prints "<status>\t<conclusion>" (either may be empty), or fails.
run_state() {
  local json
  json="$(gh run view "$1" --json status,conclusion)" || return 1
  printf '%s\n' "$json" | jq -r '"\(.status // "")\t\(.conclusion // "")"'
}

# push_main SHA: push the worktree's HEAD (SHA) to main, never forced. A client
# that reports failure may still have updated origin, so origin is read back.
# 0 main is SHA; 1 rejected; 2 unknown (the push failed and origin is unreadable).
push_main() {
  local remote
  trap '' HUP # a closed terminal must not kill land.sh between push and watch
  g -C "$wt" push origin HEAD:main >&2 && return 0
  remote="$(g -C "$wt" ls-remote origin refs/heads/main)" || return 2
  if [ "${remote%%[[:space:]]*}" = "$1" ]; then
    say "the push reported failure, but origin main is $1"
    return 0
  fi
  return 1
}

# Resume: this issue's newest landing within HISTORY first-parent commits of
# base, unless a revert of it is newer.
history="$(g -C "$top" log --first-parent -n "$HISTORY" --format='%H %s' "$base")" ||
  die "could not read main's history"
landed=""
while read -r h s; do
  case "$s" in
    *"$suffix"*)
      case "$s" in 'Revert "'*) ;; *) landed="$h" ;; esac
      break
      ;;
  esac
done <<<"$history"
if [ -n "$landed" ] && [ -n "$branch_sha" ]; then
  # The branch still exists: resume only if it carries exactly that landing,
  # not new work on the same issue.
  merged="$(g -C "$top" merge-tree --write-tree "$landed^" "$branch_sha" 2>/dev/null)"
  if [ "${merged%%$'\n'*}" != "$(g -C "$top" rev-parse "$landed^{tree}")" ]; then
    say "issue #$issue landed before as $landed; $branch carries new work"
    landed=""
  fi
fi

if [ -n "$landed" ]; then
  say "issue #$issue already landed as $landed; watching it instead of landing again"
  g -C "$top" worktree add --detach "$wt" "$base" >&2 || die "could not add worktree $wt on $base"
  sha="$landed"
else
  [ -n "$branch_sha" ] || die "branch $branch is not on origin"

  # Main must not already be red. No run, or no status fields: proceed.
  json="$(gh run list --workflow ci --branch main --commit "$base" --json databaseId,url)" || json='[]'
  row="$(printf '%s\n' "$json" | jq -r '[.[] | "\(.databaseId)\t\(.url)"] | first // empty' 2>/dev/null)"
  if [ -n "$row" ]; then
    main_id="${row%%$'\t'*}"
    main_url="${row#*$'\t'}"
    i=0
    while state="$(run_state "$main_id")"; do
      status="${state%%$'\t'*}"
      conclusion="${state#*$'\t'}"
      if [ -n "$status" ] && [ "$status" != completed ]; then
        i=$((i + 1))
        if [ "$i" -ge "$MAIN_POLLS" ]; then
          say "main's run $main_url is still $status after $MAIN_POLLS polls; landing anyway"
          break
        fi
        sleep "$poll"
        continue
      fi
      case "$conclusion" in
        '' | success | neutral | skipped) ;;
        *)
          say "main's CI run $main_url concluded $conclusion; not landing on a red main"
          echo "MAIN-RED $main_url"
          exit 8
          ;;
      esac
      break
    done
  fi

  g -C "$top" worktree add --detach "$wt" "$branch_sha" >&2 ||
    die "could not add worktree $wt on origin/$branch"

  if ! g -C "$wt" rebase "$base" >&2; then
    g -C "$wt" rebase --abort >&2
    echo "PARKED rebase-conflict"
    exit 6
  fi

  # Step 4: gates, with the lock descriptors closed so a straggler a test leaves
  # behind cannot hold the merge lock, and a bounded wait for the gate pool.
  gates="${CF_LAND_GATES:-}"
  if [ -z "$gates" ]; then
    gates="make lint && make lint-strict"
    changed="$(g -C "$wt" diff --name-only "$base" HEAD -- '*.go')" ||
      die "could not list changed Go files"
    seen=$'\n'
    pkgs=""
    while IFS= read -r f; do
      [ -n "$f" ] || continue
      case "$f" in
        testdata/*) p=. ;;
        */testdata/*) p="./${f%%/testdata/*}" ;;
        */*) p="./${f%/*}" ;;
        *) p=. ;;
      esac
      # A package whose last Go file was deleted is gone; nothing to test there.
      ls "$wt/$p"/*.go >/dev/null 2>&1 || continue
      case "$seen" in *$'\n'"$p"$'\n'*) continue ;; esac
      seen="$seen$p"$'\n'
      pkgs="$pkgs $(printf '%q' "$p")"
    done <<<"$changed"
    [ -z "$pkgs" ] || gates="$gates && go test -short$pkgs"
  fi
  say "gates: $gates"
  if ! (cd "$wt" && CF_LOCK_TIMEOUT_SEC="$gate_lock_timeout" && export CF_LOCK_TIMEOUT_SEC &&
    /bin/bash -c "$gates") </dev/null >&2 3>&- 4>&- 5>&- 6>&- 7>&- 8>&- 9>&-; then
    echo "PARKED gates-red"
    exit 6
  fi

  # Step 5: one commit on the pinned base; a push that is not a fast-forward of
  # origin main is rejected, so a moved main parks instead of being undone.
  # strip_ai: drop AI attribution lines (AGENTS.md §4); human co-authors stay.
  strip_ai() {
    LC_ALL=C grep -v -i -E \
      -e '^[[:space:]]*co-authored-by:.*(claude|anthropic|openai|gemini|google|antigravity|copilot|cursor|noreply@anthropic\.com)' \
      -e 'generated (with|by) .*(claude|anthropic|openai|gemini|copilot|codex|cursor|antigravity|(^|[^a-z])ai([^a-z]|$))'
    [ $? -le 1 ]
  }
  subject="$(g -C "$wt" log -1 --format=%s HEAD)" || die "could not read the branch's newest subject"
  case "$subject" in *"$suffix"*) ;; *) subject="$subject $suffix" ;; esac
  message="$subject"
  commits="$(g -C "$wt" rev-list --reverse "$base..HEAD")" || die "could not list the branch's commits"
  [ -n "$commits" ] || die "branch $branch has no commits beyond origin/main"
  for c in $commits; do
    body="$(g -C "$wt" log -1 --format=%b "$c")" || die "could not read commit $c"
    body="$(printf '%s\n' "$body" | strip_ai)" || die "could not filter commit $c"
    case "$body" in *[![:space:]]*) message="$message

$body" ;; esac
  done

  g -C "$wt" reset --soft "$base" >&2 || die "could not squash onto origin/main"
  if g -C "$wt" diff --cached --quiet; then
    die "branch $branch has nothing to land against origin/main"
  fi
  printf '%s\n' "$message" | g -C "$wt" commit --quiet --no-verify --cleanup=whitespace -F - >&2 ||
    die "could not commit the squashed landing"
  sha="$(g -C "$wt" rev-parse HEAD)" || die "could not read the landing sha"

  push_main "$sha"
  case $? in
    0) ;;
    1)
      echo "PARKED push-rejected"
      exit 6
      ;;
    *)
      say "the push of $sha failed and origin could not be read; main's state is unknown"
      echo "REVERTED-RED push-unknown"
      exit 8
      ;;
  esac
  say "pushed $sha to main"
fi

# timed_watch ID DEADLINE: `gh run watch` in the background, killed at DEADLINE
# (in $SECONDS). 0 green; 1 not green; 124 deadline passed.
timed_watch() {
  local pid rc
  gh run watch "$1" --exit-status </dev/null >&2 3>&- 4>&- 5>&- 6>&- 7>&- 8>&- 9>&- &
  pid=$!
  while kill -0 "$pid" 2>/dev/null; do
    if [ "$SECONDS" -ge "$2" ]; then
      pkill -TERM -P "$pid" 2>/dev/null
      kill -TERM "$pid" 2>/dev/null
      wait "$pid" 2>/dev/null
      return 124
    fi
    sleep 0.2
  done
  wait "$pid"
  rc=$?
  [ "$rc" -ne 124 ] || rc=1
  return "$rc"
}

# watch_run ID: 0 green; 1 red; 124 past CF_LAND_WATCH_TIMEOUT_SEC. A watch that
# exits non-zero while the run still reads in progress (a dropped connection) is
# repeated, up to WATCH_TRIES, within the same deadline.
watch_run() {
  local deadline=$((SECONDS + watch_timeout)) try=1 rc state status
  while :; do
    timed_watch "$1" "$deadline"
    rc=$?
    case "$rc" in 0 | 124) return "$rc" ;; esac
    [ "$try" -lt "$WATCH_TRIES" ] || return 1
    state="$(run_state "$1")" || return 1
    status="${state%%$'\t'*}"
    if [ -z "$status" ] || [ "$status" = completed ]; then
      return 1
    fi
    say "gh run watch $1 ended while the run is $status; watching again"
    try=$((try + 1))
    sleep "$poll"
  done
}

# e2e_flake ID: the run's only non-passing job is e2e, and it failed. Sets
# flake_attempt to the run's attempt number, when gh reports one.
e2e_flake() {
  local json
  flake_attempt=""
  json="$(gh run view "$1" --json attempt,jobs)" || return 1
  flake_attempt="$(printf '%s\n' "$json" | jq -r 'if (.attempt | type) == "number" then .attempt else empty end')" ||
    flake_attempt=""
  printf '%s\n' "$json" | jq -e '
    [.jobs[]? | select(.conclusion != "success" and .conclusion != "skipped" and .conclusion != "neutral")] as $bad
    | ($bad | length) == 1 and $bad[0].name == "e2e" and $bad[0].conclusion == "failure"
  ' >/dev/null
}

# await_rerun ID ATTEMPT: poll `gh run view` until the run shows a new attempt:
# an attempt number above ATTEMPT, or no longer the old attempt's jobs (e2e
# completed with failure). The rerun endpoint is asynchronous, and `gh run watch
# --exit-status` on a run that still reads completed exits at once with the old
# conclusion. Queued or running jobs carry an empty or null conclusion and a
# status other than completed; an empty job list is a new attempt with no jobs
# yet. Gives up after RERUN_POLLS and lets the watch decide.
await_rerun() {
  local i=0 json state before="$2"
  case "$before" in *[!0-9]*) before="" ;; esac
  while [ "$i" -lt "$RERUN_POLLS" ]; do
    [ "$i" -eq 0 ] || sleep "$poll"
    i=$((i + 1))
    json="$(gh run view "$1" --json attempt,jobs)" || continue
    state="$(printf '%s\n' "$json" | jq -r --arg before "$before" '
      if $before != "" and (.attempt | type) == "number" and .attempt > ($before | tonumber) then "started"
      elif any(.jobs[]?; .name == "e2e" and (.conclusion // "") == "failure"
                         and ((.status // "completed") == "completed")) then "old"
      else "started" end
    ')" || {
      say "could not read run $1 while waiting for its rerun"
      continue
    }
    [ "$state" != started ] || return 0
  done
  say "run $1 still reads as the failed attempt after $RERUN_POLLS polls; watching anyway"
  return 1
}

# Step 6: watch, with at most one e2e rerun.
green=
if find_run "$sha"; then
  watch_run "$run_id"
  rc=$?
  if [ "$rc" -eq 0 ]; then
    green=1
  elif [ "$rc" -eq 124 ]; then
    say "CI run $run_url passed the ${watch_timeout}s watch deadline; treating it as red"
  elif e2e_flake "$run_id"; then
    say "only e2e failed in $run_url; rerunning it once"
    if gh run rerun "$run_id" --failed >&2; then
      await_rerun "$run_id" "$flake_attempt"
      watch_run "$run_id"
      rc=$?
      [ "$rc" -ne 0 ] || green=1
      [ "$rc" -ne 124 ] || say "the rerun of $run_url passed the ${watch_timeout}s watch deadline; treating it as red"
    else
      say "gh run rerun $run_id failed"
    fi
  fi
fi

# Step 7: green.
if [ -n "$green" ]; then
  if [ -n "$branch_sha" ]; then
    g -C "$top" push --force-with-lease="refs/heads/$branch:$branch_sha" origin --delete "$branch" >&2 ||
      say "warning: could not delete origin/$branch (moved since $branch_sha, or unreachable)"
  fi
  remove_worktree
  echo "LANDED $sha $run_url"
  exit 0
fi

# Step 8: red. Revert and watch the revert (no rerun).
failed_url="$run_url"
say "CI red for $sha ($failed_url); reverting"
before_revert="$(g -C "$wt" rev-parse --verify --quiet HEAD)"
if ! g -C "$wt" revert --no-edit "$sha" >&2; then
  say "could not revert $sha; main is red"
  echo "REVERTED-RED $failed_url"
  exit 8
fi
revert_sha="$(g -C "$wt" rev-parse --verify --quiet HEAD)"
if [ -z "$revert_sha" ] || [ "$revert_sha" = "$before_revert" ]; then
  say "git revert of $sha made no commit; main is red"
  echo "REVERTED-RED $failed_url"
  exit 8
fi
if ! push_main "$revert_sha"; then
  say "could not push the revert of $sha; main is red"
  echo "REVERTED-RED $failed_url"
  exit 8
fi
say "pushed revert $revert_sha to main"
if find_run "$revert_sha"; then
  watch_run "$run_id"
  rc=$?
  if [ "$rc" -eq 0 ]; then
    remove_worktree
    echo "REVERTED $failed_url"
    exit 7
  fi
  [ "$rc" -ne 124 ] || say "the revert's CI run $run_url passed the ${watch_timeout}s watch deadline"
fi
say "the revert's CI ($run_url) is not green; main is red"
echo "REVERTED-RED $failed_url"
exit 8
