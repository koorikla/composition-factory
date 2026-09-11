#!/bin/bash
# claim.sh [--dry-run] <issue> <branch> <driver-id> <file>...
#
# The only way a driver takes an issue. Reads the issue and every other open
# issue from GitHub, checks the lease and the files the driver will touch, and
# records the claim as the in-progress label plus a comment whose first line is
# exactly:
#
#   taking — <branch> · driver <driver-id> · lease until <YYYY-MM-DDTHH:MMZ> · files: <f1> <f2> ...
#
# with " · takeover of <old-driver>" between the lease and " · files:" when an
# expired in-progress lease is taken over. land.sh parses this line.
#
# The whole script runs under lock pool `claim` (1 slot): it re-execs itself
# through lock.sh, which leaves the lock on an inherited descriptor (7-9).
# Open issues are read with a plain `gh issue list` (never --label, which goes
# through eventually consistent search and can miss a claim made seconds ago).
#
# Callers decide by the stdout line, not the exit code alone:
#   0  CLAIMED | CLAIMED (dry run)
#   2  REFUSED closed | REFUSED wontfix | REFUSED handed-back
#   3  TAKEN <driver> until <iso>
#   4  OVERLAP #<holder> <file>
#  64  usage error (stderr only, empty stdout)
#  70  gh or jq failed (stderr only, empty stdout)
# lock.sh may also end the run with 2, 73 or 75 and an empty stdout (see its header).
#
# Environment: CF_NOW (epoch seconds, default now), CF_LEASE_MIN (default 120),
# plus lock.sh's. --dry-run does every read and check but no gh writes.
# Written for /bin/bash 3.2; all JSON and date arithmetic is jq.
set -u

LIST_LIMIT=500

usage() {
  echo "usage: claim.sh [--dry-run] <issue> <branch> <driver-id> <file>..." >&2
  echo "       <issue> is a positive integer; branch, driver-id and files are non-empty," >&2
  echo "       do not start with '-', and contain no whitespace" >&2
  exit 64
}

die() {
  echo "claim.sh: $*" >&2
  exit 70
}

validate() {
  [ "${1:-}" != "--dry-run" ] || shift
  [ $# -ge 4 ] || usage
  case "$1" in '' | 0* | *[!0-9]*) usage ;; esac
  shift
  local arg
  for arg in "$@"; do
    case "$arg" in '' | -* | *[[:space:]]*) usage ;; esac
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

# lease: from the issue's newest `taking —` comment by a project member. The
# repo is public, so a comment whose authorAssociation is present and not
# OWNER, MEMBER or COLLABORATOR is ignored (as in land.sh); one without the
# field is trusted. A missing or unparseable `lease until` is a legacy lease
# from the comment time; no such comment, a legacy lease from updatedAt.
# Yields {driver, expiry (epoch), until (iso), files, live}.
# shellcheck disable=SC2016 # $now, $min, $c ... are jq variables
JQ_LEASE='
def labelled($l): any(.labels[]?; .name == $l);
def trusted: .authorAssociation as $a
  | $a == null or $a == "OWNER" or $a == "MEMBER" or $a == "COLLABORATOR";
