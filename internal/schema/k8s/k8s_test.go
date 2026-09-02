package k8s

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/koorikla/compositionfactory/internal/schema"
)

// wantKinds is the vendored subset by (apiVersion, kind) — the composable
// native kinds the design pinned. A regeneration that drops one must fail
// here, not surface as a kind quietly missing from /api/kinds.
var wantKinds = map[string]string{
	"Deployment":              "apps/v1",
	"StatefulSet":             "apps/v1",
	"DaemonSet":               "apps/v1",
	"Job":                     "batch/v1",
	"CronJob":                 "batch/v1",
	"Service":                 "v1",
	"ConfigMap":               "v1",
	"Secret":                  "v1",
	"ServiceAccount":          "v1",
	"PersistentVolumeClaim":   "v1",
	"Ingress":                 "networking.k8s.io/v1",
	"NetworkPolicy":           "networking.k8s.io/v1",
	"HorizontalPodAutoscaler": "autoscaling/v2",
	"PodDisruptionBudget":     "policy/v1",
	"Role":                    "rbac.authorization.k8s.io/v1",
	"RoleBinding":             "rbac.authorization.k8s.io/v1",
}

func mustKinds(t *testing.T) []schema.CRD {
	t.Helper()
	kinds, err := Kinds()
	if err != nil {
		t.Fatalf("Kinds: %v", err)
	}
	return kinds
}

func kindByName(t *testing.T, name string) schema.CRD {
	t.Helper()
	for _, c := range mustKinds(t) {
		if c.Kind == name {
			return c
		}
	}
	t.Fatalf("kind %q not among the vendored native kinds", name)
	return schema.CRD{}
}

func leafPaths(t *testing.T, c schema.CRD) map[string]*schema.Node {
	t.Helper()
	nodes, err := c.FieldTree()
	if err != nil {
		t.Fatalf("%s FieldTree: %v", c.Kind, err)
	}
	out := make(map[string]*schema.Node)
	for _, l := range schema.Leaves(nodes, "") {
		out[l.Path] = l.Node
	}
	return out
}

func TestKindsServesThePinnedSubset(t *testing.T) {
	kinds := mustKinds(t)
	if len(kinds) != len(wantKinds) {
		t.Fatalf("Kinds returned %d kinds, want %d", len(kinds), len(wantKinds))
	}
	for _, c := range kinds {
		wantAPIVersion, ok := wantKinds[c.Kind]
		if !ok {
			t.Errorf("unexpected native kind %q", c.Kind)
			continue
		}
		av, err := c.APIVersion()
		if err != nil {
			t.Errorf("%s APIVersion: %v", c.Kind, err)
			continue
		}
		if av != wantAPIVersion {
			t.Errorf("%s apiVersion = %q, want %q", c.Kind, av, wantAPIVersion)
		}
		if !c.Native {
			t.Errorf("%s: Native must be set — it is what routes the emitter around the forProvider envelope", c.Kind)
		}
		if c.IsManaged() {
			t.Errorf("%s: a native kind must never claim the managed category — label, don't blur", c.Kind)
		}
		if !c.Namespaced() {
			t.Errorf("%s: every vendored kind is a namespaced object", c.Kind)
		}
		if c.Plural == "" {
			t.Errorf("%s: no plural", c.Kind)
		}
	}
}

// The design's own acceptance line for the schema shape: a Deployment must
// expose the NESTED pod-template hierarchy through the same BuildTree/Leaves
// pipeline the CRD path uses — indexed paths for object arrays, whole plain
// paths for scalar arrays, map leaves for additionalProperties.
func TestDeploymentFieldTreeReachesThePodTemplate(t *testing.T) {
	paths := leafPaths(t, kindByName(t, "Deployment"))

	wantTypes := map[string]string{
		"spec.replicas":                          "integer",
		"spec.selector.matchLabels":              "map",
		"spec.template.metadata.labels":          "map",
		"spec.template.spec.containers[0].name":  "string",
		"spec.template.spec.containers[0].image": "string",
		// A scalar array is assigned whole: plain path, no [0].
		"spec.template.spec.containers[0].args": "array",
		// An object array nests further, each level indexed.
		"spec.template.spec.containers[0].ports[0].containerPort": "integer",
		"spec.template.spec.volumes[0].configMap.name":            "string",
	}
	for path, wantType := range wantTypes {
		node, ok := paths[path]
		if !ok {
			t.Errorf("Deployment field tree is missing %q", path)
			continue
		}
		if node.Type != wantType {
			t.Errorf("%s type = %q, want %q", path, node.Type, wantType)
		}
	}

	for _, absent := range []string{
		"apiVersion", "kind", "status.replicas",
	} {
		if _, ok := paths[absent]; ok {
			t.Errorf("Deployment field tree must not offer %q", absent)
		}
	}
	if _, ok := paths["metadata.name"]; !ok {
		t.Error("Deployment field tree must offer metadata.name")
	}
	// …but the pod template's own metadata is a real, settable subtree.
	if _, ok := paths["spec.template.metadata.annotations"]; !ok {
		t.Error("spec.template.metadata.annotations missing: only TOP-LEVEL metadata is excluded, not the pod template's")
	}

	// The descriptions are the field help text; resolution must carry them
	// through $ref indirection, not lose them to the reference node.
	if node := paths["spec.template.spec.containers[0].image"]; node != nil && node.Description == "" {
		t.Error("containers[0].image has no description — $ref resolution dropped the upstream docs")
	}
}

