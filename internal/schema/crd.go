// Package schema turns CustomResourceDefinition documents into a form the
// generator can walk: a preferred version per kind and a path-addressed tree.
package schema

import (
	"fmt"
	"strings"
	"sync"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"sigs.k8s.io/yaml"
)

// CRD is the subset of a CustomResourceDefinition the generator needs.
//
// Despite the name, a CRD is also how a native Kubernetes kind (Deployment,
// Service, ...) travels through the generator: internal/schema/k8s builds one
// per vendored kind, with Native set, so the index, the API and the emitter
// share one schema shape instead of growing a parallel native-kind type.
type crdCache struct {
	mu    sync.Mutex
	trees map[string][]*Node
}

// Cached returns c with a tree memo attached when it has none. ParseCRDs
// attaches one to every CRD it builds; constructors that assemble a CRD by
// hand (the vendored native kinds in schema/k8s) call this so their trees
// are memoised too — those are the largest trees served (a Deployment has
// ~250 fields) and are rebuilt on every palette and inspector request
// otherwise. Copies of the returned CRD share the same memo.
func (c CRD) Cached() CRD {
	if c.cache == nil {
		c.cache = &crdCache{trees: make(map[string][]*Node)}
	}
	return c
}

type CRD struct {
	Group      string
	Kind       string
	Plural     string
	Scope      string
	Categories []string
	Versions   []Version

	// Native marks a vendored native Kubernetes kind rather than a
	// provider-shipped managed resource. It changes what "the settable
	// fields" means (the object's own schema, not spec.forProvider — see
	// FieldTree) and what envelope the emitter renders (none: the composed
	// object IS the Kubernetes object, with no forProvider wrapper and no
	// providerConfigRef). Only internal/schema/k8s sets it; ParseCRDs never
	// does, so no fetched package can smuggle a kind into the native path.
	Native bool `json:"native,omitempty"`

	// Function marks a Crossplane Function Input CRD.
	Function bool `json:"function,omitempty"`

	cache *crdCache
}

// Version is one served version of a CRD.
type Version struct {
	Name       string
	Served     bool
	Storage    bool
	Deprecated bool
	// Properties is spec.versions[].schema.openAPIV3Schema.properties, left as
	// decoded JSON so the tree builder can walk it without a typed OpenAPI model.
	Properties map[string]any
}

// crdDoc mirrors only the fields we read.
type crdDoc struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Spec       struct {
		Group string `json:"group"`
		Scope string `json:"scope"`
		Names struct {
			Kind       string   `json:"kind"`
			Plural     string   `json:"plural"`
			Categories []string `json:"categories"`
		} `json:"names"`
		Versions []struct {
			Name       string `json:"name"`
			Served     bool   `json:"served"`
			Storage    bool   `json:"storage"`
			Deprecated bool   `json:"deprecated"`
			Schema     struct {
				OpenAPIV3Schema struct {
					Properties map[string]any `json:"properties"`
				} `json:"openAPIV3Schema"`
			} `json:"schema"`
		} `json:"versions"`
	} `json:"spec"`
}

// ParseCRDs decodes every CustomResourceDefinition in docs, skipping any
// document that is not one (such as the package meta object).
func ParseCRDs(docs [][]byte) ([]CRD, error) {
	var out []CRD
	for i, d := range docs {
		var doc crdDoc
		if err := yaml.Unmarshal(d, &doc); err != nil {
			return nil, fmt.Errorf("document %d: %w", i, err)
		}
		if doc.Kind != "CustomResourceDefinition" {
			continue
		}
		c := CRD{
			Group:      doc.Spec.Group,
			Kind:       doc.Spec.Names.Kind,
			Plural:     doc.Spec.Names.Plural,
			Scope:      doc.Spec.Scope,
			Categories: doc.Spec.Names.Categories,
			Function:   strings.HasSuffix(doc.Spec.Group, ".fn.crossplane.io") || strings.Contains(doc.Spec.Group, ".fn."),
			cache:      &crdCache{trees: make(map[string][]*Node)},
		}
		for _, v := range doc.Spec.Versions {
			c.Versions = append(c.Versions, Version{
				Name:       v.Name,
				Served:     v.Served,
				Storage:    v.Storage,
				Deprecated: v.Deprecated,
				Properties: v.Schema.OpenAPIV3Schema.Properties,
			})
		}
		out = append(out, c)
	}
	return out, nil
}

// Preferred returns the storage version, falling back to the first served
// non-deprecated version. It never blindly returns Versions[0].
//
// If more than one version has storage: true (a malformed CRD; the
// apiextensions API only allows one), the first such version in list order
// wins. This is a deliberate, documented tie-break, not an oversight.
func (c CRD) Preferred() (Version, error) {
	for _, v := range c.Versions {
		if v.Storage {
			return v, nil
		}
	}
	for _, v := range c.Versions {
		if v.Served && !v.Deprecated {
			return v, nil
		}
	}
	return Version{}, fmt.Errorf("%s.%s: no storage or served non-deprecated version", c.Plural, c.Group)
}

// IsManaged reports whether this CRD is a Crossplane managed resource.
func (c CRD) IsManaged() bool {
	for _, cat := range c.Categories {
		if cat == "managed" {
			return true
		}
	}
	return false
}

// IsFunctionInput reports whether this CRD is a Crossplane function input object.
func (c CRD) IsFunctionInput() bool {
	if c.Function {
		return true
	}
	return strings.HasSuffix(c.Group, ".fn.crossplane.io") || strings.Contains(c.Group, ".fn.")
}

// Namespaced reports whether the CRD is namespace-scoped. In Crossplane v2 the
// namespaced managed-resource variants live in ".m." groups.
func (c CRD) Namespaced() bool { return c.Scope == "Namespaced" }

// APIVersion returns group/version for the preferred version. It returns an
// error instead of a malformed apiVersion string (e.g. "group/" with an empty
// version segment) when the CRD has no usable version.
//
// An empty Group is the core/legacy Kubernetes API group (native ConfigMap,
// Service, ...), whose apiVersion is the bare version — "v1", never "/v1".
// A parsed CustomResourceDefinition can never take this branch: the API
// server requires spec.group to be a non-empty DNS subdomain, so an empty
// group here always means a native kind from internal/schema/k8s.
func (c CRD) APIVersion() (string, error) {
	v, err := c.Preferred()
	if err != nil {
		return "", fmt.Errorf("%s: %w", c.Kind, err)
	}
	if c.Group == "" {
		return v.Name, nil
	}
	return c.Group + "/" + v.Name, nil
}

// ParseCRDManifest decodes a scanned CRD manifest — a YAML file of
// CustomResourceDefinitions the user supplies directly (a crds: source, the
// live-cluster scan's file-shaped sibling) — into object-rooted kinds:
// every CRD comes back with Native set, because a scanned kind is composed
// as the object itself (an Argo Workflow, another composition's XR), never
// through a forProvider envelope. This is a deliberate door into the
// object-rooted path; ParseCRDs, the provider-package decoder, still never
// opens it.
func ParseCRDManifest(data []byte) ([]CRD, error) {
	docs := blueprint.SplitDocs(data)
	crds, err := ParseCRDs(docs)
	if err != nil {
		return nil, err
	}
	if len(crds) == 0 {
		return nil, fmt.Errorf("no CustomResourceDefinition documents found")
	}
	for i := range crds {
		crds[i].Native = true
	}
	return crds, nil
}