def legacy($at; $min): {driver: "legacy", expiry: (($at | fromdateiso8601) + $min * 60), files: []};
def lease($now; $min):
  ([.comments[]? | select((.body // "") | startswith("taking —")) | select(trusted)] | last) as $c
  | if $c == null then legacy(.updatedAt; $min)
    else
      ($c.body | split("\n")[0] | sub("\r$"; "")) as $line
      | ([$line | capture(" · lease until (?<iso>\\S+)") | .iso] | first) as $iso
      | (try ($iso | strptime("%Y-%m-%dT%H:%MZ") | mktime) catch null) as $expiry
      | if $expiry == null then legacy($c.createdAt; $min)
        else
          {driver: ([$line | capture(" · driver (?<d>\\S+)") | .d] | first // "legacy"),
           expiry: $expiry,
           files: ([$line | capture(" · files: (?<f>.*)$") | .f] | first // ""
                   | split(" ") | map(select(. != "")))}
        end
    end
  | . + {live: ($now < .expiry), until: (.expiry | strftime("%Y-%m-%dT%H:%MZ"))};
'

issue_json="$(gh issue view "$issue" --json number,title,state,labels,comments,updatedAt)" ||
  die "gh issue view $issue failed"

# Steps 1 and 2: "refused\t<why>", "taken\t<driver> until <iso>" or
# "ok\t<parked 0|1>\t<driver whose expired lease is taken over, or empty>".
verdict="$(printf '%s\n' "$issue_json" | jq -r --argjson now "$now" --argjson min "$lease_min" "$JQ_LEASE"'
  (if labelled("parked") then 1 else 0 end) as $parked
  | if (.state | ascii_downcase) != "open" then "refused\tclosed"
    elif labelled("wontfix") then "refused\twontfix"
    elif labelled("handed-back") then "refused\thanded-back"
    elif labelled("in-progress") then
      lease($now; $min) as $l
      | if $l.live then "taken\t\($l.driver) until \($l.until)" else "ok\t\($parked)\t\($l.driver)" end
    else "ok\t\($parked)\t" end
')" || die "could not read issue $issue"
kind="${verdict%%$'\t'*}"
value="${verdict#*$'\t'}"
case "$kind" in
  refused) echo "REFUSED $value"; exit 2 ;;
  taken) echo "TAKEN $value"; exit 3 ;;
  ok)
    parked="${value%%$'\t'*}"
    takeover="${value#*$'\t'}"
    ;;
  *) die "unexpected verdict for issue $issue: $verdict" ;;
esac

# Step 3: files held by other open issues. One unfiltered list; labels are
# filtered here. The list carries only each issue's oldest 100 comments, so a
# candidate holder with 100 or more is re-read with issue view.
open_json="$(gh issue list --state open --limit "$LIST_LIMIT" --json number,labels,comments,updatedAt)" ||
  die "gh issue list failed"
reread="$(printf '%s\n' "$open_json" | jq -r --argjson n "$issue" --argjson max "$LIST_LIMIT" "$JQ_LEASE"'
  if length >= $max then error("open issues exceed --limit \($max)") else . end
  | .[] | select(.number != $n)
  | select(labelled("in-progress") or labelled("handed-back"))
  | select((.comments | length) >= 100) | .number
')" || die "could not read the open issue list"
fresh=""
for m in $reread; do
  fresh="$fresh$(gh issue view "$m" --json number,title,state,labels,comments,updatedAt)
" || die "gh issue view $m failed"
done

overlap="$(printf '%s\n%s' "$open_json" "$fresh" |
  jq -rs --argjson now "$now" --argjson min "$lease_min" --argjson n "$issue" "$JQ_LEASE"'
    (.[1:] | map({key: (.number | tostring), value: .}) | from_entries) as $fresh
    | [ .[0][] | ($fresh[.number | tostring] // .)
        | select(.number != $n)
        | select((.state // "OPEN") | ascii_downcase == "open")
        | select(labelled("in-progress") or labelled("handed-back"))
        | lease($now; $min) as $l
        | select(labelled("handed-back") or $l.live)
        | {number, files: $l.files} ]
    | unique_by(.number) as $holders
    | [ $ARGS.positional[] as $f | $holders[] | select(any(.files[]; . == $f)) | "#\(.number) \($f)" ]
    | first // empty
  ' --args -- "$@")" || die "could not check held files"
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

if [ "$parked" = 1 ]; then
  gh issue edit "$issue" --add-label in-progress --remove-label parked >&2
else
  gh issue edit "$issue" --add-label in-progress >&2
fi || die "gh issue edit $issue failed; labels may be partially applied, no claim comment posted"
gh issue comment "$issue" --body "$line" >&2 ||
  die "gh issue comment $issue failed; not claimed — do not dispatch; label may remain"
echo "CLAIMED"
exit 0
