package blueprint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const valid = `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue
spec:
  sources:
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
  xrd:
    group: platform.hooli.tech
    kind: XQueue
    plural: xqueues
    version: v1alpha1
    scope: Namespaced
    parameters:
      location: {type: string, required: true, enum: [EU, US]}
      providerName: {type: string, required: true}
      maxMessageSize: {type: integer}
  resources:
    - name: main-queue
      kind: Queue
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
      fields:
        maxMessageSize: {from: params.maxMessageSize}
`

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "bp.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadValidBlueprint(t *testing.T) {
	b, err := Load(write(t, valid))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if b.Spec.XRD.Kind != "XQueue" {
		t.Errorf("Kind = %q, want XQueue", b.Spec.XRD.Kind)
	}
	if got := b.Spec.XRD.Parameters["location"]; !got.Required || len(got.Enum) != 2 {
		t.Errorf("location = %+v, want required with 2 enum values", got)
	}
	if len(b.Spec.Resources) != 1 || b.Spec.Resources[0].Name != "main-queue" {
		t.Errorf("resources = %+v, want one named main-queue", b.Spec.Resources)
	}
}

func TestValidateRejectsMissingScope(t *testing.T) {
	body := strings.Replace(valid, "    scope: Namespaced\n", "", 1)
	_, err := Load(write(t, body))
	if err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("err = %v, want a complaint about scope", err)
	}
}

func TestValidateRejectsLegacyClusterScope(t *testing.T) {
	body := strings.Replace(valid, "scope: Namespaced", "scope: LegacyCluster", 1)
	_, err := Load(write(t, body))
	if err == nil || !strings.Contains(err.Error(), "LegacyCluster") {
		t.Fatalf("err = %v, want LegacyCluster to be refused", err)
	}
}

func TestValidateRejectsUnknownParameterType(t *testing.T) {
	body := strings.Replace(valid, "maxMessageSize: {type: integer}", "maxMessageSize: {type: int}", 1)
	_, err := Load(write(t, body))
	if err == nil || !strings.Contains(err.Error(), "int") {
		t.Fatalf("err = %v, want an unknown-type error naming int", err)
	}
}

func TestValidateRejectsFieldWithTwoSources(t *testing.T) {
	body := strings.Replace(valid,
		"maxMessageSize: {from: params.maxMessageSize}",
		"maxMessageSize: {from: params.maxMessageSize, value: \"1024\"}", 1)
	_, err := Load(write(t, body))
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("err = %v, want a complaint that a field has more than one source", err)
	}
}

func TestValidateRejectsUnknownParameterReference(t *testing.T) {
	body := strings.Replace(valid, "params.maxMessageSize}", "params.nope}", 1)
	_, err := Load(write(t, body))
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v, want an error naming the unknown parameter", err)
	}
}
