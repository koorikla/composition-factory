#!/bin/bash
# Runs every test_* function in scripts/driver/test/*_test.sh, each in its own
# subshell with its own sandbox. lock_test.sh is skipped where lockf is absent.
set -u
cd "$(dirname "$0")" || exit 1
command -v jq >/dev/null || { echo "test-driver: jq is required" >&2; exit 1; }

pass=0 failed=0 skipped=0
for file in *_test.sh; do
  for t in $(bash -c ". ./$file >/dev/null 2>&1; declare -F" | awk '$3 ~ /^test_/ { print $3 }'); do
    if [ "$file" = lock_test.sh ] && ! command -v lockf >/dev/null; then
      echo "SKIP $file $t (no lockf)"
      skipped=$((skipped + 1))
      continue
    fi
    if ( . "./$file"; "$t"; rc=$?; cd /; rm -rf "${SANDBOX:-/nonexistent}"; exit $rc ); then
      echo "PASS $file $t"
      pass=$((pass + 1))
    else
      echo "FAIL $file $t"
      failed=$((failed + 1))
    fi
  done
done
echo "test-driver: $pass passed, $failed failed, $skipped skipped"
[ "$failed" -eq 0 ]
