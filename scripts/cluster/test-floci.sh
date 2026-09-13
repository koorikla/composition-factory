#!/usr/bin/env bash
# Lane D: In-cluster verification of all AWS starters using floci emulator,
# real Docker-backed PostgreSQL, and workspace isolation.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=workspace.sh
source "${SCRIPT_DIR}/workspace.sh"

echo "==> Running Lane D floci test for workspace: ${WORKSPACE_SLUG}"
echo "    Namespace:    ${WORKSPACE_NAMESPACE}"
echo "    Group Suffix: ${WORKSPACE_GROUP_SUFFIX}"

OUT_DIR="$(mktemp -d)"
trap 'rm -rf "${OUT_DIR}"' EXIT

# 1. Ensure cluster and floci container are running
echo "==> Ensuring kind cluster and floci container are running..."
FLOCI=1 "${SCRIPT_DIR}/cluster.sh"

# Ensure floci container has /var/run/docker.sock mounted (needed for RDS real postgres)
# and network alias 000000000000.floci on kind network (needed for S3Control bucket tagging)
FLOCI_MOUNTS=$(docker inspect floci --format '{{json .Mounts}}' 2>/dev/null || echo "[]")
FLOCI_ALIASES=$(docker inspect floci --format '{{json .NetworkSettings.Networks.kind.Aliases}}' 2>/dev/null || echo "[]")

NEED_RESTART=false
if ! echo "$FLOCI_MOUNTS" | grep -q "/var/run/docker.sock"; then
  echo "    floci missing /var/run/docker.sock mount; restarting container..."
  NEED_RESTART=true
fi
if ! echo "$FLOCI_ALIASES" | grep -q "000000000000.floci"; then
  echo "    floci missing 000000000000.floci alias; updating network connection..."
  docker network connect --alias 000000000000.floci kind floci 2>/dev/null || NEED_RESTART=true
fi

if [ "$NEED_RESTART" = true ]; then
  echo "    Restarting floci container with docker.sock mount and network aliases..."
  FLOCI_IMAGE=$(docker inspect floci --format '{{.Config.Image}}' 2>/dev/null || echo "ghcr.io/koorikla/floci:latest")
  docker rm -f floci 2>/dev/null || true
  docker run -d \
    --name floci \
    --network kind \
    --network-alias floci \
    --network-alias 000000000000.floci \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -p 127.0.0.1:4566:4566 \
    -e FLOCI_STORAGE_MODE=memory \
    -e LOCALSTACK_HOST=floci \
    "${FLOCI_IMAGE}"

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

# 2. Build cf binary
echo "==> Building cf binary..."
make build >/dev/null

# 3. Install all 4 AWS providers in kind cluster: sqs, s3, iam, rds
echo "==> Installing AWS Crossplane providers in kind cluster..."
cat <<EOF | kubectl apply -f -
apiVersion: pkg.crossplane.io/v1
kind: Provider
metadata:
  name: provider-aws-sqs
spec:
  package: xpkg.upbound.io/crossplane-contrib/provider-aws-sqs:v2.7.0
---
apiVersion: pkg.crossplane.io/v1
kind: Provider
metadata:
  name: provider-aws-s3
spec:
  package: xpkg.upbound.io/crossplane-contrib/provider-aws-s3:v2.7.0
---
apiVersion: pkg.crossplane.io/v1
kind: Provider
metadata:
  name: provider-aws-iam
spec:
  package: xpkg.upbound.io/crossplane-contrib/provider-aws-iam:v2.7.0
---
apiVersion: pkg.crossplane.io/v1
kind: Provider
metadata:
  name: provider-aws-rds
spec:
  package: xpkg.upbound.io/crossplane-contrib/provider-aws-rds:v2.7.0
EOF

