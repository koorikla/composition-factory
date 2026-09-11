#!/bin/bash
# land.sh <issue>
#
# The only way a branch reaches main. Run from inside any worktree of the clone.
# It lands the branch named by the issue's newest claim comment
# (`taking — <branch> · driver …`, or the legacy `taking — <branch> (wave …`):
# rebase onto origin/main in a scratch worktree at
# <toplevel>/.worktrees/land-CF-NNN, run the gates, squash to ONE commit
# ("<newest subject> (CF-NNN, #<issue>)", the branch's commit bodies oldest
# first), push it to main, watch its CI run, and revert it if CI is red.
#
# NEVER run this by hand against the real repository casually: it pushes main.
# There is no dry run. Tests drive it against a local bare origin and a fake gh.
#
# The whole script, including the CI watch and any revert, runs under lock pool
# `merge` (1 slot): it re-execs itself through lock.sh, which leaves the lock on
# an inherited descriptor (the highest free one in 9..3). One landing at a time,
# and nobody lands on top of an unwatched push. Every git call disables auto gc
# and auto maintenance so no detached git process inherits the lock; the gates
# run with descriptors 3-9 closed.
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
#  64  usage error, or the issue has no claim naming a CF-<digits> branch
#      (stderr only, empty stdout)
#  70  gh, git or jq failed, the branch is missing, or it has nothing to land,
#      before anything was pushed (stderr only, empty stdout)
# When no CI run appears for a pushed sha, the run url reads `none`.
# lock.sh may also end the run with 2, 73 or 75 and an empty stdout (see its header).
# The only e2e rerun: when a run's single non-passing job is `e2e` with conclusion
# failure, it is rerun once (`gh run rerun <id> --failed`) and watched again once
# `gh run view` no longer shows the failed attempt (at most 30 polls, every
# CF_CI_POLL_SEC).
#
# Environment:
#   CF_LAND_GATES   shell command run in the rebased worktree. Default:
#                   make lint && make lint-strict && go test -short <packages with
#                   changed .go files> (no go test when no Go file changed)
#   CF_CI_POLL_SEC  wait between `gh run list` polls for a pushed sha (default 10);
#                   gives up after 60 polls and treats the landing as red
#   plus lock.sh's.
# land.sh never edits, comments on or closes issues.
# Written for /bin/bash 3.2; all JSON is jq.
set -u

MAX_POLLS=60
RERUN_POLLS=30

usage() {
  echo "usage: land.sh <issue>" >&2
  echo "       <issue> is a positive integer" >&2
  exit 64
}

[ $# -eq 1 ] || usage
case "$1" in '' | 0* | *[!0-9]*) usage ;; esac

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

# Every git call: no auto gc, no auto maintenance (they detach and would keep the
# merge lock's descriptor open after land.sh exits).
g() { git -c gc.auto=0 -c maintenance.auto=false "$@"; }

poll="${CF_CI_POLL_SEC:-10}"
case "$poll" in '' | *[!0-9.]* | *.*.*) say "CF_CI_POLL_SEC must be a number of seconds"; exit 64 ;; esac

# Step 1: the issue must be handed back, before anything is fetched.
issue_json="$(gh issue view "$issue" --json number,title,state,labels,comments,updatedAt)" ||
  die "gh issue view $issue failed"
handed="$(printf '%s\n' "$issue_json" | jq -r 'if any(.labels[]?; .name == "handed-back") then 1 else 0 end')" ||
  die "could not read issue $issue"
if [ "$handed" != 1 ]; then
  echo "NOT-HANDED-BACK"
  exit 5
fi

