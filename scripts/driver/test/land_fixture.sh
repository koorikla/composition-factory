# shellcheck shell=bash
# The land_repo setup shared by land_rerun_test.sh and land_hardening_test.sh,
# the same shape as land_test.sh (which stays verbatim and keeps its own copy).
# Sourced after lib.sh. Defines no test_* functions, so run.sh never runs it.

LAND="$DRIVER_DIR/land.sh"

# land_repo: a bare origin and a working clone. main has one commit; topic branch
# CF-900-thing has two commits on thing.txt; both are pushed. Issue #42 is handed
# back with a claim naming that branch. Leaves the shell in the clone.
land_repo() {
  new_sandbox
  export CF_LAND_GATES=true CF_CI_POLL_SEC=0
  git init -q --bare -b main "$SANDBOX/origin.git"
  git clone -q "$SANDBOX/origin.git" "$SANDBOX/work" 2>/dev/null
  cd "$SANDBOX/work" || return 1
  git config user.email tester@example.com
  git config user.name tester
  git checkout -q -b main
  echo base > thing.txt
  git add thing.txt
  git commit -q -m "base"
  git push -q origin main
  git checkout -q -b CF-900-thing
  echo one >> thing.txt
  git commit -q -am "Teach thing a first trick" -m "First body."
  echo two >> thing.txt
  git commit -q -am "Teach thing a second trick" -m "Second body."
  git push -q origin CF-900-thing
  git checkout -q main
  issue_fixture 42 OPEN "handed-back,severity:P2" \
    "taking — CF-900-thing · driver d06-0300Z · lease until $(iso_at 30) · files: thing.txt" 90
  BASE_SHA="$(git rev-parse main)"
  export BASE_SHA
}

origin_git() { git -C "$SANDBOX/origin.git" "$@"; }
has_branch() { origin_git show-ref --verify --quiet "refs/heads/$1" && echo yes || echo no; }
has_worktree() { [ -d "$SANDBOX/work/.worktrees/land-CF-900" ] && echo yes || echo no; }

# other_clone: a second clone of origin, as another session or a human would have.
other_clone() {
  git clone -q "$SANDBOX/origin.git" "$SANDBOX/other" 2>/dev/null
  git -C "$SANDBOX/other" config user.email other@example.com
  git -C "$SANDBOX/other" config user.name other
}

# shim NAME: writes stdin to $SANDBOX/bin/NAME, makes it executable, and puts
# $SANDBOX/bin first on PATH.
shim() {
  mkdir -p "$SANDBOX/bin"
  cat > "$SANDBOX/bin/$1"
  chmod +x "$SANDBOX/bin/$1"
  case ":$PATH:" in *":$SANDBOX/bin:"*) ;; *) export PATH="$SANDBOX/bin:$PATH" ;; esac
}
