# CF-225 — KCL and Python emitters silently drop metadata.labels on native Kubernetes resources

## Severity & Scope
- **Severity**: P1
- **Scale**: engine
- **Touch set**: `internal/emit/composition.go`, `internal/emit/kcl.go`, `internal/emit/kcl_test.go`, `internal/emit/native.go`, `internal/emit/native_test.go`, `internal/emit/python.go`, `internal/emit/python_test.go`, `docs/tasks/CF-225-kcl-python-native-labels.md`

## Background & Problem
Native Kubernetes resources (provider `k8s`) that declare labels via `metadata.labels[...]` had those labels silently dropped when generated using `engine: python` or `engine: kcl`.

While Go-templating rendered non-name, non-annotation planned fields from `metaPlan` into the `metadata:` block, `internal/emit/kcl.go` and `internal/emit/python.go` only inspected `pres.MetaPlan` for `metadata.name` or `name` and completely ignored the remainder of `metaPlan`.

## Expected Behavior & Contract
`internal/emit/kcl.go` and `internal/emit/python.go` emit `metadata.labels` and other valid native metadata fields under `metadata:`, matching the behavior of `internal/emit/composition.go`.

## Acceptance Test
- `internal/emit/native_test.go:TestNativeMetadataLabelsParityAcrossEngines` verifies Go-template, KCL, and Python parity for native metadata labels and looped resources.
- `internal/emit/kcl_test.go:TestEmitKCLNativeMetadataLabels`
- `internal/emit/python_test.go:TestEmitPythonNativeMetadataLabels`
