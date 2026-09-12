# CF-375 — matchesProvider fails on digest-pinned provider refs (@sha256) breaking Composition emit

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 |
| **Closes** | `CF-375 — matchesProvider fails on digest-pinned provider refs (@sha256) breaking Composition emit` (#266) |
| **Worktree** | `.worktrees/CF-375` on branch `CF-375-matches-provider-digest-refs`, branched from `main` |
| **May write** | `internal/emit/composition.go`, `internal/adopt/xrdless.go`, `internal/emit/composition_test.go` |
| **Merges after** | nothing |

## Defect Summary

When a provider is pinned by digest in a Blueprint's `spec.sources` and resource `provider` field:
`provider: xpkg.upbound.io/upbound/provider-aws-sqs@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef`

In both `internal/emit/composition.go:matchesProvider` and `internal/adopt/xrdless.go:matchesProvider`:
```go
func matchesProvider(provider, pkgRef string) bool {
    p := provider
    if idx := strings.LastIndex(p, "/"); idx != -1 {
        p = p[idx+1:]
    }
    if idx := strings.Index(p, ":"); idx != -1 {
        p = p[:idx]
    }
    p = strings.TrimPrefix(p, "provider-")
...
```
Because the digest ref uses `@sha256:digest`, `strings.Index(p, ":")` splits on the colon inside `@sha256:`, leaving `provider-aws-sqs@sha256` as `p`. After `strings.TrimPrefix(p, "provider-")`, `p` becomes `aws-sqs@sha256`. It fails to match `aws-sqs` from `pkgRef`.

Consequently, `emit.Composition(bp, crds)` fails with:
`resource "main-queue": kind "Queue" not found in provider "xpkg.upbound.io/upbound/provider-aws-sqs@sha256:..."`

## Acceptance Test

In `internal/emit/composition_test.go`:

```go
func TestCompositionWithDigestPinnedProvider(t *testing.T) {
	bp := testBlueprint()
	digestRef := "xpkg.upbound.io/upbound/provider-aws-sqs@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	bp.Spec.Sources = []blueprint.Source{
		{Provider: digestRef},
	}
	bp.Spec.Resources[0].Provider = digestRef

	if err := bp.Validate(); err != nil {
		t.Fatalf("bp.Validate error: %v", err)
	}

	crds := testfixture.QueueBothCRDs(t)

	_, err := Composition(bp, crds)
	if err != nil {
		t.Fatalf("Composition failed with digest provider: %v", err)
	}
}
```

## Contract

In both `internal/emit/composition.go:matchesProvider` and `internal/adopt/xrdless.go:matchesProvider`, strip `@...` before stripping `:...`:

```go
	p := provider
	if idx := strings.LastIndex(p, "/"); idx != -1 {
		p = p[idx+1:]
	}
	if idx := strings.Index(p, "@"); idx != -1 {
		p = p[:idx]
	}
	if idx := strings.Index(p, ":"); idx != -1 {
		p = p[:idx]
	}
	p = strings.TrimPrefix(p, "provider-")
```

Also ensure the same handling is applied to `pkgRef` in `matchesProvider` if it can contain `@`.

## Verification

```sh
make lint
make lint-strict
make test-race
make test-docker
go test ./internal/emit -run TestCompositionWithDigestPinnedProvider -v
```
