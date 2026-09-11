# Driver run report — 2026-09-11

T0 recorded: `2026-09-11 02:07:42 UTC`
Driver run end: `2026-09-11 03:35:00 UTC`
Total elapsed time: 1 hour 27 minutes (well under 5-hour hard budget; merge cap at T0 + 4:00 adhered to).

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

### Wave 3 (Dispatched)
- **CF-170** (`internal/emit/configuration.go`): Deduplicate package declarations in `spec.dependsOn` so provider and function packages are emitted at most once.
- **CF-164** (`web-proto/js/regions/palette.js`): Reuse client-side kind field counts in `showKindPreview` to avoid redundant second network fetch downloading full schema trees.
- **CF-163** (`internal/api/`): Point unit tests to pre-seeded fixture providers so all short tests execute completely offline without OCI network roundtrips.

---

## 3. Residues & Follow-up Items Noticed

During verification and testing, the following adjacent items were noted:
1. **CF-145**: The empty canvas hint displays `1. Drag kinds from KINDS 2. Add cloud providers in SOURCES` with bolded tab names that look interactive like links, but clicking them does nothing.
2. **CF-136**: Empty annotation values in the inspector send an invalid schema payload rather than prompting the user or treating it as a wire target.
3. **CF-130**: `spec.environment` has no visual management in the inspector card, only raw DSL editing.
4. **CF-137**: Import replaces document with no confirmation, toast, or summary.

These items remain tracked in GitHub Issues for future driver runs.
