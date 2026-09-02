// Package k8s serves the native Kubernetes kinds cf can compose directly —
// Crossplane v2 composes any Kubernetes object, and 36% of v2 Compositions
// in the corpus do — from a vendored, pinned OpenAPI subset. NO network at
// runtime: the vendored files under this directory are the only schema
// source, regenerated exclusively by `go run ./internal/schema/k8s/gen`
// against the pinned upstream tag (see Version).
//
// The output shape is deliberately the CRD path's shape: Kinds returns
// []schema.CRD with Native set, whose preferred version's Properties are the
// object's fully $ref-resolved top-level OpenAPI properties. Everything
// downstream — BuildTree, Leaves, the index, /api/fields, the emitter's
// field-path check — walks it exactly the way it walks a provider CRD, so a
// Deployment exposes spec.template.spec.containers[0].image through the same
// code path a Queue exposes region.
package k8s

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/koorikla/compositionfactory/internal/schema"
)

// Version is the pinned upstream Kubernetes release the vendored OpenAPI
// subset was extracted from. It must equal the k8sVersion recorded inside
// every vendored file — load refuses to serve a file whose pin disagrees,
// so bumping one without regenerating the other fails loudly at first use,
// never silently serving schemas from a different Kubernetes version than
// the one this constant claims.
const Version = "v1.34.1"

//go:embed openapi_apps_v1.json openapi_autoscaling_v2.json openapi_batch_v1.json openapi_core_v1.json openapi_networking_v1.json openapi_policy_v1.json openapi_rbac_v1.json
var vendored embed.FS

// vendoredFile mirrors the on-disk shape gen/main.go writes.
type vendoredFile struct {
	K8sVersion   string                     `json:"k8sVersion"`
	Source       string                     `json:"source"`
	SourceSHA256 string                     `json:"sourceSha256"`
	Schemas      map[string]json.RawMessage `json:"schemas"`
}

// nativeKind pins one composable native kind to its vendored schema. The
// plural is stated rather than derived: English pluralization is not a
// function, and the API server's own names are what RBAC and kubectl use.
type nativeKind struct {
	file       string // vendored file the schema lives in
	schemaName string // components.schemas key
	group      string // "" is the core/legacy group
	version    string
	kind       string
	plural     string
}

var nativeKinds = []nativeKind{
	{"openapi_apps_v1.json", "io.k8s.api.apps.v1.Deployment", "apps", "v1", "Deployment", "deployments"},
	{"openapi_apps_v1.json", "io.k8s.api.apps.v1.StatefulSet", "apps", "v1", "StatefulSet", "statefulsets"},
	{"openapi_apps_v1.json", "io.k8s.api.apps.v1.DaemonSet", "apps", "v1", "DaemonSet", "daemonsets"},
	{"openapi_batch_v1.json", "io.k8s.api.batch.v1.Job", "batch", "v1", "Job", "jobs"},
	{"openapi_batch_v1.json", "io.k8s.api.batch.v1.CronJob", "batch", "v1", "CronJob", "cronjobs"},
	{"openapi_core_v1.json", "io.k8s.api.core.v1.Service", "", "v1", "Service", "services"},
	{"openapi_core_v1.json", "io.k8s.api.core.v1.ConfigMap", "", "v1", "ConfigMap", "configmaps"},
	{"openapi_core_v1.json", "io.k8s.api.core.v1.Secret", "", "v1", "Secret", "secrets"},
	{"openapi_core_v1.json", "io.k8s.api.core.v1.ServiceAccount", "", "v1", "ServiceAccount", "serviceaccounts"},
	{"openapi_core_v1.json", "io.k8s.api.core.v1.PersistentVolumeClaim", "", "v1", "PersistentVolumeClaim", "persistentvolumeclaims"},
	{"openapi_networking_v1.json", "io.k8s.api.networking.v1.Ingress", "networking.k8s.io", "v1", "Ingress", "ingresses"},
	{"openapi_networking_v1.json", "io.k8s.api.networking.v1.NetworkPolicy", "networking.k8s.io", "v1", "NetworkPolicy", "networkpolicies"},
	{"openapi_autoscaling_v2.json", "io.k8s.api.autoscaling.v2.HorizontalPodAutoscaler", "autoscaling", "v2", "HorizontalPodAutoscaler", "horizontalpodautoscalers"},
	{"openapi_policy_v1.json", "io.k8s.api.policy.v1.PodDisruptionBudget", "policy", "v1", "PodDisruptionBudget", "poddisruptionbudgets"},
	{"openapi_rbac_v1.json", "io.k8s.api.rbac.v1.Role", "rbac.authorization.k8s.io", "v1", "Role", "roles"},
	{"openapi_rbac_v1.json", "io.k8s.api.rbac.v1.RoleBinding", "rbac.authorization.k8s.io", "v1", "RoleBinding", "rolebindings"},
}

