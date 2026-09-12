package adopt

import (
	"strings"
	"testing"
)

// cf448Composition wraps a Go-template body in a minimal pipeline Composition.
func cf448Composition(name, body string) string {
	indented := strings.ReplaceAll(body, "\n", "\n            ")
	return `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: ` + name + `
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XStorage
  mode: Pipeline
  pipeline:
    - step: render
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        source: Inline
        inline:
          template: |
            ` + indented + `
`
}

// Go template comments render to nothing, so adopt must treat them as absent
// rather than as expressions: they must not break chunk parsing and must not
// survive into field values.
func TestCF448AdoptSkipsGoTemplateComments(t *testing.T) {
	t.Run("standalone comments do not drop the resource", func(t *testing.T) {
		manifest := cf448Composition("test-comments", `---
apiVersion: s3.aws.upbound.io/v1beta1
kind: Bucket
{{/* Configure bucket metadata */}}
metadata:
  name: test-bucket
spec:
  forProvider:
    {{- /* Regional placement */ -}}
    region: us-east-1`)

		bp, report, err := Adopt([]byte(manifest), Options{})
		if err != nil {
			t.Fatalf("Adopt failed: %v", err)
		}
		if report != nil && len(report.Drops) > 0 {
			t.Errorf("expected 0 loss drops, got %d: %+v", len(report.Drops), report.Drops)
		}
		res := bp.ResourceNamed("test-bucket")
		if res == nil {
			t.Fatalf("resource test-bucket not recovered from the template; resources: %+v", bp.Spec.Resources)
		}
		if res.Kind != "Bucket" {
			t.Errorf("resource kind: got %q, want %q", res.Kind, "Bucket")
		}
		fld, ok := res.Fields["region"]
		if !ok {
			t.Fatalf("region field missing; fields: %+v", res.Fields)
		}
		if fld.Value != "us-east-1" {
			t.Errorf("region: got %+v, want Value %q", fld, "us-east-1")
		}
	})

	t.Run("a trailing comment does not contaminate the literal", func(t *testing.T) {
		manifest := cf448Composition("test-inline-comments", `---
apiVersion: s3.aws.upbound.io/v1beta1
kind: Bucket
metadata:
  name: test-bucket
spec:
  forProvider:
    region: us-east-1 {{/* Regional placement */}}`)

		bp, report, err := Adopt([]byte(manifest), Options{})
		if err != nil {
			t.Fatalf("Adopt failed: %v", err)
		}
		if report != nil && len(report.Drops) > 0 {
			t.Errorf("expected 0 loss drops, got %d: %+v", len(report.Drops), report.Drops)
		}
		res := bp.ResourceNamed("test-bucket")
		if res == nil {
			t.Fatalf("resource test-bucket not recovered from the template; resources: %+v", bp.Spec.Resources)
		}
		fld, ok := res.Fields["region"]
		if !ok {
			t.Fatalf("region field missing; fields: %+v", res.Fields)
		}
		if strings.Contains(fld.Raw, "{{") || strings.Contains(fld.Value, "{{") ||
			strings.Contains(fld.Template, "{{") {
			t.Errorf("comment fragment survived into the field: %+v", fld)
		}
		if fld.Value != "us-east-1" {
			t.Errorf("region: got %+v, want Value %q", fld, "us-east-1")
		}
	})
}

func TestCF448CommentOnlyTemplate(t *testing.T) {
	manifest := cf448Composition("test-comments-only", `
{{/* This template has only comments */}}
{{- /* Another comment */ -}}
`)
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if report != nil && len(report.Drops) > 0 {
		t.Errorf("expected 0 loss drops for comment-only template, got %d: %+v", len(report.Drops), report.Drops)
	}
	if len(bp.Spec.Resources) != 0 {
		t.Errorf("expected 0 resources, got %d", len(bp.Spec.Resources))
	}
}

func TestCF448CommentDoesNotLeakParametersOrEnvironment(t *testing.T) {
	manifest := cf448Composition("test-comments-leak", `---
apiVersion: s3.aws.upbound.io/v1beta1
kind: Bucket
{{/* Note: $spec.ignoredParam should not create a parameter */}}
{{/* Note: $env.IGNORED_ENV should not create an environment key */}}
{{- /* Note: .spec.anotherIgnored should not wire */ -}}
metadata:
  name: test-bucket
spec:
  forProvider:
    region: us-east-1`)

	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if report != nil && len(report.Drops) > 0 {
		t.Errorf("expected 0 loss drops, got %d: %+v", len(report.Drops), report.Drops)
	}
	if bp.Spec.XRD.Parameters != nil {
		if _, ok := bp.Spec.XRD.Parameters["ignoredParam"]; ok {
			t.Errorf("comment prose leaked into parameters: ignoredParam found")
		}
		if _, ok := bp.Spec.XRD.Parameters["anotherIgnored"]; ok {
			t.Errorf("comment prose leaked into parameters: anotherIgnored found")
		}
	}
	if bp.Spec.Environment != nil {
		if _, ok := bp.Spec.Environment["IGNORED_ENV"]; ok {
			t.Errorf("comment prose leaked into environment: IGNORED_ENV found")
		}
	}
}

func TestCF448CommentFormsAndDelimiters(t *testing.T) {
	manifest := cf448Composition("test-comment-forms", `---
apiVersion: s3.aws.upbound.io/v1beta1
kind: Bucket
{{/*no space*/}}
{{-/*trim no space*/-}}
{{-   /*   internal whitespace   */   -}}
{{/* comment with * and / and }} and {{ */}}
metadata:
  name: test-bucket
spec:
  forProvider:
    {{/* comment 1 */}} {{/* comment 2 */}}
    region: us-east-1 {{- /* trailing with trim */ -}}`)

	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if report != nil && len(report.Drops) > 0 {
		t.Errorf("expected 0 loss drops, got %d: %+v", len(report.Drops), report.Drops)
	}
	res := bp.ResourceNamed("test-bucket")
	if res == nil {
		t.Fatalf("resource test-bucket not recovered; resources: %+v", bp.Spec.Resources)
	}
	fld, ok := res.Fields["region"]
	if !ok {
		t.Fatalf("region field missing; fields: %+v", res.Fields)
	}
	if fld.Value != "us-east-1" {
		t.Errorf("region: got %+v, want Value %q", fld, "us-east-1")
	}
}