echo "==> Waiting for AWS providers to become Healthy..."
for p in provider-aws-sqs provider-aws-s3 provider-aws-iam provider-aws-rds; do
  echo "    Waiting for $p..."
  kubectl wait --for=condition=Healthy "provider/$p" --timeout=180s
done

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
kubectl wait --for=condition=Established crd/buckets.s3.aws.m.upbound.io --timeout=120s || \
kubectl wait --for=condition=Established crd/buckets.s3.aws.upbound.io --timeout=120s
kubectl wait --for=condition=Established crd/roles.iam.aws.m.upbound.io --timeout=120s || \
kubectl wait --for=condition=Established crd/roles.iam.aws.upbound.io --timeout=120s
kubectl wait --for=condition=Established crd/instances.rds.aws.m.upbound.io --timeout=120s || \
kubectl wait --for=condition=Established crd/instances.rds.aws.upbound.io --timeout=120s

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

echo "==> Applying emulator ProviderConfig with all required AWS services..."
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
      - s3
      - s3control
      - iam
      - sts
      - rds
  skip_credentials_validation: true
  skip_region_validation: true
  skip_requesting_account_id: true
  skip_metadata_api_check: true
  s3_use_path_style: true
EOF

# 6. Ensure provider schemas are cached
echo "==> Ensuring provider schemas for all 4 AWS starters are cached..."
./bin/cf provider add ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0 || true
./bin/cf provider add ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0 || true
./bin/cf provider add ghcr.io/crossplane-contrib/provider-aws-iam:v2.7.0 || true
./bin/cf provider add ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0 || true

# 7. Ensure workspace namespace exists
echo "==> Ensuring namespace ${WORKSPACE_NAMESPACE} exists..."
kubectl create namespace "${WORKSPACE_NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

# AWS CLI test credentials
export AWS_ACCESS_KEY_ID="000000000000"
export AWS_SECRET_ACCESS_KEY="test"
export AWS_DEFAULT_REGION="us-east-1"

# -----------------------------------------------------------------------------
# STARTER 1: s3-bucket
# -----------------------------------------------------------------------------
echo ""
echo "========================================================================"
echo "==> [Starter 1/4: s3-bucket] Reconciling S3 Bucket into floci..."
echo "========================================================================"
mkdir -p "${OUT_DIR}/s3"
./bin/cf gen internal/examples/s3-bucket.cf.yaml --out "${OUT_DIR}/s3" --group-suffix="${WORKSPACE_GROUP_SUFFIX}"

kubectl apply -f "${OUT_DIR}/s3/xrds/"
kubectl wait --for=condition=Established "xrd/xbuckets.storage.sparky.ee.${WORKSPACE_GROUP_SUFFIX}" --timeout=60s
kubectl apply -f "${OUT_DIR}/s3/compositions/"
kubectl apply -f "${OUT_DIR}/s3/functions.yaml"

S3_XR_MANIFEST="${OUT_DIR}/s3/xr-instance.yaml"
cat <<EOF > "${S3_XR_MANIFEST}"
apiVersion: storage.sparky.ee.${WORKSPACE_GROUP_SUFFIX}/v1alpha1
kind: XBucket
metadata:
  name: test-s3-bucket
  namespace: ${WORKSPACE_NAMESPACE}
spec:
  providerName: default
  bucketName: test-s3-bucket
  region: us-east-1
  versioning: false
  blockPublicAccess: false
EOF

echo "==> Applying XBucket instance..."
kubectl apply -f "${S3_XR_MANIFEST}"

