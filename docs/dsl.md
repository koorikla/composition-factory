# Blueprint DSL Reference

Composition Factory uses a single declarative Blueprint format (`*.cf.yaml`) that unifies the CompositeResourceDefinition (XRD) schema, Crossplane pipeline definition, and composed resources into one cohesive source of truth.

---

## Blueprint Structure

```yaml
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: <blueprint-name>
spec:
  sources:
    - provider: <oci-image-ref>
    - crds: <manifest-path>
  xrd:
    group: <api-group>
    kind: <composite-kind>
    plural: <composite-plural>
    version: <api-version>
    scope: Namespaced # Note: "Cluster" is refused in M1 ("use Namespaced")
    parameters:
      <param-name>:
        type: string | integer | number | boolean | object
        required: true | false
        default: <default-val>
        enum: [<val1>, <val2>]
        description: <doc-string>
        properties: # nested definitions for type: object
          <member-name>:
            type: string | integer | number | boolean | object
            required: true | false
            properties: { ... }
  pipeline: # optional custom composition pipeline steps
    steps:
      - step: auto-ready
        functionRef:
          name: function-auto-ready
      - step: environment-configs
        functionRef:
          name: function-environment-configs
  templates:
    <template-name>: |
      <go-template-body>
  conventions:
    - match: <field-suffix> # Suffix match on field name (refused on native kinds)
      template: <template-name>
  resources:
    - name: <resource-name>
      kind: <crd-kind>
      provider: <provider-ref | k8s>
      fields:
        <path>: {value | from | raw | template}
        <mapField>[<key>]: {value | from | raw} # Bracket grammar for map entries
      envelope:
        <envelope-path>: {value | from | raw} # Note: status wires and template: are refused in envelope
      annotations:
        <annotation-key>: {value | from | raw | template}
      when: params.<boolParam> | params.<param> == "<lit>" | params.<param> != "<lit>"
      forEach: params.<intParam> | resources.<name>.status.atProvider.<field>
```

---

## Parameter Types

Parameters in `spec.xrd.parameters` define the OpenAPI schema for the XRD and claim:

- **`string`**: String value, with optional `enum` list.
- **`integer`**: 64-bit integer value.
- **`number`**: Floating point / decimal number value.
- **`boolean`**: Boolean flag (`true` / `false`).
- **`object`**: Structured object. Supports arbitrary nesting via recursive `properties` definitions. Members are wired in fields via `params.<object>.<member>`.

*(Note: `type: "array"` is explicitly refused in M1).*

---

## Field Modes

Every field in `resources[*].fields` supports exactly one of four authoring forms:

### 1. `value`
Assigns a literal scalar or primitive value.
```yaml
fields:
  region: {value: "eu-north-1"}
  engine: {value: postgres}
```

### 2. `from`
Wires an XRD parameter or upstream resource status output:
- **XRD Parameter binding**: `params.<name>` or nested member `params.<obj>.<member>`
  - Required parameters dereference directly: `{{ .spec.<name> }}`
  - Optional parameters are wrapped in Go-template `hasKey` guards to prevent missing key errors.
- **Cross-Resource Status reference**: `resources.<name>.status.atProvider.<field>`
  - Generates guarded status dereferencing so unobserved upstream resources omit the field cleanly on early reconcile passes.
```yaml
fields:
  allocatedStorage: {from: params.storageGB}
  vpcId:            {from: resources.vpc.status.atProvider.id}
```

### 3. `raw`
Emits verbatim YAML or Go template expressions. This is the primary escape hatch for custom expressions, helper functions, and index-based math.

```yaml
fields:
  spec.selector.matchLabels: {raw: "{app: web}"}
  cidrBlock: {raw: 'printf "10.0.%d.0/24" $i'}
  tags[Name]: {raw: 'printf "%s-subnet-%d" $xr $i'}
```

#### Runtime Variables Available in `raw`:
When using the default `go-templating` engine, `raw:` expressions have direct access to the following scoped template variables:
- **`$spec`**: The composite resource's parameter map (`.observed.composite.resource.spec`). E.g., `$spec.region`, `$spec.dbName`.
- **`$xr`**: The composite resource's metadata name string (`.observed.composite.resource.metadata.name`), useful for deterministic resource naming and tagging.
- **`$xrMeta`**: The composite resource's entire `metadata` map (including `.labels`, `.annotations`, `.namespace`, and `.uid`).
- **`$observed`**: The map of all observed composed resources (`.observed.resources`). Each resource is accessed via `(index $observed "<resource-name>").resource`.
- **`$i`**: The current iteration index (`0, 1, 2, ...`) available inside looped resources (`forEach`).

*Note: In `python` and `kcl` engines, `{{ ... }}` Go-template syntax inside `raw:` is rejected with a validation error.*

### 4. `template`
Executes a reusable named template from `spec.templates`.
```yaml
templates:
  trust-policy: >-
    '{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Federated":"{{ .spec.oidcProviderArn }}"},"Action":"sts:AssumeRoleWithWebIdentity"}]}'
resources:
  - name: role
    kind: Role
    provider: ghcr.io/crossplane-contrib/provider-aws-iam:v2.7.0
    fields:
      assumeRolePolicy: {template: trust-policy}
```
*(Note: `template:` is refused on native Kubernetes resource fields like `Deployment.spec`, but is fully supported in `annotations`).*

### Map-Entry Bracket Grammar
Map fields support direct key subscripting using bracket grammar:
```yaml
fields:
  labels[environment]: {value: "production"}
  selector.matchLabels[app]: {from: params.appName}
```

---