// required must survive the allOf/$ref flattening: DeploymentSpec declares
// selector and template required, and their leaves' Required flags are what
// the index counts and the canvas badges.
func TestRequiredSurvivesResolution(t *testing.T) {
	nodes, err := kindByName(t, "Deployment").FieldTree()
	if err != nil {
		t.Fatal(err)
	}
	var spec *schema.Node
	for _, n := range nodes {
		if n.Name == "spec" {
			spec = n
		}
	}
	if spec == nil {
		t.Fatal("Deployment has no spec node")
	}
	required := map[string]bool{}
	for _, child := range spec.Children {
		required[child.Name] = child.Required
	}
	if !required["selector"] || !required["template"] {
		t.Errorf("DeploymentSpec requires selector and template upstream; got %v", required)
	}
	if required["replicas"] {
		t.Error("replicas is optional upstream but came through required")
	}
}

// IntOrString and Quantity carry oneOf and no type upstream; the resolver
// normalizes both to a string leaf — the one deliberate lossy step, pinned
// here so a future resolver change cannot silently turn them into typeless
// nodes BuildTree treats as empty leaves.
func TestIntOrStringResolvesToAStringLeaf(t *testing.T) {
	paths := leafPaths(t, kindByName(t, "Service"))
	node, ok := paths["spec.ports[0].targetPort"]
	if !ok {
		t.Fatal("Service field tree is missing spec.ports[0].targetPort")
	}
	if node.Type != "string" {
		t.Errorf("targetPort (IntOrString) type = %q, want string", node.Type)
	}

	dep := leafPaths(t, kindByName(t, "Deployment"))
	limits, ok := dep["spec.template.spec.containers[0].resources.limits"]
	if !ok {
		t.Fatal("Deployment field tree is missing container resources.limits")
	}
	if limits.Type != "map" {
		t.Errorf("resources.limits (map of Quantity) type = %q, want map", limits.Type)
	}
}

func TestConfigMapAndSecretExposeTheirDataFields(t *testing.T) {
	cm := leafPaths(t, kindByName(t, "ConfigMap"))
	for _, path := range []string{"data", "binaryData", "immutable"} {
		if _, ok := cm[path]; !ok {
			t.Errorf("ConfigMap field tree is missing %q", path)
		}
	}
	if cm["data"] != nil && cm["data"].Type != "map" {
		t.Errorf("ConfigMap data type = %q, want map", cm["data"].Type)
	}
	sec := leafPaths(t, kindByName(t, "Secret"))
	for _, path := range []string{"data", "stringData", "type"} {
		if _, ok := sec[path]; !ok {
			t.Errorf("Secret field tree is missing %q", path)
		}
	}
}

// A native kind has no Crossplane envelope: no forProvider to strip, nothing
// left over to serve. Envelope must say "empty", not echo the object's spec.
func TestNativeKindsHaveNoEnvelope(t *testing.T) {
	for _, c := range mustKinds(t) {
		nodes, err := c.Envelope()
		if err != nil {
			t.Errorf("%s Envelope: %v", c.Kind, err)
			continue
		}
		if len(nodes) != 0 {
			t.Errorf("%s Envelope returned %d nodes, want none — a native object has no Crossplane wrapper", c.Kind, len(nodes))
		}
	}
}

// Determinism is a correctness requirement: two loads must produce the same
// trees byte-for-byte once serialized, or generated output could churn.
func TestKindsIsDeterministic(t *testing.T) {
	a := mustKinds(t)
	b := mustKinds(t)
	aj, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	bj, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(aj, bj) {
		t.Error("two Kinds() calls serialized differently")
	}
}

// The returned slice must be the caller's to append to — cmd/cf/gen.go
// appends provider CRDs and native kinds into one working slice.
func TestKindsReturnsACopy(t *testing.T) {
	a := mustKinds(t)
	a[0] = schema.CRD{Kind: "Mangled"}
	b := mustKinds(t)
	for _, c := range b {
		if c.Kind == "Mangled" {
			t.Fatal("mutating a returned slice leaked into the cache")
		}
	}
}

