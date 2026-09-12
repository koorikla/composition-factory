# Driver run report — 2026-09-12, T0 23:05 UTC (2026-09-11)

T0 recorded: `2026-09-11T23:05:14Z`. Report written: `2026-09-12T01:58:00Z` (T0 + 2:53).
Total elapsed time: ~2 hours 53 minutes (well within the 5-hour hard budget; merge cutoff at T0 + 4:00 and subagent dispatch cutoff at T0 + 3:00 adhered to).

## What happened

Preflight recorded T0 at `23:05:14Z`. Confirmed clean `origin/main` working tree and executed 13 sequential waves of disjoint tasks across engine emitters (KCL, Go templating, Python), adopt parsers (parameter index syntax, boolean when guards, hasKey, status rewrites, single-quoted environment index expressions), schema builders, CLI validation, package integrity, and UI/canvas layout.

Over the 13 waves of this driver run, **37 backlog issues** were resolved, verified with new test-first reproduction tests, gated locally with race-enabled tests and linters (`make lint && make lint-strict && make test-race`), fast-forward merged to `main`, pushed to GitHub, verified green across all 5 CI jobs (`test`, `docker-build`, `cluster`, `e2e`, `acceptance`), closed on GitHub with references to the guarding tests, and all topic branch worktrees cleaned up.

---

## Detailed Wave Breakdown

