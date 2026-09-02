#!/usr/bin/env bash
# Helpers for workspace namespace, group suffix, and port derivation.
set -euo pipefail

TOPLEVEL=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
BASENAME=$(basename "$TOPLEVEL" | tr '[:upper:]' '[:lower:]' | tr -cs 'a-z0-9' '-' | sed 's/^-//;s/-$//')
HASH=$(printf "%s" "$TOPLEVEL" | shasum -a 256 | cut -c1-6)
SLUG="${BASENAME}-${HASH}"
NAMESPACE="cf-${SLUG}"
# The group suffix lands inside cluster-scoped names like
# xworkloads.workloads.sparky.ee.<suffix>, and Crossplane copies the
# Composition's name verbatim into the crossplane.io/composition-name *label*
# of every CompositionRevision. Label values cap at 63 characters, so a suffix
# built from the (arbitrarily long) directory basename silently breaks
# CompositionRevision creation. Keep the group suffix short and bounded: the
# 6-char path hash is what actually provides isolation, and it is the same hash
# that appears in the namespace, so group and namespace remain easy to pair up.
GROUP_SUFFIX="w${HASH}.cf-test"
PORT_OFFSET=$(node -e "console.log(parseInt('$HASH', 16) % 10000)" 2>/dev/null || echo "8080")
LOCAL_PORT=$((19000 + PORT_OFFSET))

export WORKSPACE_SLUG="${SLUG}"
export WORKSPACE_NAMESPACE="${NAMESPACE}"
export WORKSPACE_GROUP_SUFFIX="${GROUP_SUFFIX}"
export WORKSPACE_LOCAL_PORT="${LOCAL_PORT}"

if [[ "${1:-}" == "env" ]]; then
  echo "WORKSPACE_SLUG=${WORKSPACE_SLUG}"
  echo "WORKSPACE_NAMESPACE=${WORKSPACE_NAMESPACE}"
  echo "WORKSPACE_GROUP_SUFFIX=${WORKSPACE_GROUP_SUFFIX}"
  echo "WORKSPACE_LOCAL_PORT=${WORKSPACE_LOCAL_PORT}"
fi
