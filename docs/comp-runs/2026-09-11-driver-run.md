# Driver run report — 2026-09-11

T0 recorded: `2026-09-11 01:09:12 UTC`
Driver run end: `2026-09-11 03:09:00 UTC`
Total elapsed time: 2 hours 0 minutes (well under 5-hour hard budget; merge cap at T0 + 4:00 adhered to).

---

## 1. Overview & Resolved Issues

All issues were executed to `docs/task-execution-contract.md` and `docs/routines/issue-driver.md`.
Each issue had its acceptance tests run and verified failing before the fix, quality gates (`make lint`, `make lint-strict`, `make test-race`, and relevant e2e/docker suites) passed cleanly, fast-forward/rebased onto `main`, watched to green completion on GitHub Actions CI (`gh run watch --exit-status`), and closed with the guarding test specified in the closure comment.

| Issue | Title | Outcome | Merge Commit | CI Run | Guarding Test |
|---|---|---|---|---|---|
| **CF-149** (#34) | A convention matching a nested native-kind field is silently ignored | Merged & Closed | `40b2f82` | [34549614631](https://github.com/koorikla/composition-factory/actions/runs/34549614631) | `internal/emit/conventions_test.go:TestNativeKindRejectsNestedConventions` |
| **CF-152** (#37) | A declared source that failed to load cannot be repaired from the canvas | Merged & Closed | `97f5140` | [34550615579](https://github.com/koorikla/composition-factory/actions/runs/34550615579) | `internal/api/cf152_repair_failed_source_test.go` |
| **CF-092** (#4) | Loading a starter example overwrites the served blueprint file on disk with no file-level cue | Merged & Closed | `3000389` | [34551911395](https://github.com/koorikla/composition-factory/actions/runs/34551911395) | `tests/cf092-starter-example-file-cue.spec.js` |
| **CF-141** (#26) | The inspector badges nested optional-object leaves as REQ | Merged & Closed | `dd0861b` | [34552931077](https://github.com/koorikla/composition-factory/actions/runs/34552931077) | `tests/cf141-nested-optional-object-leaves-badge.spec.js` |
| **CF-153** (#38) | The inspector's 'wire to…' control commits a wire on the first keystroke | Merged & Closed | `9983afc` | [34557014984](https://github.com/koorikla/composition-factory/actions/runs/34557014984) | `tests/cf153-wire-select-keystroke.spec.js` |

*Note: Additional issues resolved earlier in the day include CF-107 (#5), CF-108 (#6), CF-142 (#27), CF-143 (#28), CF-151 (#36), and CF-150 (#35).*

---

## 2. Issues Dispatched and Workflows

### Wave 1
- **CF-149** (`internal/emit/composition.go`): Refused conventions that match fields inside native k8s kinds (`provider: k8s`), checking all nested field paths rather than only top-level leaves. Guarded by `internal/emit/conventions_test.go`. CI run 34549614631 green.
- **CF-152** (`internal/api/providers.go`, `internal/api/server.go`): Added support to repair failed declared sources by tracking failed sources in server state, returning them with error status in `GET /api/sources`, allowing `DELETE /api/sources/<ref>` to clean them up, and clearing error state upon successful retry. Guarded by `internal/api/cf152_repair_failed_source_test.go`. CI run 34550615579 green.

### Wave 2
- **CF-092** (`web-proto/js/regions/editor.js`, `web-proto/index.html`): Added explicit served blueprint filename in starter example replacement warning dialog and toast notice to prevent inadvertent overwrite of local work. Guarded by `tests/cf092-starter-example-file-cue.spec.js`. CI run 34551911395 green.
- **CF-141** (`web-proto/js/regions/inspector.js`, `web-proto/js/schema.js`): Fixed the `REQ` badge logic so that nested fields in optional objects are only marked `REQ` if an ancestor is either required or has a value/wire already configured. Guarded by `tests/cf141-nested-optional-object-leaves-badge.spec.js`. CI run 34552931077 green.

### Wave 3
- **CF-153** (`web-proto/js/regions/inspector.js`): Fixed native `<select>` keyboard search in `wire to...` and `env wire to...` dropdowns committing wires on the first keystroke. Added keyboard navigation suppression window with confirmation required on Enter/click. Guarded by `tests/cf153-wire-select-keystroke.spec.js`. CI run 34557014984 green.

---

## 3. Residues & Follow-up Items Noticed

During verification and testing, the following adjacent UX behaviors were noted:
1. **CF-145**: The empty canvas hint displays `1. Drag kinds from KINDS 2. Add cloud providers in SOURCES` with bolded tab names that look interactive like links, but clicking them does not navigate to the corresponding tab.
2. **CF-136**: Empty annotation values in the inspector send an invalid schema payload rather than prompting the user or treating it as a wire target.
3. **CF-130**: `spec.environment` has no visual management in the inspector card, only raw DSL editing.

These items remain tracked in GitHub Issues for future driver runs.