// The vendored files record the Kubernetes version they were generated
// from; the loader's pin check is what turns "bumped the constant, forgot to
// regenerate" into a loud failure. This test pins the recorded versions so
// the two can never drift silently even if the loader's check changes.
func TestVendoredFilesRecordThePinnedVersion(t *testing.T) {
	for _, name := range []string{
		"openapi_apps_v1.json",
		"openapi_autoscaling_v2.json",
		"openapi_batch_v1.json",
		"openapi_core_v1.json",
		"openapi_networking_v1.json",
		"openapi_policy_v1.json",
		"openapi_rbac_v1.json",
	} {
		raw, err := vendored.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		var f vendoredFile
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		if f.K8sVersion != Version {
			t.Errorf("%s records k8sVersion %q, but the package pins %q — run: go run ./internal/schema/k8s/gen", name, f.K8sVersion, Version)
		}
		if f.Source == "" || f.SourceSHA256 == "" {
			t.Errorf("%s is missing its provenance (source %q, sourceSha256 %q)", name, f.Source, f.SourceSHA256)
		}
		if len(f.Schemas) == 0 {
			t.Errorf("%s carries no schemas", name)
		}
	}
}

// TestNativeKindTreesAreMemoised pins the memo on hand-built native kinds:
// the vendored Deployment is the largest tree the palette and inspector
// serve, and without CRD.Cached() every request rebuilt it from the raw
// OpenAPI map.
func TestNativeKindTreesAreMemoised(t *testing.T) {
	kinds, err := Kinds()
	if err != nil {
		t.Fatal(err)
	}
	var dep *schema.CRD
	for i := range kinds {
		if kinds[i].Kind == "Deployment" {
			dep = &kinds[i]
		}
	}
	if dep == nil {
		t.Fatal("no Deployment among native kinds")
	}
	first, err := dep.FieldTree()
	if err != nil {
		t.Fatal(err)
	}
	second, err := dep.FieldTree()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) == 0 || len(second) == 0 {
		t.Fatal("empty Deployment field tree")
	}
	if first[0] != second[0] {
		t.Fatalf("Deployment.FieldTree() rebuilt the tree on a repeated call; want the memoised graph")
	}
}

func TestKindNames(t *testing.T) {
	names := KindNames()
	if len(names) != len(wantKinds) {
		t.Fatalf("KindNames returned %d names, want %d", len(names), len(wantKinds))
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Errorf("KindNames not sorted: %s >= %s", names[i-1], names[i])
		}
	}
	for _, name := range names {
		if _, ok := wantKinds[name]; !ok {
			t.Errorf("unexpected kind in KindNames: %s", name)
		}
	}
}

func TestNewNativeKindsExposeFieldTrees(t *testing.T) {
	tests := []struct {
		kind      string
		wantPaths map[string]string
	}{
		{
			kind: "Ingress",
			wantPaths: map[string]string{
				"spec.rules[0].host":                               "string",
				"spec.rules[0].http.paths[0].path":                 "string",
				"spec.rules[0].http.paths[0].backend.service.name": "string",
			},
		},
		{
			kind: "HorizontalPodAutoscaler",
			wantPaths: map[string]string{
				"spec.scaleTargetRef.kind": "string",
				"spec.minReplicas":         "integer",
				"spec.maxReplicas":         "integer",
			},
		},
		{
			kind: "PersistentVolumeClaim",
			wantPaths: map[string]string{
				"spec.accessModes":        "array",
				"spec.resources.requests": "map",
			},
		},
		{
			kind: "NetworkPolicy",
			wantPaths: map[string]string{
				"spec.podSelector.matchLabels":                    "map",
				"spec.ingress[0].from[0].podSelector.matchLabels": "map",
			},
		},
		{
			kind: "PodDisruptionBudget",
			wantPaths: map[string]string{
				"spec.minAvailable":         "string",
				"spec.selector.matchLabels": "map",
			},
		},
		{
			kind: "Role",
			wantPaths: map[string]string{
				"rules[0].apiGroups": "array",
				"rules[0].resources": "array",
				"rules[0].verbs":     "array",
			},
		},
		{
			kind: "RoleBinding",
			wantPaths: map[string]string{
				"roleRef.kind":     "string",
				"subjects[0].kind": "string",
				"subjects[0].name": "string",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			paths := leafPaths(t, kindByName(t, tt.kind))
			for path, wantType := range tt.wantPaths {
				node, ok := paths[path]
				if !ok {
					t.Errorf("%s missing path %q", tt.kind, path)
					continue
				}
				if node.Type != wantType {
					t.Errorf("%s path %q type = %q, want %q", tt.kind, path, node.Type, wantType)
				}
			}
		})
	}
}
