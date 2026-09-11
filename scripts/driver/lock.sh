#!/bin/bash
# lock.sh <pool> <slots> -- <command> [args...]
#
# Runs a command while holding one of <slots> machine-wide slots in <pool>.
# Slots are flock(2) locks on <lock dir>/<pool>.<i>, held by the kernel, so every
# worktree of the clone shares them and a killed holder frees its slot at once.
#
# Descriptor form only: the slot file is opened on a free descriptor (the highest
# free one in 9..3), `lockf -s -t 0 <fd>` locks that open file, and this script
# then execs the command, which inherits the descriptor. The lock lives exactly
# as long as the command or any descendant keeps it open. The command form
# (`lockf <file> <command>`) is not used: killing that lockf process with -9
# releases the lock while its child keeps running.
#
# - Any descendant that detaches (e.g. `git gc --auto`) keeps the slot until it
#   exits. `lsof <slot file>` shows who holds it.
# - Never delete slot files: a recreated file is a new inode, and a second holder
#   would lock it while the first still holds the old one.
# - Nesting in one process is safe: `lock.sh a 1 -- lock.sh b 1 -- cmd` holds both
#   a and b until cmd exits, so an exec chain never gives just one lock.
#   Re-entrancy is the caller's job: nesting the same pool waits on itself.
# - Without lockf (Linux CI) every pool runs unlocked, with a warning. Drivers
#   must run where lockf exists.
#
# Environment: CF_LOCK_DIR, CF_LOCK_RETRY_SEC, CF_LOCK_TIMEOUT_SEC, CF_LOCKF_BIN,
# CF_GATE_SLOTS (see docs/superpowers/plans/2026-09-11-continuous-drivers.md).
# Exit: the command's status; 75 on timeout; 73 when a slot cannot be opened or
# locked (lockf error other than busy, no free descriptor); 2 on usage errors.
# Written for /bin/bash 3.2.
set -u

usage() {
  echo "usage: lock.sh <pool> <slots> -- <command> [args...]" >&2
  echo "       <pool> uses only A-Z a-z 0-9 _ -; <slots> is a positive integer" >&2
  exit 2
}

# Spelled out rather than A-Za-z: glob ranges follow the locale's collation.
pool_chars=ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-

[ $# -ge 4 ] || usage
pool="$1"
slots="$2"
[ "$3" = "--" ] || usage
shift 3
case "$pool" in '' | *[!$pool_chars]*) usage ;; esac
case "$slots" in '' | *[!0-9]*) usage ;; esac
[ "$slots" -ge 1 ] || usage

bad_env() {
  echo "lock.sh: $1" >&2
  exit 2
}

retry="${CF_LOCK_RETRY_SEC:-10}"
case "$retry" in *[!0-9.]* | *.*.*) bad_env "CF_LOCK_RETRY_SEC must be a positive number of seconds" ;; esac
case "$retry" in *[1-9]*) ;; *) bad_env "CF_LOCK_RETRY_SEC must be a positive number of seconds" ;; esac
timeout="${CF_LOCK_TIMEOUT_SEC:-}"
case "$timeout" in *[!0-9]*) bad_env "CF_LOCK_TIMEOUT_SEC must be whole seconds" ;; esac
[ -z "$timeout" ] || timeout=$((10#$timeout))

if [ "$pool" = gate ] && [ "${CF_GATE_SLOTS:-}" = off ]; then
  exec "$@"
fi

lockf_bin="$(type -P "${CF_LOCKF_BIN:-lockf}")"
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

# An outer lock.sh that exec'd into this one still holds its lock on an open
# descriptor; reusing that descriptor would close it and drop the outer lock.
fd=9
while [ "$fd" -ge 3 ] && { : >&"$fd"; } 2>/dev/null; do
  fd=$((fd - 1))
done
if [ "$fd" -lt 3 ]; then
  echo "lock.sh: no free descriptor in 3..9" >&2
  exit 73
fi

SECONDS=0
announced=
while :; do
  i=1
  while [ "$i" -le "$slots" ]; do
    slot="$lock_dir/$pool.$i"
    eval "exec $fd>>\"\$slot\"" || exit 73
    "$lockf_bin" -s -t 0 "$fd"
    rc=$?
    case "$rc" in
      0)
        echo "lock.sh: $pool acquired after ${SECONDS}s" >&2
        exec "$@"
        ;;
      75)
        eval "exec $fd>&-"
        ;;
      *)
        eval "exec $fd>&-"
        echo "lock.sh: lockf failed on $slot (exit $rc); not running $pool" >&2
        exit 73
        ;;
    esac
    i=$((i + 1))
  done
  nap="$retry"
  if [ -n "$timeout" ]; then
    remaining=$((timeout - SECONDS))
    if [ "$remaining" -le 0 ]; then
      echo "lock.sh: timed out waiting for $pool" >&2
      exit 75
    fi
    whole="${retry%%.*}"
    [ "${whole:-0}" -lt "$remaining" ] || nap="$remaining"
  fi
  if [ -z "$announced" ]; then
    echo "lock.sh: waiting for $pool" >&2
    announced=1
  fi
  sleep "$nap"
done
