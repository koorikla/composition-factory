# CF-402: Canvas cleanMemberRefs leaves dangling references in spec.templates when deleting XRD parameter member properties

## Problem
When deleting a member property of an XRD object parameter in the canvas parameter inspector, `cleanMemberRefs` in `web-proto/js/regions/inspector/xrd.js` cleans referencing resource fields, annotations, and envelope entries, but completely omits `draft.spec.templates` and `draft.spec.conventions`. Any template referencing the deleted member property (e.g. `{{ .spec.cfg.region }}`) is preserved with a dangling reference, which subsequently causes runtime render failures (`missingkey=error`) in Crossplane's `function-go-templating`.

## Acceptance Criteria
- `cleanMemberRefs` inspects `draft.spec.templates` for references to the deleted member using `isRawParamRef(templateBody, paramName + "." + memberPath)`.
- Any template that references the deleted member is removed from `draft.spec.templates`.
- Conventions referencing deleted templates in `draft.spec.conventions` are cleaned.
- Resource fields, annotations, and envelopes referencing deleted templates via `f.template` are unwired.
- Guarded by an automated test.
