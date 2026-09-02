package blueprint

import (
	"testing"
)

func TestYAMLToJSONPreservesKeywords(t *testing.T) {
	rawYAML := `
spec:
  xrd:
    parameters:
      n:
        type: integer
      yes:
        type: string
`
	_, err := Parse([]byte(rawYAML))
	// Parse will validate root / XRD fields, but let us verify full round-trip on valid document
	if err == nil {
		t.Fatal("expected validation error on partial doc")
	}

	validYAML := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: test-bp
spec:
  xrd:
    group: platform.example.com
    kind: XApp
    plural: xapps
    version: v1alpha1
    scope: Namespaced
    parameters:
      n:
        type: integer
      providerName:
        type: string
        required: true
  resources:
    - name: app
      kind: Queue
      fields:
        maxMessageSize:
          from: params.n
`
	bp, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if p, ok := bp.Spec.XRD.Parameters["n"]; !ok || p.Type != "integer" {
		t.Errorf("parameter n not preserved as integer, got %+v", p)
	}
}
