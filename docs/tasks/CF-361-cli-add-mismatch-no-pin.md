# CF-361 — cf provider add and cf function add fail on package type mismatch but still pin the lockfile

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 |
| **Closes** | `CF-361 — cf provider add and cf function add fail on package type mismatch but still pin the lockfile` (#253) |
| **Worktree** | `.worktrees/CF-361` on branch `CF-361-cli-add-mismatch-no-pin`, branched from `main` |
| **May write** | `cmd/cf/provider.go`, `cmd/cf/provider_test.go`, `cmd/cf/function.go`, `cmd/cf/function_test.go` |
| **Merges after** | nothing |

## Defect Summary

When `cf provider add` is invoked with a package that defines only function inputs and zero managed resources, or `cf function add` is invoked with a provider package defining managed resources, both commands correctly detect the mismatch and fail with exit code 1 (`"package ... is a function package, not a provider (use 'cf function add ...')"` and `"package ... is a provider package, not a function (use 'cf provider add ...')"`).

However, in `cmd/cf/provider.go:35`:
```go
pkg, crds, err := store.FetchAndSave(context.Background(), c.Lock, c.Ref, c.fetch)
```
and in `cmd/cf/function.go:30`:
```go
pkg, crds, err := store.FetchAndSave(context.Background(), c.Lock, c.Ref, c.fetch)
```
both commands pass `c.Lock` directly into `store.FetchAndSave`. `store.FetchAndSave` unconditionally pins the package into the lockfile on disk via `PinLock` before returning `crds` to the caller.

Only after `FetchAndSave` returns do `ProviderAddCmd.Run` and `FunctionAddCmd.Run` inspect the CRDs and return an error. By that time, `.cf.lock` has already been created/updated with a pin for the rejected package.

A CLI command exiting with an error status due to invalid input must not mutate project state or leave uncommitted lockfile changes behind. Both commands should pass `""` as `lockPath` to `FetchAndSave`, validate the package CRDs, and only call `store.PinLock(c.Lock, pkg.Ref, pkg.Digest)` once validation passes.

## Reproduction & Acceptance Tests

In `cmd/cf/provider_test.go` and `cmd/cf/function_test.go`:

```go
func TestProviderAddRejectsFunctionPackageWithoutMutatingLockfile(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".cf.lock")
	fnFetch := func(ref string) (*xpkg.Package, error) {
		return &xpkg.Package{
			Ref:    ref,
			Digest: "sha256:fn123",
			Docs: [][]byte{[]byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: autoreadies.autoready.fn.crossplane.io}
spec:
  group: autoready.fn.crossplane.io
  scope: Namespaced
  names: {kind: AutoReady, plural: autoreadies}
  versions:
  - {name: v1alpha1, served: true, storage: true}
`)},
		}, nil
	}
	cmd := &ProviderAddCmd{
		Ref:      "xpkg.crossplane.io/crossplane-contrib/function-auto-ready:v0.5.0",
		CacheDir: filepath.Join(dir, "cache"),
		Lock:     lockPath,
		fetch:    fnFetch,
	}
	var out bytes.Buffer
	err := cmd.Run(&out)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if _, statErr := os.Stat(lockPath); statErr == nil {
		t.Fatalf("lockfile %s was written despite command failure", lockPath)
	}
}
```

And symmetrically for `cf function add` in `cmd/cf/function_test.go`:
```go
func TestFunctionAddRejectsProviderPackageWithoutMutatingLockfile(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".cf.lock")
	provFetch := func(ref string) (*xpkg.Package, error) {
		return &xpkg.Package{
			Ref:    ref,
			Digest: "sha256:prov123",
			Docs: [][]byte{[]byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: queues.sqs.aws.upbound.io}
spec:
  group: sqs.aws.upbound.io
  scope: Cluster
  names: {kind: Queue, plural: queues}
  versions:
  - {name: v1beta1, served: true, storage: true}
`)},
		}, nil
	}
	cmd := &FunctionAddCmd{
		Ref:      "xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0",
		CacheDir: filepath.Join(dir, "cache"),
		Lock:     lockPath,
		fetch:    provFetch,
	}
	var out bytes.Buffer
	err := cmd.Run(&out)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if _, statErr := os.Stat(lockPath); statErr == nil {
		t.Fatalf("lockfile %s was written despite command failure", lockPath)
	}
}
```

## Contract

1. In `cmd/cf/provider.go:35`, pass `""` as lockPath to `store.FetchAndSave`. After validating that the package is not a function package (has managed CRDs), call `store.PinLock(c.Lock, pkg.Ref, pkg.Digest)`.
2. In `cmd/cf/function.go:30`, pass `""` as lockPath to `store.FetchAndSave`. After validating that the package is not a provider package (has no managed CRDs), call `store.PinLock(c.Lock, pkg.Ref, pkg.Digest)`.
3. If validation fails in either command, `.cf.lock` must not be created or modified.

## Verification

```sh
make lint
make lint-strict
make test-race
go test ./cmd/cf -v
```
