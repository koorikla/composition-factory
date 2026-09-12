# CF-386 — validateFields accepts empty field paths and control characters in resource field keys

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 |
| **Closes** | `CF-386 — validateFields accepts empty field paths and control characters in resource field keys` (#276) |
| **Worktree** | `.worktrees/CF-386` on branch `CF-386-validate-resource-field-paths`, branched from `main` |
| **May write** | `internal/blueprint/validate_resources.go`, `internal/blueprint/validate_resources_test.go` |
| **Merges after** | nothing |

## Defect Summary

In `internal/blueprint/validate_resources.go:171-220`, `validateFields` iterates over the map of resource fields (`for _, p := range paths`). While it performs checks on the field values (`f.From`, `f.Value`, `f.Raw`, `f.Template`) and on map bracket keys (`mapKey`), it completely misses validating the field path key `p` itself:

1. **Empty field path accepted**: A resource field with an empty key `""` (`r.Fields[""] = Field{Value: "val"}`) passes `b.Validate()` without error.
2. **Control characters and newlines in field paths accepted**: While scalar declarations throughout the blueprint run `checkScalar` to prevent control characters and newline escapes that corrupt single-line YAML emission, `checkScalar` is never called on the field path `p` (or `basePath`). As a result, a field path key containing newlines or control characters (`"spec.forProvider.tag\n  injected: true"`) passes `b.Validate()` silently.

## Acceptance Test

In `internal/blueprint/validate_resources_test.go`:

```go
func TestValidateResourceFieldPaths(t *testing.T) {
	tests := []struct {
		name      string
		fieldPath string
		wantErr   string
	}{
		{
			name:      "empty field path",
			fieldPath: "",
			wantErr:   "empty field path",
		},
		{
			name:      "control character newline in field path",
			fieldPath: "spec.forProvider.name\n  injected: true",
			wantErr:   "contains the control character",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bp := &Blueprint{
				APIVersion: "factory.crossplane.io/v1alpha1",
				Kind:       "Blueprint",
				Metadata:   Metadata{Name: "test-bp"},
				Spec: Spec{
					XRD: XRD{
						Group: "example.org",
						Names: Names{Kind: "XTest", Plural: "xtests"},
						Parameters: map[string]Parameter{
							"region": {Type: "string"},
						},
					},
					Resources: []Resource{
						{
							Name: "test-res",
							Type: "ec2.aws.upbound.io/Subnet",
							Fields: map[string]Field{
								tc.fieldPath: {Value: "test"},
							},
						},
					},
				},
			}

			err := bp.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}
```

## Contract

In `internal/blueprint/validate_resources.go` inside `validateFields`:
1. Check that `strings.TrimSpace(p) != ""` and return an error: `fmt.Errorf("resource %q: empty field path", r.Name)` if empty.
2. Call `checkScalar(fmt.Sprintf("resource %q field %q", r.Name, p), p)` before processing the field.

## Verification

```sh
make lint
make lint-strict
make test-race
go test ./internal/blueprint -run TestValidateResourceFieldPaths -v
```
