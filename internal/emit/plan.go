package emit

import (
	"fmt"
	"sort"
	"strings"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/index"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// plannedResource holds all resolved schemas, convention merges, and planned field/envelope/annotation lists for a single resource.
type plannedResource struct {
	Resource   blueprint.Resource
	CRD        schema.CRD
	APIVersion string
	Looped     bool
	Plan       []forProviderField
	MetaPlan   []forProviderField
	BodyPlan   []forProviderField
	EnvPlan    []envField
	AnnPlan    []forProviderField
}

// planSingleResource performs the common validation, CRD resolution, conventions merge,
// field planning, and envelope/annotation planning across all three emitters.
func planSingleResource(r blueprint.Resource, b *blueprint.Blueprint, crds []schema.CRD, wantNamespaced bool) (plannedResource, error) {
	crd, err := resolveKind(crds, r, wantNamespaced)
	if err != nil {
		return plannedResource{}, err
	}

	apiVersion, err := crd.APIVersion()
	if err != nil {
		return plannedResource{}, fmt.Errorf("resource %q (kind %q): %w", r.Name, r.Kind, err)
	}

	if err := checkFieldPaths(r, crd); err != nil {
		return plannedResource{}, err
	}

	envNodes, err := checkEnvelopePaths(r, crd)
	if err != nil {
		return plannedResource{}, err
	}

	if err := checkStatusRefs(r, b, crds, wantNamespaced); err != nil {
		return plannedResource{}, err
	}

	annPlan, err := planAnnotations(r, b, crds, wantNamespaced)
	if err != nil {
		return plannedResource{}, err
	}

	fields, cerr := conventionFields(r, b, crd)
	if cerr != nil {
		return plannedResource{}, cerr
	}
	rc := r
	rc.Fields = fields
	plan, err := planFields(rc, b, crds, wantNamespaced)
	if err != nil {
		return plannedResource{}, err
	}

	var metaPlan, bodyPlan []forProviderField
	if crd.Native {
		for _, fld := range plan {
			if strings.HasPrefix(fld.path, "metadata.") || fld.path == "name" {
				metaPlan = append(metaPlan, fld)
			} else {
				bodyPlan = append(bodyPlan, fld)
			}
		}
		var err error
		annPlan, err = mergeNativeAnnotations(r.Name, annPlan, metaPlan)
		if err != nil {
			return plannedResource{}, err
		}
	} else {
		bodyPlan = plan
	}

	envPlan, err := planEnvelope(r, b, envNodes)
	if err != nil {
		return plannedResource{}, err
	}

	return plannedResource{
		Resource:   r,
		CRD:        crd,
		APIVersion: apiVersion,
		Looped:     r.ForEach != "",
		Plan:       plan,
		MetaPlan:   metaPlan,
		BodyPlan:   bodyPlan,
		EnvPlan:    envPlan,
		AnnPlan:    annPlan,
	}, nil
}

// refuseGoTemplateOnlyFeatures rejects conventions, template: blocks, template: fields/annotations,
// and Go-template syntax "{{ ... }}" in raw: fields when running non-Go engines (KCL, Python).
func refuseGoTemplateOnlyFeatures(b *blueprint.Blueprint) error {
	if len(b.Spec.Conventions) > 0 {
		return fmt.Errorf("spec.conventions: engine %q does not support template: conventions", b.Engine())
	}
	if len(b.Spec.Templates) > 0 {
		return fmt.Errorf("spec.templates: engine %q does not support template: blocks", b.Engine())
	}
	for _, r := range b.Spec.Resources {
		for k, f := range r.Fields {
			if f.Template != "" {
				return fmt.Errorf("resource %q field %q: engine %q does not support template: fields", r.Name, k, b.Engine())
			}
			if f.Raw != "" && (strings.Contains(f.Raw, "{{") || blueprint.IsBareGoTemplateExpr(f.Raw)) {
				return fmt.Errorf("resource %q field %q: raw %q contains Go-template syntax which is only supported with the go-templating engine (current engine is %q)", r.Name, k, f.Raw, b.Engine())
			}
		}
		for k, a := range r.Annotations {
			if a.Template != "" {
				return fmt.Errorf("resource %q annotation %q: engine %q does not support template: fields", r.Name, k, b.Engine())
			}
			if a.Raw != "" && (strings.Contains(a.Raw, "{{") || blueprint.IsBareGoTemplateExpr(a.Raw)) {
				return fmt.Errorf("resource %q annotation %q: raw %q contains Go-template syntax which is only supported with the go-templating engine (current engine is %q)", r.Name, k, a.Raw, b.Engine())
			}
		}
		for k, ef := range r.Envelope {
			if ef.Raw != "" && (strings.Contains(ef.Raw, "{{") || blueprint.IsBareGoTemplateExpr(ef.Raw)) {
				return fmt.Errorf("resource %q envelope %q: raw %q contains Go-template syntax which is only supported with the go-templating engine (current engine is %q)", r.Name, k, ef.Raw, b.Engine())
			}
		}
	}
	return nil
}

// CheckRequiredFields validates that every resource in b has all CRD-required fields specified.
// Native K8s resources are skipped as their schemas include fields populated by admission controllers.
func CheckRequiredFields(b *blueprint.Blueprint, crds []schema.CRD) error {
	return checkRequiredFields(b, crds, false)
}

// CheckRequiredFieldsDraft validates that every configured resource in b has all CRD-required fields specified.
// Unconfigured resources (fields: {}) are skipped so canvas draft workflow and preview generation
// remain green until fields are authored.
func CheckRequiredFieldsDraft(b *blueprint.Blueprint, crds []schema.CRD) error {
	return checkRequiredFields(b, crds, true)
}

func checkRequiredFields(b *blueprint.Blueprint, crds []schema.CRD, allowUnconfigured bool) error {
	wantNamespaced := b.Spec.XRD.Scope == "Namespaced"
	for _, r := range b.Spec.Resources {
		if allowUnconfigured && len(r.Fields) == 0 {
			continue
		}
		crd, err := resolveKind(crds, r, wantNamespaced)
		if err != nil {
			return err
		}
		fields, cerr := conventionFields(r, b, crd)
		if cerr != nil {
			return cerr
		}
		rc := r
		rc.Fields = fields
		if err := checkResourceRequiredFields(rc, crd, allowUnconfigured); err != nil {
			return err
		}
	}
	return nil
}

func checkResourceRequiredFields(r blueprint.Resource, crd schema.CRD, allowUnconfigured bool) error {
	if crd.Native || (allowUnconfigured && len(r.Fields) == 0) {
		return nil
	}
	nodes, err := crd.FieldTree()
	if err != nil {
		return fmt.Errorf("resource %q (kind %q): %w", r.Name, r.Kind, err)
	}
	if len(nodes) == 0 {
		return nil
	}

	reqFields := index.Fields(nodes, index.FieldQuery{RequiredOnly: true})
	reqBranches := index.RequiredBranches(nodes)

	var missing []string
	for _, l := range reqFields {
		found := false
		ancestors := ancestorPaths(l.Path)
		for p := range r.Fields {
			basePath, _, isMap := blueprint.ParseFieldPath(p)
			norm := arrayIdxRE.ReplaceAllString(p, "[0]")
			normBase := arrayIdxRE.ReplaceAllString(basePath, "[0]")
			if norm == l.Path || normBase == l.Path || (isMap && normBase == l.Path) {
				found = true
				break
			}
			for _, anc := range ancestors {
				if norm == anc || normBase == anc || (isMap && normBase == anc) {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			missing = append(missing, l.Path)
		}
	}

	for _, br := range reqBranches {
		found := false
		for p := range r.Fields {
			basePath, _, _ := blueprint.ParseFieldPath(p)
			norm := arrayIdxRE.ReplaceAllString(p, "[0]")
			normBase := arrayIdxRE.ReplaceAllString(basePath, "[0]")
			if norm == br.Path || normBase == br.Path ||
				strings.HasPrefix(norm, br.Path+".") || strings.HasPrefix(normBase, br.Path+".") ||
				strings.HasPrefix(norm, br.Path+"[") || strings.HasPrefix(normBase, br.Path+"[") {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, br.Path)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("resource %q: missing required field %q in %s spec.forProvider", r.Name, missing[0], crd.Kind)
	}
	return nil
}
