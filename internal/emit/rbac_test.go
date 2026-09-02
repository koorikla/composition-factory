package emit

import (
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema/k8s"
)

func TestRBACNilWhenOnlyPreGranted(t *testing.T) {
	k8sCRDs, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}
	b := &blueprint.Blueprint{
		Metadata: blueprint.Metadata{Name: "k8s-pregranted"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{Group: "example.org", Kind: "App", Plural: "apps"},
			Resources: []blueprint.Resource{
				{Name: "deploy", Kind: "Deployment", Provider: "k8s"},
				{Name: "svc", Kind: "Service", Provider: "k8s"},
				{Name: "cfg", Kind: "ConfigMap", Provider: "k8s"},
				{Name: "secret", Kind: "Secret", Provider: "k8s"},
				{Name: "sa", Kind: "ServiceAccount", Provider: "k8s"},
			},
		},
	}
	rbacBytes, err := RBAC(b, k8sCRDs)
	if err != nil {
		t.Fatalf("RBAC: %v", err)
	}
	if rbacBytes != nil {
		t.Errorf("RBAC returned %q, want nil for only pre-granted native kinds", string(rbacBytes))
	}
}

func TestRBACEmissionForNonPreGranted(t *testing.T) {
	k8sCRDs, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}
	b := &blueprint.Blueprint{
		Metadata: blueprint.Metadata{Name: "k8s-ingress-hpa"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{Group: "example.org", Kind: "App", Plural: "apps"},
			Resources: []blueprint.Resource{
				{Name: "deploy", Kind: "Deployment", Provider: "k8s"},
				{Name: "ing1", Kind: "Ingress", Provider: "k8s"},
				{Name: "ing2", Kind: "Ingress", Provider: "k8s"}, // duplicate kind
				{Name: "hpa", Kind: "HorizontalPodAutoscaler", Provider: "k8s"},
			},
		},
	}
	rbacBytes, err := RBAC(b, k8sCRDs)
	if err != nil {
		t.Fatalf("RBAC: %v", err)
	}
	if rbacBytes == nil {
		t.Fatal("RBAC returned nil, want ClusterRole YAML for non-pre-granted kinds")
	}
	out := string(rbacBytes)
	if !strings.Contains(out, "kind: ClusterRole") {
		t.Errorf("RBAC output missing kind: ClusterRole:\n%s", out)
	}
	if !strings.Contains(out, "name: compositionfactory:apps.example.org:aggregate-to-crossplane") {
		t.Errorf("RBAC output missing aggregated ClusterRole name:\n%s", out)
	}
	if !strings.Contains(out, `rbac.crossplane.io/aggregate-to-crossplane: "true"`) {
		t.Errorf("RBAC output missing aggregate-to-crossplane label:\n%s", out)
	}
	if !strings.Contains(out, "ingresses") || !strings.Contains(out, "horizontalpodautoscalers") {
		t.Errorf("RBAC output missing ingresses or horizontalpodautoscalers rules:\n%s", out)
	}
	if strings.Contains(out, "deployments") {
		t.Errorf("RBAC output should not include pre-granted deployments:\n%s", out)
	}
	// Verify deduplication: "ingresses" should only appear once in resources:
	if strings.Count(out, `      - "ingresses"`) != 1 {
		t.Errorf("ingresses rule appears %d times, want exactly 1", strings.Count(out, `      - "ingresses"`))
	}
}
