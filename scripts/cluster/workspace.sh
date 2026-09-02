#!/usr/bin/env bash
# Helpers for workspace namespace, group suffix, and port derivation.
set -euo pipefail

TOPLEVEL=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
BASENAME=$(basename "$TOPLEVEL" | tr '[:upper:]' '[:lower:]' | tr -cs 'a-z0-9' '-' | sed 's/^-//;s/-$//')
HASH=$(printf "%s" "$TOPLEVEL" | shasum -a 256 | cut -c1-6)
SLUG="${BASENAME}-${HASH}"
NAMESPACE="cf-${SLUG}"
GROUP_SUFFIX="${SLUG}.cf-test"
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
