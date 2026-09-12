package emit

import (
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
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
	if !strings.Contains(out, "# Regenerate with: cf gen") || !strings.Contains(out, "# Source: k8s-ingress-hpa") {
		t.Errorf("RBAC output missing standard header:\n%s", out)
	}
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

func TestRBACEmissionForCustomCRDSource(t *testing.T) {
	crds := []schema.CRD{
		{
			Group:    "cert-manager.io",
			Versions: []schema.Version{{Name: "v1"}},
			Kind:     "Certificate",
			Plural:   "certificates",
			Native:   true,
		},
	}
	b := &blueprint.Blueprint{
		Metadata: blueprint.Metadata{Name: "custom-crd-app"},
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{{CRDs: "crds/cert-manager.yaml"}},
			XRD: blueprint.XRD{
				Group:  "example.org",
				Kind:   "App",
				Plural: "apps",
				Scope:  "Cluster",
			},
			Resources: []blueprint.Resource{
				{
					Name:     "cert",
					Kind:     "Certificate",
					Provider: "crds/cert-manager.yaml",
				},
			},
		},
	}

	rules, err := NonPreGrantedNativeRules(b, crds)
	if err != nil {
		t.Fatalf("NonPreGrantedNativeRules: %v", err)
	}
	if len(rules) == 0 {
		t.Fatalf("expected 1 rule for Certificate from CRD manifest source, got 0")
	}

	rbacBytes, err := RBAC(b, crds)
	if err != nil {
		t.Fatalf("RBAC: %v", err)
	}
	if rbacBytes == nil {
		t.Fatal("RBAC returned nil, want ClusterRole YAML")
	}
	out := string(rbacBytes)
	if !strings.Contains(out, `"cert-manager.io"`) {
		t.Errorf("RBAC output missing cert-manager.io apiGroup:\n%s", out)
	}
	if !strings.Contains(out, `"certificates"`) {
		t.Errorf("RBAC output missing certificates resource:\n%s", out)
	}
}

func TestRBACEmissionForClusterNative(t *testing.T) {
	crds := []schema.CRD{
		{
			Group:    "monitoring.coreos.com",
			Versions: []schema.Version{{Name: "v1"}},
			Kind:     "ServiceMonitor",
			Plural:   "servicemonitors",
			Native:   true,
		},
	}
	b := &blueprint.Blueprint{
		Metadata: blueprint.Metadata{Name: "cluster-native-app"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group:  "example.org",
				Kind:   "App",
				Plural: "apps",
				Scope:  "Cluster",
			},
			Resources: []blueprint.Resource{
				{
					Name:     "sm",
					Kind:     "ServiceMonitor",
					Provider: "cluster",
				},
			},
		},
	}

	rules, err := NonPreGrantedNativeRules(b, crds)
	if err != nil {
		t.Fatalf("NonPreGrantedNativeRules: %v", err)
	}
	if len(rules) == 0 {
		t.Fatalf("expected 1 rule for ServiceMonitor from cluster provider, got 0")
	}

	rbacBytes, err := RBAC(b, crds)
	if err != nil {
		t.Fatalf("RBAC: %v", err)
	}
	if rbacBytes == nil {
		t.Fatal("RBAC returned nil, want ClusterRole YAML")
	}
	out := string(rbacBytes)
	if !strings.Contains(out, `"monitoring.coreos.com"`) {
		t.Errorf("RBAC output missing monitoring.coreos.com apiGroup:\n%s", out)
	}
	if !strings.Contains(out, `"servicemonitors"`) {
		t.Errorf("RBAC output missing servicemonitors resource:\n%s", out)
	}
}

func TestRBACEmissionIgnoresManagedResources(t *testing.T) {
	crds := []schema.CRD{
		{
			Group:      "s3.aws.m.upbound.io",
			Versions:   []schema.Version{{Name: "v1beta1"}},
			Kind:       "Bucket",
			Plural:     "buckets",
			Categories: []string{"managed"},
			Native:     false,
		},
		{
			Group:    "cert-manager.io",
			Versions: []schema.Version{{Name: "v1"}},
			Kind:     "Certificate",
			Plural:   "certificates",
			Native:   true,
		},
	}
	b := &blueprint.Blueprint{
		Metadata: blueprint.Metadata{Name: "mixed-app"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group:  "example.org",
				Kind:   "App",
				Plural: "apps",
				Scope:  "Cluster",
			},
			Resources: []blueprint.Resource{
				{
					Name:     "bucket",
					Kind:     "Bucket",
					Provider: "upbound/provider-aws-s3",
				},
				{
					Name:     "cert",
					Kind:     "Certificate",
					Provider: "crds/cert-manager.yaml",
				},
			},
		},
	}

	rules, err := NonPreGrantedNativeRules(b, crds)
	if err != nil {
		t.Fatalf("NonPreGrantedNativeRules: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule (only Certificate), got %d: %v", len(rules), rules)
	}
	if rules[0].plural != "certificates" {
		t.Errorf("expected rule for certificates, got %s", rules[0].plural)
	}
}
