# Driver run report — 2026-09-12, T0 05:05 UTC

T0 recorded: `2026-09-12T05:05:15Z`. Report written: `2026-09-12T08:12:00Z` (T0 + 3:07).
Total elapsed time: ~3 hours 7 minutes (well within the 5-hour hard budget; dispatch cutoff at T0 + 3:00 and merge cutoff at T0 + 4:00 strictly adhered to).

## What happened

Preflight recorded T0 at `05:05:15Z`. Confirmed clean `origin/main` working tree and executed waves across engine pipeline validation, canvas undo/redo interaction, canvas parameter member cleanup, adopt environment configs handling, Go template validation under non-Go engines, and bracket-quoted map patch normalization.

Across this driver run, **10 backlog issues** were resolved, verified with new test-first reproduction tests, gated locally with race-enabled tests, staticcheck, and linters (`make lint && make lint-strict && make test-race`), fast-forward merged to `main`, pushed to GitHub, verified green across all 5 CI jobs (`test`, `docker-build`, `cluster`, `e2e`, `acceptance`), closed on GitHub with references to the guarding tests, and all topic branch worktrees cleaned up.

---

## Detailed Wave Breakdown

### Wave 1
- **CF-371 (#263)** (`scale:engine`, `severity:P1`): Pipeline input validation allows custom function-environment-configs step name when configured. Landed in `f658ef2829241b181284eb3bca9aa6a3d6cb4432`, CI run [34679064599](https://github.com/koorikla/composition-factory/actions/runs/34679064599) green, closed. Guarded by `internal/emit/pipeline_test.go:TestValidatePipelineInputs_CustomEnvironmentConfigsStepName`.
- **CF-393 (#284)** (`scale:ux`, `severity:P2`): Canvas Redo keyboard shortcut `Ctrl+Shift+Z` / `Cmd+Shift+Z` and `Ctrl+Y`. Landed in `40c12c2a38096cf9fc9735d47155aebf13bda828`, CI run [34679275254](https://github.com/koorikla/composition-factory/actions/runs/34679275254) green, closed. Guarded by `tests/cf393-canvas-redo-shortcut.spec.js`.
- **CF-394 (#285)** (`scale:ux`, `severity:P2`): Unwire referencing fields when switching XRD parameter type from object/array to scalar. Landed in `e6cfaf6c22cb85bc4b8e21a221f0ce516e8aa78e`, CI run [34679500831](https://github.com/koorikla/composition-factory/actions/runs/34679500831) green, closed. Guarded by `tests/cf394-xrd-param-type-switch-clean.spec.js`.

### Wave 2
- **CF-369 (#261)** (`scale:engine`, `severity:P1`): Blueprint.Validate rejects Go-template features and environment under non-Go engines (KCL, Python). Landed in `91e1bfd83627d7aeafdb9ebbf4c947ebf60878ff`, CI run [34680641276](https://github.com/koorikla/composition-factory/actions/runs/34680641276) green, closed. Guarded by `internal/blueprint/emit_test.go:TestValidateRejectsGoTemplateFeaturesWithNonGoEngines`.
- **CF-404 (#295)** (`scale:ux`, `severity:P2`): Canvas deleteEnvKeyFromDoc removes orphan `function-environment-configs` step from pipeline when all env keys are deleted. Landed in `0ecd9ff6748fb66e575e2a0bba753b938d0102d6`, CI run [34680912272](https://github.com/koorikla/composition-factory/actions/runs/34680912272) green, closed. Guarded by `tests/cf404-delete-env-key-pipeline.spec.js`.
- **CF-402 (#293)** (`scale:ux`, `severity:P2`): Canvas cleanMemberRefs cleans referencing templates, conventions, and fields when deleting member properties. Landed in `095366114eb7e1ceb1156557b4c6e61f43eb91f5`, CI run [34681163843](https://github.com/koorikla/composition-factory/actions/runs/34681163843) green, closed. Guarded by `tests/cf402-clean-member-refs-templates.spec.js`.

### Wave 3
- **CF-400 (#291)** (`scale:ux`, `severity:P2`): Canvas rename and delete environment keys rewrites and cleans `spec.templates` and enforces camelCase naming. Landed in `6d8654516346702e5b7fb5f09cb8d9e2fbfaae9f`, CI run [34681420574](https://github.com/koorikla/composition-factory/actions/runs/34681420574) green, closed. Guarded by `tests/cf400-env-key-templates-validation.spec.js`.
- **CF-401 (#292)** (`scale:engine`, `severity:P2`): Adopt prunes canonical `function-environment-configs` pipeline step with custom configs to ensure subsequent IR modifications are emitted. Landed in `fb4017aa75c571e7f54192b71ca4fc9baf2be229`, CI run [34681642598](https://github.com/koorikla/composition-factory/actions/runs/34681642598) green, closed. Guarded by `internal/adopt/adopt_test.go:TestCF401_AdoptPreservesCustomEnvironmentConfigsPipelineStep`.
- **CF-403 (#294)** (`scale:engine`, `severity:P2`): Adopt supports `FromEnvironmentFieldPath` patches and retains environment configs. Landed in `00d1c8c6138ae7400ad9866cf62a462e01dd5abd`, CI run [34681867066](https://github.com/koorikla/composition-factory/actions/runs/34681867066) green, closed. Guarded by `internal/adopt/adopt_test.go:TestCF403_AdoptFromEnvironmentFieldPath`.
- **CF-406 (#297)** (`scale:engine`, `severity:P2`): Adopt unquotes bracket-quoted annotation and label patch keys (`metadata.annotations['...']`). Landed in `f5d590b59870523b0093adbfb2115d0bddd2c219`, CI run [34682087681](https://github.com/koorikla/composition-factory/actions/runs/34682087681) green, closed. Guarded by `internal/adopt/adopt_test.go:TestCF406_AdoptBracketQuotedAnnotationPatches`.

---

## Verification and Quality Gates

Every wave strictly followed the verification lifecycle:
1. **Test-First Backlog Ticking**: Added failing reproduction tests before implementing each fix.
2. **Local Gating**: Ran `make lint && make lint-strict && make test-race` (and `make test-docker` / Playwright specs as appropriate) locally before merging.
3. **Sequential Merge Invariant**: Merged one task at a time to `main`, pushed to `origin/main`, and watched CI to 100% green before proceeding to the next merge.
4. **Remote CI Pipeline**: Every push was confirmed green across all 5 CI jobs:
   - `test` (vet, js lint, typecheck, staticcheck, test, test-race)
   - `docker-build` (Docker build verification)
   - `acceptance` (Docker + Crossplane CLI acceptance tests)
   - `cluster` (Local kind cluster Crossplane and functions verification)
   - `e2e` (Playwright browser e2e test suite)
5. **Issue Closing**: Only closed after remote CI run returned 100% green status code 0, referencing the merge commit SHA and guarding test.

---

## Final Repository State

- `origin/main` commit: `f5d590b59870523b0093adbfb2115d0bddd2c219` (clean tree, up to date with remote).
- All temporary worktrees cleaned up; no orphaned processes on testing or dev ports.
- Total closed issues in this run: **10 issues** (#263, #284, #285, #261, #295, #293, #291, #292, #294, #297).
