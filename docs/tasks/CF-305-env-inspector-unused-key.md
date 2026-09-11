# CF-305: Environment inspector reports key as unused when referenced by forEach, when, or connectionSecret

## Context
In the canvas environment inspector, keys in `blueprint.spec.environment` that are referenced in:
- `resource.forEach` (e.g. `forEach: env.count`)
- `resource.when` (e.g. `when: env.stage == "prod"`)
- `resource.connectionSecret` (e.g. `name: env.secretName`)
are incorrectly flagged as "unused" because usage scanning only looks at `resource.fields[...].from == "env.<key>"`.

## Contract
In `web-proto/js/regions/inspector.js`:
When determining which environment keys are in use:
Inspect across all resources:
1. `r.fields`: `from` matching `env.<key>` or template expressions referencing `env.<key>`
2. `r.forEach`: string matching or referencing `env.<key>`
3. `r.when`: condition expression referencing `env.<key>`
4. `r.connectionSecret`: references to `env.<key>`
Any key referenced in any of these places must NOT be marked as unused.

## Acceptance Test
In `tests/cf305-env-inspector-unused-key.spec.js`:
Create blueprint with environment keys referenced by `forEach`, `when`, and `connectionSecret`.
Verify in inspector that none of these keys are flagged as unused.

## Gates
- `npm run lint:js`
- `make lint`
- `make lint-strict`
- `make test`
- `make test-e2e`
