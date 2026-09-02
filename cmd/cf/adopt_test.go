package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdoptCLI(t *testing.T) {
	tmpDir := t.TempDir()
	compPath := filepath.Join(tmpDir, "composition.yaml")
	outBlueprintPath := filepath.Join(tmpDir, "blueprint.yaml")

	compContent := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xqueues.aws.example.org
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XQueue
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
            apiVersion: sqs.aws.upbound.io/v1beta1
            kind: Queue
            metadata:
              name: main-queue
            spec:
              forProvider:
                region: {{ $spec.region }}
`

	if err := os.WriteFile(compPath, []byte(compContent), 0644); err != nil {
		t.Fatalf("write composition: %v", err)
	}

	cmd := &AdoptCmd{
		Composition: compPath,
		Out:         outBlueprintPath,
		Provider:    "xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0",
	}

	var out bytes.Buffer
	if err := cmd.Run(&out); err != nil {
		t.Fatalf("run: %v", err)
	}

	if !strings.Contains(out.String(), "Adopted blueprint written to") {
		t.Errorf("stdout = %q, want mention of written blueprint", out.String())
	}

	bpBytes, err := os.ReadFile(outBlueprintPath)
	if err != nil {
		t.Fatalf("read generated blueprint: %v", err)
	}

	bpStr := string(bpBytes)
	if !strings.Contains(bpStr, "kind: Blueprint") {
		t.Errorf("blueprint missing 'kind: Blueprint'")
	}
	if !strings.Contains(bpStr, "region:") {
		t.Errorf("blueprint missing parameter 'region'")
	}
}
