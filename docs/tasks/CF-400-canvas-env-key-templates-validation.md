# CF-400: Renaming or deleting environment keys leaves dangling references in spec.templates and accepts illegal names

## Problem
1. `renameEnvKeyInDoc` in `web-proto/js/utils.js` rewrites environment key references across `spec.resources` and `spec.environmentConfigs`, but completely ignores `spec.templates`. Any custom Go templates referencing the key (e.g., `$env.<key>`, `.env.<key>`, or `(index $env \"<key>\")`) remain pointing to the stale key name.
2. `cleanEnvRefs` in `web-proto/js/utils.js` (called when deleting an environment key) cleans references from `spec.resources`, but does not scan or clean `spec.templates`.
3. `renameEnvKey` in `web-proto/js/regions/inspector.js` allows hyphens and underscores (`/^[a-zA-Z][a-zA-Z0-9_-]*$/`), but the backend rejects non-camelCase names (`/^[a-zA-Z][a-zA-Z0-9]*$/` and YAML keywords).

## Acceptance Criteria
1. `renameEnvKeyInDoc` rewrites environment key references in `d.spec.templates`.
2. `cleanEnvRefs` removes referencing templates from `draft.spec.templates` (and cleans `conventions` and referencing fields/envelopes/annotations).
3. `renameEnvKey` strictly enforces camelCase naming matching backend rules.
4. Guarded by automated tests.
