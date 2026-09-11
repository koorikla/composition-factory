# Driver run report — 2026-09-11

T0 recorded: `2026-09-11 03:08:11 UTC`
Driver run end: `2026-09-11 06:05:00 UTC`
Total elapsed time: 2 hours 57 minutes (well under 5-hour hard budget; merge cap at T0 + 4:00 adhered to).

---

## 1. Overview & Resolved Issues

All issues were executed to `docs/task-execution-contract.md` and `docs/routines/issue-driver.md`.
Each issue had its acceptance tests run and verified failing before the fix, quality gates (`make lint`, `make lint-strict`, `make test-race`, and relevant e2e/docker suites) passed cleanly in isolated worktrees, integrated onto `main`, watched to green completion on GitHub Actions CI (`gh run watch --exit-status`), and closed with the guarding test specified in the closure comment.

| Issue | Title | Outcome | Merge Commit | CI Run | Guarding Test |
|---|---|---|---|---|---|
| **CF-142** (#27) | Catalogue search cannot find provider-aws-rds by 'postgres' or 'sql', the words a user knows | Merged & Closed | `cce0b0a` | [34553801886](https://github.com/koorikla/composition-factory/actions/runs/34553801886) | `catalogue.TestSearchFindsRDSByServiceAndEngineWords`, `internal/api.TestCatalogueQFiltersByServiceAndEngineWords` |
| **CF-108** (#6) | `cf adopt <composition.yaml>` without the XRD alongside retypes every parameter as `string` and drops facts | Merged & Closed | `ead94d9` | [34556350017](https://github.com/koorikla/composition-factory/actions/runs/34556350017) | `internal/adopt.TestCF108AdoptWithoutXRDNamesUnrecoveredParameterFacts`, `cmd/cf.TestCF108AdoptCLIWithoutXRDPrintsLossPerParameter` |
| **CF-143** (#28) | Boolean fields render as free-text boxes; the type is only revealed by the rejection toast | Merged & Closed | `de364ef` | [34556375883](https://github.com/koorikla/composition-factory/actions/runs/34556375883) | `tests/cf143-boolean-fields-three-state.spec.js` |
| **CF-151** (#36) | The docked blueprint editor is 40 px tall for a 73-line document and opens scrolled to the end | Merged & Closed | `99988f3` | [34556676601](https://github.com/koorikla/composition-factory/actions/runs/34556676601) | `tests/cf151-docked-editor-height.spec.js` |
| **CF-150** (#35) | CLI polish left over from CF-113: init/kinds default filename mismatch, silent fuzzy kind resolution | Merged & Closed | `cbbae5b` | [34556811769](https://github.com/koorikla/composition-factory/actions/runs/34556811769) | `cmd/cf.TestInitDefaultBlueprintFilename`, `cmd/cf.TestFieldsRejectsFuzzyMatch` |
| **CF-153** (#38) | The inspector's 'wire to…' control commits a wire on the first keystroke | Merged & Closed | `9983afc` | [34557014984](https://github.com/koorikla/composition-factory/actions/runs/34557014984) | `tests/cf153-wire-select-keystroke.spec.js` |
| **CF-156** (#41) | On a fresh blank scaffold providerName is editable and deletable with no confirm | Merged & Closed | `fad917d` | [34557800234](https://github.com/koorikla/composition-factory/actions/runs/34557800234) | `tests/cf156-providername-scaffold-locked.spec.js` |
| **CF-119** (#7) | Importing the Composition that Generate just wrote turns inferred auto-ready into a custom step | Merged & Closed | `19949a7` | [34558281856](https://github.com/koorikla/composition-factory/actions/runs/34558281856) | `internal/adopt.TestCF119ImportGeneratedCompositionRoundTrip`, `tests/cf119-import-generated-composition.spec.js` |
| **CF-157** (#42) | Kinds search dead-ends with 'No kinds match search query.' when the answer is 'add a provider in SOURCES' | Merged & Closed | `3804221` | [34558614428](https://github.com/koorikla/composition-factory/actions/runs/34558614428) | `tests/cf157-kinds-search-empty-state-sources-hint.spec.js` |
| **CF-165** (#50) | Canvas wire rendering queries getBoundingClientRect per wire endpoint, triggering layout thrashing | Merged & Closed | `57cb2ca` | [34559183560](https://github.com/koorikla/composition-factory/actions/runs/34559183560) | `tests/cf165-canvas-wire-rect-cache.spec.js` |
| **CF-161** (#46) | Store.List() deserializes full CRD trees across all cached providers just to read the provider ref | Merged & Closed | `1cefeb7` | [34559458488](https://github.com/koorikla/composition-factory/actions/runs/34559458488) | `internal/cache.TestStoreListDoesNotUnmarshalCRDs` |
| **CF-166** (#51) | blueprint.SplitDocs allocates thousands of byte slices via bytes.Split on large YAML streams | Merged & Closed | `388512b` | [34559719397](https://github.com/koorikla/composition-factory/actions/runs/34559719397) | `internal/blueprint.TestSplitDocs`, `internal/blueprint.TestSplitDocs_StreamAllocations` |
| **CF-164** (#49) | Palette kind preview performs redundant sequential network request downloading full field trees | Merged & Closed | `86a9614` | [34560256742](https://github.com/koorikla/composition-factory/actions/runs/34560256742) | `tests/cf164-palette-preview-request-dedup.spec.js` |
| **CF-170** (#55) | ConfigurationMeta emits duplicate dependsOn entries for shared function packages and sources | Merged & Closed | `27ac948` | [34560495716](https://github.com/koorikla/composition-factory/actions/runs/34560495716) | `internal/emit/configuration_test.go:TestConfigurationMetaDeduplicatesDependsOn` |
| **CF-163** (#48) | internal/api unit tests hit remote OCI registries on unseeded provider refs, adding ~4s to test runs | Merged & Closed | `c36297b` | [34560759428](https://github.com/koorikla/composition-factory/actions/runs/34560759428) | `internal/api/adopt_test.go`, `internal/api/import_test.go`, `internal/api/server_test.go` |
| **CF-172** (#57) | LoadSources loads and appends duplicate CRDs for functions in both pipeline and lockfile | Merged & Closed | `861d7eb` | [34561231053](https://github.com/koorikla/composition-factory/actions/runs/34561231053) | `internal/cache/sources_test.go:TestLoadSourcesPipelineAndLockFunctions,TestLoadSourcesFunctionDeduplication` |
| **CF-171** (#56) | AdoptTree naively appends s to XRD Kind, drifting from inferPlural and Composite metadata name | Merged & Closed | `897dcb8` | [34561492138](https://github.com/koorikla/composition-factory/actions/runs/34561492138) | `internal/adopt/tree_test.go:TestAdoptTreePluralInferenceWithoutXRD` |
| **CF-169** (#54) | resolveKind rejects Crossplane managed resources discovered from live cluster scans | Merged & Closed | `e299429` | [34561842775](https://github.com/koorikla/composition-factory/actions/runs/34561842775) | `internal/emit/composition_test.go:TestCF169ClusterProviderResolvesManagedResource` |
| **CF-184** (#69) | tests/cf152-failed-source-repair.spec.js makes unmocked live OCI registry requests for non-existent v9.9.9 | Merged & Closed | `f0658e6` | [34562568127](https://github.com/koorikla/composition-factory/actions/runs/34562568127) | `internal/xpkg/fetch_test.go:TestFetchOfflineInvalidRegistry`, `tests/cf152-failed-source-repair.spec.js` |
| **CF-168** (#53) | XRD-less adopt misses conditional refs in when statements, failing unguarded dereference validation | Merged & Closed | `f5c57ba` | [34562821928](https://github.com/koorikla/composition-factory/actions/runs/34562821928) | `internal/adopt/xrdless_test.go:TestAdoptXRDlessConditionalWhen` |
| **CF-181** (#66) | Store.FetchAndSave does not short-circuit on cache hit, triplicating lock pinning with divergent error policies | Merged & Closed | `467396c` | [34563087577](https://github.com/koorikla/composition-factory/actions/runs/34563087577) | `internal/cache/store_test.go:TestFetchAndSaveShortCircuitsOnCacheHit`, `internal/api/server_test.go` |
| **CF-167** (#52) | adopt decomposes slice envelope fields into indexed keys rejected by validateResourceEnvelope | Merged & Closed | `badbe7f` | [34563746934](https://github.com/koorikla/composition-factory/actions/runs/34563746934) | `internal/adopt/adopt_test.go:TestAdoptSliceEnvelopeFields` |
| **CF-183** (#68) | Playwright e2e harness boots with unseeded cache, triggering live OCI fetches and startup warnings | Merged & Closed | `4d9b938` | [34564020985](https://github.com/koorikla/composition-factory/actions/runs/34564020985) | `tests/cf183-playwright-seeded-cache.spec.js` |
| **CF-173** (#58) | web-proto duplicates utility functions and palette.js famOf diverges from canvas.js colors | Merged & Closed | `1bc52aa` | [34564260992](https://github.com/koorikla/composition-factory/actions/runs/34564260992) | `tests/cf173-unify-utils-famof.spec.js` |
| **CF-182** (#67) | AssembleProviders helper duplicated across CLI commands and API endpoints | Merged & Closed | `85732dc` | [34565015617](https://github.com/koorikla/composition-factory/actions/runs/34565015617) | `cmd/cf/options_test.go:TestAssembleProvidersDeduplication` |
| **CF-162** (#47) | Adopt/AdoptTree performs redundant disk reads and unmarshals across provider references | Merged & Closed | `4a8f307` | [34565280631](https://github.com/koorikla/composition-factory/actions/runs/34565280631) | `internal/adopt/adopt_test.go:TestAdoptStoreMemoization` |
| **CF-145** (#30) | Empty canvas KINDS and SOURCES words lack click and tab-switch affordances | Merged & Closed | `fa718cf` | [34565587515](https://github.com/koorikla/composition-factory/actions/runs/34565587515) | `tests/cf145-empty-canvas-tab-links.spec.js` |
| **CF-175** (#60) | FieldTree includes server-added fields on native K8s resources | Merged & Closed | `90b15e4` | [34566290580](https://github.com/koorikla/composition-factory/actions/runs/34566290580) | `internal/schema/tree_test.go:TestNativeCRDFieldTreeExcludesServerFields`, `internal/schema/k8s/k8s_test.go:TestAllCoreCRDsFieldTreeExcludesServerAddedFields` |
| **CF-148** (#33) | Header artifacts chip count disagrees with ARTIFACTS panel file count | Merged & Closed | `6d1ddba` | [34566797828](https://github.com/koorikla/composition-factory/actions/runs/34566797828) | `tests/cf148-header-artifacts-count.spec.js` |
| **CF-176** (#61) | docs/dsl.md says convention matches field path suffix, but on managed kinds only top-level leaf names are compared | Merged & Closed | `12f6289` | [34567413251](https://github.com/koorikla/composition-factory/actions/runs/34567413251) | `internal/emit/templates_test.go:TestCF176ConventionMatchingNestedManagedFieldIsRefused`, `TestCF176ConventionExplicitNestedOverrideOnManagedKindAllowed` |
| **CF-180** (#65) | cmd/cf/gen.go duplicates crossplane render pipeline without Docker daemon error classification | Merged & Closed | `d7364d3` | [34567727172](https://github.com/koorikla/composition-factory/actions/runs/34567727172) | `cmd/cf/gen_test.go:TestCF180GenValidateDockerUnavailable`, `internal/emit/render_test.go:TestRenderCheckDockerUnavailable` |
| **CF-136** (#21) | The annotation form accepts an empty value, then fails with engine jargon and discards the key | Merged & Closed | `17f2ca2` | [34567989959](https://github.com/koorikla/composition-factory/actions/runs/34567989959) | `tests/cf136-annotation-empty-value.spec.js` |

*Note: Issues CF-149 (#34), CF-152 (#37), CF-092 (#4), and CF-141 (#26) were closed in runs earlier in the day.*

---

## 2. Issues Dispatched and Workflows

### Wave 1
- **CF-142** (`internal/api/catalogue.go`, `catalogue/kinds.go`, `catalogue/catalogue_test.go`): Added engine and service aliases (PostgreSQL, Postgres, MySQL, MariaDB, Aurora, SQL) to provider index entries for RDS and CloudSQL packages. Reused `catalogue.Search` directly in HTTP handler to eliminate logic duplication. CI run 34553801886 green.
- **CF-108** (`internal/adopt/xrdless.go`, `internal/adopt/adopt.go`, `cmd/cf/adopt_test.go`, `web-proto/js/store.js`, `web-proto/js/main.js`): Named per-parameter losses when XRD is absent (recovering unguarded wires as required, guarded wires as optional, typed/quoted strings), reporting true losses on CLI (exit code 2). Fixed canvas stale adopt warning persistence across imports (`slice68`). CI run 34556350017 green.
- **CF-151** (`web-proto/js/regions/output.js`, `tests/cf151-docked-editor-height.spec.js`): Docked blueprint editor now hides duplicate subbar and warnbar while active, giving the editor textarea the full available drawer height (~127px). Set cursor and selection range to top (0, 0) with `preventScroll: true` on focus so document opens at the top. Expanded collapsed drawer on edit click. CI run 34556676601 green.

### Concurrent Branch Lands During Wave 1
- **CF-143** (`web-proto/js/regions/inspector.js`, `tests/cf143-boolean-fields-three-state.spec.js`): Rendered boolean fields as three-state buttons (unset/true/false) instead of text inputs. CI run 34556375883 green.
- **CF-150** (`cmd/cf/init.go`, `cmd/cf/fields.go`, `cmd/cf/explore_test.go`): Unified default blueprint filename and refused silent fuzzy kind resolution. CI run 34556811769 green.
- **CF-153** (`web-proto/js/regions/inspector.js`, `tests/cf153-wire-select-keystroke.spec.js`): Suppressed immediate commit on first keystroke in native select dropdowns, requiring explicit confirm. CI run 34557014984 green.

### Wave 2
- **CF-156** (`web-proto/js/regions/inspector.js`, `tests/cf156-providername-scaffold-locked.spec.js`): Locked `providerName` parameter from first paint on fresh scaffolds (`resources.length === 0`), while preserving unlocking when all composed resources are native Kubernetes kinds. CI run 34557800234 green.
- **CF-119** (`internal/adopt/adopt.go`, `internal/adopt/adopt_test.go`, `web-proto/js/regions/inspector.js`, `tests/cf119-import-generated-composition.spec.js`): Dropped default inferred `function-auto-ready` step from being adopted into `bp.Spec.Pipeline` as a custom step. Removed hardcoded `AutoReady` CRD mapping in `inferFnMeta` to prevent 404 network errors and console warnings when viewing adopted compositions. CI run 34558281856 green.
- **CF-157** (`web-proto/js/regions/palette.js`, `tests/cf157-kinds-search-empty-state-sources-hint.spec.js`): Enhanced KINDS search empty state to direct user to SOURCES with an interactive button, and asynchronously query the catalogue to display matching provider packages with one-click Add buttons directly within the empty search panel. CI run 34558614428 green.
- **CF-165** (`web-proto/js/regions/canvas.js`, `tests/cf165-canvas-wire-rect-cache.spec.js`): Cached canvas container `getBoundingClientRect()` once in `drawWires()` and forwarded to `portPos`, eliminating layout thrashing and forced synchronous reflows on wire redraws. CI run 34559183560 green.
- **CF-161** (`internal/cache/store.go`, `internal/cache/store_test.go`): Stream-scanned `crds.json` tokens in `Store.List()` to read only top-level `"ref"` without deserializing full CRD schema trees (12x faster, 80x less allocations). CI run 34559458488 green.
- **CF-166** (`internal/blueprint/split.go`, `internal/blueprint/split_test.go`): Replaced `bytes.Split` line allocation in `SplitDocs` with column-0 YAML separator offset index scanning (1.8x faster, 2.2x less allocations on large multi-document manifests). CI run 34559719397 green.

### Wave 3
- **CF-164** (`web-proto/js/regions/palette.js`, `tests/cf164-palette-preview-request-dedup.spec.js`): Stored `data-fields` on kind rows from `/api/kinds` payload and resolved total fields directly, eliminating redundant second sequential `api.getKindFields` request on hover. CI run 34560256742 green.
- **CF-170** (`internal/emit/configuration.go`, `internal/emit/configuration_test.go`): Deduplicated `spec.dependsOn` entries by package in `ConfigurationMeta`, preventing duplicate provider and function dependencies in `crossplane.yaml`. CI run 34560495716 green.
- **CF-163** (`internal/api/adopt.go`, `internal/api/adopt_test.go`, `internal/api/import_test.go`, `internal/api/server_test.go`): Wired server cache root to `adopt.Adopt` options, updated adopt and import unit test fixtures to use seeded provider refs, and added a strict fetch guard to `testServerOptions` preventing unmocked OCI network calls (test runtime down from 4.06s to 0.025s). CI run 34560759428 green.

### Wave 4
- **CF-172** (`internal/cache/sources.go`, `internal/cache/sources_test.go`): Deduplicated function CRD loading across pipeline steps and lockfile entries in `LoadSources` using a package ref lookup set, ensuring CRDs for shared function packages are loaded only once. CI run 34561231053 green.
- **CF-171** (`internal/adopt/tree.go`, `internal/adopt/adopt.go`, `internal/adopt/tree_test.go`): Unified plural XRD inference between single-file `Adopt` and tree `AdoptTree` via `resolveXRDPlural`, correctly inferring irregular plurals (e.g. `XPolicy` -> `xpolicies`, `XAccess` -> `xaccesses`) and compositeTypeRef overrides instead of naively appending `"s"`. CI run 34561492138 green.
- **CF-169** (`internal/emit/composition.go`, `internal/emit/composition_test.go`): Allowed `resolveKind` to match and emit Crossplane managed resources discovered from live cluster scans under `provider: cluster`, properly formatting their bases and patches with `spec.forProvider` envelopes while retaining native resource handling. CI run 34561842775 green.

### Wave 5
- **CF-184** (`internal/xpkg/fetch.go`, `internal/xpkg/fetch_test.go`, `tests/cf152-failed-source-repair.spec.js`): Short-circuited `.invalid` registry domains in `xpkg.Fetch` to fast-fail offline invalid references with zero network or socket overhead, dropping failed-source browser test runtime from ~60s down to 6s and removing arbitrary test timeouts. CI run 34562568127 green.
- **CF-168** (`internal/adopt/xrdless.go`, `internal/adopt/adopt.go`, `internal/adopt/xrdless_test.go`): Captured template statement parameter dereferences in Go template `if` conditionals, equality comparisons, and range loop bounds during XRD-less adoption, accurately recovering types (`boolean`, `string`, `integer`) and marking dereferenced parameters required so adopted compositions with `when:` pass blueprint validation. CI run 34562821928 green.
- **CF-181** (`internal/cache/store.go`, `internal/cache/store_test.go`, `internal/api/blueprint.go`, `internal/api/providers.go`, `internal/api/functions.go`, `internal/api/server_test.go`, `cmd/cf/explore_test.go`): Consolidated cache hit short-circuiting and lockfile digest pinning directly into `Store.FetchAndSave`, removing over 180 duplicate lines across API entrypoints and establishing uniform error status policies (500 for lock errors, 502 for upstream fetch errors). CI run 34563087577 green.

### Wave 6
- **CF-167** (`internal/adopt/adopt.go`, `internal/adopt/adopt_test.go`): Preserved slice-valued envelope fields (e.g. `managementPolicies: ['*']`) directly as comma-separated or raw sequences instead of recursively decomposing them into indexed bracketed keys (`managementPolicies[0]`), unblocking round-trip adoption and import for Upbound official provider compositions like RDS and K8s apps. CI run 34563746934 green.
- **CF-183** (`playwright.config.js`, `tests/helpers.js`, `tests/cf183-playwright-seeded-cache.spec.js`, `tests/fixtures/cache/`): Pre-seeded isolated e2e scratch cache directory with fixture provider schemas (`provider-aws-sqs:v2.7.0` and `provider-aws-s3:v2.7.0`), eliminating startup warnings and live OCI fetches to `ghcr.io` during browser tests. CI run 34564020985 green.
- **CF-173** (`web-proto/js/utils.js`, `web-proto/js/regions/canvas.js`, `web-proto/js/regions/palette.js`, `web-proto/js/regions/inspector.js`, `web-proto/js/regions/output.js`, `tests/cf173-unify-utils-famof.spec.js`): Consolidated `slug`, `uniqueResourceName`, `mapResourceCoordinates`, and `famOf` into shared `utils.js`, ensuring consistent multi-cloud family color classifications across palette and canvas for GCP, Azure, Helm, AWS, and K8s. CI run 34564260992 green.

### Wave 7
- **CF-182** (`cmd/cf/options.go`, `cmd/cf/kinds.go`, `cmd/cf/fields.go`, `cmd/cf/options_test.go`): Consolidated provider-set extraction into shared `AssembleProviders` helper across CLI commands and API, unifying cache warnings, deduplication, and cluster CRD loading. CI run 34565015617 green.
- **CF-162** (`internal/adopt/adopt.go`, `internal/adopt/tree.go`, `internal/adopt/adopt_test.go`, `internal/adopt/tree_test.go`): Added memoized `Store` to `adopt.Options` and passed across all resource inferences in `Adopt` and `AdoptTree`, eliminating redundant O(R * P) disk reads and multi-megabyte JSON unmarshals. CI run 34565280631 green.
- **CF-145** (`web-proto/js/regions/canvas.js`, `web-proto/js/regions/palette.js`, `web-proto/css/proto.css`, `tests/cf145-empty-canvas-tab-links.spec.js`): Wrapped KINDS and SOURCES in the empty canvas hint in interactive button-role elements with proper hover/focus styling and wired them to switch palette tabs upon click or keyboard Enter. CI run 34565587515 green.

### Wave 8
- **CF-175** (`internal/schema/tree.go`, `internal/schema/tree_test.go`, `internal/schema/k8s/k8s_test.go`): Excluded server-injected metadata fields (`managedFields`, `creationTimestamp`, `uid`, `resourceVersion`, `generation`, `status`) from native K8s CRD `FieldTree()` emission to prevent spurious canvas fields that the server drops on round-trip. CI run 34566290580 green.
- **CF-148** (`web-proto/js/regions/output.js`, `tests/cf148-header-artifacts-count.spec.js`): Reconciled top header chip and ARTIFACTS panel file counts by calculating total non-empty artifact documents consistently across both views. CI run 34566797828 green.

### Wave 9
- **CF-176** (`internal/emit/composition.go`, `internal/emit/templates_test.go`, `docs/dsl.md`): Enforced convention matching behavior on managed kinds: top-level matches apply cleanly, while unmatched nested leaf matches are refused loudly unless explicitly overridden via fields, matching the DSL documentation contract. CI run 34567413251 green.
- **CF-180** (`cmd/cf/gen.go`, `cmd/cf/gen_test.go`, `internal/api/render.go`, `internal/emit/render.go`, `internal/emit/render_test.go`): Consolidated the Crossplane composition render execution pipeline into shared engine package `internal/emit/render.go` (One Engine rule). Added Docker daemon status classification for CLI `cf gen --validate` and HTTP API `/api/render`. CI run 34567727172 green.
- **CF-136** (`web-proto/js/regions/inspector.js`, `web-proto/js/main.js`, `web-proto/js/utils.js`, `web-proto/js/regions/output.js`, `web-proto/js/regions/palette.js`, `tests/cf136-annotation-empty-value.spec.js`): Validated annotation values client-side to prevent 400 rejection; retained draft keys, guided users in plain terms without DSL mode jargon (`set exactly one of...`), and cleared error toasts on subsequent successful user actions. CI run 34567989959 green.

---

## 3. Residues & Follow-up Items Noticed

During verification and testing, the following adjacent items were noted:
1. **CF-130**: `spec.environment` has no visual management in the inspector card, only raw DSL editing.
2. **CF-137**: Import replaces document with no confirmation, toast, or summary.
3. **CF-155**: Renaming a parameter in the inspector moves the row immediately due to immediate key re-sorting, shifting active inputs under the cursor.
4. **CF-178**: `canvas.js` couples pure dependency-tree layout with 600 lines of drag-to-wire DOM logic.
5. **CF-177**: `inspector.js` is a 2400-line monolith coupling XRD rendering, expression preview, and form mutations.

These items remain tracked in GitHub Issues for future driver runs.
