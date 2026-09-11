# shellcheck shell=bash
# Helpers for scripts/driver tests. Sourced by run.sh and every *_test.sh.
# Written for /bin/bash 3.2: no associative arrays, no mapfile.
set -u

DRIVER_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_DIR="$DRIVER_DIR/test"

fail() { echo "    FAIL: $*" >&2; return 1; }

assert_eq() { # expected actual message
  [ "$1" = "$2" ] || fail "$3: expected [$1], got [$2]"
}

assert_contains() { # haystack needle message
  case "$1" in *"$2"*) return 0 ;; esac
  fail "$3: [$2] not found in [$1]"
}

assert_not_contains() { # haystack needle message
  case "$1" in *"$2"*) fail "$3: [$2] unexpectedly present"; return 1 ;; esac
  return 0
}

# new_sandbox: a fresh temp dir, the fake gh first on PATH, a private lock dir,
# short lock retries, and a fixed clock (CF_NOW = 2026-09-11T06:00Z).
new_sandbox() {
  SANDBOX="$(mktemp -d "${TMPDIR:-/tmp}/cf-driver-test.XXXXXX")"
  export FAKE_GH_DIR="$SANDBOX/gh"
  mkdir -p "$FAKE_GH_DIR/issues" "$FAKE_GH_DIR/runs"
  : > "$FAKE_GH_DIR/calls.log"
  export CF_LOCK_DIR="$SANDBOX/locks"
  export CF_LOCK_RETRY_SEC=0.2
  export CF_NOW=1789106400
  export PATH="$TEST_DIR/fakebin:$PATH"
}

# iso_at MINUTES -> CF_NOW + MINUTES as YYYY-MM-DDTHH:MMZ (the lease format).
iso_at() {
  jq -nr --argjson t "$((CF_NOW + $1 * 60))" '$t | strftime("%Y-%m-%dT%H:%MZ")'
}

# issue_fixture NUMBER STATE LABELS [BODY MINUTES_AGO]...
# Writes the full issue object the fake gh serves. LABELS is comma-separated.
issue_fixture() {
  local n="$1" state="$2" labels="$3" comments='[]' at
  shift 3
  while [ $# -ge 2 ]; do
    at="$(jq -nr --argjson t "$((CF_NOW - $2 * 60))" '$t | strftime("%Y-%m-%dT%H:%M:%SZ")')"
    comments="$(jq -c --arg b "$1" --arg at "$at" '. + [{body: $b, createdAt: $at}]' <<<"$comments")"
    shift 2
  done
  jq -n --argjson n "$n" --arg s "$state" --arg l "$labels" --argjson c "$comments" \
    --arg u "$(jq -nr --argjson t "$((CF_NOW - 300 * 60))" '$t | strftime("%Y-%m-%dT%H:%M:%SZ")')" \
    '{number: $n, title: ("issue " + ($n | tostring)), state: $s, updatedAt: $u,
      labels: [$l | split(",")[] | select(. != "") | {name: .}], comments: $c}' \
    > "$FAKE_GH_DIR/issues/$n.json"
}

# wait_for FILE: poll until FILE exists (a holder's "I have the lock" marker).
# Fixed sleeps race on a loaded machine; markers do not.
wait_for() {
  local i=0
  while [ ! -s "$1" ]; do
    [ $i -ge 200 ] && { fail "timed out waiting for $1"; return 1; }
    sleep 0.05
    i=$((i + 1))
  done
}