// KindNames returns the names of all supported vendored native Kubernetes kinds, sorted alphabetically.
func KindNames() []string {
	names := make([]string, len(nativeKinds))
	for i, k := range nativeKinds {
		names[i] = k.kind
	}
	sort.Strings(names)
	return names
}

var (
	once      sync.Once
	cached    []schema.CRD
	cachedErr error
)

// Kinds returns the vendored native kinds as schema.CRDs, resolved once per
// process. The returned slice is a fresh copy so a caller appending to it
// (the way cmd/cf/gen.go appends provider CRDs and native kinds into one
// slice) can never corrupt the cache; the CRDs' inner maps are shared, which
// is safe because nothing downstream mutates a schema tree.
func Kinds() ([]schema.CRD, error) {
	once.Do(func() { cached, cachedErr = build() })
	if cachedErr != nil {
		return nil, cachedErr
	}
	out := make([]schema.CRD, len(cached))
	copy(out, cached)
	return out, nil
}

// build loads every vendored file, resolves each native kind's schema and
// assembles the CRDs. Any failure here is a build/vendoring defect (the
// inputs are compiled into the binary), so unlike index.Build there is no
// skip-and-continue: one bad kind fails the whole load, loudly.
func build() ([]schema.CRD, error) {
	files := make(map[string]map[string]json.RawMessage)
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
			return nil, fmt.Errorf("k8s: read vendored %s: %w", name, err)
		}
		var f vendoredFile
		if err := json.Unmarshal(raw, &f); err != nil {
			return nil, fmt.Errorf("k8s: parse vendored %s: %w", name, err)
		}
		if f.K8sVersion != Version {
			return nil, fmt.Errorf("k8s: vendored %s was generated from Kubernetes %s but this build pins %s; "+
				"run: go run ./internal/schema/k8s/gen", name, f.K8sVersion, Version)
		}
		files[name] = f.Schemas
	}

	out := make([]schema.CRD, 0, len(nativeKinds))
	for _, k := range nativeKinds {
		schemas := files[k.file]
		resolved, err := resolveRef(k.schemaName, schemas, nil)
		if err != nil {
			return nil, fmt.Errorf("k8s: %s: %w", k.kind, err)
		}
		props, ok := resolved["properties"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("k8s: %s: vendored schema %s has no properties", k.kind, k.schemaName)
		}
		out = append(out, schema.CRD{
			Group:  k.group,
			Kind:   k.kind,
			Plural: k.plural,
			// Every vendored kind is a namespaced object; a Namespaced XRD
			// composes it into the XR's own namespace.
			Scope:  "Namespaced",
			Native: true,
			Versions: []schema.Version{{
				Name:       k.version,
				Served:     true,
				Storage:    true,
				Properties: props,
			}},
		}.Cached())
	}
	return out, nil
}

const refPrefix = "#/components/schemas/"

