// Package schema turns CustomResourceDefinition documents into a form the
// generator can walk: a preferred version per kind and a path-addressed tree.
package schema

import (
	"fmt"

	"sigs.k8s.io/yaml"
)

// CRD is the subset of a CustomResourceDefinition the generator needs.
type CRD struct {
	Group      string
	Kind       string
	Plural     string
	Scope      string
	Categories []string
	Versions   []Version
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

// Namespaced reports whether the CRD is namespace-scoped. In Crossplane v2 the
// namespaced managed-resource variants live in ".m." groups.
func (c CRD) Namespaced() bool { return c.Scope == "Namespaced" }

// APIVersion returns group/version for the preferred version. It returns an
// error instead of a malformed apiVersion string (e.g. "group/" with an empty
// version segment) when the CRD has no usable version.
func (c CRD) APIVersion() (string, error) {
	v, err := c.Preferred()
	if err != nil {
		return "", fmt.Errorf("%s: %w", c.Kind, err)
	}
	return c.Group + "/" + v.Name, nil
}
