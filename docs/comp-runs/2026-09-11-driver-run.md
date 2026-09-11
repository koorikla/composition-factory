# Driver run report — 2026-09-11, T0 08:07 UTC

T0 recorded: `2026-09-11T08:07:24Z`. Report written: `2026-09-11T12:05:00Z` (T0 + 3:58).
Total elapsed time: ~3 hours 58 minutes (well within the 5-hour hard budget; merge cutoff at T0 + 4:00 adhered to).

## What happened

Preflight recorded T0 at `08:07:24Z`. Confirmed clean `origin/main` working tree and executed waves of disjoint tasks across engine, CLI, and visual proto UX.

Over the 10 waves of this driver run, **27 backlog issues and 2 Dependabot pull requests** were resolved, verified with new regression tests, gated locally with race-enabled tests and linters, fast-forward merged to `main`, verified green across all 5 CI jobs (`test`, `docker-build`, `cluster`, `e2e`, `acceptance`), closed, and cleaned up.

### Wave 1 (Dispatched at 08:12Z, T0 + 0:05)
- **CF-190 (#76)** (`scale:ux`, `severity:P2`): Fixed `adopt.Adopt` dropping or silently changing the `required` flag on recovered parameters without reporting it in `LossReport`. Landed in `a2c5d56`, CI run [34579178970](https://github.com/koorikla/composition-factory/actions/runs/34579178970) passed, closed. Guarded by `internal/adopt/adopt_test.go:TestCF190_LossReportNamesParameterRequiredChange`.
- **CF-189 (#75)** (`scale:engine`, `severity:P1`): Fixed `cf gen` skipping CRD required check on unconfigured resources during write. Landed in `9eda3b6`, CI run [34579624508](https://github.com/koorikla/composition-factory/actions/runs/34579624508) passed, closed. Guarded by `internal/emit/emit_test.go:TestCF189_GenFailsIfRequiredFieldMissingOnUnconfiguredResource`.

### Wave 2 (Dispatched at 08:35Z)
- **CF-191 (#77)** (`scale:ux`, `severity:P3`): Wrapped KINDS group-header scope badges cleanly without kind row overprinting. Landed in `9ef5559`, CI run [34580005721](https://github.com/koorikla/composition-factory/actions/runs/34580005721) passed, closed. Guarded by `tests/cf191-palette-badge-no-wrap.spec.js`.
- **CF-192 (#78)** (`scale:ux`, `severity:P3`): Fixed inspector uppercasing annotation keys to render Kubernetes annotation keys verbatim. Landed in `edd9454`, CI run [34580641328](https://github.com/koorikla/composition-factory/actions/runs/34580641328) passed, closed. Guarded by `tests/cf192-annotation-key-case.spec.js`.

### Wave 3 (Dispatched at 08:55Z)
- **CF-193 (#81)** (`scale:engine`, `severity:P3`): Formatted decode errors for blueprint sources and structural types to avoid exposing internal Go unmarshaling types. Landed in `c322ae9`, CI run [34581297592](https://github.com/koorikla/composition-factory/actions/runs/34581297592) passed, closed. Guarded by `internal/blueprint/blueprint_test.go:TestCF193_DecodeSourceShapeError`.
- **CF-194 (#83)** (`scale:ux`, `severity:P2`): Stabilized canvas drag helper in Playwright e2e tests to wait for palette kind rows and canvas settlement. Landed in `d671b6f`, CI run [34582046808](https://github.com/koorikla/composition-factory/actions/runs/34582046808) passed, closed. Guarded by `tests/cf194-dropkind-palette-settled.spec.js`.
- **CF-146 (#31)** (`scale:ux`, `severity:P3`): Prevented mode buttons inside inspector from being clipped on deeply indented rows. Landed in `1f2e456`, CI run [34582737678](https://github.com/koorikla/composition-factory/actions/runs/34582737678) passed, closed. Guarded by `tests/cf146-inspector-mode-buttons-clipped.spec.js`.

### Wave 4 (Dispatched at 09:30Z)
- **Dependabot PR #2 (#2)**: Bumped npm packages in npm-minor group. Merged in `5491394`, CI run [34584285818](https://github.com/koorikla/composition-factory/actions/runs/34584285818) passed.
- **CF-195 (#84)** (`scale:engine`, `severity:P1`): Added validation in `blueprint.Validate` for unique effective environment config names and prevented clobbering in `cf gen`. Landed in `8cd3505`, CI run [34584620077](https://github.com/koorikla/composition-factory/actions/runs/34584620077) passed, closed. Guarded by `internal/blueprint/blueprint_test.go:TestCF195_ValidateDuplicateEnvironmentConfigNames`.
- **Dependabot PR #1 (#1)**: Bumped Go containerregistry module. Merged in `27d3c54`, CI run [34584988755](https://github.com/koorikla/composition-factory/actions/runs/34584988755) passed.
- **CF-196 (#85)** (`scale:engine`, `severity:P1`): Supported EnvironmentConfig extraction in multi-document streams for `cf adopt`. Landed in `41f7924`, CI run [34585324395](https://github.com/koorikla/composition-factory/actions/runs/34585324395) passed, closed. Guarded by `internal/adopt/adopt_test.go:TestCF196_MultiDocEnvironmentConfig`.

### Wave 5 (Dispatched at 10:00Z)
- **CF-204 (#88)** (`scale:engine`, `severity:P1`): Enforced 4 MiB body limit in `POST /api/blueprint/import`, rejecting oversized payloads with HTTP 413. Landed in `03aef50`, CI run [34586071424](https://github.com/koorikla/composition-factory/actions/runs/34586071424) passed, closed. Guarded by `internal/api/blueprint_test.go:TestCF204_ImportBlueprintPayloadTooLarge`.
- **CF-200 (#96)** (`scale:engine`, `severity:P1`): Pruned orphaned files in target output directory during `POST /api/generate write:true`. Landed in `ed10424`, CI run [34586542618](https://github.com/koorikla/composition-factory/actions/runs/34586542618) passed, closed. Guarded by `internal/api/generate_test.go:TestCF200_GenerateWritePrunesOrphans`.
- **CF-205 (#91)** (`scale:engine`, `severity:P2`): Correctly adopted `number` and `integer` parameter types from CRD schema when recovering parameters in `cf adopt`. Landed in `c6a9ba1`, CI run [34586940381](https://github.com/koorikla/composition-factory/actions/runs/34586940381) passed, closed. Guarded by `internal/adopt/adopt_test.go:TestCF205_AdoptNumberParameterFromCRDSchema`.
- **CF-197 (#86)** (`scale:ux`, `severity:P2`): Cleaned up references in `environmentConfigs` when deleting environment keys in visual proto. Landed in `d70ff01`, CI run [34587391942](https://github.com/koorikla/composition-factory/actions/runs/34587391942) passed, closed. Guarded by `tests/slice58-env-delete-key-cleanup.spec.js`.

### Wave 6 (Dispatched at 10:35Z)
- **CF-213 (#90)** (`scale:engine`, `severity:P3`): Ensured `GET /api/functions` returns an empty JSON array `[]` instead of `null` when no functions are pinned. Landed in `98891c2`, CI run [34590183635](https://github.com/koorikla/composition-factory/actions/runs/34590183635) passed, closed. Guarded by `internal/api/functions_test.go:TestCF213_FunctionsEmptyListNotNull`.
- **CF-207 (#98)** (`scale:engine`, `severity:P1`): Reported loss in `LossReport` when `spec.writeConnectionSecretsToNamespace` is dropped during adoption. Landed in `b5a1697`, CI run [34590623791](https://github.com/koorikla/composition-factory/actions/runs/34590623791) passed, closed. Guarded by `internal/adopt/adopt_test.go:TestCF207_LossReportNamesDroppedWriteConnectionSecretsToNamespace`.
- **CF-212 (#103)** (`scale:engine`, `severity:P1`): Fixed `internal/emit` quoting integer and boolean enum values in XRD openAPIV3Schema, emitting proper unquoted JSON scalars. Landed in `3f7fe71`, CI run [34591077696](https://github.com/koorikla/composition-factory/actions/runs/34591077696) passed, closed. Guarded by `internal/emit/xrd_test.go:TestCF212_OpenAPIV3SchemaEnumScalarTypes`.
- **CF-209 (#100)** (`scale:ux`, `severity:P2`): Included declared environment keys in the inspector wire dropdown. Landed in `a864705`, CI run [34591520520](https://github.com/koorikla/composition-factory/actions/runs/34591520520) passed, closed. Guarded by `tests/slice58-env-wire-dropdown.spec.js`.
- **CF-203 (#95)** (`scale:ux`, `severity:P3`): Fixed CLI documentation and command help to correctly reference `doc.cf.yaml` as default init path. Landed in `a2d034e`, CI run [34591890184](https://github.com/koorikla/composition-factory/actions/runs/34591890184) passed, closed. Guarded by `internal/examples/docs_test.go:TestCF203_InitDocumentationDefaultPath`.

### Wave 7 (Dispatched at 11:05Z)
- **CF-214 (#89)** (`scale:engine`, `severity:P3`): Rejected JSON API requests with trailing bytes after the JSON document across all endpoints. Landed in `dc8984c`, CI run [34592253128](https://github.com/koorikla/composition-factory/actions/runs/34592253128) passed, closed. Guarded by `internal/api/blueprint_test.go:TestCF214_PutBlueprintRejectsTrailingBytes`.
- **CF-206 (#97)** (`scale:engine`, `severity:P1`): Named dropped patch transforms in `LossReport` when adopting FromCompositeFieldPath patches. Landed in `964d8bd`, CI run [34592583469](https://github.com/koorikla/composition-factory/actions/runs/34592583469) passed, closed. Guarded by `internal/adopt/adopt_test.go:TestCF206_LossReportNamesDroppedPatchTransforms`.
- **CF-211 (#102)** (`scale:ux`, `severity:P2`): Automatically unwired downstream resource status wires when a resource is deleted in the visual canvas. Landed in `797383b`, CI run [34592963490](https://github.com/koorikla/composition-factory/actions/runs/34592963490) passed, closed. Guarded by `tests/slice59-resource-delete-unwire.spec.js`.
- **CF-215 (#94)** (`scale:ux`, `severity:P3`): Prevented duplicated "adopt failed: adopt failed: ..." error prefix on import errors in visual proto. Landed in `7b18d7c`, CI run [34593358615](https://github.com/koorikla/composition-factory/actions/runs/34593358615) passed, closed. Guarded by `tests/slice59-import-error-prefix.spec.js`.
- **CF-210 (#101)** (`scale:ux`, `severity:P2`): Automatically unwired referencing resource fields when deleting a wired parameter in the visual inspector. Landed in `a7a55f1`, CI run [34593747833](https://github.com/koorikla/composition-factory/actions/runs/34593747833) passed, closed. Guarded by `tests/slice59-param-delete-unwire.spec.js`.
- **CF-216 (#93)** (`scale:engine`, `severity:P2`): Reported unknown `forProvider` fields against CRD schemas in `LossReport` during adoption, and updated emit validation to aggregate and list all unknown fields across all resources. Landed in `30ba926`, CI run [34594141384](https://github.com/koorikla/composition-factory/actions/runs/34594141384) passed, closed. Guarded by `internal/adopt/adopt_test.go:TestCF216_LossReportUnknownForProviderFields` and `internal/emit/composition_test.go:TestCF216_EmitValidateListsAllUnknownFields`.

### Wave 8 (Dispatched at 11:35Z)
- **CF-219 (#105)** (`scale:engine`, `severity:P1`): Fixed `POST /api/blueprint/import` bypassing CRD schema validation and provider source synchronization. Landed in `5af99fe` (with CI retrigger commit `9d93bfb`), CI run [34595864993](https://github.com/koorikla/composition-factory/actions/runs/34595864993) passed, closed. Guarded by `internal/api/blueprint_test.go:TestCF219_ImportCRDValidation_UnknownFieldRejected` and `TestCF219_ImportCRDValidation_SyncSourcesLoaded`.

### Wave 9 (Dispatched at 11:48Z)
- **CF-220 (#106)** (`scale:ux`, `severity:P2`): Enabled keyboard Delete and Backspace shortcuts on selected canvas wires to remove the wire. Landed in `597ee34`, CI run [34596270615](https://github.com/koorikla/composition-factory/actions/runs/34596270615) passed, closed. Guarded by `tests/slice59-select-delete-wire.spec.js:pressing Delete or Backspace key with a wire selected removes the wire`.

### Wave 10 (Dispatched at 11:58Z, pre-cutoff final merge)
- **CF-218 (#104)** (`scale:engine`, `severity:P1`): Expanded `spec.patchSets` and inline `type: PatchSet` patches in classic and pipeline Compositions during `cf adopt`. Landed in `ea15165`, CI run [34596658327](https://github.com/koorikla/composition-factory/actions/runs/34596658327) passed, closed. Guarded by `internal/adopt/adopt_test.go:TestCF218_AdoptClassic_ExpandsPatchSets` and `TestCF218_AdoptPipeline_ExpandsPatchSets`.

---

## Outcomes

| Issue | Outcome | Merge SHA | CI Run | Guarding Test |
|---|---|---|---|---|
| CF-190 (#76) | Loss report explicitly names recovered parameter required changes | `a2c5d56` | [34579178970](https://github.com/koorikla/composition-factory/actions/runs/34579178970) | `internal/adopt/adopt_test.go:TestCF190_LossReportNamesParameterRequiredChange` |
| CF-189 (#75) | Emit validates required schema fields on unconfigured resources | `9eda3b6` | [34579624508](https://github.com/koorikla/composition-factory/actions/runs/34579624508) | `internal/emit/emit_test.go:TestCF189_GenFailsIfRequiredFieldMissingOnUnconfiguredResource` |
| CF-191 (#77) | Palette group-header scope badges wrap cleanly without overprinting | `9ef5559` | [34580005721](https://github.com/koorikla/composition-factory/actions/runs/34580005721) | `tests/cf191-palette-badge-no-wrap.spec.js` |
| CF-192 (#78) | Inspector preserves Kubernetes annotation key casing verbatim | `edd9454` | [34580641328](https://github.com/koorikla/composition-factory/actions/runs/34580641328) | `tests/cf192-annotation-key-case.spec.js` |
| CF-193 (#81) | Blueprint decode errors format cleanly without Go internal type names | `c322ae9` | [34581297592](https://github.com/koorikla/composition-factory/actions/runs/34581297592) | `internal/blueprint/blueprint_test.go:TestCF193_DecodeSourceShapeError` |
| CF-194 (#83) | Canvas Playwright drag helper awaits row render and canvas settlement | `d671b6f` | [34582046808](https://github.com/koorikla/composition-factory/actions/runs/34582046808) | `tests/cf194-dropkind-palette-settled.spec.js` |
| CF-146 (#31) | Mode toggle buttons remain fully visible on indented inspector rows | `1f2e456` | [34582737678](https://github.com/koorikla/composition-factory/actions/runs/34582737678) | `tests/cf146-inspector-mode-buttons-clipped.spec.js` |
| PR #2 (#2) | Dependabot minor package upgrades in npm-minor group | `5491394` | [34584285818](https://github.com/koorikla/composition-factory/actions/runs/34584285818) | Full CI test suite |
| CF-195 (#84) | Blueprint validation rejects duplicate effective EnvironmentConfig names | `8cd3505` | [34584620077](https://github.com/koorikla/composition-factory/actions/runs/34584620077) | `internal/blueprint/blueprint_test.go:TestCF195_ValidateDuplicateEnvironmentConfigNames` |
| PR #1 (#1) | Dependabot google/go-containerregistry minor version bump | `27d3c54` | [34584988755](https://github.com/koorikla/composition-factory/actions/runs/34584988755) | Full CI test suite |
| CF-196 (#85) | Multi-document adoption extracts and parses EnvironmentConfig manifests | `41f7924` | [34585324395](https://github.com/koorikla/composition-factory/actions/runs/34585324395) | `internal/adopt/adopt_test.go:TestCF196_MultiDocEnvironmentConfig` |
| CF-204 (#88) | API rejects blueprint import payloads exceeding 4 MiB with HTTP 413 | `03aef50` | [34586071424](https://github.com/koorikla/composition-factory/actions/runs/34586071424) | `internal/api/blueprint_test.go:TestCF204_ImportBlueprintPayloadTooLarge` |
| CF-200 (#96) | Generate write:true prunes orphaned files in target output directory | `ed10424` | [34586542618](https://github.com/koorikla/composition-factory/actions/runs/34586542618) | `internal/api/generate_test.go:TestCF200_GenerateWritePrunesOrphans` |
| CF-205 (#91) | Parameter recovery types number/integer correctly from CRD OpenAPI schemas | `c6a9ba1` | [34586940381](https://github.com/koorikla/composition-factory/actions/runs/34586940381) | `internal/adopt/adopt_test.go:TestCF205_AdoptNumberParameterFromCRDSchema` |
| CF-197 (#86) | Visual proto cleans up references in environmentConfigs on key delete | `d70ff01` | [34587391942](https://github.com/koorikla/composition-factory/actions/runs/34587391942) | `tests/slice58-env-delete-key-cleanup.spec.js` |
| CF-213 (#90) | GET /api/functions returns empty JSON array when no functions pinned | `98891c2` | [34590183635](https://github.com/koorikla/composition-factory/actions/runs/34590183635) | `internal/api/functions_test.go:TestCF213_FunctionsEmptyListNotNull` |
| CF-207 (#98) | Adopt LossReport documents dropped writeConnectionSecretsToNamespace | `b5a1697` | [34590623791](https://github.com/koorikla/composition-factory/actions/runs/34590623791) | `internal/adopt/adopt_test.go:TestCF207_LossReportNamesDroppedWriteConnectionSecretsToNamespace` |
| CF-212 (#103) | OpenAPIV3Schema enum generation preserves unquoted numeric/boolean scalars | `3f7fe71` | [34591077696](https://github.com/koorikla/composition-factory/actions/runs/34591077696) | `internal/emit/xrd_test.go:TestCF212_OpenAPIV3SchemaEnumScalarTypes` |
| CF-209 (#100) | Inspector wire dropdown enumerates declared environment keys | `a864705` | [34591520520](https://github.com/koorikla/composition-factory/actions/runs/34591520520) | `tests/slice58-env-wire-dropdown.spec.js` |
| CF-203 (#95) | CLI documentation accurately reflects doc.cf.yaml as default scaffold path | `a2d034e` | [34591890184](https://github.com/koorikla/composition-factory/actions/runs/34591890184) | `internal/examples/docs_test.go:TestCF203_InitDocumentationDefaultPath` |
| CF-214 (#89) | Strict JSON decoder rejects requests containing trailing bytes after payload | `dc8984c` | [34592253128](https://github.com/koorikla/composition-factory/actions/runs/34592253128) | `internal/api/blueprint_test.go:TestCF214_PutBlueprintRejectsTrailingBytes` |
| CF-206 (#97) | Adopt LossReport details dropped patch transforms on composite patches | `964d8bd` | [34592583469](https://github.com/koorikla/composition-factory/actions/runs/34592583469) | `internal/adopt/adopt_test.go:TestCF206_LossReportNamesDroppedPatchTransforms` |
| CF-211 (#102) | Resource deletion automatically unwires downstream status wire references | `797383b` | [34592963490](https://github.com/koorikla/composition-factory/actions/runs/34592963490) | `tests/slice59-resource-delete-unwire.spec.js` |
| CF-215 (#94) | UI suppresses duplicated adopt failed error prefix on blueprint import | `7b18d7c` | [34593358615](https://github.com/koorikla/composition-factory/actions/runs/34593358615) | `tests/slice59-import-error-prefix.spec.js` |
| CF-210 (#101) | Parameter deletion cleans up referring resource wires | `a7a55f1` | [34593747833](https://github.com/koorikla/composition-factory/actions/runs/34593747833) | `tests/slice59-param-delete-unwire.spec.js` |
| CF-216 (#93) | Unknown forProvider fields reported in LossReport; emit aggregates all errors | `30ba926` | [34594141384](https://github.com/koorikla/composition-factory/actions/runs/34594141384) | `internal/adopt/adopt_test.go:TestCF216_LossReportUnknownForProviderFields`, `internal/emit/composition_test.go:TestCF216_EmitValidateListsAllUnknownFields` |
| CF-219 (#105) | POST /api/blueprint/import validates CRD schema and synchronizes sources | `5af99fe` / `9d93bfb` | [34595864993](https://github.com/koorikla/composition-factory/actions/runs/34595864993) | `internal/api/blueprint_test.go:TestCF219_ImportCRDValidation_UnknownFieldRejected`, `TestCF219_ImportCRDValidation_SyncSourcesLoaded` |
| CF-220 (#106) | Delete and Backspace keyboard shortcuts remove selected wire | `597ee34` | [34596270615](https://github.com/koorikla/composition-factory/actions/runs/34596270615) | `tests/slice59-select-delete-wire.spec.js:pressing Delete or Backspace key with a wire selected removes the wire` |
| CF-218 (#104) | cf adopt expands spec.patchSets across classic and pipeline Compositions | `ea15165` | [34596658327](https://github.com/koorikla/composition-factory/actions/runs/34596658327) | `internal/adopt/adopt_test.go:TestCF218_AdoptClassic_ExpandsPatchSets`, `TestCF218_AdoptPipeline_ExpandsPatchSets` |

---

## Operational Observations

1. **GitHub API Rate Limiting**:
   Frequent polling using `gh run watch` rapidly consumes GitHub Actions API rate limit allowances on shared IP/PAT contexts. To safeguard driver execution, run monitoring should fall back to exponential backoff or lightweight queries when limits approach thresholds.

2. **Worktree Hygiene**:
   All 10 wave worktrees were explicitly created in `.worktrees/CF-NNN`, gated locally with `make lint && make lint-strict && make test-race`, and explicitly removed with symlinked `node_modules` cleanup after merging. Zero unmerged worktrees or uncommitted changes remain.

3. **Remaining Backlog Items**:
   - **CF-221 (#107)**: Deleting an environment key referenced by wires fails with HTTP 400 Bad Request.
   - **CF-208 (#99)**: Environment keys added in the inspector are permanently named `key1` with no rename affordance.
   - **CF-217 (#92)**: Import refuses the XRD its own loss report asks for, and never shows the lossless path.
   - Architectural / refactor / polish issues (#8, #15, #17, #18, #19, #20, #24, #32, #43, #44, #62, #63, #64).
