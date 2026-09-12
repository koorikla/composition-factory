# CF-404: Canvas deleteEnvKeyFromDoc leaves orphan function-environment-configs in spec.pipeline

## Problem
`deleteEnvKeyFromDoc` in `web-proto/js/utils.js:234-250` deletes `spec.environment` and `spec.environmentConfigs` when all environment keys are removed, but fails to clean `spec.pipeline`. Because `updateEnvSelection` in `web-proto/js/regions/inspector.js:1088-1096` injects an explicit `function-environment-configs` step into `spec.pipeline`, removing all environment keys leaves an orphaned pipeline step referencing an unmanaged EnvironmentConfig.
When generating crossplane resources, `functions.yaml` and `compositions/*.yaml` include the `environment-configs` pipeline step referencing `default`, but no `EnvironmentConfig` is emitted. Applying to a Crossplane cluster fails fatally.

## Repro
1. Open blueprint with environment config or configure selection mode.
2. Observe `spec.pipeline` gains a step for `function-environment-configs`.
3. Delete all environment keys in doc.
4. `spec.environment` and `spec.environmentConfigs` are deleted, but `spec.pipeline` retains the `function-environment-configs` step.

## Acceptance Criteria
When all environment keys are deleted in `deleteEnvKeyFromDoc`, any `function-environment-configs` step in `d.spec.pipeline` is removed. If `d.spec.pipeline` is empty after removal, `d.spec.pipeline` is deleted from the document.
Guarded by an automated test.
