#!/bin/bash
# scripts/driver/claim.sh — the only way a driver takes an issue.
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CLAIM="$DRIVER_DIR/claim.sh"

claim_line() { jq -r '.comments[-1].body | split("\n")[0]' "$FAKE_GH_DIR/issues/$1.json"; }
labels_of() { jq -r '[.labels[].name] | sort | join(",")' "$FAKE_GH_DIR/issues/$1.json"; }
comment_count() { jq '.comments | length' "$FAKE_GH_DIR/issues/$1.json"; }

test_clean_claim_labels_and_comments() {
  new_sandbox
  issue_fixture 10 OPEN "severity:P2"
  local out rc
  out="$("$CLAIM" 10 CF-200-fix d06-0300Z internal/a.go internal/a_test.go)"; rc=$?
  assert_eq 0 "$rc" "clean claim exit" &&
    assert_eq "CLAIMED" "$out" "clean claim output" &&
    assert_eq "in-progress,severity:P2" "$(labels_of 10)" "clean claim labels" &&
    assert_eq "taking — CF-200-fix · driver d06-0300Z · lease until $(iso_at 120) · files: internal/a.go internal/a_test.go" \
      "$(claim_line 10)" "claim comment format"
}

test_unexpired_lease_is_taken() {
  new_sandbox
  issue_fixture 11 OPEN "in-progress" \
    "taking — CF-201-x · driver d05-0200Z · lease until $(iso_at 30) · files: internal/b.go" 90
  local out rc
  out="$("$CLAIM" 11 CF-201-x d06-0300Z internal/b.go)"; rc=$?
  assert_eq 3 "$rc" "live lease exit" &&
    assert_eq "TAKEN d05-0200Z until $(iso_at 30)" "$out" "live lease output" &&
    assert_eq 1 "$(comment_count 11)" "a refused claim must not comment"
}

test_expired_lease_is_taken_over() {
  new_sandbox
  issue_fixture 12 OPEN "in-progress" \
    "taking — CF-202-y · driver d04-0100Z · lease until $(iso_at -10) · files: internal/c.go" 130
  local out rc
  out="$("$CLAIM" 12 CF-202-y d06-0300Z internal/c.go)"; rc=$?
  assert_eq 0 "$rc" "takeover exit" &&
    assert_eq "CLAIMED" "$out" "takeover output" &&
    assert_eq "taking — CF-202-y · driver d06-0300Z · lease until $(iso_at 120) · takeover of d04-0100Z · files: internal/c.go" \
      "$(claim_line 12)" "takeover comment format"
}

test_legacy_lock_leased_from_comment_time() {
  new_sandbox
  issue_fixture 13 OPEN "in-progress" "taking — CF-203-z (wave 1, driver run 2026-09-11)" 60
  local out rc
  out="$("$CLAIM" 13 CF-203-z d06-0300Z internal/d.go)"; rc=$?
  assert_eq 3 "$rc" "a 60-minute-old legacy lock is still leased" &&
    assert_eq "TAKEN legacy until $(iso_at 60)" "$out" "legacy lease = comment time + 120 min"
}

test_label_without_claim_comment_uses_updated_at() {
  new_sandbox
  issue_fixture 26 OPEN "in-progress"
  local out rc
  out="$("$CLAIM" 26 CF-216-e d06-0300Z internal/j.go)"; rc=$?
  assert_eq 0 "$rc" "a bare label untouched for 300 min has expired" &&
    assert_contains "$(claim_line 26)" "· takeover of legacy ·" "taking over a bare label is recorded"
}

