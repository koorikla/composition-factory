#!/bin/bash
# claim.sh [--dry-run] <issue> <branch> <driver-id> <file>...
#
# The only way a driver takes an issue. Reads the issue and every other open
# in-progress / handed-back issue from GitHub, checks the lease and the files the
# driver will touch, and records the claim as the in-progress label plus a
# comment whose first line is exactly:
#
#   taking — <branch> · driver <driver-id> · lease until <YYYY-MM-DDTHH:MMZ> · files: <f1> <f2> ...
#
# with " · takeover of <old-driver>" between the lease and " · files:" when an
# expired in-progress lease is taken over. land.sh parses this line.
#
# The whole script runs under lock pool `claim` (1 slot): it re-execs itself
# through lock.sh, which leaves the lock on an inherited descriptor (7-9).
#
# stdout is exactly one result line:
#   0  CLAIMED | CLAIMED (dry run)
#   2  REFUSED closed | REFUSED wontfix | REFUSED handed-back
#   3  TAKEN <driver> until <iso>
#   4  OVERLAP #<holder> <file>
#  64  usage error (message on stderr, no stdout)
#  70  gh or jq failed (message on stderr, no stdout)
#  73, 75 from lock.sh (see its header)
#
# Environment: CF_NOW (epoch seconds, default now), CF_LEASE_MIN (default 120),
# plus lock.sh's. --dry-run does every read and check but no gh writes.
# Written for /bin/bash 3.2; all JSON and date arithmetic is jq.
set -u

usage() {
  echo "usage: claim.sh [--dry-run] <issue> <branch> <driver-id> <file>..." >&2
  echo "       <issue> is a number; branch, driver-id and files are non-empty, without whitespace" >&2
  exit 64
}

die() {
  echo "claim.sh: $*" >&2
  exit 70
}

no_space() { # value -> fails when empty or containing whitespace
  case "$1" in '' | *[[:space:]]*) return 1 ;; esac
}

validate() {
  [ "${1:-}" != "--dry-run" ] || shift
  [ $# -ge 4 ] || usage
  case "$1" in '' | *[!0-9]*) usage ;; esac
  shift
  local arg
  for arg in "$@"; do
    no_space "$arg" || usage
  done
}
validate "$@"

# Serialize: the pid survives both execs, so a marker inherited from some
# other process never matches and cannot skip the lock.
if [ "${CF_CLAIM_LOCKED:-}" != "$$" ]; then
  CF_CLAIM_LOCKED=$$
  export CF_CLAIM_LOCKED
  exec "$(dirname "$0")/lock.sh" claim 1 -- "$0" "$@"
fi

dry_run=
if [ "$1" = "--dry-run" ]; then
  dry_run=1
  shift
fi
issue="$1"
branch="$2"
driver="$3"
shift 3

now="${CF_NOW:-$(date +%s)}"
lease_min="${CF_LEASE_MIN:-120}"
case "$now" in '' | *[!0-9]*) echo "claim.sh: CF_NOW must be epoch seconds" >&2; exit 64 ;; esac
case "$lease_min" in '' | *[!0-9]*) echo "claim.sh: CF_LEASE_MIN must be whole minutes" >&2; exit 64 ;; esac

# lease: the issue's newest `taking —` comment, read as described in the plan.
# Yields {driver, expiry (epoch), until (iso), files, live}.
# shellcheck disable=SC2016 # $now, $min, $c ... are jq variables
JQ_LEASE='
def labelled($l): any(.labels[]?; .name == $l);
def lease($now; $min):
  ([.comments[]? | select((.body // "") | startswith("taking —"))] | last) as $c
  | if $c == null then
      {driver: "legacy", expiry: ((.updatedAt | fromdateiso8601) + $min * 60), files: []}
    else
      ($c.body | split("\n")[0] | sub("\r$"; "")) as $line
      | ([$line | capture(" · lease until (?<iso>[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}Z)")] | first) as $m
      | if $m == null then
          {driver: "legacy", expiry: (($c.createdAt | fromdateiso8601) + $min * 60), files: []}
        else
          {driver: ([$line | capture(" · driver (?<d>[^ ]+)") | .d] | first // "legacy"),
           expiry: ($m.iso | strptime("%Y-%m-%dT%H:%MZ") | mktime),
           files: ([$line | capture(" · files: (?<f>.*)$") | .f] | first // ""
                   | split(" ") | map(select(. != "")))}
        end
    end
  | . + {live: ($now < .expiry), until: (.expiry | strftime("%Y-%m-%dT%H:%MZ"))};
'

issue_json="$(gh issue view "$issue" --json number,title,state,labels,comments,updatedAt)" ||
  die "gh issue view $issue failed"

# Steps 1 and 2: "<kind>\t<value>" with kind refused | taken | ok.
verdict="$(printf '%s\n' "$issue_json" | jq -r --argjson now "$now" --argjson min "$lease_min" "$JQ_LEASE"'
  if (.state | ascii_downcase) != "open" then "refused\tclosed"
  elif labelled("wontfix") then "refused\twontfix"
  elif labelled("handed-back") then "refused\thanded-back"
  elif labelled("in-progress") then
    lease($now; $min) as $l
    | if $l.live then "taken\t\($l.driver) until \($l.until)" else "ok\t\($l.driver)" end
  else "ok\t" end
')" || die "could not read issue $issue"
kind="${verdict%%$'\t'*}"
value="${verdict#*$'\t'}"
case "$kind" in
  refused) echo "REFUSED $value"; exit 2 ;;
  taken) echo "TAKEN $value"; exit 3 ;;
  ok) takeover="$value" ;;
  *) die "unexpected verdict for issue $issue: $verdict" ;;
esac

# Step 3: files held by other open issues.
in_progress="$(gh issue list --state open --label in-progress --limit 200 --json number,labels,comments,updatedAt)" ||
  die "gh issue list --label in-progress failed"
handed_back="$(gh issue list --state open --label handed-back --limit 200 --json number,labels,comments,updatedAt)" ||
  die "gh issue list --label handed-back failed"
overlap="$(printf '%s\n%s\n' "$in_progress" "$handed_back" |
  jq -rs --argjson now "$now" --argjson min "$lease_min" --argjson n "$issue" "$JQ_LEASE"'
    [ add[] | select(.number != $n)
      | select(labelled("handed-back") or (labelled("in-progress") and lease($now; $min).live))
      | {number, files: lease($now; $min).files} ]
    | unique_by(.number) as $holders
    | [ $ARGS.positional[] as $f | $holders[] | select(any(.files[]; . == $f)) | "#\(.number) \($f)" ]
    | first // empty
  ' --args "$@")" || die "could not read open in-progress / handed-back issues"
if [ -n "$overlap" ]; then
  echo "OVERLAP $overlap"
  exit 4
fi

if [ -n "$dry_run" ]; then
  echo "CLAIMED (dry run)"
  exit 0
fi

# Step 5: label, then comment.
lease_until="$(jq -nr --argjson t "$((now + lease_min * 60))" '$t | strftime("%Y-%m-%dT%H:%MZ")')" ||
  die "could not compute the lease"
line="taking — $branch · driver $driver · lease until $lease_until"
[ -z "$takeover" ] || line="$line · takeover of $takeover"
line="$line · files:"
for f in "$@"; do
  line="$line $f"
done

gh issue edit "$issue" --add-label in-progress --remove-label parked >&2 ||
  die "gh issue edit $issue failed; issue not claimed"
gh issue comment "$issue" --body "$line" >&2 ||
  die "gh issue comment $issue failed after labelling in-progress; the claim comment is missing"
echo "CLAIMED"
exit 0
