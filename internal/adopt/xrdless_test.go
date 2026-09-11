package adopt

import (
	"os"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/emit"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// TestAdoptXRDlessConditionalWhen reproduces CF-168: adopting a Composition
// without its XRD must detect parameter references in template conditional
// statements (e.g. {{- if $spec.versioning }} or {{- if eq $spec.tier "prod" }})
// and settle required and type state such that adopted blueprints with when
// conditions pass blueprint validation.
func TestAdoptXRDlessConditionalWhen(t *testing.T) {
	// 1. Repro from issue #53 using s3-bucket composition manifest
	manifestS3 := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xbuckets.storage.sparky.ee
spec:
  compositeTypeRef:
    apiVersion: storage.sparky.ee/v1alpha1
    kind: XBucket
  mode: Pipeline
  pipeline:
  - step: render-resources
    functionRef:
      name: function-go-templating
    input:
      apiVersion: gotemplating.fn.crossplane.io/v1beta1
      kind: GoTemplate
      source: Inline
      options: ["missingkey=error"]
      inline:
        template: |
          {{- $spec := .observed.composite.resource.spec -}}
          {{- $xr := .observed.composite.resource.metadata.name -}}
          {{- $xrMeta := .observed.composite.resource.metadata -}}
          ---
          apiVersion: s3.aws.m.upbound.io/v1beta1
          kind: Bucket
          metadata:
            annotations:
              {{ setResourceNameAnnotation "bucket" }}
          spec:
            forProvider:
              objectLockEnabled: false
              {{- if hasKey $spec "region" }}
              region: {{ $spec.region | quote }}
              {{- end }}
            providerConfigRef:
              kind: ClusterProviderConfig
              name: {{ $spec.providerName }}
          {{- if $spec.versioning }}
          ---
          apiVersion: s3.aws.m.upbound.io/v1beta1
          kind: BucketVersioning
          metadata:
            annotations:
              {{ setResourceNameAnnotation "versioning" }}
          spec:
            forProvider:
              {{- if hasKey (dig "resources" "bucket" "resource" "status" "atProvider" dict $.observed) "id" }}
              bucket: {{ (index $.observed.resources "bucket").resource.status.atProvider.id | quote }}
              {{- end }}
              {{- if hasKey $spec "region" }}
              region: {{ $spec.region | quote }}
              {{- end }}
              versioningConfiguration:
                status: 'Enabled'
            providerConfigRef:
              kind: ClusterProviderConfig
              name: {{ $spec.providerName }}
          {{- end }}
          {{- if $spec.blockPublicAccess }}
          ---
          apiVersion: s3.aws.m.upbound.io/v1beta1
          kind: BucketPublicAccessBlock
          metadata:
            annotations:
              {{ setResourceNameAnnotation "public-access-block" }}
          spec:
            forProvider:
              blockPublicAcls: true
              blockPublicPolicy: true
              {{- if hasKey (dig "resources" "bucket" "resource" "status" "atProvider" dict $.observed) "id" }}
              bucket: {{ (index $.observed.resources "bucket").resource.status.atProvider.id | quote }}
              {{- end }}
              ignorePublicAcls: true
              {{- if hasKey $spec "region" }}
              region: {{ $spec.region | quote }}
              {{- end }}
              restrictPublicBuckets: true
            providerConfigRef:
              kind: ClusterProviderConfig
              name: {{ $spec.providerName }}
          {{- end }}
  - step: auto-ready
    functionRef:
      name: function-auto-ready
`

	adoptedBP, report, err := Adopt([]byte(manifestS3), Options{
		DefaultProviderRef: "ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0",
	})
	if err != nil {
		t.Fatalf("Adopt failed on s3-bucket composition without XRD: %v", err)
	}

	// Validation must pass
	if err := adoptedBP.Validate(); err != nil {
		t.Fatalf("adopted blueprint failed validation: %v", err)
	}

	// Verify parameter recovery for versioning
	vParam, ok := adoptedBP.Spec.XRD.Parameters["versioning"]
	if !ok {
		t.Fatalf("versioning parameter missing from adopted blueprint")
	}
	if !vParam.Required {
		t.Errorf("versioning parameter must be required, got Required: %v", vParam.Required)
	}
	if vParam.Type != "boolean" {
		t.Errorf("versioning parameter must have type boolean, got Type: %q", vParam.Type)
	}

	// Verify parameter recovery for blockPublicAccess
	bpaParam, ok := adoptedBP.Spec.XRD.Parameters["blockPublicAccess"]
	if !ok {
		t.Fatalf("blockPublicAccess parameter missing from adopted blueprint")
	}
	if !bpaParam.Required {
		t.Errorf("blockPublicAccess parameter must be required, got Required: %v", bpaParam.Required)
	}
	if bpaParam.Type != "boolean" {
		t.Errorf("blockPublicAccess parameter must have type boolean, got Type: %q", bpaParam.Type)
	}

	// Verify resource when conditions
	vRes := adoptedBP.ResourceNamed("versioning")
	if vRes == nil {
		t.Fatalf("versioning resource missing")
	}
	if vRes.When != "params.versioning" {
		t.Errorf("versioning.When = %q, want params.versioning", vRes.When)
	}

	bpaRes := adoptedBP.ResourceNamed("public-access-block")
	if bpaRes == nil {
		t.Fatalf("public-access-block resource missing")
	}
	if bpaRes.When != "params.blockPublicAccess" {
		t.Errorf("public-access-block.When = %q, want params.blockPublicAccess", bpaRes.When)
	}

	// Verify loss report for boolean conditional parameters
	reasons := map[string]string{}
	for _, d := range report.Drops {
		if strings.HasPrefix(d.Path, "xrd.parameters.") {
			reasons[strings.TrimPrefix(d.Path, "xrd.parameters.")] = d.Reason
		}
	}
	for _, name := range []string{"versioning", "blockPublicAccess"} {
		reason, ok := reasons[name]
		if !ok {
			t.Errorf("expected drop entry for xrd.parameters.%s", name)
			continue
		}
		if strings.Contains(reason, "type") {
			t.Errorf("%s type is recoverable as boolean and must not be reported lost: %q", name, reason)
		}
		if strings.Contains(reason, "required") {
			t.Errorf("%s required is recoverable and must not be reported lost: %q", name, reason)
		}
		if !strings.Contains(reason, "default") || !strings.Contains(reason, "enum") || !strings.Contains(reason, "description") {
			t.Errorf("%s must report unrecovered default, enum, description: %q", name, reason)
		}
	}

	// 2. Test conditional with equality comparison: {{- if eq $spec.environment "prod" }}
	manifestEq := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xapps.example.org
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
  mode: Pipeline
  pipeline:
    - step: render
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        inline:
          template: |
            {{- $spec := .observed.composite.resource.spec -}}
            ---
            apiVersion: apps/v1
            kind: Deployment
            metadata:
              name: app-server
            spec:
              replicas: 1
            {{- if eq $spec.environment "prod" }}
            ---
            apiVersion: monitoring.coreos.com/v1
            kind: ServiceMonitor
            metadata:
              name: app-monitor
            spec:
              endpoints: []
            {{- end }}
            {{- if ne $spec.tier "dev" }}
            ---
            apiVersion: policy/v1
            kind: PodDisruptionBudget
            metadata:
              name: app-pdb
            spec:
              minAvailable: 1
            {{- end }}
    - step: auto-ready
      functionRef:
        name: function-auto-ready
`
	adoptedEq, _, err := Adopt([]byte(manifestEq), Options{
		DefaultProviderRef: "xpkg.upbound.io/crossplane-contrib/provider-kubernetes:v0.11.0",
	})
	if err != nil {
		t.Fatalf("Adopt failed on equality condition without XRD: %v", err)
	}
	if err := adoptedEq.Validate(); err != nil {
		t.Fatalf("adopted blueprint with equality condition failed validation: %v", err)
	}

	envParam, ok := adoptedEq.Spec.XRD.Parameters["environment"]
	if !ok {
		t.Fatalf("environment parameter missing from adopted blueprint")
	}
	if !envParam.Required {
		t.Errorf("environment parameter must be required, got Required: %v", envParam.Required)
	}
	if envParam.Type != "string" {
		t.Errorf("environment parameter must have type string, got Type: %q", envParam.Type)
	}

	tierParam, ok := adoptedEq.Spec.XRD.Parameters["tier"]
	if !ok {
		t.Fatalf("tier parameter missing from adopted blueprint")
	}
	if !tierParam.Required {
		t.Errorf("tier parameter must be required, got Required: %v", tierParam.Required)
	}
	if tierParam.Type != "string" {
		t.Errorf("tier parameter must have type string, got Type: %q", tierParam.Type)
	}

	// Verify when condition parsing
	monRes := adoptedEq.ResourceNamed("app-monitor")
	if monRes == nil || monRes.When != `params.environment == "prod"` {
		t.Errorf("app-monitor.When = %q, want params.environment == \"prod\"", monRes.When)
	}
	pdbRes := adoptedEq.ResourceNamed("app-pdb")
	if pdbRes == nil || pdbRes.When != `params.tier != "dev"` {
		t.Errorf("app-pdb.When = %q, want params.tier != \"dev\"", pdbRes.When)
	}

	// 3. Test loop bound: {{- range $i := until (int $spec.replicas) }}
	manifestLoop := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xclusters.example.org
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XCluster
  mode: Pipeline
  pipeline:
    - step: render
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        inline:
          template: |
            {{- $spec := .observed.composite.resource.spec -}}
            {{- range $i := until (int $spec.replicas) }}
            ---
            apiVersion: apps/v1
            kind: Deployment
            metadata:
              name: {{ printf "worker-%d" $i }}
            spec:
              replicas: 1
            {{- end }}
    - step: auto-ready
      functionRef:
        name: function-auto-ready
`
	adoptedLoop, _, err := Adopt([]byte(manifestLoop), Options{
		DefaultProviderRef: "xpkg.upbound.io/crossplane-contrib/provider-kubernetes:v0.11.0",
	})
	if err != nil {
		t.Fatalf("Adopt failed on loop bound without XRD: %v", err)
	}
	if err := adoptedLoop.Validate(); err != nil {
		t.Fatalf("adopted blueprint with loop bound failed validation: %v", err)
	}

	repParam, ok := adoptedLoop.Spec.XRD.Parameters["replicas"]
	if !ok {
		t.Fatalf("replicas parameter missing from adopted blueprint")
	}
	if !repParam.Required {
		t.Errorf("replicas parameter must be required, got Required: %v", repParam.Required)
	}
	if repParam.Type != "integer" {
		t.Errorf("replicas parameter must have type integer, got Type: %q", repParam.Type)
	}
}

// TestCF190AdoptLossReportRequiredParamChange tests CF-190 (#76):
// When adopting a Composition without its XRD turns an optional parameter into a
// required parameter to keep the document renderable, that change must appear in
// the loss/change report, naming the parameter and the old and new flag.
func TestCF190AdoptLossReportRequiredParamChange(t *testing.T) {
	compYAML := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xworkloads.workloads.sparky.ee
spec:
  compositeTypeRef:
    apiVersion: workloads.sparky.ee/v1alpha1
    kind: XWorkload
  mode: Pipeline
  pipeline:
    - step: render
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        inline:
          template: |
            {{- $spec := .observed.composite.resource.spec -}}
            ---
            apiVersion: apps/v1
            kind: Deployment
            metadata:
              name: app
            spec:
              template:
                spec:
                  containers:
                    - image: {{ $spec.image | quote }}
                      name: app
            {{- if $spec.enableService }}
            ---
            apiVersion: v1
            kind: Service
            metadata:
              name: svc
            spec:
              ports:
                - port: 80
            {{- end }}
    - step: auto-ready
      functionRef:
        name: function-auto-ready
`

	baseBP := &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
		Metadata: blueprint.Metadata{
			Name: "k8s-workload",
		},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group:   "workloads.sparky.ee",
				Version: "v1alpha1",
				Kind:    "XWorkload",
				Plural:  "xworkloads",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {
						Type:     "string",
						Required: true,
					},
					"image": {
						Type:        "string",
						Required:    true,
						Description: "Container image repository and tag.",
					},
					"enableService": {
						Type:        "boolean",
						Required:    false,
						Default:     "true",
						Description: "Whether to expose the deployment via a Kubernetes Service.",
					},
				},
			},
		},
	}

	adoptedBP, report, err := Adopt([]byte(compYAML), Options{
		DefaultProviderRef: "xpkg.upbound.io/crossplane-contrib/provider-kubernetes:v0.11.0",
		BaseBlueprint:      baseBP,
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	// enableService must be flipped to required true to keep document renderable
	p, ok := adoptedBP.Spec.XRD.Parameters["enableService"]
	if !ok {
		t.Fatalf("enableService missing from adopted parameters")
	}
	if !p.Required {
		t.Errorf("enableService must be required=true to keep document renderable without schema default")
	}

	// Loss report must report true loss
	if !report.HasTrueLoss() {
		t.Fatalf("expected true loss report")
	}

	reasons := map[string]string{}
	for _, d := range report.Drops {
		if strings.HasPrefix(d.Path, "xrd.parameters.") {
			reasons[strings.TrimPrefix(d.Path, "xrd.parameters.")] = d.Reason
		}
	}

	reason, ok := reasons["enableService"]
	if !ok {
		t.Fatalf("no loss entry for xrd.parameters.enableService; drops: %+v", report.Drops)
	}

	// Must name the old and new flag ("optional" and "required")
	if !strings.Contains(reason, "required") {
		t.Errorf("xrd.parameters.enableService reason must use the word 'required'; got: %q", reason)
	}
	if !strings.Contains(reason, "optional") {
		t.Errorf("xrd.parameters.enableService reason must name the old flag 'optional'; got: %q", reason)
	}

	// Must also name unrecovered facets (default, enum, description)
	for _, facet := range []string{"default", "enum", "description"} {
		if !strings.Contains(reason, facet) {
			t.Errorf("xrd.parameters.enableService reason must name %s as unrecovered; got: %q", facet, reason)
		}
	}

	// image was already required in baseBP: must NOT claim its required flag changed
	imageReason, ok := reasons["image"]
	if !ok {
		t.Fatalf("no loss entry for xrd.parameters.image; drops: %+v", report.Drops)
	}
	if strings.Contains(imageReason, "optional") {
		t.Errorf("image was already required, its reason must not mention optional; got: %q", imageReason)
	}
}

// TestCF205AdoptNumberTypeFromCRDSchema tests CF-205 (#91):
// When recovering parameter types in XRD-less adoption, check the wired target
// fields against the loaded CRD schema store. If the field is an integer/number/boolean
// according to the CRD schema, type the parameter as number (or boolean / integer)
// instead of falling back to string.
func TestCF205AdoptNumberTypeFromCRDSchema(t *testing.T) {
	crdYAML := `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: queues.sqs.aws.m.upbound.io
spec:
  group: sqs.aws.m.upbound.io
  scope: Namespaced
  names:
    kind: Queue
    plural: queues
    categories: [managed]
  versions:
  - name: v1beta1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        properties:
          spec:
            properties:
              forProvider:
                required: [region]
                properties:
                  region: {type: string}
                  maxMessageSize: {type: number}
                  messageRetentionSeconds: {type: integer}
                  fifoQueue: {type: boolean}
              providerConfigRef:
                type: object
                required: [name]
                properties:
                  kind: {type: string}
                  name: {type: string}
          status:
            properties:
              atProvider:
                properties:
                  arn: {type: string}
`
	crds, err := schema.ParseCRDs([][]byte{[]byte(crdYAML)})
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}

	providerRef := "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0"
	cacheDir := t.TempDir()
	store := cache.New(cacheDir)
	if err := store.SaveCRDs(providerRef, "sha256:test", crds); err != nil {
		t.Fatalf("SaveCRDs: %v", err)
	}

	goldenManifest, err := os.ReadFile("../../testdata/xqueue-pipeline.composition.golden.yaml")
	if err != nil {
		t.Fatalf("read golden composition: %v", err)
	}

	// 1. Adopt with schema store loaded.
	bp, report, err := Adopt(goldenManifest, Options{
		Store:    store,
		CacheDir: cacheDir,
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	param, ok := bp.Spec.XRD.Parameters["maxMessageSize"]
	if !ok {
		t.Fatalf("parameter maxMessageSize missing from adopted blueprint")
	}

	// The wire targets Queue.forProvider.maxMessageSize, which has type "number" in the CRD schema.
	if param.Type != "number" {
		t.Errorf("maxMessageSize parameter type = %q, want %q", param.Type, "number")
	}

	// The loss report must NOT claim type could not be recovered.
	for _, d := range report.Drops {
		if d.Path == "xrd.parameters.maxMessageSize" && strings.Contains(d.Reason, "type") {
			t.Errorf("unexpected type loss recorded for maxMessageSize: %s", d.Reason)
		}
	}

	// Generation against the loaded CRDs must succeed without type incompatibility errors.
	if _, err := emit.Generate(bp, crds, t.TempDir(), emit.WithDraftPreview()); err != nil {
		t.Errorf("emit.Generate failed on adopted blueprint: %v", err)
	}

	// 2. Also test integer and boolean target fields recovery in XRD-less adoption.
	intBoolManifest := `
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
  - step: render-resources
    functionRef:
      name: function-go-templating
    input:
      apiVersion: gotemplating.fn.crossplane.io/v1beta1
      kind: GoTemplate
      source: Inline
      inline:
        template: |
          {{- $spec := .observed.composite.resource.spec -}}
          ---
          apiVersion: sqs.aws.m.upbound.io/v1beta1
          kind: Queue
          metadata:
            annotations:
              {{ setResourceNameAnnotation "main-queue" }}
          spec:
            forProvider:
              region: 'eu-north-1'
              retention: {{ $spec.retentionPeriod }}
              isFifo: {{ $spec.fifo }}
            providerConfigRef:
              kind: ClusterProviderConfig
              name: {{ $spec.providerName }}
`
	// Adjust CRD for retentionPeriod and fifo fields
	crdYAML2 := strings.Replace(crdYAML, "messageRetentionSeconds: {type: integer}", "retention: {type: integer}", 1)
	crdYAML2 = strings.Replace(crdYAML2, "fifoQueue: {type: boolean}", "isFifo: {type: boolean}", 1)
	crds2, err := schema.ParseCRDs([][]byte{[]byte(crdYAML2)})
	if err != nil {
		t.Fatalf("ParseCRDs 2: %v", err)
	}
	cacheDir2 := t.TempDir()
	store2 := cache.New(cacheDir2)
	if err := store2.SaveCRDs(providerRef, "sha256:test", crds2); err != nil {
		t.Fatalf("SaveCRDs 2: %v", err)
	}

	bp2, report2, err := Adopt([]byte(intBoolManifest), Options{
		Store:    store2,
		CacheDir: cacheDir2,
	})
	if err != nil {
		t.Fatalf("Adopt int/bool manifest failed: %v", err)
	}

	pRet := bp2.Spec.XRD.Parameters["retentionPeriod"]
	if pRet.Type != "integer" {
		t.Errorf("retentionPeriod parameter type = %q, want %q", pRet.Type, "integer")
	}
	pFifo := bp2.Spec.XRD.Parameters["fifo"]
	if pFifo.Type != "boolean" {
		t.Errorf("fifo parameter type = %q, want %q", pFifo.Type, "boolean")
	}

	for _, d := range report2.Drops {
		if (d.Path == "xrd.parameters.retentionPeriod" || d.Path == "xrd.parameters.fifo") && strings.Contains(d.Reason, "type") {
			t.Errorf("unexpected type loss recorded: path=%s reason=%s", d.Path, d.Reason)
		}
	}

	// 3. Verify fallback behavior when store has no schema for the provider:
	// falls back to string and records the loss text.
	bpNoStore, reportNoStore, err := Adopt(goldenManifest, Options{})
	if err != nil {
		t.Fatalf("Adopt without store failed: %v", err)
	}
	pNoStore := bpNoStore.Spec.XRD.Parameters["maxMessageSize"]
	if pNoStore.Type != "string" {
		t.Errorf("without store, maxMessageSize type = %q, want string", pNoStore.Type)
	}
	foundLoss := false
	for _, d := range reportNoStore.Drops {
		if d.Path == "xrd.parameters.maxMessageSize" && strings.Contains(d.Reason, "type (rendered unquoted") {
			foundLoss = true
		}
	}
	if !foundLoss {
		t.Errorf("expected type loss to be recorded when store has no schema, drops: %+v", reportNoStore.Drops)
	}
}
