#!/bin/bash
# lock.sh <pool> <slots> -- <command> [args...]
#
# Runs a command while holding one of <slots> machine-wide slots in <pool>.
# Slots are flock(2) locks on <lock dir>/<pool>.<i>, held by the kernel, so every
# worktree of the clone shares them and a killed holder frees its slot at once.
#
# Descriptor form only: the slot file is opened on fd 9, `lockf -s -t 0 9` locks
# that open file, and this script then execs the command, which inherits fd 9.
# The lock lives exactly as long as the command or any descendant keeps fd 9
# open. The command form (`lockf <file> <command>`) is not used: killing that
# lockf process with -9 releases the lock while its child keeps running.
#
# Environment: CF_LOCK_DIR, CF_LOCK_RETRY_SEC, CF_LOCK_TIMEOUT_SEC, CF_LOCKF_BIN,
# CF_GATE_SLOTS (see docs/superpowers/plans/2026-09-11-continuous-drivers.md).
# Exit: the command's status; 75 on timeout; 2 on usage errors.
# Written for /bin/bash 3.2.
set -u

usage() {
  echo "usage: lock.sh <pool> <slots> -- <command> [args...]" >&2
  exit 2
}

[ $# -ge 4 ] || usage
pool="$1"
slots="$2"
[ "$3" = "--" ] || usage
shift 3
[ -n "$pool" ] || usage
case "$slots" in '' | *[!0-9]*) usage ;; esac
[ "$slots" -ge 1 ] || usage

retry="${CF_LOCK_RETRY_SEC:-10}"
case "$retry" in '' | . | *[!0-9.]* | *.*.*)
  echo "lock.sh: CF_LOCK_RETRY_SEC must be a number of seconds" >&2
  exit 2 ;;
esac
timeout="${CF_LOCK_TIMEOUT_SEC:-}"
case "$timeout" in *[!0-9]*)
  echo "lock.sh: CF_LOCK_TIMEOUT_SEC must be whole seconds" >&2
  exit 2 ;;
esac

if [ "$pool" = gate ] && [ "${CF_GATE_SLOTS:-}" = off ]; then
  exec "$@"
fi

lockf_bin="$(command -v "${CF_LOCKF_BIN:-lockf}")"
if [ -z "$lockf_bin" ] || [ ! -x "$lockf_bin" ]; then
  echo "lock.sh: lockf not found; running $pool unlocked" >&2
  exec "$@"
fi

lock_dir="${CF_LOCK_DIR:-}"
if [ -z "$lock_dir" ]; then
  common="$(git rev-parse --git-common-dir)" || {
    echo "lock.sh: CF_LOCK_DIR is unset and this is not a git repository" >&2
    exit 2
  }
  lock_dir="$common/cf-locks"
fi
mkdir -p "$lock_dir" || exit 73

SECONDS=0
announced=
while :; do
  i=1
  while [ "$i" -le "$slots" ]; do
    exec 9>>"$lock_dir/$pool.$i" || exit 73
    if "$lockf_bin" -s -t 0 9; then
      echo "lock.sh: $pool acquired after ${SECONDS}s" >&2
      exec "$@"
    fi
    exec 9>&-
    i=$((i + 1))
  done
  if [ -n "$timeout" ] && [ "$SECONDS" -ge "$timeout" ]; then
    echo "lock.sh: timed out waiting for $pool" >&2
    exit 75
  fi
  if [ -z "$announced" ]; then
    echo "lock.sh: waiting for $pool" >&2
    announced=1
  fi
  sleep "$retry"
done