### Wave 1
- **CF-300 (#188)** (`scale:ux`, `severity:P2`): Respect forEach status wires in canvas auto-layout dependency layers. Landed in `56aded0`, closed. Guarded by `tests/cf300-canvas-foreach-status-wires.spec.js`.
- **CF-301 (#189)** (`scale:engine`, `severity:P1`): Incorporate loop index into custom metadata.name for looped resources to prevent duplicate resource names. Landed in `9077178`, merge `d382011`, closed. Guarded by `internal/emit/gotemplate_test.go:TestGoTemplateForEachCustomMetadataNameIndex`.
- **CF-309 (#198)** (`scale:engine`, `severity:P2`): Permit YAML 1.2 boolean keywords (`true`, `false`, `yes`, `no`, `on`, `off`) as object parameter member names. Landed in `0be86f7`, closed. Guarded by `internal/blueprint/blueprint_test.go:TestValidateObjectParameterWithBooleanPropertyNames`.

### Wave 2
- **CF-307 (#196)** (`scale:engine`, `severity:P1`): Align preview template helper signatures with Crossplane (`getComposedResource`, `hasKey`, `composite`). Landed in `0cc1774`, merge `7321aad`, closed. Guarded by `internal/emit/preview_test.go:TestPreviewExpressionHelperSignatures`.
- **CF-310 (#199)** (`scale:engine`, `severity:P2`): Preserve empty string comparisons in when guards during adoption (`eq .spec.foo ""`). Landed in `872de49`, closed. Guarded by `internal/adopt/adopt_test.go:TestAdoptGoTemplate_EmptyStringWhenGuard`.

### Wave 3
- **CF-311 (#200)** (`scale:engine`, `severity:P1`): Prioritize declared properties over `additionalProperties` in schema `buildNode`. Landed in `1098bdc`, closed. Guarded by `internal/schema/schema_test.go:TestBuildNode_PrioritizesDeclaredPropertiesOverAdditionalProperties`.
- **CF-312 (#201)** (`scale:engine`, `severity:P1`): Support reversed operand Go template when guards (`eq "val" .spec.param`). Landed in `217ea8c`, `9a98660`, closed. Guarded by `internal/adopt/adopt_test.go:TestAdoptGoTemplate_WhenParamReversedOperands`.
- **CF-313 (#202)** (`scale:engine`, `severity:P1`): Retain interpolated Go template strings as raw fields in adopt rather than dropping them. Landed in `c67089c`, closed. Guarded by `internal/adopt/adopt_test.go:TestAdoptGoTemplate_InterpolatedStringsRetainedAsRaw`.

### Wave 4
- **CF-314 (#203)** (`scale:engine`, `severity:P1`): Exclude target resource in `StatusReferencingResources` for self-referential raw expressions to prevent circular dependency cycles. Landed in `346c052`, `8c343d1`, closed. Guarded by `internal/blueprint/reference_test.go:TestStatusReferencingResources_SelfRefExcluded`.
- **CF-315 (#204)** (`scale:engine`, `severity:P1`): Validate required and declared properties when `additionalProperties` is declared. Landed in `85a2d45`, `da98ad2`, closed. Guarded by `internal/schema/validate_test.go:TestValidateSchema_AdditionalPropertiesDeclared`.

### Wave 5
- **CF-316 (#205)** (`scale:engine`, `severity:P2`): Support status wires from looped resources via index expressions (`resources[i].status...`). Closed. Guarded by `internal/blueprint/reference_test.go:TestStatusReferencingResources_IndexedResource`.
- **CF-318 (#207)** (`scale:engine`, `severity:P1`): Preserve `function-environment-configs` when an unrelated pipeline step is named `environment-configs`. Landed in `2eab88f`, merge `8af2fa6`, closed. Guarded by `internal/emit/pipeline_test.go:TestEffectivePipeline_UnrelatedStepNamedEnvironmentConfigs`.
- **CF-308 (#197)** (`scale:engine`, `severity:P1`): Adopt `gotemplating.fn.crossplane.io/composition-resource-name` annotation. Landed in `23d8a31`, merge `35c0398`, closed. Guarded by `internal/adopt/adopt_test.go:TestAdoptGoTemplate_CompositionResourceNameAnnotation`.

### Wave 6
- **CF-325 (#214)** (`scale:engine`, `severity:P1`): Enforce boundary delimiters on raw resource references to prevent prefix matching bugs (e.g. `bucket` vs `bucket-backup`). Landed in `f728277`, merge `f5842d5`, closed. Guarded by `internal/blueprint/reference_test.go:TestRawReferences_ResourcePrefixMatching`.
- **CF-320 (#209)** (`scale:engine`, `severity:P1`): Refuse `spec.templates` for non-Go engines in `refuseGoTemplateOnlyFeatures`. Landed in `1bf9579`, merge `165f208`, closed. Guarded by `internal/emit/emit_test.go:TestRefuseTemplatesForNonGoEngines`.
- **CF-322 (#211)** (`scale:engine`, `severity:P1`): Support index syntax for Go template parameter references in adopt (`index .spec "param"`). Landed in `35a43f8`, merge `875467d`, closed. Guarded by `internal/adopt/adopt_test.go:TestAdoptGoTemplate_IndexSpecParamRef`.

### Wave 7
- **CF-329 (#218)** (`scale:engine`, `severity:P1`): Require resource context for quoted raw references to prevent unanchored string false positives. Landed in `3283cff`, merge `ae7d8d6`, closed. Guarded by `internal/blueprint/reference_test.go:TestRawReferences_ResourceUnanchoredQuotedStrings`.
- **CF-328 (#217)** (`scale:engine`, `severity:P1`): Recognize Go template boolean when guards in adopt (`when: .spec.enableFoo`). Landed in `e7c051b`, merge `963dd35`, closed. Guarded by `internal/adopt/adopt_test.go:TestAdoptGoTemplate_BooleanWhenGuards`.

### Wave 8
- **CF-326 (#215)** (`scale:engine`, `severity:P1`): Index function input CRDs on add and pipeline changes. Landed in `39c91a5`, merge `4664a95`, closed. Guarded by `internal/api/pipeline_test.go:TestAddFunctionStep_IndexesCRDs`.
- **CF-334 (#223)** (`scale:engine`, `severity:P1`): Support index syntax in raw parameter references and rewrites (`index .params "key"`). Landed in `ecc3d99`, merge `1ccaf57`, closed. Guarded by `internal/blueprint/reference_test.go:TestRawReferences_ParamIndexSyntax`.
- **CF-331 (#220)** (`scale:engine`, `severity:P1`): Support index syntax in Go template when guards in adopt. Landed in `85f313a`, merge `0c4bbcc`, closed. Guarded by `internal/adopt/adopt_test.go:TestAdoptGoTemplate_WhenGuardsIndexSyntax`.

### Wave 9
- **CF-324 (#213)** (`scale:ux`, `severity:P2`): Prompt for package on custom preset and update cel-filter package reference. Landed in `5939ca0`, merge `5b665dc`, closed. Guarded by `tests/cf324-pipeline-presets-package-validity.spec.js`.

### Wave 10
- **CF-344 (#232)** (`scale:engine`, `severity:P0`): Quote boolean and null keywords in YAML keys and required lists to prevent YAML 1.1 / 1.2 boolean coercion bugs in emitted manifests. Landed in `2562ec0`, merge `d998a3e`, closed. Guarded by `internal/emit/emit_test.go:TestEmitYamlBooleanAndNullKeywordsQuoted`.
- **CF-336 (#225)** (`scale:engine`, `severity:P1`): Recognize `getComposedResource` in raw references and rewrites. Landed in `6c30c96`, merge `f763674`, closed. Guarded by `internal/blueprint/reference_test.go:TestRawReferences_GetComposedResource`.
- **CF-342 (#231)** (`scale:engine`, `severity:P1`): Admit single-quoted environment index references in adopt (`index $env 'key'`). Landed in `1d91286`, merge `6a7924b`, closed. Guarded by `internal/adopt/adopt_test.go:TestAdoptGoTemplate_SingleQuotedEnvIndexWires`.
- **CF-347 (#239)** (`scale:engine`, `severity:P1`): Support index syntax on observed composite spec in parameter references and rewrites. Landed in `b1dae89`, merge `5f8e636`, closed. Guarded by `internal/blueprint/reference_test.go:TestRawReferences_ParamObservedSpec`.
- **CF-321 (#210)** (`scale:engine`, `severity:P1`): Support `hasKey` guards in Go template parameter when conditions. Landed in `99d8257`, merge `a3af31f`, closed. Guarded by `internal/adopt/adopt_test.go:TestAdoptGoTemplate_HasKeyParamWhenGuard`.

### Wave 11
- **CF-348 (#240)** (`scale:engine`, `severity:P2`): Fail fast when a resource's declared provider does not match the resolved CRD's provider package. Landed in `ee515f3`, `e0b293c`, closed. Guarded by `internal/emit/emit_test.go:TestEmitFailsWhenResourceProviderMismatchesCRD`.
- **CF-351 (#243)** (`scale:engine`, `severity:P1`): Propagate `ReadLock` error in `LoadSources` rather than silently returning nil. Landed in `93a8264`, `489129c`, closed. Guarded by `internal/cache/cache_test.go:TestLoadSources_PropagatesReadLockError`.
- **CF-352 (#244)** (`scale:engine`, `severity:P2`): Fail cleanly on missing explicit blueprint file in `cf kinds` and `cf fields` CLI commands. Landed in `ffb78bb`, `f15756d`, closed. Guarded by `cmd/cf/cli_test.go:TestKindsAndFieldsMissingExplicitBlueprint`.
- **CF-356 (#248)** (`scale:engine`, `severity:P1`): Recognize and rewrite `hasKey` expressions in parameter references. Landed in `0afd2b7`, `53a2892`, closed. Guarded by `internal/blueprint/reference_test.go:TestRawReferences_HasKeyExpressions`.

### Wave 12
- **CF-355 (#247)** (`scale:engine`, `severity:P1`): Support `hasKey` on observed resources in status references and rewrites (`hasKey $r.status "ready"`). Landed in `7153be5`, `4317738`, closed. Guarded by `internal/blueprint/reference_test.go:TestRawReferences_ObservedHasKey`.
- **CF-359 (#251)** (`scale:engine`, `severity:P2`): Reject invalid package types in `cf catalogue` and `cf search` CLI commands. Landed in `79cafa0`, `14e16d3`, closed. Guarded by `cmd/cf/cli_test.go:TestCatalogueAndSearchRejectInvalidType`.
- **CF-299 (#187)** (`scale:engine`, `severity:P1`): Rewrite forEach status references when resources are renamed during adoption. Landed in `43da587`, merge `7a333ac`, closed. Guarded by `internal/adopt/adopt_test.go:TestAdoptRewriteForEachStatusReferences`.

### Wave 13
- **CF-362 (#254)** (`scale:engine`, `severity:P2`): Validate blueprint via `b.Validate()` in `cf package` before building to prevent corrupt xpkg packaging. Landed in `f333886`, `221aabe`, closed. Guarded by `cmd/cf/package_test.go:TestPackageRejectsInvalidBlueprint`.
- **CF-364 (#256)** (`scale:engine`, `severity:P1`): Check engine-specific pipeline step collision dynamically (`b.Engine()`) allowing `function-go-templating` when using KCL or Python engines while prohibiting `function-kcl` under KCL. Landed in `405b627`, `e2d773f`, closed. Guarded by `internal/blueprint/pipeline_test.go:TestValidatePipelineKCLBuiltinCollision` and `TestValidatePipelineKCLAllowsGoTemplating`.
- **CF-365 (#257)** (`scale:engine`, `severity:P1`): Validate pipeline rejects `position: after` on `function-environment-configs` (context providers must precede consumers), and adopt normalizes `function-environment-configs` position to `before`. Landed in `405b627`, `8533bcd`, `cb42bf2`, CI run [34665996421](https://github.com/koorikla/composition-factory/actions/runs/34665996421) passed, closed. Guarded by `internal/blueprint/pipeline_test.go:TestValidatePipeline_EnvironmentConfigsPositionAfterRejected` and `internal/adopt/adopt_test.go:TestAdoptPipeline_EnvironmentConfigsForcedBefore`.

---

## Verification and Quality Gates

Every wave followed the mandatory verification lifecycle:
1. **Test-First Backlog Ticking**: Added failing reproduction tests before implementing the fix.
2. **Local Gating**: Ran `make lint && make lint-strict && make test-race` locally before merging or pushing.
3. **CI Pipeline**: All pushed commits were tracked and confirmed green across all 5 CI jobs:
   - `test` (vet, js lint, typecheck, staticcheck, test, test-race)
   - `docker-build` (Docker build verification)
   - `acceptance` (Docker + Crossplane CLI acceptance tests)
   - `cluster` (Local kind cluster Crossplane and functions verification)
   - `e2e` (Playwright browser e2e test suite)
4. **Issue Closing**: Only closed after remote CI run returned 100% green status code 0, referencing the merge commit SHA and guarding test.

---

## Final Repository State

- `origin/main` commit: `cb42bf2` (clean tree, up to date with remote).
- All temporary worktrees cleaned up; no orphaned processes on testing or dev ports.
- Total closed issues in this run: **37 issues**.
