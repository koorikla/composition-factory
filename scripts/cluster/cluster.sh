#!/usr/bin/env bash
# Idempotently sets up a local kind cluster with Crossplane and required functions.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=versions.env
source "${SCRIPT_DIR}/versions.env"

CLUSTER_NAME="cf-test"

echo "==> Checking if kind cluster '${CLUSTER_NAME}' exists..."
if ! kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
  echo "==> Creating kind cluster '${CLUSTER_NAME}' with node image ${KIND_NODE_IMAGE}..."
  kind create cluster --name "${CLUSTER_NAME}" --image "${KIND_NODE_IMAGE}"
else
  echo "==> Cluster '${CLUSTER_NAME}' already exists."
fi

# Ensure kubeconfig context is selected
kubectl config use-context "kind-${CLUSTER_NAME}"

echo "==> Installing / upgrading Crossplane via Helm (v${CROSSPLANE_VERSION})..."
helm repo add crossplane-stable https://charts.crossplane.io/stable --force-update >/dev/null 2>&1
helm repo update crossplane-stable >/dev/null 2>&1

helm upgrade --install crossplane crossplane-stable/crossplane \
  --namespace crossplane-system \
  --create-namespace \
  --version "${CROSSPLANE_VERSION}" \
  --wait --timeout 180s

echo "==> Applying Crossplane functions (function-go-templating, function-auto-ready)..."
kubectl apply -f - <<EOF
apiVersion: pkg.crossplane.io/v1
kind: Function
metadata:
  name: function-go-templating
spec:
  package: xpkg.upbound.io/crossplane-contrib/function-go-templating:${FUNCTION_GO_TEMPLATING_VERSION}
---
apiVersion: pkg.crossplane.io/v1
kind: Function
metadata:
  name: function-auto-ready
spec:
  package: xpkg.upbound.io/crossplane-contrib/function-auto-ready:${FUNCTION_AUTO_READY_VERSION}
EOF

echo "==> Waiting for Crossplane functions to become Healthy..."
kubectl wait --for=condition=Healthy function/function-go-templating --timeout=180s
kubectl wait --for=condition=Healthy function/function-auto-ready --timeout=180s

echo "==> Cluster '${CLUSTER_NAME}' is ready with Crossplane and functions."
