#!/usr/bin/env bash
# Lane C/D: Live cluster CRD discovery ("crd mode"), provider parity assertion,
# byte-identical generation from discovered kinds, and round-trip on reconciled objects.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=workspace.sh
source "${SCRIPT_DIR}/workspace.sh"

echo "==> Running Lane C/D CRD mode cluster test for workspace: ${WORKSPACE_SLUG}"
echo "    Namespace:    ${WORKSPACE_NAMESPACE}"
echo "    Group Suffix: ${WORKSPACE_GROUP_SUFFIX}"
echo "    Port:         ${WORKSPACE_LOCAL_PORT}"

OUT_DIR="$(mktemp -d)"
SERVE_PID=""
cleanup() {
  if [ -n "${SERVE_PID}" ]; then
    kill -9 "${SERVE_PID}" 2>/dev/null || true
  fi
  rm -rf "${OUT_DIR}"
}
trap cleanup EXIT

# 1. Ensure kind cluster and floci container are running
echo "==> Ensuring kind cluster and floci container are running..."
FLOCI=1 "${SCRIPT_DIR}/cluster.sh"

# 2. Build cf binary
echo "==> Building cf binary..."
make build >/dev/null

# 3. Ensure provider-aws-sqs:v2.7.0 is installed in kind cluster
echo "==> Installing provider-aws-sqs:v2.7.0 in kind cluster..."
kubectl apply -f - <<EOF
apiVersion: pkg.crossplane.io/v1
kind: Provider
metadata:
  name: provider-aws-sqs
spec:
  package: xpkg.upbound.io/crossplane-contrib/provider-aws-sqs:v2.7.0
EOF

echo "==> Waiting for provider-aws-sqs to become Healthy..."
kubectl wait --for=condition=Healthy provider/provider-aws-sqs --timeout=180s

