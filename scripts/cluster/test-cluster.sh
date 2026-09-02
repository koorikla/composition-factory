#!/usr/bin/env bash
# Lane C: In-cluster verification using kind, Crossplane, and workspace isolation.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=workspace.sh
source "${SCRIPT_DIR}/workspace.sh"

echo "==> Running Lane C cluster test for workspace: ${WORKSPACE_SLUG}"
echo "    Namespace:    ${WORKSPACE_NAMESPACE}"
echo "    Group Suffix: ${WORKSPACE_GROUP_SUFFIX}"

OUT_DIR="$(mktemp -d)"
trap 'rm -rf "${OUT_DIR}"' EXIT

# Build cf binary
echo "==> Building cf binary..."
make build >/dev/null

# 1. Generate k8s-workload example with group suffix
echo "==> Generating k8s-workload example with --group-suffix=${WORKSPACE_GROUP_SUFFIX}..."
./bin/cf gen internal/examples/k8s-workload.cf.yaml --out "${OUT_DIR}" --group-suffix="${WORKSPACE_GROUP_SUFFIX}"

# 2. Ensure workspace namespace exists
echo "==> Ensuring namespace ${WORKSPACE_NAMESPACE} exists..."
kubectl create namespace "${WORKSPACE_NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

# 3. Apply XRD first and wait for it to become Established
echo "==> Applying XRD to cluster..."
kubectl apply -f "${OUT_DIR}/xrds/"

XRD_NAME="xworkloads.workloads.sparky.ee.${WORKSPACE_GROUP_SUFFIX}"
echo "==> Waiting for XRD ${XRD_NAME} to become Established..."
kubectl wait --for=condition=Established "xrd/${XRD_NAME}" --timeout=60s

# 4. Apply Composition and functions
echo "==> Applying Composition and functions to cluster..."
kubectl apply -f "${OUT_DIR}/compositions/"
kubectl apply -f "${OUT_DIR}/functions.yaml"

COMP_NAME="xworkloads.workloads.sparky.ee.${WORKSPACE_GROUP_SUFFIX}"
echo "==> Waiting for CompositionRevision for ${COMP_NAME}..."
for i in {1..30}; do
  if kubectl get compositionrevision -l "crossplane.io/composition-name=${COMP_NAME}" 2>/dev/null | grep -q "${COMP_NAME}"; then
    echo "    Found CompositionRevision for ${COMP_NAME}."
    break
  fi
  sleep 1
done

# 4. Create XR in workspace namespace
XR_MANIFEST="${OUT_DIR}/xr-instance.yaml"
cat <<EOF > "${XR_MANIFEST}"
apiVersion: workloads.sparky.ee.${WORKSPACE_GROUP_SUFFIX}/v1alpha1
kind: XWorkload
metadata:
  name: test-workload
  namespace: ${WORKSPACE_NAMESPACE}
spec:
  providerName: in-cluster
  image: nginx:alpine
  replicas: 1
  port: 8080
  logLevel: info
  enableService: true
EOF

echo "==> Applying XR instance test-workload..."
kubectl apply -f "${XR_MANIFEST}"

# 5. Wait for composed Deployment to become Available
echo "==> Waiting for composed Deployment test-workload-app to become Available..."
# Crossplane names composed resources with a hash or name prefix; find deployment in namespace
DEPLOYMENT_FOUND=false
for i in {1..30}; do
  if kubectl get deployment -n "${WORKSPACE_NAMESPACE}" | grep -q "test-workload"; then
    DEPLOY_NAME=$(kubectl get deployment -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.items[0].metadata.name}')
    echo "    Found deployment ${DEPLOY_NAME}, waiting for condition=Available..."
    kubectl wait --for=condition=Available "deployment/${DEPLOY_NAME}" -n "${WORKSPACE_NAMESPACE}" --timeout=60s
    DEPLOYMENT_FOUND=true
    break
  fi
  sleep 2
done

if [ "$DEPLOYMENT_FOUND" = false ]; then
  echo "ERROR: Composed Deployment did not appear in namespace ${WORKSPACE_NAMESPACE}"
  echo "=== XWorkload ==="
  kubectl get xworkload -n "${WORKSPACE_NAMESPACE}" -o yaml || true
  echo "=== Compositions ==="
  kubectl get composition -o yaml || true
  echo "=== CompositionRevisions ==="
  kubectl get compositionrevision -o yaml || true
  echo "=== Crossplane logs ==="
  kubectl logs -n crossplane-system deployment/crossplane --tail=100 || true
  exit 1
fi

echo "==> Checking for composed Service..."
SVC_FOUND=false
for i in {1..15}; do
  if kubectl get service -n "${WORKSPACE_NAMESPACE}" | grep -q "test-workload"; then
    echo "    Found composed Service in ${WORKSPACE_NAMESPACE}."
    SVC_FOUND=true
    break
  fi
  sleep 2
done

if [ "$SVC_FOUND" = false ]; then
  echo "ERROR: Composed Service did not appear in namespace ${WORKSPACE_NAMESPACE}"
  exit 1
fi

echo "==> Teardown: deleting XR..."
kubectl delete -f "${XR_MANIFEST}" --timeout=60s || true

echo "==> Teardown: deleting workspace definitions..."
kubectl delete -f "${OUT_DIR}/compositions/" --timeout=30s || true
kubectl delete -f "${OUT_DIR}/xrds/" --timeout=30s || true

echo "==> Lane C cluster test passed successfully!"
