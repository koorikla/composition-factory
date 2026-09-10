# CF-107 — Omitting a CRD-required field (`region` on every `Bucket`) passes `cf gen`, `PUT /api/blueprint` and `POST /api/generate`

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 |
| **Closes** | `#5` — `CF-107 — Omitting a CRD-required field (`region` on every `Bucket`) passes `cf gen`, `PUT /api/blueprint` and `POST /api/generate` with exit 0 and no warning; only `--validate` (a real render) catches it.` |
| **Worktree** | `.worktrees/CF-107` on branch `CF-107-check-required-fields` |
| **May write** | `internal/emit/plan.go`, `internal/emit/plan_test.go`, `internal/emit/emit.go`, `cmd/cf/gen.go`, `cmd/cf/gen_test.go`, `internal/api/blueprint.go`, `internal/api/blueprint_test.go`, `internal/api/server.go` |
| **Merges after** | `nothing` |

## Symptom

Omitting a CRD-required field (`region` on every `Bucket`) passes `cf gen`, `PUT /api/blueprint` and `POST /api/generate` with exit 0 and no warning; only `--validate` (a real render) catches it. The API server rejects the composed resource at apply time. README:10 claims strict validation at generate time; generation must refuse, or at least warn, when an effective-required field has no value, wire or guard.

## Evidence

Repro on `f45c2a8` and `64120e5`, twice:
```sh
$ grep -v '^        region: {from: params.region}$' internal/examples/s3-bucket.cf.yaml > noregion.cf.yaml   # drops region from all three Buckets
$ ./bin/cf gen noregion.cf.yaml -o out          # freshly built HEAD
wrote out/compositions/xbuckets.storage.sparky.ee.yaml
wrote out/functions.yaml
wrote out/providerconfigs/aws.yaml
wrote out/xrds/xbuckets.storage.sparky.ee.yaml
exit=0                                          # twice
$ grep -c region out/compositions/*.yaml
0
$ ./bin/cf fields Bucket --required | grep region
region  string  true  Region where this resource will be managed…
```

`PUT /api/blueprint` with the same document answers 200 and `POST /api/generate {"write":false}` answers 200 with outputs and no warning.

## Location

In a previous commit (`ff1b551`, later reverted in `ceaafcb`), `CheckRequiredFields` was added to `internal/emit/plan.go` using `schema.RequiredLeaves`. However, on real Upjet CRDs (such as `provider-aws-s3:v2.7.0`), `schema.RequiredLeaves` does not match the effective required fields computed by `internal/index/fields.go:filterRequiredOnly` (the `requiredChain`).

Examine `internal/index/fields.go` to see how `filterRequiredOnly` and `requiredChain` find required fields on real CRDs, and ensure `CheckRequiredFields` uses effective required fields so that real provider CRDs like `Bucket` correctly detect `region` as missing when not wired or set.

## Acceptance test

Write this test **first**, verbatim, and watch it fail before changing production code:

```go
// cmd/cf/gen_test.go or internal/emit/plan_test.go
func TestCheckRequiredFieldsOnRealCRD(t *testing.T) {
	// Load s3-bucket starter YAML, strip the region wire:
	// "region: {from: params.region}"
	// Attempt emit.Generate or cf gen.
	// Must fail with an error stating that required field "spec.forProvider.region" (or "region") on resource "Bucket" is missing.
}
```

**Fails today with:**
```
Generation succeeds with exit 0 and zero warnings when region is omitted from s3-bucket starter.
```

## Contract

1. `emit.Generate` (and by extension `cf gen` and `POST /api/generate`) must validate that all effective required fields for each resource's CRD (e.g. `spec.forProvider.region` for AWS `Bucket`) have either:
   - A literal value, OR
   - A wire / parameter mapping (`from: params.X`), OR
   - An expression / template mapping, OR
   - An active guard / condition.
2. If any required field is missing, generation must return an error identifying the resource and the missing required field.
3. Existing valid blueprints (including all starter examples in `internal/examples/*.cf.yaml`) must continue to generate valid byte-identical output.

## Verification

```sh
make lint && make test-race
make test-docker
```

## Out of scope

Refactoring schema storage or altering CLI flag structures.

## Handover

Branch `CF-107-check-required-fields`, committed, not pushed, not merged. In your final report: the failing run and the passing run of the acceptance test, both pasted; every gate you ran; every judgement call you made where the brief was silent.
