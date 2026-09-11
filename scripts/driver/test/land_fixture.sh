# shellcheck shell=bash
# The land_repo setup shared by the land test files, the same shape as
# land_test.sh (which stays verbatim and keeps its own copy). Sourced after
# lib.sh. Defines no test_* functions, so run.sh never runs it.
#
# Run these tests only through `make test-driver` or scripts/driver/test/run.sh.
# land.sh pushes; a land test that ran outside its sandbox would commit to,
# check out in and push from the repository it was started in. So land_repo
# ends with sandbox_guard and returns non-zero when anything is off, every test
# starts with `land_repo || return 1`, and every git command a test body runs
# without `git -C "$SANDBOX/…"` is preceded by `sandbox_guard || return 1`.

# shellcheck disable=SC2034 # used by the test files that source this one
LAND="${DRIVER_DIR:-/nonexistent}/land.sh"

refuse() {
  echo "sandbox_guard: refusing to run: $*" >&2
  return 1
}

# sandbox_dir_guard: SANDBOX is an existing directory strictly under
# ${TMPDIR:-/tmp}, the driver test helpers are loaded, and no GIT_* variable
# redirects git to another repository.
sandbox_dir_guard() {
  local tmp real v
  [ -n "${SANDBOX:-}" ] || { refuse "SANDBOX is not set (new_sandbox did not run)"; return 1; }
  [ -d "$SANDBOX" ] || { refuse "SANDBOX $SANDBOX is not a directory"; return 1; }
  if [ -z "${TEST_DIR:-}" ] || [ ! -x "$TEST_DIR/fakebin/gh" ]; then
    refuse "the driver test helpers (lib.sh) are not loaded"
    return 1
  fi
  tmp="$(cd "${TMPDIR:-/tmp}" 2>/dev/null && pwd -P)"
  real="$(cd "$SANDBOX" 2>/dev/null && pwd -P)"
  case "$real" in
    "$tmp"/?*) ;;
    *) refuse "SANDBOX $SANDBOX is not under ${TMPDIR:-/tmp}"; return 1 ;;
  esac
  for v in GIT_DIR GIT_WORK_TREE GIT_COMMON_DIR GIT_INDEX_FILE GIT_OBJECT_DIRECTORY; do
    if [ -n "${!v:-}" ]; then
      refuse "$v is set and would redirect git"
      return 1
    fi
  done
}

# sandbox_guard: sandbox_dir_guard, and the shell is in the sandbox clone
# ($SANDBOX/work, its own top level) whose origin is the sandbox's bare repo,
# and gh is the fake (or a shim under $SANDBOX that forwards to it).
sandbox_guard() {
  local work gh_path
  sandbox_dir_guard || return 1
  work="$(cd "$SANDBOX/work" 2>/dev/null && pwd -P)"
  if [ -z "$work" ] || [ "$(pwd -P)" != "$work" ]; then
    refuse "the working directory $(pwd) is not $SANDBOX/work"
    return 1
  fi
  [ "$(git rev-parse --show-toplevel 2>/dev/null)" = "$work" ] ||
    { refuse "$SANDBOX/work is not its own git top level"; return 1; }
  [ "$(git remote get-url origin 2>/dev/null)" = "$SANDBOX/origin.git" ] ||
    { refuse "origin is not $SANDBOX/origin.git"; return 1; }
  gh_path="$(command -v gh)"
  case "$gh_path" in
    "$TEST_DIR/fakebin/gh") ;;
    "$SANDBOX"/*)
      grep -qF "$TEST_DIR/fakebin/gh" "$gh_path" ||
        { refuse "gh shim $gh_path does not forward to the fake gh"; return 1; }
      ;;
    *) refuse "gh resolves to ${gh_path:-nothing}, not the fake gh"; return 1 ;;
  esac
}

# land_repo: a bare origin and a working clone. main has one commit; topic branch
# CF-900-thing has two commits on thing.txt; both are pushed. Issue #42 is handed
# back with a claim naming that branch. Leaves the shell in the clone. Returns
# non-zero, before any git command that writes outside the sandbox could run,
# when the sandbox is not in place.
land_repo() {
  new_sandbox || { refuse "new_sandbox failed"; return 1; }
  sandbox_dir_guard || return 1
  export CF_LAND_GATES=true CF_CI_POLL_SEC=0
  git init -q --bare -b main "$SANDBOX/origin.git" || return 1
  git clone -q "$SANDBOX/origin.git" "$SANDBOX/work" 2>/dev/null || return 1
  cd "$SANDBOX/work" || return 1
  sandbox_guard || return 1
  git config user.email tester@example.com &&
    git config user.name tester &&
    git checkout -q -b main &&
    echo base > thing.txt &&
    git add thing.txt &&
    git commit -q -m "base" &&
    git push -q origin main &&
    git checkout -q -b CF-900-thing &&
    echo one >> thing.txt &&
    git commit -q -am "Teach thing a first trick" -m "First body." &&
    echo two >> thing.txt &&
    git commit -q -am "Teach thing a second trick" -m "Second body." &&
    git push -q origin CF-900-thing &&
    git checkout -q main &&
    issue_fixture 42 OPEN "handed-back,severity:P2" \
      "taking — CF-900-thing · driver d06-0300Z · lease until $(iso_at 30) · files: thing.txt" 90 ||
    return 1
  BASE_SHA="$(git rev-parse main)" || return 1
  export BASE_SHA
  sandbox_guard
}

origin_git() { git -C "${SANDBOX:-/nonexistent}/origin.git" "$@"; }
has_branch() { origin_git show-ref --verify --quiet "refs/heads/$1" && echo yes || echo no; }
has_worktree() { [ -d "${SANDBOX:-/nonexistent}/work/.worktrees/land-CF-900" ] && echo yes || echo no; }

# other_clone: a second clone of origin, as another session or a human would have.
other_clone() {
  sandbox_dir_guard || return 1
  git clone -q "$SANDBOX/origin.git" "$SANDBOX/other" 2>/dev/null &&
    [ "$(git -C "$SANDBOX/other" remote get-url origin)" = "$SANDBOX/origin.git" ] &&
    git -C "$SANDBOX/other" config user.email other@example.com &&
    git -C "$SANDBOX/other" config user.name other
}

# shim NAME: writes stdin to $SANDBOX/bin/NAME, makes it executable, and puts
# $SANDBOX/bin first on PATH.
shim() {
  sandbox_dir_guard || return 1
  mkdir -p "$SANDBOX/bin" &&
    cat > "$SANDBOX/bin/$1" &&
    chmod +x "$SANDBOX/bin/$1" || return 1
  case ":$PATH:" in *":$SANDBOX/bin:"*) ;; *) export PATH="$SANDBOX/bin:$PATH" ;; esac
}

# labels_of N: issue N's label names, sorted, space-separated.
labels_of() { jq -r '[.labels[].name] | sort | join(" ")' "$FAKE_GH_DIR/issues/$1.json"; }

# last_comment N: the body of issue N's newest comment.
last_comment() { jq -r '.comments | last | .body' "$FAKE_GH_DIR/issues/$1.json"; }

# relabel_handed_back N: a driver hands issue N back again (handed-back on,
# parked off), without going through the gh log.
relabel_handed_back() {
  local f="${FAKE_GH_DIR:-/nonexistent}/issues/$1.json"
  jq '.labels = ([.labels[] | select(.name != "parked")] + [{name: "handed-back"}] | unique_by(.name))' \
    "$f" > "$f.tmp" && mv "$f.tmp" "$f"
}
