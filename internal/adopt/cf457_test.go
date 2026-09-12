package adopt

import (
	"strings"
	"testing"
)

func TestCF457_FromCompositeFieldPath_IndexedEnvelopePatch(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp
spec:
  compositeTypeRef:
    apiVersion: example.org/v1
    kind: App
  resources:
    - name: bucket
      base:
        apiVersion: s3.aws.upbound.io/v1beta1
        kind: Bucket
        spec:
          forProvider:
            region: us-east-1
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.policy
          toFieldPath: spec.managementPolicies[0]
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if bp == nil {
		t.Fatal("expected non-nil blueprint")
	}
	res := bp.ResourceNamed("bucket")
	if res == nil {
		t.Fatal("expected resource bucket in adopted blueprint")
	}
	if res.Envelope != nil {
		if _, exists := res.Envelope["managementPolicies[0]"]; exists {
			t.Errorf("res.Envelope should not contain indexed key managementPolicies[0]")
		}
	}
	if report == nil {
		t.Fatal("expected report to be non-nil")
	}
	foundDrop := false
	for _, d := range report.Drops {
		if d.Path == "resource.bucket.patches[0]" && d.Reason == `unsupported toFieldPath "spec.managementPolicies[0]" in patch` {
			foundDrop = true
			break
		}
	}
	if !foundDrop {
		t.Errorf("expected drop for resource.bucket.patches[0] with unsupported toFieldPath, got: %+v", report.Drops)
	}
}

func TestCF457_FromEnvironmentFieldPath_IndexedEnvelopePatch(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp-env
spec:
  compositeTypeRef:
    apiVersion: example.org/v1
    kind: App
  resources:
    - name: bucket
      base:
        apiVersion: s3.aws.upbound.io/v1beta1
        kind: Bucket
        spec:
          forProvider:
            region: us-east-1
      patches:
        - type: FromEnvironmentFieldPath
          fromFieldPath: policy
          toFieldPath: spec.managementPolicies[0]
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if bp == nil {
		t.Fatal("expected non-nil blueprint")
	}
	res := bp.ResourceNamed("bucket")
	if res == nil {
		t.Fatal("expected resource bucket in adopted blueprint")
	}
	if res.Envelope != nil {
		if _, exists := res.Envelope["managementPolicies[0]"]; exists {
			t.Errorf("res.Envelope should not contain indexed key managementPolicies[0]")
		}
	}
	if report == nil {
		t.Fatal("expected report to be non-nil")
	}
	foundDrop := false
	for _, d := range report.Drops {
		if d.Path == "resource.bucket.patches[0]" && d.Reason == `unsupported toFieldPath "spec.managementPolicies[0]" in patch` {
			foundDrop = true
			break
		}
	}
	if !foundDrop {
		t.Errorf("expected drop for resource.bucket.patches[0] with unsupported toFieldPath, got: %+v", report.Drops)
	}
}

func TestCF457_ValidEnvelopePatchRetained(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp-valid
spec:
  compositeTypeRef:
    apiVersion: example.org/v1
    kind: App
  resources:
    - name: bucket
      base:
        apiVersion: s3.aws.upbound.io/v1beta1
        kind: Bucket
        spec:
          forProvider:
            region: us-east-1
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.deletionPolicy
          toFieldPath: spec.deletionPolicy
        - type: FromEnvironmentFieldPath
          fromFieldPath: configName
          toFieldPath: spec.providerConfigRef.name
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	res := bp.ResourceNamed("bucket")
	if res == nil {
		t.Fatal("expected resource bucket")
	}
	if f, ok := res.Envelope["deletionPolicy"]; !ok || f.From != "params.deletionPolicy" {
		t.Errorf("expected envelope deletionPolicy wired to params.deletionPolicy, got: %+v", res.Envelope["deletionPolicy"])
	}
	if f, ok := res.Envelope["providerConfigRef.name"]; !ok || f.From != "env.configName" {
		t.Errorf("expected envelope providerConfigRef.name wired to env.configName, got: %+v", res.Envelope["providerConfigRef.name"])
	}
	if report != nil {
		for _, d := range report.Drops {
			if strings.HasPrefix(d.Path, "resource.bucket.patches") {
				t.Errorf("unexpected patch drop for valid envelope patch: %+v", d)
			}
		}
	}
}

func TestCF457_InvalidEnvelopeKeyKeywordsRejected(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp-kw
spec:
  compositeTypeRef:
    apiVersion: example.org/v1
    kind: App
  resources:
    - name: bucket
      base:
        apiVersion: s3.aws.upbound.io/v1beta1
        kind: Bucket
        spec:
          forProvider:
            region: us-east-1
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.flag
          toFieldPath: spec.true
        - type: FromEnvironmentFieldPath
          fromFieldPath: flag
          toFieldPath: spec.yes
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	res := bp.ResourceNamed("bucket")
	if res == nil {
		t.Fatal("expected resource bucket")
	}
	if _, ok := res.Envelope["true"]; ok {
		t.Errorf("res.Envelope should not contain keyword 'true'")
	}
	if _, ok := res.Envelope["yes"]; ok {
		t.Errorf("res.Envelope should not contain keyword 'yes'")
	}
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	dropsByPath := make(map[string]string)
	for _, d := range report.Drops {
		dropsByPath[d.Path] = d.Reason
	}
	if r, ok := dropsByPath["resource.bucket.patches[0]"]; !ok || r != `unsupported toFieldPath "spec.true" in patch` {
		t.Errorf("expected drop for patch 0 with unsupported toFieldPath, got: %q", r)
	}
	if r, ok := dropsByPath["resource.bucket.patches[1]"]; !ok || r != `unsupported toFieldPath "spec.yes" in patch` {
		t.Errorf("expected drop for patch 1 with unsupported toFieldPath, got: %q", r)
	}
}
