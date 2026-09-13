#!/usr/bin/env bash
# Lane D: In-cluster verification using floci AWS emulator, provider-aws-sqs, and workspace isolation.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=workspace.sh
source "${SCRIPT_DIR}/workspace.sh"

echo "==> Running Lane D floci test for workspace: ${WORKSPACE_SLUG}"
echo "    Namespace:    ${WORKSPACE_NAMESPACE}"
echo "    Group Suffix: ${WORKSPACE_GROUP_SUFFIX}"

OUT_DIR="$(mktemp -d)"
trap 'rm -rf "${OUT_DIR}"' EXIT

# 1. Ensure cluster and floci are running
echo "==> Ensuring kind cluster and floci container are running..."
FLOCI=1 "${SCRIPT_DIR}/cluster.sh"

# 2. Build cf binary
echo "==> Building cf binary..."
make build >/dev/null

# 3. Install provider-aws-sqs:v2.7.0
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

# 6. Generate sqs-queue blueprint with status wires into composed Secret and workspace isolation
echo "==> Ensuring provider schema for sqs-queue is cached..."
./bin/cf provider add ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0 || true

cat <<EOF > "${OUT_DIR}/sqs-blueprint.yaml"
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: sqs-queue
spec:
  sources:
    - provider: ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0
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
        default: us-east-1
        description: AWS region for SQS queues.
  resources:
    - name: main-queue
      kind: Queue
      provider: ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0
      fields:
        region: {from: params.region}
        sqsManagedSseEnabled: {value: true}
    - name: queue-secret
      kind: Secret
      provider: k8s
      fields:
        type: {value: "Opaque"}
        stringData[queueUrl]: {from: resources.main-queue.status.atProvider.url}
        stringData[queueArn]: {from: resources.main-queue.status.atProvider.arn}
EOF

echo "==> Generating sqs-queue blueprint with status wires and --group-suffix=${WORKSPACE_GROUP_SUFFIX}..."
./bin/cf gen "${OUT_DIR}/sqs-blueprint.yaml" --out "${OUT_DIR}" --group-suffix="${WORKSPACE_GROUP_SUFFIX}"

# 7. Ensure workspace namespace exists
echo "==> Ensuring namespace ${WORKSPACE_NAMESPACE} exists..."
kubectl create namespace "${WORKSPACE_NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

# 8. Apply XRD first and wait for Established
echo "==> Applying XRD to cluster..."
kubectl apply -f "${OUT_DIR}/xrds/"

XRD_NAME="xqueues.messaging.sparky.ee.${WORKSPACE_GROUP_SUFFIX}"
if [ "${#XRD_NAME}" -gt 63 ]; then
  echo "ERROR: generated name '${XRD_NAME}' is ${#XRD_NAME} characters." >&2
  echo "       Crossplane labels CompositionRevisions with this name and labels" >&2
  echo "       cap at 63 characters. Shorten WORKSPACE_GROUP_SUFFIX in" >&2
  echo "       scripts/cluster/workspace.sh." >&2
  exit 1
fi

echo "==> Waiting for XRD ${XRD_NAME} to become Established..."
kubectl wait --for=condition=Established "xrd/${XRD_NAME}" --timeout=60s

# 9. Apply Composition and functions
echo "==> Applying Composition and functions to cluster..."
kubectl apply -f "${OUT_DIR}/compositions/"
kubectl apply -f "${OUT_DIR}/functions.yaml"

COMP_NAME="xqueues.messaging.sparky.ee.${WORKSPACE_GROUP_SUFFIX}"
echo "==> Waiting for CompositionRevision for ${COMP_NAME}..."
REVISION_FOUND=false
for i in {1..30}; do
  if kubectl get compositionrevision -l "crossplane.io/composition-name=${COMP_NAME}" 2>/dev/null | grep -q "${COMP_NAME}"; then
    echo "    Found CompositionRevision for ${COMP_NAME}."
    REVISION_FOUND=true
    break
  fi
  sleep 1
done

if [ "$REVISION_FOUND" = false ]; then
  echo "ERROR: no CompositionRevision was created for ${COMP_NAME}" >&2
  kubectl describe composition "${COMP_NAME}" >&2 || true
  exit 1
fi

# Clean up any leftover test XR/queues from previous runs
kubectl delete xqueue test-queue -n "${WORKSPACE_NAMESPACE}" --timeout=15s 2>/dev/null || true
LEFTOVERS=$(kubectl get queue -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || true)
for q in $LEFTOVERS; do
  kubectl patch queue "$q" -n "${WORKSPACE_NAMESPACE}" -p '{"metadata":{"finalizers":[]}}' --type=merge 2>/dev/null || true
done

# 10. Create XR in workspace namespace
XR_MANIFEST="${OUT_DIR}/xr-instance.yaml"
cat <<EOF > "${XR_MANIFEST}"
apiVersion: messaging.sparky.ee.${WORKSPACE_GROUP_SUFFIX}/v1alpha1
kind: XQueue
metadata:
  name: test-queue
  namespace: ${WORKSPACE_NAMESPACE}
spec:
  providerName: default
  region: us-east-1
EOF

echo "==> Applying XR instance test-queue..."
kubectl apply -f "${XR_MANIFEST}"

# 11. Wait for composed Queue to report Ready=True
echo "==> Waiting for composed managed Queue to appear..."
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
  kubectl get compositionrevision -o yaml >&2 || true
  exit 1
fi

echo "==> Waiting for managed Queues to report condition=Ready..."
for q in $QUEUES; do
  echo "    Waiting for queue/${q} to become Ready..."
  kubectl wait --for=condition=Ready "queue/${q}" -n "${WORKSPACE_NAMESPACE}" --timeout=180s
