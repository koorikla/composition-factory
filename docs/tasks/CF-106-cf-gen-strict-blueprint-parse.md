# CF-106 — `cf gen` accepts unknown keys under `spec` and exits 0 emitting a Composition with zero composed resources

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P0 (engine scale: emits wrong output silently; typos silently delete resources on next apply) |
| **Closes** | `CF-106 — *(engine)* cf gen accepts an unknown key under spec (resourcez:), emits a Composition with zero composed resources and exits 0; the HTTP and MCP doors reject the same document with unknown field "resourcez". [V]` |
| **Worktree** | `.worktrees/CF-106` on branch `CF-106-cf-gen-strict-blueprint-parse` |
| **May write** | `internal/blueprint/load.go`, `internal/blueprint/load_test.go`, `cmd/cf/gen_test.go` |
| **Merges after** | nothing |

## Symptom

A user makes a typo in their blueprint YAML under `spec`, e.g. `resourcez:` instead of `resources:`.
Running `cf gen` parses the document without error, silently ignores `resourcez:`, emits a Composition
with zero composed resources, and exits 0. Applying this generated Composition to a Kubernetes cluster
silently deletes every composed resource.

Meanwhile, the HTTP and MCP doors (`PUT /api/blueprint`) reject the identical document with:
`json: unknown field "resourcez"`.

All doors must reject unknown blueprint keys consistently and loudly.

## Evidence

Running `cf gen` with a typo key under `spec`:
```sh
$ sed 's/^  resources:$/  resourcez:/' internal/examples/s3-bucket.cf.yaml > /tmp/bad-bp.yaml
$ ./bin/cf gen /tmp/bad-bp.yaml -o /tmp/out
wrote /tmp/out/compositions/xbuckets.storage.sparky.ee.yaml
wrote /tmp/out/functions.yaml
wrote /tmp/out/providerconfigs/aws.yaml
wrote /tmp/out/xrds/xbuckets.storage.sparky.ee.yaml
$ echo $?
0
```

The resulting composition's pipeline contains only `$spec/$xr/$xrMeta` assignments and zero resources.

## Location

- `internal/blueprint/load.go:353-366` — `Parse` unmarshals the JSON bytes with `json.Unmarshal(jsonBytes, &b)`, which silently drops unknown keys.
- Contrast with `internal/api/blueprint.go:793` / `:827` where `dec.DisallowUnknownFields()` is configured on the JSON decoder.

## Acceptance test

Write this test **first**, verbatim in `internal/blueprint/load_test.go`, and watch it fail before changing production code.

```go
func TestCF106RejectUnknownFieldUnderSpec(t *testing.T) {
	badYAML := `apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: test
spec:
  resourcez:
    - name: main-queue
      kind: Queue
`
	_, err := Parse([]byte(badYAML))
	if err == nil {
		t.Fatal("expected error for unknown field 'resourcez', got nil")
	}
	if !strings.Contains(err.Error(), "resourcez") {
		t.Fatalf("expected error mentioning 'resourcez', got: %v", err)
	}
}
```

**Fails today with:**

```
--- FAIL: TestCF106RejectUnknownFieldUnderSpec (0.00s)
    load_test.go:1718: expected error for unknown field 'resourcez', got nil
FAIL
```

## Contract

- `blueprint.Parse` (and by extension `blueprint.Load`) must reject unknown fields at all levels of the Blueprint document, using `DisallowUnknownFields()`.
- The returned error must name the unknown field path (e.g. `unknown field "resourcez"`).
- `cf gen` must refuse documents with unknown keys and exit non-zero.
- Existing valid blueprints and starter examples must continue to parse cleanly.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- UI editor layout fixes (CF-122).
- CLI-specific flag parsing.

## Handover

Branch `CF-106-cf-gen-strict-blueprint-parse`, committed, not pushed, not merged. In your final report:
the failing run and the passing run of the acceptance test, both pasted; every gate
you ran; every judgement call you made where the brief was silent.