# Step 2: branch from the newest `taking —` comment; CF-NNN from its prefix.
claim="$(printf '%s\n' "$issue_json" | jq -r '
  ([.comments[]? | select((.body // "") | startswith("taking —"))] | last) as $c
  | if $c == null then ""
    else ([$c.body | split("\n")[0] | capture("^taking —\\s+(?<b>\\S+)") | .b] | first // "") as $b
      | ([$b | capture("^(?<cf>CF-[0-9]+)") | .cf] | first // "") as $cf
      | "\($b)\t\($cf)"
    end
')" || die "could not read the claim on issue $issue"
branch="${claim%%$'\t'*}"
cf="${claim#*$'\t'}"
if [ -z "$claim" ] || [ -z "$branch" ]; then
  say "issue $issue has no \`taking — <branch>\` claim comment"
  exit 64
fi
if [ -z "$cf" ] || ! g check-ref-format "refs/heads/$branch"; then
  say "claimed branch '$branch' on issue $issue is not a valid CF-<digits> branch"
  exit 64
fi
say "issue #$issue: landing $branch as $cf"

# Step 3: fetch, scratch worktree on origin/<branch>, rebase onto origin/main.
top="$(g rev-parse --show-toplevel)" || die "not inside a git worktree"
wt="$top/.worktrees/land-$cf"

# remove_worktree: remove the scratch worktree if present, then prune, so a
# registration whose directory was deleted cannot block the next `worktree add`.
remove_worktree() {
  if [ -e "$wt" ]; then
    g -C "$top" worktree remove --force "$wt" >/dev/null 2>&1 || rm -rf "$wt"
  fi
  g -C "$top" worktree prune >&2
}

g fetch origin >&2 || die "git fetch origin failed"
g rev-parse --verify --quiet "refs/remotes/origin/$branch^{commit}" >/dev/null ||
  die "branch $branch is not on origin"

remove_worktree # a leftover from a crashed run
trap 'remove_worktree' EXIT
g -C "$top" worktree add --detach "$wt" "origin/$branch" >&2 ||
  die "could not add worktree $wt on origin/$branch"

if ! g -C "$wt" rebase origin/main >&2; then
  g -C "$wt" rebase --abort >&2
  echo "PARKED rebase-conflict"
  exit 6
fi

# Step 4: gates, with the lock descriptors closed so a straggler a test leaves
# behind cannot hold the merge lock.
gates="${CF_LAND_GATES:-}"
if [ -z "$gates" ]; then
  gates="make lint && make lint-strict"
  changed="$(g -C "$wt" diff --name-only origin/main HEAD -- '*.go')" ||
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
if ! (cd "$wt" && /bin/bash -c "$gates") </dev/null >&2 3>&- 4>&- 5>&- 6>&- 7>&- 8>&- 9>&-; then
  echo "PARKED gates-red"
  exit 6
fi

# Step 5: one commit on origin/main.
subject="$(g -C "$wt" log -1 --format=%s HEAD)" || die "could not read the branch's newest subject"
suffix="($cf, #$issue)"
case "$subject" in *"$suffix"*) ;; *) subject="$subject $suffix" ;; esac
message="$subject"
commits="$(g -C "$wt" rev-list --reverse origin/main..HEAD)" || die "could not list the branch's commits"
[ -n "$commits" ] || die "branch $branch has no commits beyond origin/main"
for c in $commits; do
  body="$(g -C "$wt" log -1 --format=%b "$c")" || die "could not read commit $c"
  case "$body" in *[![:space:]]*) message="$message

$body" ;; esac
done

g -C "$wt" reset --soft origin/main >&2 || die "could not squash onto origin/main"
if g -C "$wt" diff --cached --quiet; then
  die "branch $branch has nothing to land against origin/main"
fi
printf '%s\n' "$message" | g -C "$wt" commit --quiet --no-verify --cleanup=whitespace -F - >&2 ||
  die "could not commit the squashed landing"
sha="$(g -C "$wt" rev-parse HEAD)" || die "could not read the landing sha"

if ! g -C "$wt" push origin HEAD:main >&2; then
  echo "PARKED push-rejected"
  exit 6
fi
say "pushed $sha to main"

# find_run SHA: sets run_id and run_url, polling until gh lists a run for SHA.
find_run() {
  local i=0 json row
  run_id=""
  run_url="none"
  while [ "$i" -lt "$MAX_POLLS" ]; do
    [ "$i" -eq 0 ] || sleep "$poll"
    i=$((i + 1))
    json="$(gh run list --branch main --commit "$1" --json databaseId,url)" || continue
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

watch_run() { gh run watch "$1" --exit-status </dev/null >&2; }

# e2e_flake ID: the run's only non-passing job is e2e, and it failed.
e2e_flake() {
  local json
  json="$(gh run view "$1" --json jobs)" || return 1
  printf '%s\n' "$json" | jq -e '
    [.jobs[]? | select(.conclusion != "success" and .conclusion != "skipped" and .conclusion != "neutral")] as $bad
    | ($bad | length) == 1 and $bad[0].name == "e2e" and $bad[0].conclusion == "failure"
  ' >/dev/null
}

# await_rerun ID: poll `gh run view` until the run no longer reads as the old
# attempt (e2e completed with failure). The rerun endpoint is asynchronous, and
# `gh run watch --exit-status` on a run that still reads completed exits at once
# with the old conclusion. Queued or running jobs carry an empty or null
# conclusion and a status other than completed; an empty job list is a new
# attempt that has no jobs yet. Gives up after RERUN_POLLS and lets the watch
# decide.
await_rerun() {
  local i=0 json
  while [ "$i" -lt "$RERUN_POLLS" ]; do
    [ "$i" -eq 0 ] || sleep "$poll"
    i=$((i + 1))
    json="$(gh run view "$1" --json jobs)" || continue
    printf '%s\n' "$json" | jq -e '
      any(.jobs[]?; .name == "e2e" and (.conclusion // "") == "failure"
                    and ((.status // "completed") == "completed"))
    ' >/dev/null
    case $? in
      1) return 0 ;;
      0) ;;
      *) say "could not read run $1 while waiting for its rerun" ;;
    esac
  done
  say "run $1 still reads as the failed attempt after $RERUN_POLLS polls; watching anyway"
  return 1
}

# Step 6: watch, with at most one e2e rerun.
green=
if find_run "$sha"; then
  if watch_run "$run_id"; then
    green=1
  elif e2e_flake "$run_id"; then
    say "only e2e failed in $run_url; rerunning it once"
    if gh run rerun "$run_id" --failed >&2; then
      await_rerun "$run_id"
      watch_run "$run_id" && green=1
    else
      say "gh run rerun $run_id failed"
    fi
  fi
fi

# Step 7: green.
if [ -n "$green" ]; then
  g -C "$wt" push origin --delete "$branch" >&2 || say "warning: could not delete origin/$branch"
  remove_worktree
  echo "LANDED $sha $run_url"
  exit 0
fi

# Step 8: red. Revert and watch the revert (no rerun).
failed_url="$run_url"
say "CI red for $sha ($failed_url); reverting"
if ! g -C "$wt" revert --no-edit "$sha" >&2 || ! g -C "$wt" push origin HEAD:main >&2; then
  say "could not push the revert of $sha; main is red"
  echo "REVERTED-RED $failed_url"
  exit 8
fi
revert_sha="$(g -C "$wt" rev-parse HEAD)"
say "pushed revert $revert_sha to main"
if find_run "$revert_sha" && watch_run "$run_id"; then
  remove_worktree
  echo "REVERTED $failed_url"
  exit 7
fi
say "the revert's CI ($run_url) is red too; main is red"
echo "REVERTED-RED $failed_url"
exit 8
