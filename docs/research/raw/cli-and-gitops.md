# CLI + GitOps Ergonomics Brief — Crossplane Composition/XRD Generator

## Decisions this enables

1. **Use kong, not cobra.** Verified: `github.com/crossplane/cli/v2` go.mod requires `github.com/alecthomas/kong v1.16.1` + `github.com/willabides/kongplete v0.4.0`, go 1.26.0; cobra appears only as an indirect dep. Syncthing pins the *identical* kong+kongplete versions. Matching the Crossplane CLI's own framework means the tool's `--help` and completions feel native next to `crossplane`.
2. **Ship one binary with `serve` as a kong `default:"1"` command and a `//go:embed all:dist` UI behind a `no_ui` build tag** — PocketBase's exact pattern (`ui/embed.go` / `ui/embed_no_ui.go`). Gives an in-cluster web UI *and* a 0-dependency CI binary from one artifact.
3. **The blueprint is the source of truth; Composition+XRD are build outputs. Do not attempt to parse go-templates back into a graph.** I parsed the user's real template's AST — the recoverable structure is template nodes over *non-YAML text fragments*, not a resource graph. Offer lossless "adopt" (opaque `rawTemplate`) instead.
4. **`generate` must be pure: no Docker, no cluster.** Verified both offline (`crossplane dependency add <xpkg> --api-only` → 39 JSON schemas, digest-pinned lock, 10.8 s, zero cluster calls) and cluster (`/openapi/v3/apis/sqs.aws.m.upbound.io/v1beta1`, 283,877 bytes) schema sources exist. Keep Docker confined to `render`/`test`.
5. **Emit one document per file into `crossplane/xrds/<kind>.yaml` and `crossplane/compositions/<variant>/<kind>.yaml`, with no `kustomization.yaml` and no `argocd.argoproj.io/tracking-id`.** Verified against the live repo: `sourceType: Directory`, `directory.recurse: true`, code search for `kustomization.yaml` → **0 hits**, and the tracking-id is **absent from git** (ArgoCD v3.5.1 injects it; `application.resourceTrackingMethod: annotation`).

---

## 0. The competitive baseline — what already ships, and where it fails

This is the most important context for scoping, and it is all **verified by running it**.

`crossplane` v2.5.0 already has `xrd generate` and `composition generate` (both `[BETA]`).

### `crossplane composition generate` produces a stub

Ran against the user's real XRD (exported from the cluster) inside a scaffolded project:

```
$ crossplane composition generate ./apis/definition.yaml
Ensuring function-auto-ready dependency...
✓ Ensuring function-auto-ready dependency
Writing Composition...
✓ Writing Composition
```

Output — the **entire** file, at `apis/xqueues/composition.yaml`:

```yaml
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xqueues.platform.sparky.ee
spec:
  compositeTypeRef:
    apiVersion: platform.sparky.ee/v1alpha1
    kind: XQueue
  mode: Pipeline
  pipeline:
  - functionRef:
      name: crossplane-contrib-function-auto-ready
    step: crossplane-contrib-function-auto-ready
```

Three concrete gaps:
- **Zero composed resources.** No go-templating step at all. Everything the user actually cares about is unimplemented.
- **`functionRef.name: crossplane-contrib-function-auto-ready` does not match this cluster.** Verified installed Function objects are named `function-auto-ready` and `function-go-templating` (packages `xpkg.upbound.io/crossplane-contrib/…:v0.5.0` / `:v0.12.0`). Applying the generated file would dangle the ref. **Your tool must resolve `functionRef.name` against live `Function` objects, or take it as explicit blueprint input — never guess from the package name.**
- **It mutates `crossplane-project.yaml`** to append a dependency, and pulls `xpkg.crossplane.io/crossplane-contrib/function-auto-ready:v0.7.0` — a *different registry and version* than what is installed.

### `crossplane xrd generate` is lossy

Ran in an isolated project on an example XR, then compared to the hand-written XRD in git:

| Property | Hand-written (git) | `xrd generate` output |
|---|---|---|
| `scope` | `Namespaced` | **`Cluster`** (hardcoded) |
| `location` | `enum: [EU, US]` | `type: string` — enum lost |
| `maxMessageSize` | `minimum: 1024` | `type: integer` — bound lost |
| `visibilityTimeoutSeconds` | `minimum: 0` | dropped entirely |
| `required` | `[location, providerName]` | absent |
| `tags` | `additionalProperties: {type: string}` (open map) | `properties: {env: {type: string}}` — **map inferred as a struct from one sample** |
| `additionalPrinterColumns` | `LOCATION` | absent |

The `scope: Cluster` default alone is disqualifying for a Crossplane v2 / no-claims shop. **Your differentiator is not "generate an XRD" — it is "generate a *correct, constrained, namespaced* XRD plus a real go-templating Composition, from provider schemas."**

Also verified: `crossplane render` exists as a hidden top-level alias for `crossplane composition render`. There is no top-level `crossplane validate` in v2.5.0.

### Non-obvious constraint: it's all project-scoped

Both generators hard-require `crossplane-project.yaml`:

```
crossplane: error: failed to read project file "crossplane-project.yaml": open …: no such file or directory
```

