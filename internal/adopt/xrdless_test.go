package adopt

import (
	"strings"
	"testing"
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