## Annotations Authoring

The `annotations` block maps directly to `metadata.annotations` on the emitted resource (supporting managed and native Kubernetes kinds):

```yaml
resources:
  - name: sa
    kind: ServiceAccount
    provider: k8s
    annotations:
      eks.amazonaws.com/role-arn: {from: resources.role.status.atProvider.arn}
      custom.io/policy: {template: trust-policy}
```

If the referenced status value is not yet available, the generated Composition skips rendering the annotation key cleanly rather than rendering empty or invalid strings.

---

## Envelope Configuration

Crossplane-native envelope fields (`providerConfigRef`, `writeConnectionSecretToRef`, `managementPolicies`, etc.) are authored per-resource in `envelope`:

```yaml
resources:
  - name: db-instance
    kind: Instance
    provider: ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0
    envelope:
      writeConnectionSecretToRef.name:      {from: params.dbName}
      writeConnectionSecretToRef.namespace: {value: "default"}
      managementPolicies:                  {value: "Observe, Create, Update, Delete, LateInitialize"}
```

#### Envelope Rules:
- Supports `value`, `from` (with `params.<name>`), and `raw`.
- Status wires (`resources.<name>.status...`) and `template:` are refused in `envelope`.
- Array-typed envelope leaves (such as `managementPolicies`) accept comma-separated strings in `value` (e.g. `value: "Observe, Create, Update"`) or literal lists in `raw`.

---

## Flow Control: Loops and Conditionals

### Conditional Inclusion (`when:`)
Conditionally renders a resource based on a boolean parameter or literal comparison:
```yaml
resources:
  - name: backup-vault
    kind: BackupVault
    provider: ghcr.io/crossplane-contrib/provider-aws-backup:v2.7.0
    when: params.enableBackups

  - name: regional-config
    kind: ConfigMap
    provider: k8s
    when: params.environment == "prod"
```

Grammar supported:
- `params.<boolParam>`: evaluated as truthy boolean flag.
- `params.<param> == "<literal>"`: equality comparison against a literal string.
- `params.<param> != "<literal>"`: inequality comparison against a literal string.

### Resource Loops (`forEach:`)
Replicates a resource N times driven by an integer parameter or upstream observed count:
```yaml
resources:
  - name: cluster-node
    kind: ClusterInstance
    provider: ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0
    forEach: params.nodeCount
```
Emits indexed resource names and `setResourceNameAnnotation` bindings for each replica in the loop.

---

## Conventions (`spec.conventions`)

Conventions apply a named template to any field across resources that matches a given suffix:

```yaml
conventions:
  - match: tags
    template: standard-tags
```

- `match` performs a case-sensitive suffix match against field paths (e.g., `tags` matches `tags` and `spec.tags`).
- Conventions are refused on native Kubernetes kinds (`provider: k8s`).

---

## Composition Pipeline (`spec.pipeline`)

Custom pipeline steps can be declared in `spec.pipeline.steps` to run alongside or in addition to function steps:

```yaml
spec:
  pipeline:
    steps:
      - step: auto-ready
        functionRef:
          name: function-auto-ready
```

---

## Emission Options (`spec.emit`)

The blueprint controls the generator output engine and structure via `spec.emit`:

```yaml
spec:
  emit:
    engine: go-templating    # "go-templating" (default), "kcl" (function-kcl), or "python" (function-python)
    templateSource: Inline   # "Inline" (default) or "FileSystem"
```

- **`engine: go-templating`**: Generates `function-go-templating` pipeline step with Go template expressions and `missingkey=error`.
- **`engine: kcl`**: Generates `function-kcl` (`krm.kcl.dev/v1alpha1` `KCLInput`) pipeline step with typed KCL expressions (`oxr`, `_spec`, `ocds`, `items = _items`), automatically configured in `functions.yaml` and `package.yaml`.
- **`engine: python`**: Generates `function-python` (`python.fn.crossplane.io/v1beta1` `Script`) pipeline step with native Python composition logic (`req: fnv1.RunFunctionRequest, rsp: fnv1.RunFunctionResponse`), desired resource mappings, and readiness signals, automatically configured in `functions.yaml` and `package.yaml`.
- **`templateSource: FileSystem`**: Emits one template file per object in a `templates/` folder packed into ConfigMaps and mounted via a `DeploymentRuntimeConfig`.

---

## Common Errors & Diagnostics

The `cf` generator validates blueprints strictly and returns actionable error messages:

| Error Message | Cause & Remedy |
|---|---|
| `spec.xrd.scope: Cluster is not supported in M1 -- use Namespaced.` | M1 requires Namespaced composite resource definitions. Change `scope: Namespaced`. |
| `spec.xrd.parameters.<param>: type "array" is not supported in M1.` | Array parameter types are not supported. Use scalar types (`string`, `integer`, `number`, `boolean`) or `object` with nested properties. |
| `resource "<name>" field "<path>": unknown path -- did you mean "<suggestion>"?` | Field path does not exist in CRD schema. Check typo or update to suggested schema path. |
| `resource "<name>" field "<path>": template: fields are not supported on a native Kubernetes kind...` | `template:` references are only supported on managed provider fields or in `annotations:`. Use `value:`, `from:`, or `raw:` on native fields. |
| `resource "<name>" envelope "<path>": status wires (...) are not supported in envelope` | `from: resources.<name>.status...` is not permitted in `envelope`. Status references can only be wired into `fields:` or `annotations:`. |
| `resource "<name>": conventions cannot match native Kubernetes kind` | `conventions` can only target managed provider resources, not native Kubernetes resources. |
