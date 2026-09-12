# CF-365 — validatePipeline permits function-environment-configs with position: after

## 1. Context & Invariant

Crossplane Composition pipeline requires context providers (specifically `function-environment-configs`) to execute before context consumers (`render-resources`). Allowing `position: after` on `function-environment-configs` causes environment expressions to evaluate against empty contexts.

## 2. Requirements & Contract

1. In `internal/blueprint/pipeline.go`:
   - If `s.FunctionRef == EnvironmentConfigsFunctionName` and `s.Position == PositionAfter`, reject with an error explaining that environment configs must run before templating.
2. Guard with automated test in `internal/blueprint/pipeline_test.go`:
   - `TestValidatePipeline_EnvironmentConfigsPositionAfterRejected`
3. Pass `make lint && make lint-strict && make test-race`.