test_overlapping_files_refused() {
  new_sandbox
  issue_fixture 14 OPEN "in-progress" \
    "taking — CF-204-p · driver d05-0200Z · lease until $(iso_at 45) · files: web-proto/js/regions/inspector.js tests/cf204.spec.js" 75
  issue_fixture 15 OPEN "severity:P2"
  local out rc
  out="$("$CLAIM" 15 CF-205-q d06-0300Z internal/e.go web-proto/js/regions/inspector.js)"; rc=$?
  assert_eq 4 "$rc" "overlap exit" &&
    assert_eq "OVERLAP #14 web-proto/js/regions/inspector.js" "$out" "overlap names the holder and the file" &&
    assert_eq "severity:P2" "$(labels_of 15)" "a refused claim must not label"
}

test_expired_and_closed_claims_do_not_block() {
  new_sandbox
  issue_fixture 16 OPEN "in-progress" \
    "taking — CF-206-r · driver d04-0100Z · lease until $(iso_at -1) · files: internal/f.go" 121
  issue_fixture 17 CLOSED "in-progress" \
    "taking — CF-207-s · driver d05-0200Z · lease until $(iso_at 60) · files: internal/f.go" 60
  issue_fixture 18 OPEN ""
  local out rc
  out="$("$CLAIM" 18 CF-208-t d06-0300Z internal/f.go)"; rc=$?
  assert_eq 0 "$rc" "expired leases and closed issues must not hold files" &&
    assert_eq "CLAIMED" "$out" "claim succeeds"
}

test_handed_back_holds_files_until_landed() {
  new_sandbox
  issue_fixture 19 OPEN "handed-back" \
    "taking — CF-209-u · driver d04-0100Z · lease until $(iso_at -200) · files: internal/g.go" 320
  issue_fixture 20 OPEN ""
  local out rc
  out="$("$CLAIM" 20 CF-210-v d06-0300Z internal/g.go)"; rc=$?
  assert_eq 4 "$rc" "a handed-back branch still owns its files" &&
    assert_eq "OVERLAP #19 internal/g.go" "$out" "overlap names the handed-back holder"
}

test_parked_issue_is_resumable() {
  new_sandbox
  issue_fixture 21 OPEN "parked,severity:P1" \
    "taking — CF-211-w · driver d04-0100Z · lease until $(iso_at -60) · files: internal/h.go" 180
  local out rc
  out="$("$CLAIM" 21 CF-211-w d06-0300Z internal/h.go)"; rc=$?
  assert_eq 0 "$rc" "parked issues are claimable" &&
    assert_eq "in-progress,severity:P1" "$(labels_of 21)" "claiming a parked issue removes parked" &&
    assert_not_contains "$(claim_line 21)" "takeover of" "resuming a parked issue is not a takeover"
}

test_refuses_closed_wontfix_and_handed_back() {
  new_sandbox
  issue_fixture 22 CLOSED ""
  issue_fixture 23 OPEN "wontfix"
  issue_fixture 24 OPEN "handed-back"
  local a b c ra rb rc
  a="$("$CLAIM" 22 CF-212-a d06-0300Z x.go)"; ra=$?
  b="$("$CLAIM" 23 CF-213-b d06-0300Z y.go)"; rb=$?
  c="$("$CLAIM" 24 CF-214-c d06-0300Z z.go)"; rc=$?
  assert_eq "2 REFUSED closed" "$ra $a" "closed" &&
    assert_eq "2 REFUSED wontfix" "$rb $b" "wontfix" &&
    assert_eq "2 REFUSED handed-back" "$rc $c" "handed-back is for landing, not claiming"
}

test_dry_run_writes_nothing() {
  new_sandbox
  issue_fixture 25 OPEN ""
  local out rc
  out="$("$CLAIM" --dry-run 25 CF-215-d d06-0300Z internal/i.go)"; rc=$?
  assert_eq 0 "$rc" "dry run exit" &&
    assert_eq "CLAIMED (dry run)" "$out" "dry run output" &&
    assert_not_contains "$(cat "$FAKE_GH_DIR/calls.log")" "issue edit" "dry run must not label" &&
    assert_not_contains "$(cat "$FAKE_GH_DIR/calls.log")" "issue comment" "dry run must not comment"
}
