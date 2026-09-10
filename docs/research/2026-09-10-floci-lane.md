# Lane D: real AWS objects against floci (2026-09-10)

Facts gathered before filing CF-131–CF-134. Nothing here was run yet; each item's
acceptance run is the first execution.

## What floci is

- Local AWS emulator (LocalStack-style), MIT, https://github.com/floci-io/floci, docs
  https://floci.io/floci/. Image `floci/floci:<x.y.z>` (pin a tag; `latest` in CI is a
  determinism hole). One port, `4566`. Default region `us-east-1`, account `000000000000`.
- Any non-empty `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY` is accepted; IAM and STS are
  emulated. A 12-digit access key id selects an isolated account — usable per workspace.
- Services relevant to the starters: S3, SQS, SNS, IAM, KMS, Secrets Manager, RDS
  (PostgreSQL/MySQL/MariaDB via real backend containers — needs the Docker socket mounted),
  ElastiCache, DynamoDB, Lambda. 150+ in total.
- Storage `FLOCI_STORAGE_MODE=memory|persistent|hybrid|wal`; memory is right for a test lane.
- No documented health endpoint of its own; LocalStack compatibility mode serves
  `/_localstack/health`. Startup is milliseconds.
- S3 path-style and virtual-hosted both work; from inside a kind node path-style is the
  safe choice (`s3_use_path_style: true`).

## Pointing the crossplane-contrib AWS providers at it

From `provider-upjet-aws` `package/crds/aws.upbound.io_providerconfigs.yaml` (verify the
namespaced family kind under `aws.m.upbound.io/v1beta1` carries the same fields — the
scaffold emitter already resolves the kind per family):

```yaml
spec:
  credentials:
    source: Secret
    secretRef: {namespace: crossplane-system, name: aws-creds, key: creds}   # any non-empty keys
  endpoint:
    url: {type: Static, static: http://floci.<ns>.svc:4566}    # or the kind-network address
    hostnameImmutable: true
    services: []            # empty = all
    signingRegion: us-east-1
  skip_credentials_validation: true
  skip_region_validation: true
  skip_requesting_account_id: true
  skip_metadata_api_check: true
  s3_use_path_style: true
```

Field names are snake_case for the `skip_*`/`s3_use_path_style` flags and camelCase under
`endpoint` — the CRD is inconsistent and the scaffold must copy it exactly.

## Where it plugs into the existing lanes

- `scripts/cluster/cluster.sh` creates kind (`versions.env`: node v1.31.2, Crossplane 2.4.0)
  and installs only the two functions. No provider is installed today; Lane C
  (`scripts/cluster/test-cluster.sh`) applies the k8s-workload starter and waits for a
  Deployment. Lane D adds: a floci container reachable from the kind nodes (simplest:
  `docker run --network kind --name floci …` and address it as `http://floci:4566`; or a
  Deployment+Service inside the cluster from the same image), one provider per starter
  family (`ghcr.io/crossplane-contrib/provider-aws-{s3,sqs,iam,rds}:v2.7.0` plus the family
  provider they pull in), the credentials Secret, and the emulator ProviderConfig.
- Workspace isolation stays as in AGENTS.md §2 (namespace `cf-<slug>`, `--group-suffix`);
  the floci account id (12-digit access key) can carry the same hash for per-workspace
  object isolation inside the emulator.
- The verifying side is the AWS CLI against `--endpoint-url http://127.0.0.1:4566`
  (publish the port on the host) — `aws sqs list-queues`, `aws s3api head-bucket`,
  `aws iam get-role`.

## Why this lane matters

Today status wires are only ever exercised by `crossplane composition render` with a
hand-written observed resource (`--validate`), never by a provider writing
`status.atProvider`. Lane D is the first oracle for: status wires reaching XR status and
Secrets, `writeConnectionSecretToRef` (RDS on floci's real postgres), the cluster-discovery
("crd mode") path against real provider CRDs, and the Round-Trip Rule on server-round-tripped
objects that actually reconciled.

## Open questions for the first implementer

- Does the namespaced (`.m.`) family's ProviderConfig accept `endpoint.url.static`
  identically? Check the installed CRD before writing the scaffold.
- kind node → floci name resolution: `--network kind` gives DNS by container name inside
  Docker, which the kind nodes (also containers) resolve; confirm from a node
  (`docker exec cf-test-control-plane curl -s http://floci:4566/_localstack/health`).
- RDS on floci needs `/var/run/docker.sock` mounted into the floci container; decide
  whether the lane allows that (CI runners do).
