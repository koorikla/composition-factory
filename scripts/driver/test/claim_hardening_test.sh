#!/bin/bash
# scripts/driver/claim.sh — hardening found in review: option-like arguments,
# consistent reads of held issues, malformed leases, and label edits.
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CLAIM="$DRIVER_DIR/claim.sh"

labels_of() { jq -r '[.labels[].name] | sort | join(",")' "$FAKE_GH_DIR/issues/$1.json"; }
calls() { cat "$FAKE_GH_DIR/calls.log"; }

test_option_like_file_is_rejected() {
  new_sandbox
  issue_fixture 14 OPEN "in-progress" \
    "taking — CF-204-p · driver d05-0200Z · lease until $(iso_at 45) · files: internal/held.go" 75
  issue_fixture 15 OPEN "severity:P2"
  local out rc
  out="$("$CLAIM" 15 CF-205-q d06-0300Z --arg x internal/held.go 2>/dev/null)"; rc=$?
  assert_eq 64 "$rc" "an option-like file argument is a usage error" &&
    assert_eq "" "$out" "a usage error prints no result line" &&
    assert_not_contains "$(calls)" "issue edit" "a usage error must not label" &&
    assert_not_contains "$(calls)" "issue comment" "a usage error must not comment" &&
    assert_eq "severity:P2" "$(labels_of 15)" "labels unchanged"
}

test_option_like_branch_is_rejected() {
  new_sandbox
  issue_fixture 25 OPEN ""
  local out rc
  out="$("$CLAIM" 25 --dry-run d06-0300Z internal/i.go 2>/dev/null)"; rc=$?
  assert_eq 64 "$rc" "a --dry-run in the branch position is a usage error" &&
    assert_eq "" "$out" "a usage error prints no result line" &&
    assert_not_contains "$(calls)" "issue edit" "a usage error must not label" &&
    assert_not_contains "$(calls)" "issue comment" "a usage error must not comment"
}

test_malformed_lease_date_falls_back_to_legacy() {
  new_sandbox
  issue_fixture 30 OPEN "in-progress" \
    "taking — CF-230-m · driver d05-0200Z · lease until 2026-19-41T99:99Z · files: internal/m.go" 30
  issue_fixture 31 OPEN ""
  local a b ra rb
  a="$("$CLAIM" 30 CF-230-m d06-0300Z internal/m.go)"; ra=$?
  b="$("$CLAIM" 31 CF-231-n d06-0300Z internal/m.go)"; rb=$?
  assert_eq "3 TAKEN legacy until $(iso_at 90)" "$ra $a" "a malformed lease is leased from the comment time" &&
    assert_eq "0 CLAIMED" "$rb $b" "a malformed lease elsewhere does not halt other claims"
}

test_held_issues_are_listed_without_label_search() {
  new_sandbox
  issue_fixture 40 OPEN ""
  local out rc
  out="$("$CLAIM" 40 CF-240-s d06-0300Z internal/s.go)"; rc=$?
  assert_eq 0 "$rc" "claim exit" &&
    assert_contains "$(calls)" "issue list" "held issues are listed" &&
    assert_eq "" "$(grep -- 'issue list' "$FAKE_GH_DIR/calls.log" | grep -- '--label')" \
      "issue list must not use --label (it goes through eventually consistent search)"
}

test_remove_parked_only_when_present() {
  new_sandbox
  issue_fixture 50 OPEN "severity:P2"
  issue_fixture 51 OPEN "parked"
  local ra rb
  "$CLAIM" 50 CF-250-t d06-0300Z internal/t.go >/dev/null; ra=$?
  "$CLAIM" 51 CF-251-u d06-0300Z internal/u.go >/dev/null; rb=$?
  assert_eq "0 0" "$ra $rb" "both claims succeed" &&
    assert_not_contains "$(grep -- 'issue edit 50' "$FAKE_GH_DIR/calls.log")" "--remove-label" \
      "no --remove-label for an issue without parked" &&
    assert_contains "$(grep -- 'issue edit 51' "$FAKE_GH_DIR/calls.log")" "--remove-label parked" \
      "a parked issue has parked removed" &&
    assert_eq "in-progress" "$(labels_of 51)" "parked replaced by in-progress"
}

test_held_issue_with_many_comments_is_reread() {
  new_sandbox
  issue_fixture 60 OPEN "in-progress"
  jq --arg b "taking — CF-260-v · driver d05-0200Z · lease until $(iso_at 45) · files: internal/v.go" \
    '.comments = ([range(100) | {body: "note", createdAt: "2026-09-11T01:00:00Z"}] + [{body: $b, createdAt: "2026-09-11T05:00:00Z"}])' \
    "$FAKE_GH_DIR/issues/60.json" > "$FAKE_GH_DIR/issues/60.tmp" && mv "$FAKE_GH_DIR/issues/60.tmp" "$FAKE_GH_DIR/issues/60.json"
  issue_fixture 61 OPEN ""
  local out rc
  out="$("$CLAIM" 61 CF-261-w d06-0300Z internal/v.go)"; rc=$?
  assert_eq "4 OVERLAP #60 internal/v.go" "$rc $out" "the newest claim of a busy issue still holds its files" &&
    assert_contains "$(calls)" "issue view 60 " "an issue with 100+ comments is re-read with issue view"
}
