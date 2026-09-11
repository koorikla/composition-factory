#!/bin/bash
# claim.sh and land.sh trust the same comment authors. The repo is public, so a
# `taking —` comment counts only when its authorAssociation is OWNER, MEMBER or
# COLLABORATOR, or missing; both scripts report the claims they ignore.
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
. "$(dirname "${BASH_SOURCE[0]}")/land_fixture.sh"
CLAIM="$DRIVER_DIR/claim.sh"

# association:expected — "missing" means the comment has no authorAssociation.
TRUST_TABLE="OWNER:trusted MEMBER:trusted COLLABORATOR:trusted CONTRIBUTOR:outside NONE:outside FIRST_TIME_CONTRIBUTOR:outside missing:trusted"

# associate ISSUE INDEX ASSOCIATION: set (or, for "missing", delete) the
# authorAssociation of comment INDEX (0-based; -1 is the newest).
associate() {
  local f="$FAKE_GH_DIR/issues/$1.json"
  jq --argjson i "$2" --arg v "$3" '
    if $v == "missing" then .comments[$i] |= del(.authorAssociation)
    else .comments[$i].authorAssociation = $v end' "$f" > "$f.tmp" && mv "$f.tmp" "$f"
}

# fresh_sandbox: new_sandbox, removing the previous row's sandbox first (run.sh
# removes only the last one).
fresh_sandbox() {
  if [ -n "${SANDBOX:-}" ]; then
    cd / && rm -rf "$SANDBOX"
  fi
  new_sandbox
}

# sandboxed: the sandbox exists and gh is the fake, so nothing below can reach
# the real repository or GitHub.
sandboxed() {
  if [ -n "${SANDBOX:-}" ] && [ -d "$SANDBOX" ] && [ -n "${FAKE_GH_DIR:-}" ] &&
    [ "$(command -v gh)" = "$TEST_DIR/fakebin/gh" ]; then
    return 0
  fi
  fail "sandbox setup failed"
}

test_claim_trusts_project_authors_only() {
  local row assoc want out rc err failed=0
  SANDBOX=""
  for row in $TRUST_TABLE; do
    assoc="${row%%:*}"
    want="${row#*:}"
    fresh_sandbox
    sandboxed || return 1
    issue_fixture 80 OPEN "in-progress" \
      "taking — CF-280-p · driver d05-0200Z · lease until $(iso_at 60) · files: internal/p.go" 5
    associate 80 -1 "$assoc"
    issue_fixture 81 OPEN ""
    out="$("$CLAIM" 81 CF-281-q d06-0300Z internal/p.go 2>"$SANDBOX/err")"; rc=$?
    err="$(cat "$SANDBOX/err")"
    if [ "$want" = trusted ]; then
      assert_eq "4 OVERLAP #80 internal/p.go" "$rc $out" "claim.sh: a $assoc claim holds its file" &&
        assert_not_contains "$err" "outside the project" "claim.sh: a $assoc claim is not reported as ignored"
    else
      assert_eq "0 CLAIMED" "$rc $out" "claim.sh: a $assoc claim holds nothing" &&
        assert_contains "$err" "claim.sh: ignored 1 claim comment(s) from outside the project on #80" \
          "claim.sh: a $assoc claim is reported as ignored"
    fi || failed=1
  done
  [ "$failed" = 0 ]
}

test_land_trusts_project_authors_only() {
  local row assoc want expected out rc err w f failed=0
  SANDBOX=""
  for row in $TRUST_TABLE; do
    assoc="${row%%:*}"
    want="${row#*:}"
    [ -z "${SANDBOX:-}" ] || { cd / && rm -rf "$SANDBOX"; }
    SANDBOX=""
    # Every git call below names the sandbox clone, whose origin must be the
    # sandbox's bare repo: if setup failed, nothing may run in (or push from)
    # the repository the test was started in.
    land_repo
    w="${SANDBOX:-/nonexistent}/work"
    if ! { sandboxed && [ "$(git -C "$w" remote get-url origin 2>/dev/null)" = "$SANDBOX/origin.git" ]; }; then
      fail "land_repo failed"
      return 1
    fi
    if ! { git -C "$w" checkout -q -b CF-901-second main &&
      echo second > "$w/second.txt" &&
      git -C "$w" add second.txt &&
      git -C "$w" commit -q -m "Add the second thing" &&
      git -C "$w" push -q origin CF-901-second &&
      git -C "$w" checkout -q main; }; then
      fail "second branch setup failed"
      return 1
    fi
    # The older claim (CF-900-thing, from land_repo) has no authorAssociation.
    f="$FAKE_GH_DIR/issues/42.json"
    jq --arg b "taking — CF-901-second · driver d07-0400Z · lease until $(iso_at 60) · files: second.txt" \
      '.comments += [{body: $b, createdAt: "2026-09-11T05:59:00Z"}]' "$f" > "$f.tmp" && mv "$f.tmp" "$f"
    associate 42 -1 "$assoc"
    printf 'green\n' > "$FAKE_GH_DIR/ci-results"
    out="$(cd "$w" && "$LAND" 42 2>"$SANDBOX/err")"; rc=$?
    err="$(cat "$SANDBOX/err")"
    if [ "$want" = trusted ]; then
      expected="Add the second thing (CF-901, #42)"
    else
      expected="Teach thing a second trick (CF-900, #42)"
    fi
    {
      assert_eq 0 "$rc" "land.sh with a $assoc newest claim lands" &&
        assert_eq "$expected" "$(origin_git log -1 --format=%s main)" "land.sh: which claim a $assoc comment leaves in charge" &&
        if [ "$want" = trusted ]; then
          assert_not_contains "$err" "outside the project" "land.sh: a $assoc claim is not reported as ignored"
        else
          assert_contains "$err" "ignoring 1 claim comment(s) on issue 42 from outside the project" \
            "land.sh: a $assoc claim is reported as ignored"
        fi
    } || failed=1
  done
  [ "$failed" = 0 ]
}
