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

if [ "${FLOCI:-0}" = "1" ] || [ "${FLOCI:-}" = "true" ]; then
  FLOCI_IMAGE="${FLOCI_IMAGE:-floci/floci@sha256:4e451c39c7bb88e3cd4f87e8fc0c25d5b47695a51185d521e2241fa00486e8eb}"
  echo "==> Ensuring floci container is running on kind network..."
  if docker ps -a --format '{{.Names}}' | grep -q "^floci$"; then
    if ! docker ps --format '{{.Names}}' | grep -q "^floci$"; then
      echo "    Starting existing floci container..."
      docker start floci
    else
      echo "    floci container is already running."
    fi
  else
    echo "    Starting floci container (${FLOCI_IMAGE})..."
    docker run -d \
      --name floci \
      --network kind \
      -p 127.0.0.1:4566:4566 \
      -e FLOCI_STORAGE_MODE=memory \
      -e LOCALSTACK_HOST=floci \
      "${FLOCI_IMAGE}"
  fi

  # Ensure connected to kind network
  docker network connect kind floci 2>/dev/null || true

  echo "==> Waiting for floci to become ready..."
  FLOCI_READY=false
  for i in {1..30}; do
    if curl -sf http://127.0.0.1:4566/_floci/health >/dev/null 2>&1 || curl -sf http://127.0.0.1:4566/_localstack/health >/dev/null 2>&1; then
      echo "    floci is ready."
      FLOCI_READY=true
      break
    fi
    sleep 1
  done

  if [ "$FLOCI_READY" = false ]; then
    echo "ERROR: floci container failed to become ready" >&2
    docker logs floci --tail=50 >&2 || true
    exit 1
  fi
fi

echo "==> Cluster '${CLUSTER_NAME}' is ready with Crossplane and functions."

