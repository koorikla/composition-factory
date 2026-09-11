# CF-131 — Lane D bring-up: floci on kind cluster, emulator ProviderConfig, and sqs-queue smoke path

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | Milestone / Lane Item (Engine scale: Lane D AWS emulator bring-up) |
| **Closes** | `#17` — `CF-131 — Lane D bring-up: floci on kind cluster, emulator ProviderConfig, and sqs-queue smoke path` |
| **Worktree** | `.worktrees/CF-131` on branch `CF-131-lane-d-bringup` |
| **May write** | `Makefile`, `scripts/cluster/cluster.sh`, `scripts/cluster/test-floci.sh`, `internal/emit/providerconfigs.go`, `internal/emit/providerconfigs_test.go` |
| **Merges after** | nothing |

## Symptom

Today, Crossplane composition status wires, secret emission (`writeConnectionSecretToRef`), and provider reconciliation are validated only by unit tests and `crossplane composition render` using hand-crafted observed mock resources. There is no automated integration lane verifying real AWS managed resources against a local emulator.

## Contract

1. **Floci Container on Kind Network**:
   - Update `scripts/cluster/cluster.sh` (or provide `scripts/cluster/floci.sh`) so `FLOCI=1 make cluster` or a dedicated target starts a pinned `floci/floci:<tag>` container connected to the `kind` network (`--network kind --name floci`), exposing port `4566` on the host (`127.0.0.1:4566`).
   - Floci storage mode set to `FLOCI_STORAGE_MODE=memory`.
2. **Emulator ProviderConfig Scaffold**:
   - In `internal/emit/providerconfigs.go`, support generating an AWS emulator ProviderConfig referencing endpoint `http://floci:4566` with exact field casing:
     - `spec.endpoint.url.type: Static`
     - `spec.endpoint.url.static: http://floci:4566`
     - `spec.endpoint.hostnameImmutable: true`
     - `spec.skip_credentials_validation: true`
     - `spec.skip_region_validation: true`
     - `spec.skip_requesting_account_id: true`
     - `spec.skip_metadata_api_check: true`
     - `spec.s3_use_path_style: true`
3. **Smoke Verification Target (`make test-floci`)**:
   - Automated test script `scripts/cluster/test-floci.sh` that:
     - Installs `provider-aws-sqs:v2.7.0` in the kind cluster.
     - Creates the dummy credentials secret and emulator ProviderConfig.
     - Applies the `sqs-queue` starter XRD, Composition, and XR in workspace namespace `cf-<slug>`.
     - Waits for the managed `Queue` resource to report `Ready=True`.
     - Uses `aws --endpoint-url http://127.0.0.1:4566 sqs list-queues` to confirm the queue physically exists in floci.

## Acceptance test

```sh
make test-floci
```
Passes only when the managed Queue reconciles to `Ready=True` in kind and is listed in `aws --endpoint-url http://127.0.0.1:4566 sqs list-queues`.

## Verification

```sh
make lint && make lint-strict && make test-race
make test-floci
```

## Out of scope

- Status wire round-trip assertions against AWS CLI (belongs to CF-132).
- CRD cluster discovery mode (belongs to CF-133).
- Full AWS starter matrix (S3, IRSA, RDS) (belongs to CF-134).

## Handover

Branch `CF-131-lane-d-bringup`, committed, not pushed, not merged. Final report includes passing run of `make test-floci` and standard lint/unit gates.
