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
    scope: Namespaced | Cluster
    parameters:
      <param-name>:
        type: string | integer | boolean | object
        required: true | false
        default: <default-val>
        enum: [<val1>, <val2>]
        description: <doc-string>
        properties: # nested for type: object
          <member-name>: {type: string, ...}
  templates:
    <template-name>: |
      <go-template-body>
  conventions:
    - match: <field-name>
      template: <template-name>
  resources:
    - name: <resource-name>
      kind: <crd-kind>
      provider: <provider-ref | k8s>
      fields:
        <path>: {value | from | raw | template}
      envelope:
        <envelope-path>: {value | from | raw | template}
      annotations:
        <annotation-key>: {value | from | raw | template}
      when: params.<boolParam>
      forEach: params.<intParam> | resources.<name>.status.atProvider.<field>
```

---

## Field Modes

Every field path in `resources[*].fields` and `resources[*].envelope` supports exactly one of four authoring forms:

### 1. `value`
Assigns a literal scalar or primitive value. Strings are automatically quoted in generated YAML.
```yaml
fields:
  region: {value: "eu-north-1"}
  engine: {value: postgres}
```

### 2. `from`
Wires a parameter or cross-resource reference:
- **XRD Parameter binding**: `params.<name>` or `params.<object>.<member>`
  - Required parameters dereference directly: `{{ .spec.<name> }}`
  - Optional parameters are wrapped in Go-template `hasKey` guards to prevent runtime evaluation errors.
- **Cross-Resource Status reference**: `resources.<name>.status.atProvider.<field>`
  - Generates guarded status dereferencing so unobserved upstream resources omit the field cleanly on early reconcile passes.
```yaml
fields:
  allocatedStorage: {from: params.storageGB}
  vpcId:            {from: resources.vpc.status.atProvider.id}
```

### 3. `raw`
Emits verbatim YAML or Go template expressions. Useful for complex nested blocks or map objects:
```yaml
fields:
  spec.selector.matchLabels: {raw: "{app: web}"}
```

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

---

## Annotations Authoring

The `annotations` block maps directly to `metadata.annotations` on the emitted resource (for both managed and native Kubernetes kinds):

```yaml
resources:
  - name: sa
    kind: ServiceAccount
    provider: k8s
    annotations:
      eks.amazonaws.com/role-arn: {from: resources.role.status.atProvider.arn}
```

If the referenced status value is not yet available, the generated Composition skips rendering the annotation key cleanly rather than rendering an invalid or empty string.

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

---

## Flow Control: Loops and Conditionals

### Conditional Inclusion (`when:`)
Conditionally renders a resource based on a boolean parameter:
```yaml
resources:
  - name: backup-vault
    kind: BackupVault
    provider: ghcr.io/crossplane-contrib/provider-aws-backup:v2.7.0
    when: params.enableBackups
```

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

