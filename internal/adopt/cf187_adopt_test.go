package adopt

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/emit"
)

func TestCF187_AdoptRoundTrip_DeclaredConfigs(t *testing.T) {
	bpYAML := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xirsa
spec:
  sources:
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
  xrd:
    group: platform.example.org
    kind: XIrsa
    plural: xirsas
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName: {type: string, required: true}
  environment:
    clusterName:
      type: string
    region:
      type: string
      default: "us-east-1"
  environmentConfigs:
    - name: cluster-prod-eu
      selector:
        matchLabels:
          cluster: prod-eu
      data:
        clusterName: prod-eu
    - name: default
  resources:
    - name: main-queue
      kind: Queue
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
      fields:
        region: {from: env.region}
`
	bpOriginal, err := blueprint.Parse([]byte(bpYAML))
	if err != nil {
		t.Fatalf("blueprint.Parse failed: %v", err)
	}

	crds := testCRDs(t)
	compGen1, err := emit.Composition(bpOriginal, crds)
	if err != nil {
		t.Fatalf("emit.Composition failed: %v", err)
	}

	bpAdopted, report, err := Adopt(compGen1, Options{
		DefaultProviderRef: "xpkg.upbound.io/upbound/provider-aws-sqs:v2",
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if report.HasTrueLoss() {
		t.Errorf("unexpected true loss: %+v", report.Drops)
	}

	compGen2, err := emit.Composition(bpAdopted, crds)
	if err != nil {
		t.Fatalf("emit.Composition on adopted blueprint failed: %v", err)
	}

	if diff := cmp.Diff(string(compGen1), string(compGen2)); diff != "" {
		t.Errorf("Round-trip Composition diff (-gen1 +gen2):\n%s", diff)
	}
}
