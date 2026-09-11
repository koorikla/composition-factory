# CF-263 — adopt extracts invalid Helm "# Source:" comments as blueprint metadata.name, breaking adoption

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: adopt robustness against standard Helm manifests) |
| **Closes** | `CF-263 — adopt extracts invalid Helm "# Source:" comments as blueprint metadata.name, breaking adoption` |
| **Worktree** | `.worktrees/CF-263` on branch `CF-263-adopt-helm-source-comment` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go` |
| **Merges after** | nothing |

## Symptom

When adopting a Crossplane Composition from a manifest rendered by Helm or containing Helm `# Source: <chart>/templates/<file>.yaml` provenance comments, `cf adopt` extracts the comment header and sets `bp.Metadata.Name` to the path containing slashes. `bp.Validate()` rejects it with:
```
cf: error: adopt composition: validate adopted blueprint: metadata.name: "mychart/templates/composition.yaml" is not a valid DNS subdomain name (must be lowercase alphanumeric characters, '-' or '.', and start and end with an alphanumeric character)
```

## Mechanism

In `internal/adopt/adopt.go:283-294`:
```go
	// 1. Metadata
	var srcCommentRE = regexp.MustCompile(`(?m)^# Source:\s*([^\s]+)`)
	if m := srcCommentRE.FindSubmatch(manifest); len(m) >= 2 && string(m[1]) != "blueprint" {
		bp.Metadata.Name = string(m[1])
	}
	if meta, ok := compDoc["metadata"].(map[string]any); ok {
		if bp.Metadata.Name == "" {
			if name, ok := meta["name"].(string); ok {
				bp.Metadata.Name = name
			}
		}
```
`srcCommentRE` unconditionally matches `# Source:\s*([^\s]+)` and overrides `bp.Metadata.Name` with whatever value follows `# Source:`, even if it is a file path with forward slashes (standard Helm output) and not a valid Kubernetes metadata name.
The extracted name should only be used if it is a valid Kubernetes metadata name / DNS subdomain (matching DNS-1123 subdomain rules or validated against valid identifier syntax), and if not valid, fall back to `compDoc["metadata"]["name"]`.

## Contract

1. In `internal/adopt/adopt.go`, when inspecting `# Source:` comment headers, only adopt the comment value if it is a valid DNS subdomain name (e.g. alphanumeric characters, `-` or `.`, and starting/ending with alphanumeric) and does not contain slashes or invalid path characters. If invalid, ignore the comment and use `compDoc["metadata"]["name"]`.
2. Adopting a manifest containing `# Source: mychart/templates/composition.yaml` must succeed and set `bp.Metadata.Name` to the composition's `metadata.name` (e.g. `my-comp`).
3. Adopting a manifest containing `# Source: valid-name` without slashes should continue to set `bp.Metadata.Name = "valid-name"` if intended, or fallback to `metadata.name` if present.
4. Unit test in `internal/adopt/adopt_test.go` asserts adopting a manifest with `# Source: path/to/file.yaml` succeeds with `metadata.name` from the Composition resource.

## Acceptance Test

Unit test `TestAdopt_HelmSourceComment` in `internal/adopt/adopt_test.go`:
Given a manifest with `# Source: mychart/templates/composition.yaml` and Composition `metadata.name: my-comp`, assert `adopt.Adopt(manifest)` succeeds without error and `bp.Metadata.Name == "my-comp"`.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Handover

Branch `CF-263-adopt-helm-source-comment`, committed, not pushed, not merged. Final report includes failing (RED) and passing (GREEN) runs of the acceptance test and all standard gates.