echo "==> Waiting for provider-family-aws dependency to become Healthy..."
for i in {1..60}; do
  if kubectl get provider 2>/dev/null | grep -q "provider-family-aws"; then
    FAM_NAME=$(kubectl get provider -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' | grep "provider-family-aws" | head -n 1)
    kubectl wait --for=condition=Healthy "provider/${FAM_NAME}" --timeout=180s
    break
  fi
  sleep 2
done

echo "==> Waiting for CRDs to become Established..."
kubectl wait --for=condition=Established crd/clusterproviderconfigs.aws.m.upbound.io --timeout=120s
kubectl wait --for=condition=Established crd/queues.sqs.aws.m.upbound.io --timeout=120s || \
kubectl wait --for=condition=Established crd/queues.sqs.aws.upbound.io --timeout=120s

# 4. RBAC for provider-aws to read ClusterProviderConfig and ProviderConfig
echo "==> Configuring RBAC for Crossplane provider to access ProviderConfigs..."
kubectl apply -f - <<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: crossplane-provider-aws-providerconfig-reader
rules:
- apiGroups:
  - aws.m.upbound.io
  - aws.upbound.io
  resources:
  - clusterproviderconfigs
  - providerconfigs
  - providerconfigusages
  verbs:
  - "*"
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: crossplane-provider-aws-providerconfig-reader
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: crossplane-provider-aws-providerconfig-reader
subjects:
- kind: Group
  name: system:serviceaccounts:crossplane-system
  apiGroup: rbac.authorization.k8s.io
EOF

# 5. Create dummy credentials secret and emulator ProviderConfig
echo "==> Creating dummy credentials secret in crossplane-system..."
kubectl create secret generic aws-creds \
  --namespace crossplane-system \
  --from-literal=creds="[default]
aws_access_key_id = 000000000000
aws_secret_access_key = test" \
  --dry-run=client -o yaml | kubectl apply -f -

echo "==> Applying emulator ProviderConfig..."
cat <<EOF | kubectl apply -f -
apiVersion: aws.m.upbound.io/v1beta1
kind: ClusterProviderConfig
metadata:
  name: default
spec:
  credentials:
    source: Secret
    secretRef:
      namespace: crossplane-system
      name: aws-creds
      key: creds
  endpoint:
    url:
      type: Static
      static: http://floci:4566
    hostnameImmutable: true
    services:
      - sqs
  skip_credentials_validation: true
  skip_region_validation: true
  skip_requesting_account_id: true
  skip_metadata_api_check: true
  s3_use_path_style: true
EOF

TEST_CACHE="${OUT_DIR}/cache"
mkdir -p "${TEST_CACHE}"

# 6. Ensure provider schema is in package cache for comparison
echo "==> Ensuring provider schema is cached in test cache directory..."
./bin/cf provider add ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0 --cache-dir "${TEST_CACHE}" || true

# 7. Start cf serve --cluster on workspace-isolated port
SERVE_BP="${OUT_DIR}/doc.cf.yaml"
./bin/cf init "${SERVE_BP}" >/dev/null

echo "==> Starting cf serve --cluster on port ${WORKSPACE_LOCAL_PORT}..."
./bin/cf serve --cluster \
  --addr "127.0.0.1:${WORKSPACE_LOCAL_PORT}" \
  --blueprint "${SERVE_BP}" \
  --cache-dir "${TEST_CACHE}" \
  --out "${OUT_DIR}/serve-out" \
  --no-ui > "${OUT_DIR}/serve.log" 2>&1 &
SERVE_PID=$!

echo "==> Waiting for cf serve to become ready on http://127.0.0.1:${WORKSPACE_LOCAL_PORT}..."
for i in {1..30}; do
  if curl -sf "http://127.0.0.1:${WORKSPACE_LOCAL_PORT}/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

# 8. Test Connect & Sync CRDs against kind cluster
echo "==> Probing GET /api/cluster on running server..."
CLUSTER_RESP=$(curl -sf "http://127.0.0.1:${WORKSPACE_LOCAL_PORT}/api/cluster")
echo "    Cluster status: ${CLUSTER_RESP}"
if ! echo "${CLUSTER_RESP}" | grep -q '"connected":true'; then
  echo "ERROR: cf serve --cluster failed to connect to Kubernetes cluster" >&2
  cat "${OUT_DIR}/serve.log" >&2
  exit 1
fi

echo "==> Testing POST /api/cluster/sync (Connect & Sync CRDs)..."
SYNC_RESP=$(curl -sf -X POST "http://127.0.0.1:${WORKSPACE_LOCAL_PORT}/api/cluster/sync")
echo "    Sync response: ${SYNC_RESP}"
if ! echo "${SYNC_RESP}" | grep -q '"connected":true'; then
  echo "ERROR: POST /api/cluster/sync failed" >&2
  exit 1
fi

# 9. Contract Check 1: Discovered kind set for installed provider equals cached package's kind set
echo "==> Contract Check 1: Parity assertion for installed provider kind set..."
DISCOVERED_KINDS=$(curl -sf "http://127.0.0.1:${WORKSPACE_LOCAL_PORT}/api/kinds?q=sqs" | jq -r '.kinds[] | select(.group | contains("sqs")) | "\(.kind) \(.apiVersion)"' | sort -u)

# Find cached crds.json for provider-aws-sqs
CACHED_CRDS_FILE=$(find "${TEST_CACHE}" -type f -name "crds.json" | grep "provider-aws-sqs" | head -n 1)
if [ -z "${CACHED_CRDS_FILE}" ]; then
  # Fallback to user cache if not in test cache
  CACHED_CRDS_FILE=$(find "${HOME}/Library/Caches/compositionfactory" "${HOME}/.cache/compositionfactory" -type f -name "crds.json" 2>/dev/null | grep "provider-aws-sqs" | head -n 1 || true)
fi

if [ -z "${CACHED_CRDS_FILE}" ] || [ ! -f "${CACHED_CRDS_FILE}" ]; then
  echo "ERROR: could not locate cached crds.json for provider-aws-sqs" >&2
  exit 1
fi

CACHED_KINDS=$(jq -r '.crds[] | "\(.Kind) \(.Group)/\(.Versions[0].Name)"' "${CACHED_CRDS_FILE}" | sort -u)

echo "--- Discovered Kinds ---"
echo "${DISCOVERED_KINDS}"
echo "--- Cached Package Kinds ---"
echo "${CACHED_KINDS}"

if [ -z "${DISCOVERED_KINDS}" ]; then
  echo "ERROR: No discovered kinds found for installed provider sqs" >&2
  exit 1
fi

diff -u <(echo "${DISCOVERED_KINDS}") <(echo "${CACHED_KINDS}") || {
  echo "ERROR: Discovered kind set does not match cached package kind set!" >&2
  exit 1
}
echo "    Parity assertion PASSED: discovered kinds match cached package kinds (names and apiVersions)."

# Stop background cf serve
kill "${SERVE_PID}" 2>/dev/null || true
SERVE_PID=""

# 10. Contract Check 2: Blueprint authored from discovered kinds generates byte-identically to one from package cache
echo "==> Contract Check 2: Byte-identical generation from discovered kinds..."
DISCOVERED_BP="${OUT_DIR}/sqs-queue-discovered.cf.yaml"
# Author blueprint from discovered provider package
DISCOVERED_PROVIDER="xpkg.upbound.io/crossplane-contrib/provider-aws-sqs:v2.7.0"
cat <<EOF > "${DISCOVERED_BP}"
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: sqs-queue
spec:
  sources:
    - provider: ${DISCOVERED_PROVIDER}
  xrd:
    group: messaging.sparky.ee
    kind: XQueue
    plural: xqueues
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName:
        type: string
        required: true
        description: ProviderConfig reference to reconcile against.
      region:
        type: string
        default: eu-north-1
        description: AWS region for SQS queues.
      messageRetentionSeconds:
        type: integer
        default: "345600"
        description: Seconds to retain messages (default 4 days).
      maxMessageSize:
        type: integer
        default: "262144"
        description: Maximum message size in bytes (default 256 KB).
      enableDlq:
        type: boolean
        default: "true"
        description: Whether to provision a Dead Letter Queue for failed deliveries.
  resources:
    - name: dlq
      kind: Queue
      provider: ${DISCOVERED_PROVIDER}
      when: params.enableDlq
      fields:
        region: {from: params.region}
        sqsManagedSseEnabled: {value: true}
        messageRetentionSeconds: {value: 1209600}
    - name: main-queue
      kind: Queue
      provider: ${DISCOVERED_PROVIDER}
      fields:
        region: {from: params.region}
        sqsManagedSseEnabled: {value: true}
        maxMessageSize: {from: params.maxMessageSize}
        messageRetentionSeconds: {from: params.messageRetentionSeconds}
    - name: queue-policy
      kind: QueuePolicy
      provider: ${DISCOVERED_PROVIDER}
      fields:
        region: {from: params.region}
        queueUrl: {from: resources.main-queue.status.atProvider.url}
        policy: {raw: "'{\"Version\":\"2012-10-17\",\"Statement\":[{\"Effect\":\"Allow\",\"Principal\":{\"AWS\":\"*\"},\"Action\":\"sqs:SendMessage\",\"Resource\":\"*\"}]}'"}
EOF

GEN_DISCOVERED_OUT="${OUT_DIR}/gen-discovered"
GEN_CACHED_OUT="${OUT_DIR}/gen-cached"

./bin/cf gen "${DISCOVERED_BP}" --out "${GEN_DISCOVERED_OUT}" --cache-dir "${TEST_CACHE}" --group-suffix="${WORKSPACE_GROUP_SUFFIX}"
./bin/cf gen internal/examples/sqs-queue.cf.yaml --out "${GEN_CACHED_OUT}" --cache-dir "${TEST_CACHE}" --group-suffix="${WORKSPACE_GROUP_SUFFIX}"

echo "==> Verifying emitted Composition from discovered kinds matches package-cache emission..."
COMP_DISC=("${GEN_DISCOVERED_OUT}/compositions/"*.yaml)
COMP_CACHE=("${GEN_CACHED_OUT}/compositions/"*.yaml)
diff -u -I '^# Source:' "${COMP_DISC[0]}" "${COMP_CACHE[0]}" || {
  echo "ERROR: Composition generated from discovered kinds differs from package cache emission" >&2
  exit 1
}

echo "==> Verifying emitted XRD from discovered kinds matches package-cache emission..."
XRD_DISC=("${GEN_DISCOVERED_OUT}/xrds/"*.yaml)
XRD_CACHE=("${GEN_CACHED_OUT}/xrds/"*.yaml)
diff -u -I '^# Source:' "${XRD_DISC[0]}" "${XRD_CACHE[0]}" || {
  echo "ERROR: XRD generated from discovered kinds differs from package cache emission" >&2
  exit 1
}
echo "    Generation assertion PASSED: blueprint from discovered kinds generates byte-identically."

# 11. Contract Check 3: cf gen -> kubectl apply -> kubectl get <xr> -o yaml -> cf adopt -> cf gen round-trip on reconciled objects
echo "==> Contract Check 3: Applying generated artifacts and verifying reconciliation..."
echo "==> Ensuring namespace ${WORKSPACE_NAMESPACE} exists..."
kubectl create namespace "${WORKSPACE_NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

XRD_NAME="xqueues.messaging.sparky.ee.${WORKSPACE_GROUP_SUFFIX}"
COMP_NAME="xqueues.messaging.sparky.ee.${WORKSPACE_GROUP_SUFFIX}"

kubectl apply -f "${GEN_DISCOVERED_OUT}/xrds/"
echo "==> Waiting for XRD ${XRD_NAME} to become Established..."
kubectl wait --for=condition=Established "xrd/${XRD_NAME}" --timeout=60s

kubectl apply -f "${GEN_DISCOVERED_OUT}/compositions/"
kubectl apply -f "${GEN_DISCOVERED_OUT}/functions.yaml"

echo "==> Waiting for CompositionRevision for ${COMP_NAME}..."
for i in {1..30}; do
  if kubectl get compositionrevision -l "crossplane.io/composition-name=${COMP_NAME}" 2>/dev/null | grep -q "${COMP_NAME}"; then
    echo "    Found CompositionRevision for ${COMP_NAME}."
    break
  fi
  sleep 1
done

# Clean up any leftover test XR/queues
kubectl delete xqueue crd-test-queue -n "${WORKSPACE_NAMESPACE}" --timeout=15s 2>/dev/null || true
LEFTOVERS=$(kubectl get queue -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || true)
for q in $LEFTOVERS; do
  kubectl patch queue "$q" -n "${WORKSPACE_NAMESPACE}" -p '{"metadata":{"finalizers":[]}}' --type=merge 2>/dev/null || true
done

XR_MANIFEST="${OUT_DIR}/xr-instance.yaml"
cat <<EOF > "${XR_MANIFEST}"
apiVersion: messaging.sparky.ee.${WORKSPACE_GROUP_SUFFIX}/v1alpha1
kind: XQueue
metadata:
  name: crd-test-queue
  namespace: ${WORKSPACE_NAMESPACE}
spec:
  providerName: default
  region: us-east-1
EOF

echo "==> Applying XR instance crd-test-queue..."
kubectl apply -f "${XR_MANIFEST}"

echo "==> Waiting for composed Queue to appear and reconcile to Ready=True..."
QUEUE_FOUND=false
for i in {1..60}; do
  QUEUES=$(kubectl get queue.sqs.aws.m.upbound.io -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || \
           kubectl get queue.sqs.aws.upbound.io -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || \
           kubectl get queue -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || true)
  if [ -n "${QUEUES}" ]; then
    QUEUE_FOUND=true
    break
  fi
  sleep 2
done

if [ "$QUEUE_FOUND" = false ]; then
  echo "ERROR: Composed Queue did not appear in namespace ${WORKSPACE_NAMESPACE}" >&2
  kubectl get xqueue -A -o yaml >&2 || true
  exit 1
fi

for q in $QUEUES; do
  echo "    Waiting for queue/${q} to become Ready..."
  kubectl wait --for=condition=Ready "queue/${q}" -n "${WORKSPACE_NAMESPACE}" --timeout=180s
done

echo "==> Confirming physical queue existence in floci emulator..."
export AWS_ACCESS_KEY_ID="000000000000"
export AWS_SECRET_ACCESS_KEY="test"
export AWS_DEFAULT_REGION="us-east-1"
QUEUE_LIST=$(aws --endpoint-url http://127.0.0.1:4566 sqs list-queues 2>/dev/null || true)
echo "    floci list-queues: ${QUEUE_LIST}"
for q in $QUEUES; do
  Q_URL=$(kubectl get queue "$q" -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.status.atProvider.url}' 2>/dev/null || true)
  if [ -z "${Q_URL}" ]; then
    echo "ERROR: queue/${q} has no status.atProvider.url" >&2
    exit 1
  fi
  Q_PATH=$(basename "${Q_URL}")
  if ! echo "${QUEUE_LIST}" | grep -q "${Q_PATH}"; then
    echo "ERROR: physical queue ${Q_PATH} not found in floci list-queues output" >&2
    exit 1
  fi
  echo "    Confirmed queue/${q} exists physically in floci: ${Q_URL}"
done
echo "    Reconciled object confirmed in cluster and floci."

echo "==> Reading live server-round-tripped XRD, Composition and XR from API server..."
LIVE_STREAM="${OUT_DIR}/live-stream.yaml"
kubectl get xrd "${XRD_NAME}" -o yaml > "${LIVE_STREAM}"
echo -e "\n---" >> "${LIVE_STREAM}"
kubectl get composition "${COMP_NAME}" -o yaml >> "${LIVE_STREAM}"
echo -e "\n---" >> "${LIVE_STREAM}"
kubectl get xqueue crd-test-queue -n "${WORKSPACE_NAMESPACE}" -o yaml >> "${LIVE_STREAM}"

ADOPTED_BP="${OUT_DIR}/adopted.cf.yaml"
ADOPT_REPORT="${OUT_DIR}/loss-report.txt"

echo "==> Adopting live server-round-tripped stream with cf adopt..."
./bin/cf adopt "${LIVE_STREAM}" -o "${ADOPTED_BP}" --cache-dir "${TEST_CACHE}" > "${ADOPT_REPORT}" 2>&1 || true

echo "==> Checking loss report for scrubbed server-added fields and non-composition manifest..."
cat "${ADOPT_REPORT}"
if ! grep -qi "crd-test-queue" "${ADOPT_REPORT}" && ! grep -qi "XQueue" "${ADOPT_REPORT}"; then
  echo "ERROR: Loss report did not name dropped XR manifest crd-test-queue" >&2
  exit 1
fi
if ! grep -qi "metadata\." "${ADOPT_REPORT}" && ! grep -qi "status" "${ADOPT_REPORT}" && ! grep -qi "scrub" "${ADOPT_REPORT}"; then
  echo "ERROR: Loss report did not name server-added metadata/status fields" >&2
  exit 1
fi
echo "    Loss report verified: server-added fields named."

REGEN_OUT="${OUT_DIR}/regen-out"
echo "==> Regenerating Crossplane artifacts from adopted blueprint..."
./bin/cf gen "${ADOPTED_BP}" --out "${REGEN_OUT}" --cache-dir "${TEST_CACHE}"

echo "==> Verifying regenerated Composition matches original emitted Composition..."
COMP_ORIG=("${GEN_DISCOVERED_OUT}/compositions/"*.yaml)
COMP_REGEN=("${REGEN_OUT}/compositions/"*.yaml)
diff -u -I '^# Source:' "${COMP_ORIG[0]}" "${COMP_REGEN[0]}" || {
  echo "ERROR: Round-trip regenerated composition differs from original emission" >&2
  exit 1
}

echo "==> Verifying regenerated XRD matches original emitted XRD..."
XRD_ORIG=("${GEN_DISCOVERED_OUT}/xrds/"*.yaml)
XRD_REGEN=("${REGEN_OUT}/xrds/"*.yaml)
diff -u -I '^# Source:' "${XRD_ORIG[0]}" "${XRD_REGEN[0]}" || {
  echo "ERROR: Round-trip regenerated XRD differs from original emission" >&2
  exit 1
}
echo "    Round-trip assertion PASSED: server round-tripped objects reproduce original bytes."

# 12. Teardown
echo "==> Teardown: deleting XR..."
kubectl delete -f "${XR_MANIFEST}" --timeout=60s || true

echo "==> Waiting for managed Queues to be deleted..."
for i in {1..30}; do
  if ! kubectl get queue -n "${WORKSPACE_NAMESPACE}" 2>/dev/null | grep -q "crd-test-queue"; then
    echo "    Managed Queues cleanly deleted."
    break
  fi
  sleep 2
done

REMAINING=$(kubectl get queue -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || true)
for q in $REMAINING; do
  kubectl patch queue "$q" -n "${WORKSPACE_NAMESPACE}" -p '{"metadata":{"finalizers":[]}}' --type=merge 2>/dev/null || true
done

echo "==> Teardown: deleting definitions..."
kubectl delete -f "${GEN_DISCOVERED_OUT}/compositions/" --timeout=30s || true
kubectl delete -f "${GEN_DISCOVERED_OUT}/xrds/" --timeout=30s || true

echo "==> Lane C/D CRD mode cluster test passed successfully!"
