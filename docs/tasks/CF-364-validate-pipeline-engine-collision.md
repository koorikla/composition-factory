# CF-364 — validatePipeline hardcodes function-go-templating collision check ignoring KCL and Python engines

## 1. Context & Invariant

In `internal/blueprint/pipeline.go`, `validatePipeline` must check whether a pipeline step functionRef collides with the active templating engine's built-in function (`function-kcl` for KCL, `function-python` for Python, `function-go-templating` for Go Templating) rather than hardcoding `TemplatingFunctionName`.

## 2. Requirements & Contract

1. In `internal/blueprint/pipeline.go`:
   - Determine the active engine's templating function name (`bp.Engine()`).
   - Reject steps where `s.FunctionRef` matches that engine's templating function.
   - Allow non-colliding steps (e.g. `function-go-templating` under KCL or Python).
2. Guard with automated test in `internal/blueprint/pipeline_test.go`:
   - `TestValidatePipelineKCLBuiltinCollision`
3. Pass `make lint && make lint-strict && make test-race`.