`crossplane project init` scaffolds `apis/ examples/ functions/ operations/ tests/` + `crossplane-project.yaml` (`apiVersion: dev.crossplane.io/v1alpha1`, `kind: Project`). **The user's GitOps repo is not and should not become a Crossplane Project** — its layout is `crossplane/{namespaces,providers,providers-config,functions,compositions,xrds,xrs}`. Your tool must work in a plain directory. Make project-awareness opt-in, not mandatory.

---

## 1. CLI framework: kong

| | kong | cobra | urfave/cli |
|---|---|---|---|
| Stars (verified) | 3,162 | 44,524 | 24,206 |
| Model | struct tags → grammar | imperative `&cobra.Command{}` + pflag | imperative `&cli.Command{}` |
| Used by | **crossplane/cli v1.16.1**, syncthing v1.16.1, Grafana Alloy | kubectl, helm, pocketbase, argocd | gitea (`urfave/cli/v3 v3.11.0`) |
| DI | `Bind`/`BindTo`/`BindToProvider` → `Run(deps…)` | manual closures / globals | `cli.Context` bag |
| Completions | `willabides/kongplete` (crossplane + syncthing both use v0.4.0) | built-in | built-in |

**Recommend kong.** Rationale, in priority order:

1. **Ecosystem match.** Verified from `cmd/crossplane/main.go`:
   ```go
   parser := kong.Must(&cli{},
       kong.Name("crossplane"),
       kong.BindTo(logger, (*logging.Logger)(nil)),
       kong.BindTo(configcmd.ConfigPath(cfgPath), (*configcmd.ConfigPath)(nil)),
       kong.Bind(cfg),
       kong.Help(helpPrinter),
       kong.UsageOnError())
   ```
   Contributors who know `crossplane` will read your `cmd/` without a context switch, and you can lift its help-rendering conventions (`help.md`, `cmd/docs-templates/`).

2. **The struct grammar *is* your config schema.** A generator has a wide flag surface (`--out-dir`, `--layout`, `--check`, `--variant`, `--from-cluster`…). Kong's `enum:"argocd,flat,project"`, `env:"FACTORY_OUT_DIR"`, `required:""`, and `embed:"" prefix:"…"` give validation, env fallback, and `--help` from one declaration. Cobra needs a `RegisterFlags` + `viper.BindPFlag` + manual validation triad for the same result.

3. **`default:"1"` gives you the two-front-doors ergonomics for free.** `crossplane-factory` with no args → launches `serve`. `default:"withargs"` if you want `crossplane-factory ./blueprint.yaml` to imply `generate`. Cobra has no clean equivalent.

4. **`BindTo` is how you share one core between the HTTP handlers and the CLI** without globals — the same `*factory.Engine` instance is bound once and injected into every `Run(ctx *kong.Context, e *factory.Engine) error`.

Counter-argument, stated honestly: cobra's `GenMarkdownTree` / `GenBashCompletion` ecosystem is richer, and cobra is what most contributors have seen. Kong's answers are `kong.Model` reflection for docs (crossplane uses this — `cmd/crossplane/docs.go` + `docs-templates/`) and kongplete for completions. **Not a blocker; kong wins on ecosystem fit.**

Do **not** pick urfave/cli. Gitea is on it (v3.11.0) and it is perfectly serviceable, but it is a third convention in a Crossplane-adjacent tool with no offsetting benefit.

---

## 2. "One artifact, two front doors"

### Example A — PocketBase (60,849 stars, Go, cobra) — **copy this one**

The cleanest small-tool implementation of exactly your shape.

```
pocketbase/
  cmd/serve.go  cmd/superuser.go       # CLI subcommands
  apis/         base.go, middlewares.go, record_crud.go, …   # HTTP API — the shared surface
  core/                                # domain
  ui/  embed.go  embed_no_ui.go  dist/  src/  vite.config.js
  .goreleaser.yaml
```

The embedding, verbatim:

```go
// ui/embed.go
//go:build !no_ui

package ui

import ("embed"; "io/fs")

//go:embed all:dist
var distDir embed.FS

// DistDirFS contains the embedded dist directory files (without the "dist" prefix)
var DistDirFS, _ = fs.Sub(distDir, "dist")
```
```go
// ui/embed_no_ui.go
//go:build no_ui

package ui

import "io/fs"

// DistDirFS is deliberately not set to prevent bundling the UI with the binary.
var DistDirFS fs.FS
```

Three things to steal:
- **`all:dist`** — the `all:` prefix is required, or `go:embed` silently skips files beginning with `_` or `.` (Vite emits `_app/`-style names and dotfiles).
- **`fs.Sub`** strips the `dist` prefix so the HTTP handler mounts at `/`.
- **The `no_ui` build tag** gives a second artifact from one tree: `go build -tags no_ui` yields a small pure-CLI binary for CI images, while the default build carries the UI. Ship both from goreleaser as separate build IDs.

### Example B — Syncthing (88,048 stars, Go, kong v1.16.1) — steal the dev-mode escape hatch

```
cmd/syncthing/main.go  cli/  generate/  decrypt/     # kong CLI, subcommands
lib/api/  api.go  api_statics.go  auto/  confighandler.go
```