// resolveRef materializes the named schema with resolveNode. stack carries
// the $ref names currently being resolved, for cycle detection.
func resolveRef(name string, schemas map[string]json.RawMessage, stack []string) (map[string]any, error) {
	for _, s := range stack {
		if s == name {
			// No vendored kind's closure is cyclic today. If a future
			// regeneration introduces one, refusing is the only honest move:
			// a truncated-at-an-arbitrary-depth tree would silently hide
			// fields, which is this project's central defect class.
			return nil, fmt.Errorf("schema reference cycle: %s -> %s", strings.Join(stack, " -> "), name)
		}
	}
	raw, ok := schemas[name]
	if !ok {
		return nil, fmt.Errorf("schema %q is not in the vendored subset (stack: %s)", name, strings.Join(stack, " -> "))
	}
	var node map[string]any
	if err := json.Unmarshal(raw, &node); err != nil {
		return nil, fmt.Errorf("schema %q: %w", name, err)
	}
	if name == "io.k8s.api.batch.v1.JobTemplateSpec" {
		if _, hasReq := node["required"]; !hasReq {
			node["required"] = []any{"spec"}
		}
	}
	return resolveNode(node, schemas, append(stack, name))
}

// resolveNode returns a copy of node with every $ref and allOf materialized
// into plain, self-contained OpenAPI maps — the exact shape a CRD's
// openAPIV3Schema carries, which is what BuildTree walks. Two upstream
// conventions are handled and one is normalized:
//
//   - Kubernetes OpenAPI v3 wraps a documented reference as
//     {"allOf": [{"$ref": ...}], "description": ...}: the referent is
//     resolved first and the referring node's own siblings (description,
//     default) overlay it, so the field-level doc text wins over the type's.
//   - A bare {"$ref": ...} (items, additionalProperties) resolves the same
//     way.
//   - IntOrString and Quantity carry "oneOf" and no "type", which BuildTree
//     has no notion of; they become "type": "string" — the one spelling
//     that is always legal for both ("8080" and "500m" round-trip; the API
//     server coerces where it must). This is the single lossy step in the
//     whole pipeline, and it is deliberate and documented rather than an
//     accident of decoding.
func resolveNode(node map[string]any, schemas map[string]json.RawMessage, stack []string) (map[string]any, error) {
	out := make(map[string]any, len(node))

	if members, ok := node["allOf"].([]any); ok {
		for _, m := range members {
			member, ok := m.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("allOf member is not an object (stack: %s)", strings.Join(stack, " -> "))
			}
			resolved, err := resolveNode(member, schemas, stack)
			if err != nil {
				return nil, err
			}
			for k, v := range resolved {
				out[k] = v
			}
		}
	}

	if ref, ok := node["$ref"].(string); ok {
		resolved, err := resolveRef(strings.TrimPrefix(ref, refPrefix), schemas, stack)
		if err != nil {
			return nil, err
		}
		for k, v := range resolved {
			out[k] = v
		}
	}

	for k, v := range node {
		switch k {
		case "$ref", "allOf":
			continue
		case "properties":
			props, ok := v.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("properties is not an object (stack: %s)", strings.Join(stack, " -> "))
			}
			resolvedProps := make(map[string]any, len(props))
			for name, p := range props {
				prop, ok := p.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("property %q is not an object (stack: %s)", name, strings.Join(stack, " -> "))
				}
				resolved, err := resolveNode(prop, schemas, stack)
				if err != nil {
					return nil, fmt.Errorf("property %q: %w", name, err)
				}
				resolvedProps[name] = resolved
			}
			out[k] = resolvedProps
		case "items", "additionalProperties":
			if sub, ok := v.(map[string]any); ok {
				resolved, err := resolveNode(sub, schemas, stack)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", k, err)
				}
				out[k] = resolved
				continue
			}
			out[k] = v // additionalProperties: true stays as-is
		default:
			out[k] = v
		}
	}

	if _, hasType := out["type"]; !hasType {
		if _, hasOneOf := out["oneOf"]; hasOneOf {
			out["type"] = "string"
			delete(out, "oneOf")
		}
	}
	return out, nil
}
