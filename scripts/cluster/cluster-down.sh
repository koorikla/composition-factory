#!/usr/bin/env bash
# Tears down the local kind cluster.
set -euo pipefail

CLUSTER_NAME="cf-test"

if kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
  echo "==> Deleting kind cluster '${CLUSTER_NAME}'..."
  kind delete cluster --name "${CLUSTER_NAME}"
  echo "==> Cluster '${CLUSTER_NAME}' deleted."
else
  echo "==> Cluster '${CLUSTER_NAME}' does not exist, nothing to do."
fi