`lib/api/api_statics.go`:
```go
type staticsServer struct {
    assetDir        string
    assets          map[string]assets.Asset
    ...
}
func newStaticsServer(theme, assetDir string) *staticsServer {
    s := &staticsServer{assetDir: assetDir, assets: auto.Assets(), …}
```

The `assetDir` field overrides the compiled-in `auto.Assets()` at runtime (env `STGUIASSETS`). **Add this: `--ui-dir ./web/dist`.** Without it, every frontend change during development requires a Go rebuild — the single biggest friction cost of the embedded-asset pattern.

`lib/api/auto/` contains only `.gitignore`, `doc.go`, `noassets.go` — the asset file is *generated at build time*, not committed. PocketBase commits `ui/dist`. **Commit `web/dist`** (PocketBase's choice): it makes `go install github.com/you/crossplane-factory@latest` work without a Node toolchain, which matters enormously for a Go-ecosystem tool.

### Counter-example from the user's own cluster — crossview

`crossplane-contrib/crossview` (265 stars, JavaScript) is running in `crossview-system`: `ghcr.io/corpobit/crossview:latest` **plus `postgres:16-alpine`**, with `nginx/`, `helm/`, `k8s/`, `keycloak/` and a separate `crossview-go-server/`. This is the multi-artifact path — a Postgres dependency and an nginx front for a *read-only dashboard*. It is prior art for "Crossplane web UI" and a cautionary tale for architecture. **Your tool should need no database:** state lives in blueprint files in git.

### Recommended layout

```
crossplane-factory/
  cmd/crossplane-factory/
     main.go            # kong.Must(&cli{}, kong.Bind(engine), …)
     serve.go           # `default:"1"`
     generate.go  adopt.go  schema.go  render.go  validate.go  version.go
  internal/
     blueprint/    # parse + validate + JSON Schema for the IR
     schema/       # xpkg + cluster MR schema discovery, lockfile
     emit/         # blueprint -> XRD + Composition (the generator core)
     layout/       # on-disk placement: argocd | flat | project
     httpapi/      # HTTP handlers — thin wrappers over internal/emit
  pkg/blueprint/   # ONLY the stable types others may import
  web/  src/  dist/  embed.go  embed_no_ui.go
  testdata/…
  .goreleaser.yaml
```

Mirror the Crossplane CLI's own `internal/` vs `pkg/` discipline — verified: it exposes only `pkg/validate` and `pkg/xr`, keeping `internal/{xrd,schemas,project,xpkg,kube,docker,git,style,terminal,async}` private. Do the same: a narrow `pkg/blueprint` and nothing else, so you can refactor freely.

**The load-bearing rule: the HTTP API must be a thin adapter over `internal/emit`, never a parallel implementation.** Concretely — `POST /api/generate` unmarshals a blueprint and calls the exact function `generate.go` calls. If a code path exists only in the UI, the CLI cannot reproduce a UI-authored artifact, and the whole GitOps story collapses.

---

## 3. The intermediate representation

### Recommend: a Kubernetes-shaped `Blueprint` YAML

```yaml
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue
spec:
  variant: aws                       # composition-name segment + directory
  api:
    group: platform.sparky.ee
    kind: XQueue
    plural: xqueues                  # explicit — never pluralize automatically
    scope: Namespaced
    version: v1alpha1
    schema: { … }                    # verbatim OpenAPI v3 under spec.properties
    printerColumns:
      - { name: LOCATION, type: string, jsonPath: .spec.location }
  providers:
    - xpkg: xpkg.upbound.io/upbound/provider-aws-sqs:v2.7.1
      digest: sha256:dcce6930dfebf29dda07946babebca57fa6df4f6034e8a52501dca5eb85b97c1
  pipeline:
    - { step: render-resources, function: function-go-templating }
    - { step: auto-ready,       function: function-auto-ready }
  resources:
    - name: main-queue               # -> setResourceNameAnnotation "main-queue"
      apiVersion: sqs.aws.m.upbound.io/v1beta1
      kind: Queue
      providerConfigRef: { kind: ClusterProviderConfig, nameFrom: spec.providerName }
      fields:
        - path: spec.forProvider.region
          from: { mapOn: spec.location, map: { EU: eu-north-1, US: us-east-2 } }
        - path: spec.forProvider.visibilityTimeoutSeconds
          from: { field: spec.visibilityTimeoutSeconds, omitEmpty: true }
        - path: spec.forProvider.tags
          from: { field: spec.tags, omitEmpty: true }
```

Why this shape:

- **Reverse-DNS `apiVersion` buys you migration for free.** When v1alpha1 proves wrong, `v1alpha2` + a converter is a solved, familiar pattern. A bare `version: 1` field is not.
- **Audience fluency.** Every consumer already reads `apiVersion/kind/metadata/spec` and has editor schema support via `yaml.schemas`. Ship a JSON Schema and wire `# yaml-language-server: $schema=…` — instant IDE completion in the same editor where they edit XRDs.
- **Digest-pinned `providers`.** Verified that `crossplane dependency add` resolves the floating tag `:v2` → `v2.7.1` and records `sha256:dcce6930…` in `schemas/.lock.json`, transitively pulling `provider-family-aws:v2.7.1`. **Reproducible generation requires pinning the schema source, not just the provider name** — otherwise the same blueprint emits different Compositions next month. Adopt the same lockfile discipline.
- **`plural` is explicit.** The Crossplane CLI added `--plural` precisely because auto-pluralization breaks (its own help cites `postgres` → `postgreses`). Never infer; require it, defaulting to `strings.ToLower(kind)+"s"` only in the interactive UI where a human can correct it.

**Mandatory escape hatch:** `resources[].rawTemplate: |` accepting arbitrary go-template text, emitted verbatim. Without it the tool becomes a ceiling the first time someone needs the readiness logic from the user's own `xmicroservices` composition (`dig "resource" "status" "availableReplicas" 0 $deployed`) — which no field-mapping DSL will ever express. Plan for the DSL covering ~80% and `rawTemplate` covering the rest.

**Hard constraint on the emitted Composition: `source: Inline`.** `function-go-templating`'s `TemplateSource` constants are `Inline`, `FileSystem`, `Environment`. `FileSystem` reads from the *function pod's* filesystem, requiring a custom function image or a Crossplane Project build — incompatible with a plain ArgoCD directory sync. Inline block scalars are the only option here. (Corollary: strip trailing whitespace from every template line before emitting. Trailing spaces change YAML block-scalar round-tripping and produce phantom ArgoCD diffs on `selfHeal: true`.)

### Comparison to prior art

| System | IR | Round-trips? | Lesson |
|---|---|---|---|
| **Terraform** | HCL → binary plan | No — plan is opaque output | Source of truth is the authored file, never the artifact. Correct instinct. |
| **cdk8s** | TypeScript/Python → YAML | No | Full programming language = maximum power, zero GUI editability. Wrong for a canvas. |
| **Backstage software templates** | `template.yaml` (`scaffolder.backstage.io/v1beta3`), declarative `parameters` + `steps` | No | Directly relevant — the cluster runs `backstage-system`, and the git XRD carries a comment about `kubernetes-ingestor` generating scaffolder forms from the XRD schema. **The XRD's OpenAPI schema is a downstream contract.** Emitting `enum` and `type` faithfully (which `crossplane xrd generate` does not) is a hard requirement, not polish. |
| **Kratix Promise** (`platform.kratix.io/v1alpha1`) | Single CR bundling `api` + `dependencies` + `workflows` | No | Validates bundling API surface + provisioning logic in one declarative file — exactly the `spec.api` + `spec.resources` split above. |
| **Crossplane Project** (`dev.crossplane.io/v1alpha1`) | `crossplane-project.yaml` + `schemas/.lock.json` | No | Verified in-hand. **Reuse its lockfile idea; do not adopt its directory contract.** |

Every one of these is **one-directional**. That is not an accident.

### Round-tripping: don't, and here is the evidence

I parsed the user's actual production template (extracted live from `composition/xqueues.aws.platform.sparky.ee`) with Go's `text/template/parse`. Source at `tmplexp/main.go`.

**First result — parsing fails outright by default:**
```
PARSE ERROR: template: x:9: function "dict" not defined
```
`template.Parse` resolves function names at parse time. To parse third-party templates you must either replicate function-go-templating's entire FuncMap (all of sprig, plus `setResourceNameAnnotation`, `getComposedResource`, `getResourceCondition`, `include`, `toYaml`, …) or use `parse.SkipFuncCheck`. With `parse.New` + `Mode = parse.SkipFuncCheck | parse.ParseComments`:

```
ACTION    $spec := .observed.composite.resource.spec
TEXT      "apiVersion: sqs.aws.m.upbound.io/v1beta1\nkind: Queue\nmetadata:\n  an…"
ACTION    setResourceNameAnnotation "main-queue"
TEXT      "spec:\n  forProvider:\n    region:"
ACTION    index (dict "EU" "eu-north-1" "US" "us-east-2") $spec.location
WITH      $spec.visibilityTimeoutSeconds
  TEXT      "visibilityTimeoutSeconds:"
  ACTION    .
WITH      $spec.maxMessageSize
  TEXT      "maxMessageSize:"
  ACTION    .
WITH      $spec.tags
  TEXT      "tags:"
  RANGE     $key, $value := .
    ACTION    $key
    TEXT      ":"
    ACTION    $value | quote
TEXT      "providerConfigRef:\n    # Namespaced managed resources may reference e…"
ACTION    $spec.providerName
```

Read what that actually says:

1. **The TEXT nodes are not YAML.** `"spec:\n  forProvider:\n    region:"` is a dangling mapping key. You cannot hand any fragment to a YAML parser. The two grammars are interleaved, not nested, so there is no composition of `text/template` + `gopkg.in/yaml.v3` that yields a document tree.
2. **The document's *shape* is data-dependent.** Three `WITH` nodes mean `spec.forProvider` has 2^3 possible key sets. "The graph" doesn't exist until you bind an XR. A canvas node has to represent *one* structure.
3. **Indentation is semantic and lives in TEXT.** `{{- with }}` / `{{- end }}` chomping decides whether emitted YAML is valid. Any graph→template regeneration must reproduce whitespace exactly, or a round-trip silently corrupts a working Composition.
4. **Comments are lost in the direction that matters.** The `providerConfigRef` explanation is a *YAML* comment glued inside a TEXT node — survivable. But the git XRD's comments (the `enum`-vs-`oneOf` rationale, the `additionalPrinterColumns` note) are pure YAML: a parse→model→re-emit cycle destroys them. On a `selfHeal: true` repo this is a one-way loss of institutional knowledge in a PR diff.

**Therefore, three import tiers — ship 0 and 1, never 2:**

- **Tier 0 — `generate` only (default).** Blueprint → artifacts. One direction.
- **Tier 1 — `adopt` (ship this).** Read an existing Composition + XRD; map everything *structured* into the blueprint (`metadata.name`, `compositeTypeRef`, pipeline steps, `functionRef` names, `input.apiVersion`, the whole XRD `spec`); capture each go-templating step's template into `rawTemplate:` **as an opaque verbatim string**. Losslessly byte-reproducible, and it onboards the user's two existing Compositions on day one. The canvas shows a "custom template" node that opens a text editor. **This is the honest 90% of the value of round-tripping at ~5% of the risk.**
- **Tier 2 — AST → graph. Do not build.** Only tractable for templates your own tool emitted, which you can detect far more cheaply with a provenance marker.
- **Tier 2.5 — `render`-based visualization (nice-to-have).** Run `crossplane composition render` against a sample XR and build display nodes from the *concrete* output. Verified working: 1.0 s warm, yielding a real `sqs.aws.m.upbound.io/v1beta1 Queue` with `region: eu-north-1` resolved from the `dict` lookup. Recovers topology, discards conditionals. Good for "show me what this makes"; **must be read-only** — never let it write back.

---

## 4. Golden-file testing

### The idiomatic pattern — Helm's, verified verbatim

`helm/helm` `internal/test/test.go` is the model to copy for a YAML generator:

```go
// UpdateGolden writes out the golden files with the latest values, rather than failing the test.
var updateGolden = flag.Bool("update", false, "update golden files")

func AssertGoldenString(t TestingT, actual, filename string) {
	t.Helper()
	if err := compare([]byte(actual), path(filename)); err != nil {
		t.Fatalf("%v\n", err)
	}
}

func path(filename string) string {
	if filepath.IsAbs(filename) { return filename }
	return filepath.Join("testdata", filename)
}

func compare(actual []byte, filename string) error {
	actual = normalize(actual)
	if err := update(filename, actual); err != nil { return err }
	expected, err := os.ReadFile(filename)
	if err != nil { return fmt.Errorf("unable to read testdata %s: %w", filename, err) }
	expected = normalize(expected)
	if !bytes.Equal(expected, actual) {
		return fmt.Errorf("does not match golden file %s\n\nWANT:\n'%s'\n\nGOT:\n'%s'", filename, expected, actual)
	}
	return nil
}

func update(filename string, in []byte) error {
	if !*updateGolden { return nil }
	return os.WriteFile(filename, normalize(in), 0o666)
}

func normalize(in []byte) []byte { return bytes.ReplaceAll(in, []byte("\r\n"), []byte("\n")) }
```

Four details that are easy to get wrong and that Helm gets right: `flag.Bool("update", …)` at package scope (so `go test ./... -update` works everywhere at once); the implicit `testdata/` join; **CRLF normalization on both sides**; and a `TestingT` interface rather than `*testing.T` so the helper composes.

### How the alternatives actually do it — corrections to common belief

- **controller-tools (controller-gen) does NOT use a `-update` flag.** Verified: a code search for `flag.Bool` across the repo hits only `cmd/helpgen/main.go`. Its CRD fixtures live in `pkg/crd/testdata/`, which is **a separate nested Go module** (`testdata/go.mod`), and `pkg/crd/gen_integration_test.go` uses `go-cmp` inside ginkgo, `os.Chdir`-ing into `testdata/gen` ("go modules are directory-sensitive"). Regeneration is a documented manual step from `testdata/README.md`:
  > ```bash
  > go generate
  > ```
  > or … `$ /path/to/current/build/of/controller-gen crd paths=. output:dir=.`

  Reasonable for a *compiler over Go source*; **wrong model for you** — the `go generate` indirection adds a step with no benefit when your input is already a data file.
- **kustomize** does not use on-disk goldens much at all: `AssertActualEqualsExpected` appears 93 times, comparing against **inline expected strings** in `_test.go` files. Readable for small fixtures, unmanageable for multi-hundred-line Compositions. Don't.

### Recommended three-layer stack

**Layer 1 — byte-exact goldens (Helm pattern).** `testdata/<case>/blueprint.yaml` → `testdata/<case>/xrds/xqueue.yaml` + `compositions/aws/xqueue.yaml`. Byte-exact *matters more here than in most generators*: on `selfHeal: true`, formatting churn is operational noise, so indentation is a behavioral property worth pinning.

**Layer 2 — semantic diff.** Unmarshal want/got into `map[string]any` and `cmp.Diff(want, got, cmpopts.EquateEmpty())` (`google/go-cmp`, 4,678 stars; note the want-first argument order — `-` is want, `+` is got). Catches meaning changes that Layer 1 reports as an unreadable wall of text. Run both; report Layer 2's diff on failure and Layer 1's as the byte-level fallback.

**Layer 3 — CLI end-to-end via `testscript`.** Use `rogpeppe/go-internal/testscript` with txtar archives — the harness the Go team uses for `cmd/go`, and the right tool for a generator whose contract is *files written to a directory tree*:

```
# testdata/script/generate_argocd.txtar
exec crossplane-factory generate blueprint.yaml --out-dir . --layout argocd
cmp crossplane/xrds/xqueue.yaml want-xrd.yaml
cmp crossplane/compositions/aws/xqueue.yaml want-composition.yaml
! exec crossplane-factory generate blueprint.yaml --check
stderr 'drift'
```
Verified `testscript.Params` has **`UpdateScripts`** — "If a `cmp` command fails and its second argument refers to a file inside the testscript file, the command will succeed and the testscript file will be updated to reflect the actual content." So a single `-update`-style flag regenerates both the file goldens and the embedded expectations. Also set `RequireExplicitExec: true` to keep `exec` invocations obvious.

**Layer 4 (integration, docker-gated) — `render` goldens. This is the layer that proves you emitted a *working* Composition, and it is viable because render is deterministic.** Verified by running it three times on the user's real Composition:

```
sha256 of 3 runs:
332ee29a41baca893d709d2b680c3bd2bb1252f8b133fdc4588fd7d19d435e80  out1.yaml
332ee29a41baca893d709d2b680c3bd2bb1252f8b133fdc4588fd7d19d435e80  out2.yaml
332ee29a41baca893d709d2b680c3bd2bb1252f8b133fdc4588fd7d19d435e80  out3.yaml
IDENTICAL
```

Timestamps are pinned (`2024-01-01T00:00:00Z`), composed names are stable hashes (`golden-queue-d4503315608a`), owner UIDs are deterministic (`61aa1c4e-69a0-5f42-a651-a3c132a19d28`). **Byte-stable — safe as a golden oracle.** Artifacts at `…/scratchpad/cliergo/`.

Two operational caveats, both verified:
- Functions must be a **multi-document YAML stream**, not a `List`. Passing a `kind: List` fails with `cannot load functions from "…": not a function: List/`.
- **`render` leaks containers.** After a handful of runs: `silly_lederberg … function-auto-ready:v0.5.0  Up 2 minutes`, `exciting_darwin … function-go-templating:v0.12.0  Up 2 minutes`, plus an older pair still `Up 8 minutes`. In CI, set `render.crossplane.io/runtime-docker-name` to reuse named containers, or reap in `t.Cleanup`. It also pulls `xpkg.crossplane.io/crossplane/crossplane` (~105 MB) for the render engine itself.

Gate Layer 4 behind `testing.Short()` and a docker probe so `go test ./...` stays fast and hermetic; run it in CI and in a `make test-integration` target.

---

## 5. Distribution — what a small OSS Go+web tool realistically ships in 2026

**Tier 1 — do these at v0.1.0:**

1. **GoReleaser** (v2.18.0, released 2026-08-24) → GitHub Releases with checksums. Two build IDs: default (UI embedded) and `-tags no_ui` (CI-sized).
2. **Homebrew tap via `homebrew_casks:`, not `brews:`.** Verified from the GoReleaser docs source: `homebrew_casks` landed in v2.10 and `brews` is deprecated, to be removed in v3. The rationale, quoted from their docs — "Historically, the `brews` section was kind of a hack" — Formulas are meant to build from source; a cask correctly installs a prebuilt binary. **Start on `homebrew_casks` so you never migrate.** Note `alternative_names` (versioned casks) is Pro-only.
3. **OCI image to `ghcr.io`.** Non-negotiable, because the web UI wants to run in-cluster — that is how crossview is deployed in this very cluster. Distroless/static base; the embedded UI means no nginx sidecar and no database.
4. **`install.sh`** — a curl-pipe installer at repo root. The Crossplane CLI ships exactly this (`install.sh` is a top-level file in `crossplane/cli`); it is the muscle memory of this audience.
5. **`go install github.com/you/crossplane-factory/cmd/crossplane-factory@latest`** — free, provided `web/dist` is committed. This is the strongest argument for committing build output.

**Tier 2 — later, on demand:**

6. **Krew.** GoReleaser has first-class support via a `krews:` section (`name`, `ids`, `url_template`, `homepage`, `description`, `short_description`, `commit_msg_template`; manifests are `krew.googlecontainertools.github.com/v1alpha2`; krew-index currently lists **402** plugins). The mechanics work — I verified kubectl's plugin resolution empirically:
   ```
   $ PATH="$PWD/bin:$PATH" kubectl crossplane-factory gen --foo
   PLUGIN INVOKED as: …/bin/kubectl-crossplane_factory
   args: gen --foo
   ```
   A binary named `kubectl-crossplane_factory` (**underscore** in the filename) is invoked as `kubectl crossplane-factory`. So yes, `kubectl crossplane-factory` is achievable.

   **But recommend deferring.** Three reasons: (a) the Crossplane project already retired the `kubectl-crossplane` krew plugin in favor of a standalone `crossplane` binary — re-entering that namespace re-litigates a settled decision and invites confusion; (b) a plugin that starts a web server is an odd fit for the kubectl plugin contract, which assumes short-lived kubeconfig-scoped commands; (c) `generate` is verified to need **no cluster at all** — the kubectl entry point implies a cluster dependency that doesn't exist. Add it later if users ask.

7. **npx wrapper.** The esbuild pattern — a root package whose `optionalDependencies` list per-platform packages (`@you/factory-darwin-arm64`, …), npm selecting by `os`/`cpu`, and `bin` pointing at a JS shim. Better than `postinstall`-download (works offline, respects `--ignore-scripts`, cacheable). Real cost: N+1 npm packages per release, and known pnpm friction with optional platform deps. The user has npm 11.19.0 and Backstage in-cluster (a Node shop), so there's a genuine audience. **Defer to v0.2+**; it is a pure add-on once GoReleaser is producing the archives.

8. **Helm chart** for the in-cluster UI deployment, once the OCI image is stable.

**Skip:** apt/rpm/AUR (GoReleaser can, but the audience is `brew`/`go install`/container), and Snap/Flatpak.

---

## 6. On-disk layout for ArgoCD

### Verified ground truth from the live cluster and repo

ArgoCD **v3.5.1** in `argocd-system`; `argocd-cm` has `application.resourceTrackingMethod: annotation` and `application.instanceLabelKey: argocd.argoproj.io/instance`. The Applications are generated by an ApplicationSet named `crossplane-system`.

```
NAME                          WAVE   PATH                          RECURSE  DEST-NS            PRUNE  SELFHEAL  SRCTYPE
crossplane-namespaces         0      crossplane/namespaces         true     crossplane-system  true   true      Directory
crossplane-providers          0      crossplane/providers          true     crossplane-system  true   true      Directory
crossplane-providers-config   1      crossplane/providers-config   true     crossplane-system  true   true      Directory
crossplane-functions          2      crossplane/functions          true     crossplane-system  true   true      Directory
crossplane-compositions       3      crossplane/compositions       true     crossplane-system  true   true      Directory
crossplane-xrds               4      crossplane/xrds               true     crossplane-system  true   true      Directory
crossplane-xrs                5      crossplane/xrs                true     crossplane-system  true   true      Directory
```

Example platform-engineering GitOps repo layout:
```
crossplane/
  compositions/
    aws/xqueue.yaml                 (2524 B)
    kubernetes/xmicroservice.yaml   (3674 B)
  functions/function-auto-ready.yaml, function-go-templating.yaml
  providers/aws/
  providers-config/  namespaces/
  xrds/
    xqueue.yaml                     (2116 B)
    xmicroservice.yaml              (1898 B)
  xrs/  cncf-campinas-talk.yaml, cncf-pre-talk.yaml, demo-microservice.yaml
```

Code search for `filename:kustomization.yaml` across the repo: **`total_count: 0`**.

Naming, cross-checked between git and cluster:
- **XRD `metadata.name` = `<plural>.<group>`** → `xqueues.platform.sparky.ee`. A hard Crossplane invariant (same rule as CRDs), not a style choice.
- **Composition `metadata.name` = `<plural>.<variant>.<group>`** → `xqueues.aws.platform.sparky.ee`, `xmicroservices.kubernetes.sparky.ee`. A convention; nothing enforces it. The `<variant>` segment (`aws`, `kubernetes`) is a **human choice**, not derivable from the MR group (`sqs.aws.m.upbound.io` would give `sqs` or `m`, both wrong). Hence `spec.variant` in the blueprint.
- **Filenames = `<singular-kind-lowercase>.yaml`** → `xqueue.yaml`, *not* `xqueues.platform.sparky.ee.yaml`. The directory supplies the variant; the filename need not repeat the group.

### Recommendations

**`--layout argocd` (default) writes:**
```
crossplane/xrds/<kind-lower>.yaml
crossplane/compositions/<variant>/<kind-lower>.yaml
```
Make the two prefixes flags (`--xrd-dir`, `--composition-dir`) — the *convention* generalizes, the literal paths don't.

**One Kubernetes object per file. Not multi-doc.** With `prune: true` + `directory.recurse: true`, deleting a file is how you delete a live object — a clean 1:1 that multi-doc files muddy. It also matches the existing repo and keeps PR diffs surgical.

**Do NOT emit `kustomization.yaml` by default.** ArgoCD auto-detects source type (verified: the same cluster reports `sourceType: Helm` for `crossplane-app`/`kyverno-app` and `Directory` for the rest, purely from repo content). Dropping a `kustomization.yaml` in flips these Applications from Directory to Kustomize, and then **any file absent from `resources:` becomes invisible — which under `prune: true` means ArgoCD deletes the corresponding live object.** A generator that writes a file but forgets to register it silently destroys a resource. Offer `--layout kustomize` as opt-in, emitting a complete regenerated `resources:` list. *(The Directory/Helm auto-detection is verified from `status.sourceType`; the specific Kustomize-detection-under-`recurse` interaction is reasoned from that plus ArgoCD's documented behavior — worth a scratch-repo test before you ship the flag.)*

**Never emit `argocd.argoproj.io/tracking-id`.** Verified: `crossplane/xrds/xqueue.yaml` in git has no such annotation, yet the live object carries `crossplane-xrds:apiextensions.crossplane.io/CompositeResourceDefinition:crossplane-system/xqueues.platform.sparky.ee`. ArgoCD injects it at apply time under `resourceTrackingMethod: annotation`. If your tool wrote one, a wrong app-name prefix would make ArgoCD believe another Application owns the resource. Same for `kubectl.kubernetes.io/last-applied-configuration`.

Note the tracking-id's namespace segment is `crossplane-system` even though XRDs and Compositions are cluster-scoped — it reflects the Application's `destination.namespace`, another reason never to synthesize it yourself.

**Put provenance in YAML comments, never annotations.** A `factory.crossplane.io/generated-at` annotation changes on every run; under `selfHeal: true` + `prune: true` that is a perpetual sync loop. A YAML comment is discarded at parse time, never reaches the API server, and produces exactly zero diff. So:
```yaml
# Generated by crossplane-factory. Do not edit.
# blueprint: blueprints/xqueue.yaml
# blueprint-sha256: 4f3c…
apiVersion: apiextensions.crossplane.io/v2
```
This also preserves the *human* comments already in the repo — the git XRD's explanation of why `enum` beats `oneOf` for kubernetes-ingestor form generation is real institutional knowledge. Blueprints should carry a `description`/comment passthrough so generation doesn't erase it.

**Don't emit `argocd.argoproj.io/sync-wave` on the resources.** Waves live on the Applications in this repo (compositions=3, xrds=4). Note that ordering applies Compositions *before* their XRDs — which is fine, since a Composition referencing a not-yet-established XR type simply isn't selected until the XRD establishes. Your tool writing both in one run is safe.

**Deterministic output is a correctness requirement, not a nicety.** Sorted map keys, stable field order, `LF` only, trailing newline, no version stamps. Under `selfHeal: true` any nondeterminism becomes live-cluster churn.

**Blueprints live outside the synced paths.** e.g. `blueprints/xqueue.yaml` at repo root. No ArgoCD Application watches it, and `directory.recurse: true` under `crossplane/` would otherwise try to apply your `kind: Blueprint` to the cluster and fail.

### CI wiring

```yaml
- run: crossplane-factory generate blueprints/ --out-dir . --check
```
`--check` regenerates in memory, diffs against the working tree, prints a unified diff, and exits non-zero — the `gofmt -l` / `terraform fmt -check` contract. **Use distinct exit codes** (`terraform plan -detailed-exitcode` precedent): `0` = in sync, `1` = tool error, `2` = drift. CI can then distinguish "your generator crashed" from "someone hand-edited generated YAML", which are very different failures.

Also add `--check` as a pre-commit hook, and gate hand-edits with a `CODEOWNERS` entry or a header the check verifies.

---

## Verification status

**Verified by running it:** every `crossplane` CLI invocation and its output; the composition-generate stub and its function-name mismatch; xrd-generate lossiness vs. the git XRD; the go-template AST (both the `dict` parse failure and the `SkipFuncCheck` tree); render determinism (3× identical sha256); leaked render containers; offline `dependency add --api-only` (39 schemas, digest lock, `:v2`→`v2.7.1`); OpenAPI v3 MR schemas from the cluster (283,877 B); kubectl plugin underscore→dash resolution; all ArgoCD Application/ConfigMap/annotation state; the absence of tracking-ids and kustomization.yaml in git.

**Verified by reading source/API (not executed):** all go.mod contents and repo trees (crossplane/cli, pocketbase, syncthing, gitea, helm, controller-tools, goreleaser, krew-index) via authenticated `gh api`; PocketBase's embed files and Helm's `internal/test/test.go` verbatim.

**Read in docs only:** kong's `default:"1"`/`default:"withargs"` semantics; `testscript.Params.UpdateScripts`; GoReleaser's `krews:`/`homebrew_casks` keys and the v2.10 deprecation; Kratix Promise structure; npm optionalDependencies pattern.

**Could not confirm — flagging honestly:** whether adding a `kustomization.yaml` under a `directory.recurse: true` path flips `sourceType` to Kustomize *in this specific ArgoCD 3.5.1 config* (inferred from observed Helm/Directory auto-detection; test in a scratch repo before shipping `--layout kustomize`). Whether `crossplane` core rejects an XRD whose `metadata.name != <plural>.<group>` (consistent with all observed data and the CRD invariant, but I did not apply a bad manifest — the cluster is read-only per instructions). Trailing-whitespace effects on YAML block-scalar round-tripping are reasoned from the YAML spec, not measured.

**Scratch artifacts** (all under ``): `tmplexp/main.go` + `tmplexp/tmpl.txt` (AST experiment), `cliergo/` (render determinism goldens), `offl/schemas/` (offline schema pull + lockfile), `proj/` and `iso/` (crossplane project generate outputs), `bin/kubectl-crossplane_factory` (plugin-naming test).