done

# 12. Verify queue physically exists in floci via AWS CLI
echo "==> Verifying queue physically exists in floci via AWS CLI..."
export AWS_ACCESS_KEY_ID="000000000000"
export AWS_SECRET_ACCESS_KEY="test"
export AWS_DEFAULT_REGION="us-east-1"

QUEUE_LIST=$(aws --endpoint-url http://127.0.0.1:4566 sqs list-queues 2>/dev/null || true)
echo "    AWS CLI list-queues output:"
echo "${QUEUE_LIST}"

for q in $QUEUES; do
  EXTERNAL_URL=$(kubectl get queue "$q" -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.status.atProvider.url}' 2>/dev/null || true)
  EXTERNAL_NAME=$(kubectl get queue "$q" -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.status.atProvider.name}' 2>/dev/null || true)
  if [ -z "$EXTERNAL_NAME" ] && [ -n "$EXTERNAL_URL" ]; then
    EXTERNAL_NAME="${EXTERNAL_URL##*/}"
  fi
  if [ -z "$EXTERNAL_NAME" ]; then
    EXTERNAL_NAME=$(kubectl get queue "$q" -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.metadata.annotations.crossplane\.io/external-name}' 2>/dev/null || true)
    EXTERNAL_NAME="${EXTERNAL_NAME##*/}"
  fi

  echo "    Checking queue '$q' (external name: ${EXTERNAL_NAME}) in floci..."
  if ! echo "${QUEUE_LIST}" | grep -q "${EXTERNAL_NAME}"; then
    echo "ERROR: Queue ${EXTERNAL_NAME} ($q) was not found in floci list-queues output!" >&2
    exit 1
  fi
  echo "    Queue ${EXTERNAL_NAME} confirmed present in floci."

  # 13. Verify status wires reached the composed Secret and match AWS CLI
  echo "==> Verifying status wires reached composed Secret and match AWS CLI..."
  AWS_URL=$(aws --endpoint-url http://127.0.0.1:4566 sqs get-queue-url --queue-name "${EXTERNAL_NAME}" --output text --query 'QueueUrl')
  AWS_ARN=$(aws --endpoint-url http://127.0.0.1:4566 sqs get-queue-attributes --queue-url "${AWS_URL}" --attribute-names QueueArn --output text --query 'Attributes.QueueArn')

  echo "    AWS CLI reports for ${EXTERNAL_NAME}:"
  echo "      QueueUrl: ${AWS_URL}"
  echo "      QueueArn: ${AWS_ARN}"

  SECRET_NAME="test-queue-queue-secret"
  echo "    Waiting for secret ${SECRET_NAME} to contain populated status wire data..."
  SECRET_POPULATED=false
  for s in {1..30}; do
    SECRET_URL_B64=$(kubectl get secret "${SECRET_NAME}" -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.data.queueUrl}' 2>/dev/null || true)
    SECRET_ARN_B64=$(kubectl get secret "${SECRET_NAME}" -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.data.queueArn}' 2>/dev/null || true)
    if [ -n "${SECRET_URL_B64}" ] && [ -n "${SECRET_ARN_B64}" ]; then
      SECRET_POPULATED=true
      break
    fi
    sleep 2
  done

  if [ "${SECRET_POPULATED}" = false ]; then
    echo "ERROR: Secret ${SECRET_NAME} was not populated with status wires!" >&2
    kubectl get secret "${SECRET_NAME}" -n "${WORKSPACE_NAMESPACE}" -o yaml >&2 || true
    exit 1
  fi

  SECRET_URL=$(echo "${SECRET_URL_B64}" | base64 -d)
  SECRET_ARN=$(echo "${SECRET_ARN_B64}" | base64 -d)

  echo "    Secret ${SECRET_NAME} contains:"
  echo "      queueUrl: ${SECRET_URL}"
  echo "      queueArn: ${SECRET_ARN}"

  if [ "${SECRET_URL}" != "${AWS_URL}" ]; then
    echo "ERROR: Secret queueUrl '${SECRET_URL}' does not match AWS CLI URL '${AWS_URL}'!" >&2
    exit 1
  fi
  if [ "${SECRET_ARN}" != "${AWS_ARN}" ]; then
    echo "ERROR: Secret queueArn '${SECRET_ARN}' does not match AWS CLI ARN '${AWS_ARN}'!" >&2
    exit 1
  fi

  echo "    Status wires verified: Secret queueUrl and queueArn match AWS CLI output exactly."
done

echo "==> Teardown: deleting XR..."
kubectl delete -f "${XR_MANIFEST}" --timeout=60s || true
kubectl delete secret test-queue-queue-secret -n "${WORKSPACE_NAMESPACE}" --timeout=15s 2>/dev/null || true

echo "==> Waiting for managed Queues to be deleted..."
for i in {1..30}; do
  if ! kubectl get queue -n "${WORKSPACE_NAMESPACE}" 2>/dev/null | grep -q "test-queue"; then
    echo "    Managed Queues cleanly deleted."
    break
  fi
  sleep 2
done

# Clean up any remaining queue finalizers to prevent hanging
REMAINING=$(kubectl get queue -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || true)
for q in $REMAINING; do
  kubectl patch queue "$q" -n "${WORKSPACE_NAMESPACE}" -p '{"metadata":{"finalizers":[]}}' --type=merge 2>/dev/null || true
done

echo "==> Teardown: deleting starter definitions..."
kubectl delete -f "${OUT_DIR}/compositions/" --timeout=30s || true
kubectl delete -f "${OUT_DIR}/xrds/" --timeout=30s || true

echo "==> Lane D floci test passed successfully!"
