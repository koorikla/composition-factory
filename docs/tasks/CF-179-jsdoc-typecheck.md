# CF-179 — web-proto lacks compile-time type validation or JSDoc typechecking for API schemas and store state

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P3 (UX scale: type safety / maintainability) |
| **Closes** | `CF-179 — web-proto lacks compile-time type validation or JSDoc typechecking for API schemas and store state` |
| **Worktree** | `.worktrees/CF-179` on branch `CF-179-jsdoc-typecheck` |
| **May write** | `jsconfig.json`, `package.json`, `Makefile`, `web-proto/js/` |
| **Merges after** | nothing |

## Symptom

Frontend code in `web-proto/js/` relies entirely on untyped vanilla JavaScript with ESLint checking only basic syntax rules (`eslint.config.js`). Typos in property names (`spec.forProvider`, store topic payloads, API endpoint return types) are undetectable statically and only surface via runtime failures in Playwright or manual canvas clicks.

## Evidence

Check `package.json` scripts and `web-proto/js/`: no TypeScript configuration or typecheck gate exists. Renaming or mistyping a property on an API response object passes `make lint` and fails only at browser runtime.

## Contract

1. Create `jsconfig.json` configured for standard ES2022 / browser DOM environments with `"checkJs": true`, `"noEmit": true`, and type roots.
2. Annotate core types in `web-proto/js/` via JSDoc `@typedef` for:
   - Blueprint (`Blueprint`, `BlueprintSpec`, `ResourceNode`, `FieldMapping`, `StatusMapping`, `EnvironmentKey`)
   - Store (`StoreState`, `TopicMap`)
   - API endpoints (`PreviewExpressionRequest`, `PreviewExpressionResponse`, `RenderResponse`, `GenerateResponse`)
3. Add `npm run typecheck` (`tsc --noEmit`) to `package.json` scripts and wire it into `make lint` / CI.
4. Zero build tools or bundlers added: `web-proto` must continue serving native ES modules directly to the browser.

## Acceptance Test

1. Introduce an intentional property mismatch in a JSDoc-typed file (e.g. `d.spec.nonExistentField`) and verify `npm run typecheck` or `make lint` fails with exit code > 0.
2. Fix the mismatch and verify `make lint` and `make test-e2e` pass cleanly with 0 type errors.

## Verification

```sh
make lint && make lint-strict && make test
rm -rf .testrun* test-results && make test-e2e
```

## Handover

Branch `CF-179-jsdoc-typecheck`, committed, not pushed, not merged. Final report includes failing and passing runs of the typecheck gate and all standard gates.
