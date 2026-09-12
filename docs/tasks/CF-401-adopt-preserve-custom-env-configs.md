# Task Brief: CF-401 — Adopt preserves custom function-environment-configs pipeline step causing subsequent environmentConfigs edits to be ignored

## Problem
When adopting a Crossplane Composition whose pipeline invokes \`function-environment-configs\` with non-default input (for example, targeting a custom EnvironmentConfig reference or selector), \`internal/adopt\` fails to recognize the step as the canonical environment configs provider step. Consequently, it retains the step as an opaque entry in \`bp.Spec.Pipeline\`. When \`emit.Composition\` renders the pipeline, \`effectivePipeline\` detects the existing step with non-empty input and bypasses \`b.EnvironmentConfigsInput()\`. As a result, subsequent modifications to \`bp.Spec.EnvironmentConfigs\` (via CLI, Canvas, or direct IR manipulation) are silently ignored during Composition emission.

## Root Cause
In \`internal/adopt/adopt.go:1923-1953\`, \`isEnvConfigsStep(s)\` checks:
\`\`\`go
trimmed := strings.TrimSpace(s.Input)
if trimmed == "" || trimmed == strings.TrimSpace(blueprint.DefaultEnvironmentConfigsInput) || trimmed == strings.TrimSpace(bp.EnvironmentConfigsInput()) {
    return true
}
var stepDoc struct {
    Spec struct {
        EnvironmentConfigs []struct {
            Type string \`json:"type"\`
            Ref  *struct {
                Name string \`json:"name"\`
            } \`json:"ref"\`
        } \`json:"environmentConfigs"\`
    } \`json:"spec"\`
}
if err := yaml.Unmarshal([]byte(s.Input), &stepDoc); err == nil {
    cfgs := stepDoc.Spec.EnvironmentConfigs
    if len(cfgs) == 1 && (cfgs[0].Type == "Reference" || cfgs[0].Type == "") &&
        (cfgs[0].Ref == nil || cfgs[0].Ref.Name == "" || cfgs[0].Ref.Name == "default") {
        return true
    }
}
\`\`\`
Because YAML serialization ordering and formatting can differ from \`bp.EnvironmentConfigsInput()\`, string comparison fails. Furthermore, the fallback parser only checks for single reference to \`default\` or empty name. If the adopted composition targets custom names or selectors, \`isEnvConfigsStep\` returns false, keeping the step in \`bp.Spec.Pipeline\`.

## Solution
1. In \`isEnvConfigsStep\`, parse \`s.Input\` and compare the parsed configurations with \`bp.Spec.EnvironmentConfigs\`. If the parsed configurations match \`bp.Spec.EnvironmentConfigs\`, or if \`bp.Spec.EnvironmentConfigs\` was already extracted from this step (and matches), return \`true\` so the step is pruned from \`bp.Spec.Pipeline\`.
2. Ensure that adopting a composition with custom environment configs does not leave a duplicate \`function-environment-configs\` in \`bp.Spec.Pipeline\`.
3. Verify that modifying \`bp.Spec.EnvironmentConfigs\` after adopt produces updated pipeline step input upon \`emit.Composition\`.
