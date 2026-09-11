# CF-252 — Import adopts a Composition whose template is cut mid-action, reports success and names no loss

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (scale: engine) |
| **Closes** | `CF-252 — Import adopts a Composition whose template is cut mid-action, reports success and names no loss` (#119) |
| **Worktree** | `.worktrees/CF-252` on branch `CF-252-adopt-truncated-template` |
| **May write** | `internal/adopt/`, `cmd/cf/` |
| **Merges after** | nothing |

## Symptom

Importing a go-templating Composition whose inline template is truncated inside an action (`{{- $xr := .observed.composite.resource.metadat`, no closing `}}`) succeeds: `/api/blueprint/adopt` answers 200 with `"lossReport":{}`, the persisted document has `resources: []`, and the canvas toasts "📥 Adopted xqueue-pipeline — changed: name (untitled → xqueue-pipeline); parameter $providerName description rewritten". Nothing names the unreadable template or the resource that vanished. `./bin/cf adopt truncated.yaml --cache-dir …` prints the same empty blueprint (`resources: null`) at exit 0.

## Mechanism

`internal/adopt/adopt.go:1535` `parseGoTemplateBody` never parses the body as a Go template. It splits on `---`, strips `{{ … }}` lines by regex, masks the rest and YAML-parses each chunk; at `adopt.go:1675-1677` a chunk whose YAML fails to parse is skipped with a bare `continue` and no `report.Record`, so the whole template body is discarded without a trace. Nothing checks that a go-templating Composition yielded at least one resource.

## Contract

1. When adopting a go-templating Composition whose template body contains unclosed actions, malformed template chunks, or chunks that fail to parse as YAML/resources, adopt must refuse with an error or record the unparsed chunk in the `LossReport` (naming the failure and unparsed resource).
2. If a go-templating Composition has a non-empty template body but yields zero recoverable resources, adopt must refuse with an informative error or record loss indicating no resources could be recovered from the template.
3. Under CLI `cf adopt`, an unrecoverable template error must exit non-zero or exit 2 when loss is detected.

## Acceptance Test

An automated unit test in `internal/adopt/adopt_test.go`:
`TestAdopt_TruncatedGoTemplateRefusedOrLossReported`
Construct or truncate a go-template Composition (e.g. cutting `testdata/xqueue-pipeline.composition.golden.yaml` mid-action). Run `Adopt(...)`. Verify that either:
1. `Adopt` returns an error naming the template/YAML parse failure, OR
2. `Adopt` returns a non-empty `LossReport` containing entries for the unparsed chunk / dropped resource.
Assert that it does NOT silently return an empty blueprint with `resources: []` and an empty `LossReport`.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Handover

Branch `CF-252-adopt-truncated-template`, committed, not pushed, not merged.