echo "==> Waiting for composed Bucket to appear..."
BUCKET_NAME=""
for i in {1..60}; do
  BUCKET_NAME=$(kubectl get bucket.s3.aws.m.upbound.io -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || \
                kubectl get bucket.s3.aws.upbound.io -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || \
                kubectl get bucket -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
  if [ -n "${BUCKET_NAME}" ]; then
    break
  fi
  sleep 2
done

if [ -z "${BUCKET_NAME}" ]; then
  echo "ERROR: Composed Bucket did not appear in namespace ${WORKSPACE_NAMESPACE}" >&2
  exit 1
fi

echo "==> Waiting for Bucket/${BUCKET_NAME} to report condition=Ready..."
kubectl wait --for=condition=Ready "bucket/${BUCKET_NAME}" -n "${WORKSPACE_NAMESPACE}" --timeout=180s

EXTERNAL_BUCKET=$(kubectl get bucket "${BUCKET_NAME}" -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.metadata.annotations.crossplane\.io/external-name}' 2>/dev/null || true)
if [ -z "${EXTERNAL_BUCKET}" ]; then
  EXTERNAL_BUCKET="${BUCKET_NAME}"
fi

echo "==> Verifying S3 bucket '${EXTERNAL_BUCKET}' physically exists in floci via AWS CLI..."
BUCKET_LIST=$(aws --endpoint-url http://127.0.0.1:4566 s3 ls 2>/dev/null || true)
echo "    AWS CLI s3 ls output:"
echo "${BUCKET_LIST}"

if ! echo "${BUCKET_LIST}" | grep -q "${EXTERNAL_BUCKET}"; then
  echo "ERROR: S3 bucket ${EXTERNAL_BUCKET} was not found in floci s3 ls output!" >&2
  exit 1
fi
echo "    S3 bucket ${EXTERNAL_BUCKET} confirmed present in floci."

echo "==> Teardown s3-bucket: deleting XR..."
kubectl delete -f "${S3_XR_MANIFEST}" --timeout=60s || true
for i in {1..30}; do
  if ! kubectl get bucket -n "${WORKSPACE_NAMESPACE}" 2>/dev/null | grep -q "test-s3-bucket"; then
    echo "    Managed Bucket cleanly deleted."
    break
  fi
  sleep 2
done

# Clean up any leftover finalizers to prevent hanging
for b in $(kubectl get bucket -n "${WORKSPACE_NAMESPACE}" -o name 2>/dev/null || true); do
  kubectl patch "$b" -n "${WORKSPACE_NAMESPACE}" -p '{"metadata":{"finalizers":[]}}' --type=merge 2>/dev/null || true
done

kubectl delete -f "${OUT_DIR}/s3/compositions/" --timeout=30s || true
kubectl delete -f "${OUT_DIR}/s3/xrds/" --timeout=30s || true
echo "    s3-bucket verified and cleanly torn down."

# -----------------------------------------------------------------------------
# STARTER 2: sqs-queue
# -----------------------------------------------------------------------------
echo ""
echo "========================================================================"
echo "==> [Starter 2/4: sqs-queue] Reconciling SQS Queue into floci..."
echo "========================================================================"
mkdir -p "${OUT_DIR}/sqs"
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

./bin/cf gen "${OUT_DIR}/sqs-blueprint.yaml" --out "${OUT_DIR}/sqs" --group-suffix="${WORKSPACE_GROUP_SUFFIX}"

kubectl apply -f "${OUT_DIR}/sqs/xrds/"
kubectl wait --for=condition=Established "xrd/xqueues.messaging.sparky.ee.${WORKSPACE_GROUP_SUFFIX}" --timeout=60s
kubectl apply -f "${OUT_DIR}/sqs/compositions/"
kubectl apply -f "${OUT_DIR}/sqs/functions.yaml"

SQS_XR_MANIFEST="${OUT_DIR}/sqs/xr-instance.yaml"
cat <<EOF > "${SQS_XR_MANIFEST}"
apiVersion: messaging.sparky.ee.${WORKSPACE_GROUP_SUFFIX}/v1alpha1
kind: XQueue
metadata:
  name: test-queue
  namespace: ${WORKSPACE_NAMESPACE}
spec:
  providerName: default
  region: us-east-1
EOF

echo "==> Applying XQueue instance..."
kubectl apply -f "${SQS_XR_MANIFEST}"

echo "==> Waiting for composed Queue to appear..."
SQS_QUEUE_NAME=""
for i in {1..60}; do
  SQS_QUEUE_NAME=$(kubectl get queue.sqs.aws.m.upbound.io -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || \
                   kubectl get queue.sqs.aws.upbound.io -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || \
                   kubectl get queue -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
  if [ -n "${SQS_QUEUE_NAME}" ]; then
    break
  fi
  sleep 2
done

if [ -z "${SQS_QUEUE_NAME}" ]; then
  echo "ERROR: Composed Queue did not appear in namespace ${WORKSPACE_NAMESPACE}" >&2
  exit 1
fi

echo "==> Waiting for Queue/${SQS_QUEUE_NAME} to report condition=Ready..."
kubectl wait --for=condition=Ready "queue/${SQS_QUEUE_NAME}" -n "${WORKSPACE_NAMESPACE}" --timeout=180s

EXTERNAL_URL=$(kubectl get queue "${SQS_QUEUE_NAME}" -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.status.atProvider.url}' 2>/dev/null || true)
EXTERNAL_NAME=$(kubectl get queue "${SQS_QUEUE_NAME}" -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.status.atProvider.name}' 2>/dev/null || true)
if [ -z "$EXTERNAL_NAME" ] && [ -n "$EXTERNAL_URL" ]; then
  EXTERNAL_NAME="${EXTERNAL_URL##*/}"
fi
if [ -z "$EXTERNAL_NAME" ]; then
  EXTERNAL_NAME=$(kubectl get queue "${SQS_QUEUE_NAME}" -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.metadata.annotations.crossplane\.io/external-name}' 2>/dev/null || true)
  EXTERNAL_NAME="${EXTERNAL_NAME##*/}"
fi

echo "==> Verifying SQS queue '${EXTERNAL_NAME}' physically exists in floci via AWS CLI..."
AWS_GET_URL=$(aws --endpoint-url http://127.0.0.1:4566 sqs get-queue-url --queue-name "${EXTERNAL_NAME}" --output text --query 'QueueUrl' 2>/dev/null || true)
echo "    AWS CLI get-queue-url output: ${AWS_GET_URL}"
if [ -z "${AWS_GET_URL}" ]; then
  echo "ERROR: Queue ${EXTERNAL_NAME} was not verified via get-queue-url!" >&2
  exit 1
fi

SECRET_NAME="test-queue-queue-secret"
echo "    Waiting for secret ${SECRET_NAME} to contain populated status wire data..."
SECRET_POPULATED=false
for s in {1..30}; do
  SECRET_URL_B64=$(kubectl get secret "${SECRET_NAME}" -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.data.queueUrl}' 2>/dev/null || true)
  if [ -n "${SECRET_URL_B64}" ]; then
    SECRET_POPULATED=true
    break
  fi
  sleep 2
done

if [ "${SECRET_POPULATED}" = false ]; then
  echo "ERROR: Secret ${SECRET_NAME} was not populated with status wires!" >&2
  exit 1
fi

echo "==> Teardown sqs-queue: deleting XR..."
kubectl delete -f "${SQS_XR_MANIFEST}" --timeout=60s || true
kubectl delete secret "${SECRET_NAME}" -n "${WORKSPACE_NAMESPACE}" --timeout=15s 2>/dev/null || true
for i in {1..30}; do
  if ! kubectl get queue -n "${WORKSPACE_NAMESPACE}" 2>/dev/null | grep -q "test-queue"; then
    echo "    Managed Queue cleanly deleted."
    break
  fi
  sleep 2
done

for q in $(kubectl get queue -n "${WORKSPACE_NAMESPACE}" -o name 2>/dev/null || true); do
  kubectl patch "$q" -n "${WORKSPACE_NAMESPACE}" -p '{"metadata":{"finalizers":[]}}' --type=merge 2>/dev/null || true
done

kubectl delete -f "${OUT_DIR}/sqs/compositions/" --timeout=30s || true
kubectl delete -f "${OUT_DIR}/sqs/xrds/" --timeout=30s || true
echo "    sqs-queue verified and cleanly torn down."

# -----------------------------------------------------------------------------
# STARTER 3: irsa
# -----------------------------------------------------------------------------
echo ""
echo "========================================================================"
echo "==> [Starter 3/4: irsa] Reconciling IAM Role and ServiceAccount..."
echo "========================================================================"
mkdir -p "${OUT_DIR}/irsa"
./bin/cf gen internal/examples/irsa.cf.yaml --out "${OUT_DIR}/irsa" --group-suffix="${WORKSPACE_GROUP_SUFFIX}"

kubectl apply -f "${OUT_DIR}/irsa/xrds/"
kubectl wait --for=condition=Established "xrd/xirsas.platform.sparky.ee.${WORKSPACE_GROUP_SUFFIX}" --timeout=60s
kubectl apply -f "${OUT_DIR}/irsa/compositions/"
kubectl apply -f "${OUT_DIR}/irsa/functions.yaml"

IRSA_XR_MANIFEST="${OUT_DIR}/irsa/xr-instance.yaml"
cat <<EOF > "${IRSA_XR_MANIFEST}"
apiVersion: platform.sparky.ee.${WORKSPACE_GROUP_SUFFIX}/v1alpha1
kind: XIrsa
metadata:
  name: test-irsa
  namespace: ${WORKSPACE_NAMESPACE}
spec:
  providerName: default
  oidcProviderArn: "arn:aws:iam::000000000000:oidc-provider/oidc.eks.us-east-1.amazonaws.com/id/TEST"
  serviceAccountName: "test-sa"
  team: "platform"
EOF

echo "==> Applying XIrsa instance..."
kubectl apply -f "${IRSA_XR_MANIFEST}"

echo "==> Waiting for composed IAM Role to appear..."
IAM_ROLE_NAME=""
for i in {1..60}; do
  IAM_ROLE_NAME=$(kubectl get role.iam.aws.m.upbound.io -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || \
                  kubectl get role.iam.aws.upbound.io -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || \
                  kubectl get role.iam -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
  if [ -n "${IAM_ROLE_NAME}" ]; then
    break
  fi
  sleep 2
done

if [ -z "${IAM_ROLE_NAME}" ]; then
  echo "ERROR: Composed IAM Role did not appear in namespace ${WORKSPACE_NAMESPACE}" >&2
  exit 1
fi

echo "==> Waiting for Role/${IAM_ROLE_NAME} to report condition=Ready..."
kubectl wait --for=condition=Ready "role.iam.aws.m.upbound.io/${IAM_ROLE_NAME}" -n "${WORKSPACE_NAMESPACE}" --timeout=180s 2>/dev/null || \
kubectl wait --for=condition=Ready "role.iam.aws.upbound.io/${IAM_ROLE_NAME}" -n "${WORKSPACE_NAMESPACE}" --timeout=180s

echo "==> Waiting for ServiceAccount test-irsa-sa with role-arn annotation..."
SA_ROLE_ARN=""
for i in {1..30}; do
  SA_ROLE_ARN=$(kubectl get sa test-irsa-sa -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.metadata.annotations.eks\.amazonaws\.com/role-arn}' 2>/dev/null || true)
  if [ -n "${SA_ROLE_ARN}" ]; then
    break
  fi
  sleep 2
done

if [ -z "${SA_ROLE_ARN}" ]; then
  echo "ERROR: ServiceAccount test-irsa-sa was not annotated with role-arn!" >&2
  exit 1
fi
echo "    ServiceAccount annotated with role-arn: ${SA_ROLE_ARN}"

EXTERNAL_ROLE=$(kubectl get role.iam.aws.m.upbound.io "${IAM_ROLE_NAME}" -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.metadata.annotations.crossplane\.io/external-name}' 2>/dev/null || \
                kubectl get role.iam.aws.upbound.io "${IAM_ROLE_NAME}" -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.metadata.annotations.crossplane\.io/external-name}' 2>/dev/null || true)
if [ -z "${EXTERNAL_ROLE}" ]; then
  EXTERNAL_ROLE="${IAM_ROLE_NAME}"
fi

echo "==> Verifying IAM role '${EXTERNAL_ROLE}' physically exists in floci via AWS CLI..."
IAM_GET_ROLE=$(aws --endpoint-url http://127.0.0.1:4566 iam get-role --role-name "${EXTERNAL_ROLE}" 2>/dev/null || true)
echo "    AWS CLI iam get-role output for ${EXTERNAL_ROLE}:"
echo "${IAM_GET_ROLE}"

if [ -z "${IAM_GET_ROLE}" ]; then
  echo "ERROR: IAM Role ${EXTERNAL_ROLE} was not verified via iam get-role!" >&2
  exit 1
fi

CLI_ARN=$(echo "${IAM_GET_ROLE}" | grep -o '"Arn": "[^"]*"' | cut -d'"' -f4)
if [ "${CLI_ARN}" != "${SA_ROLE_ARN}" ]; then
  echo "ERROR: SA role-arn '${SA_ROLE_ARN}' does not match AWS CLI role ARN '${CLI_ARN}'!" >&2
  exit 1
fi
echo "    IAM Role and ServiceAccount annotation match verified."

echo "==> Teardown irsa: deleting XR..."
kubectl delete -f "${IRSA_XR_MANIFEST}" --timeout=60s || true
for r in $(kubectl get role.iam -n "${WORKSPACE_NAMESPACE}" -o name 2>/dev/null || true); do
  kubectl patch "$r" -n "${WORKSPACE_NAMESPACE}" -p '{"metadata":{"finalizers":[]}}' --type=merge 2>/dev/null || true
done
for r in $(kubectl get rolepolicy.iam -n "${WORKSPACE_NAMESPACE}" -o name 2>/dev/null || true); do
  kubectl patch "$r" -n "${WORKSPACE_NAMESPACE}" -p '{"metadata":{"finalizers":[]}}' --type=merge 2>/dev/null || true
done

kubectl delete -f "${OUT_DIR}/irsa/compositions/" --timeout=30s || true
kubectl delete -f "${OUT_DIR}/irsa/xrds/" --timeout=30s || true
echo "    irsa verified and cleanly torn down."

# -----------------------------------------------------------------------------
# STARTER 4: rds-postgres
# -----------------------------------------------------------------------------
echo ""
echo "========================================================================"
echo "==> [Starter 4/4: rds-postgres] Reconciling DBInstance & in-cluster psql..."
echo "========================================================================"
mkdir -p "${OUT_DIR}/rds"

# Create master password secret in workspace namespace
kubectl create secret generic rds-master-pass \
  --namespace "${WORKSPACE_NAMESPACE}" \
  --from-literal=password="flocipassword123" \
  --dry-run=client -o yaml | kubectl apply -f -

cat <<EOF > "${OUT_DIR}/rds-blueprint.yaml"
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xpostgres
spec:
  sources:
    - provider: ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0
  xrd:
    group: database.sparky.ee
    kind: XPostgresInstance
    plural: xpostgresinstances
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
        description: AWS region where the database is provisioned.
      dbName:
        type: string
        required: true
        description: PostgreSQL database name.
      instanceClass:
        type: string
        default: db.t4g.micro
        description: RDS DB compute and memory capacity class.
      allocatedStorage:
        type: integer
        default: "20"
        description: Storage amount to allocate in gigabytes (GB).
  resources:
    - name: db-instance
      kind: Instance
      provider: ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0
      envelope:
        writeConnectionSecretToRef.name: {from: params.dbName}
        managementPolicies: {value: "Observe, Create, Update, Delete, LateInitialize"}
      fields:
        region: {from: params.region}
        dbName: {from: params.dbName}
        instanceClass: {from: params.instanceClass}
        allocatedStorage: {from: params.allocatedStorage}
        engine: {value: "postgres"}
        username: {value: "postgres"}
        passwordSecretRef.name: {value: "rds-master-pass"}
        passwordSecretRef.key: {value: "password"}
        skipFinalSnapshot: {value: true}
EOF

./bin/cf gen "${OUT_DIR}/rds-blueprint.yaml" --out "${OUT_DIR}/rds" --group-suffix="${WORKSPACE_GROUP_SUFFIX}"

kubectl apply -f "${OUT_DIR}/rds/xrds/"
kubectl wait --for=condition=Established "xrd/xpostgresinstances.database.sparky.ee.${WORKSPACE_GROUP_SUFFIX}" --timeout=60s
kubectl apply -f "${OUT_DIR}/rds/compositions/"
kubectl apply -f "${OUT_DIR}/rds/functions.yaml"

RDS_XR_MANIFEST="${OUT_DIR}/rds/xr-instance.yaml"
cat <<EOF > "${RDS_XR_MANIFEST}"
apiVersion: database.sparky.ee.${WORKSPACE_GROUP_SUFFIX}/v1alpha1
kind: XPostgresInstance
metadata:
  name: test-rds
  namespace: ${WORKSPACE_NAMESPACE}
spec:
  providerName: default
  dbName: testdb
  region: us-east-1
EOF

echo "==> Applying XPostgresInstance instance..."
kubectl apply -f "${RDS_XR_MANIFEST}"

echo "==> Waiting for composed DBInstance to appear..."
RDS_INST_NAME=""
for i in {1..60}; do
  RDS_INST_NAME=$(kubectl get instance.rds.aws.m.upbound.io -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || \
                  kubectl get instance.rds.aws.upbound.io -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || \
                  kubectl get instance.rds -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
  if [ -n "${RDS_INST_NAME}" ]; then
    break
  fi
  sleep 2
done

if [ -z "${RDS_INST_NAME}" ]; then
  echo "ERROR: Composed DBInstance did not appear in namespace ${WORKSPACE_NAMESPACE}" >&2
  exit 1
fi

echo "==> Waiting for DBInstance/${RDS_INST_NAME} to report condition=Ready..."
kubectl wait --for=condition=Ready "instance.rds.aws.m.upbound.io/${RDS_INST_NAME}" -n "${WORKSPACE_NAMESPACE}" --timeout=300s 2>/dev/null || \
kubectl wait --for=condition=Ready "instance.rds.aws.upbound.io/${RDS_INST_NAME}" -n "${WORKSPACE_NAMESPACE}" --timeout=300s

echo "==> Verifying writeConnectionSecretToRef holds endpoint accepted by in-cluster psql..."
CONN_SECRET="testdb"
ENDPOINT=""
PORT=""
DB_USER=""
DB_PASS=""

for s in {1..30}; do
  HOST_B64=$(kubectl get secret "${CONN_SECRET}" -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.data.host}' 2>/dev/null || true)
  PORT_B64=$(kubectl get secret "${CONN_SECRET}" -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.data.port}' 2>/dev/null || true)
  USER_B64=$(kubectl get secret "${CONN_SECRET}" -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.data.username}' 2>/dev/null || true)
  PASS_B64=$(kubectl get secret "${CONN_SECRET}" -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.data.password}' 2>/dev/null || true)

  if [ -n "${HOST_B64}" ] && [ -n "${PORT_B64}" ]; then
    ENDPOINT=$(echo "${HOST_B64}" | base64 -d)
    PORT=$(echo "${PORT_B64}" | base64 -d)
    DB_USER=$(echo "${USER_B64}" | base64 -d)
    DB_PASS=$(echo "${PASS_B64}" | base64 -d)
    break
  fi
  sleep 2
done

if [ -z "${ENDPOINT}" ]; then
  echo "ERROR: Connection secret ${CONN_SECRET} was not populated with endpoint data!" >&2
  kubectl get secret "${CONN_SECRET}" -n "${WORKSPACE_NAMESPACE}" -o yaml >&2 || true
  exit 1
fi

echo "    Connection secret data:"
echo "      Host:     ${ENDPOINT}"
echo "      Port:     ${PORT}"
echo "      Username: ${DB_USER}"

echo "==> Running in-cluster psql query (SELECT 1;) against ${ENDPOINT}:${PORT}..."
kubectl delete pod psql-test -n "${WORKSPACE_NAMESPACE}" --ignore-not-found=true
kubectl run psql-test -n "${WORKSPACE_NAMESPACE}" \
  --restart=Never \
  --image=postgres:16-alpine \
  --command -- sh -c "PGPASSWORD=\"${DB_PASS}\" psql -h \"${ENDPOINT}\" -p \"${PORT}\" -U \"${DB_USER}\" -d testdb -c \"SELECT 1;\""

PSQL_SUCCESS=false
for i in {1..30}; do
  POD_PHASE=$(kubectl get pod psql-test -n "${WORKSPACE_NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || true)
  if [ "$POD_PHASE" = "Succeeded" ]; then
    PSQL_SUCCESS=true
    break
  elif [ "$POD_PHASE" = "Failed" ]; then
    echo "ERROR: psql test pod failed:" >&2
    kubectl logs psql-test -n "${WORKSPACE_NAMESPACE}" >&2 || true
    exit 1
  fi
  sleep 1
done

if [ "$PSQL_SUCCESS" = false ]; then
  echo "ERROR: psql test pod timed out" >&2
  kubectl logs psql-test -n "${WORKSPACE_NAMESPACE}" >&2 || true
  exit 1
fi

PSQL_OUTPUT=$(kubectl logs psql-test -n "${WORKSPACE_NAMESPACE}")
echo "    In-cluster psql output:"
echo "${PSQL_OUTPUT}"
kubectl delete pod psql-test -n "${WORKSPACE_NAMESPACE}" --ignore-not-found=true

if ! echo "${PSQL_OUTPUT}" | grep -q "1"; then
  echo "ERROR: psql output did not confirm expected SELECT 1 result!" >&2
  exit 1
fi
echo "    In-cluster psql connection accepted and verified successfully."

echo "==> Teardown rds-postgres: deleting XR..."
kubectl delete -f "${RDS_XR_MANIFEST}" --timeout=60s || true
kubectl delete secret "${CONN_SECRET}" rds-master-pass -n "${WORKSPACE_NAMESPACE}" --timeout=15s 2>/dev/null || true

for i in {1..30}; do
  if ! kubectl get instance.rds.aws.m.upbound.io -n "${WORKSPACE_NAMESPACE}" 2>/dev/null | grep -q "test-rds"; then
    echo "    Managed DBInstance cleanly deleted."
    break
  fi
  sleep 2
done

for r in $(kubectl get instance.rds.aws.m.upbound.io -n "${WORKSPACE_NAMESPACE}" -o name 2>/dev/null || true); do
  kubectl patch "$r" -n "${WORKSPACE_NAMESPACE}" -p '{"metadata":{"finalizers":[]}}' --type=merge 2>/dev/null || true
done

kubectl delete -f "${OUT_DIR}/rds/compositions/" --timeout=30s || true
kubectl delete -f "${OUT_DIR}/rds/xrds/" --timeout=30s || true
echo "    rds-postgres verified and cleanly torn down."

echo ""
echo "========================================================================"
echo "==> Lane D floci test passed successfully for all 4 AWS starters!"
echo "========================================================================"
