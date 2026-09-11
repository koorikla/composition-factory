# CF-287 — AdoptTree crashes on templates directory when adopting configuration with filesystem templates

## Summary
`AdoptTree` (`internal/adopt/tree.go`) recursively scans all `.yaml` and `.yml` files in a given directory to discover XRDs and Compositions.
When adopting a package or directory generated with `cf gen --template-source=FileSystem`, or an existing Crossplane configuration package with a `templates/` subdirectory containing Go templates (such as `000-context.yaml` or resource templates), `splitYAML` attempts to parse every file as standard YAML.

Because Go template control blocks (`{{- $spec := ... -}}`) are not valid YAML node content, `yaml.NewDecoder(bytes.NewReader(data)).Decode(&node)` fails immediately with:
`unmarshal document: error converting YAML to JSON: yaml: did not find expected node content`

This causes `cf adopt <dir>` to abort and fail completely.

## Affected Locations
- `internal/adopt/tree.go:28-48`
- `internal/adopt/tree.go:83-93`

## Steps to Reproduce
1. Create a test directory with a valid XRD, a Composition using `function-go-templating` with `source: FileSystem`, and a `templates/` directory containing `000-context.yaml`:
   ```yaml
   {{- $spec := .observed.composite.resource.spec -}}
   ```
2. Run `cf adopt <dir>`.
3. Command fails with exit code 1:
   `adopt tree: unmarshal document: error converting YAML to JSON: yaml: did not find expected node content`
4. Repeat against a clean directory: identical failure.

## Expected Behavior
`AdoptTree` should not crash on non-Kubernetes YAML/template files. It should either ignore directories named `templates` when searching for XRD and Composition manifests, or gracefully skip documents that fail YAML parsing without aborting the entire walk.

## Severity
`severity:P1` — Prevents adopting any Crossplane repository or package that uses filesystem-based Go templating.
