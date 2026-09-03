package emit

import (
	"fmt"
	"strings"

	"github.com/koorikla/compositionfactory/internal/blueprint"
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

	fields := r.Fields
	if !crd.Native {
		var cerr error
		fields, cerr = conventionFields(r, b, crd)
		if cerr != nil {
			return plannedResource{}, cerr
		}
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
			if strings.HasPrefix(fld.path, "metadata.") {
				metaPlan = append(metaPlan, fld)
			} else {
				bodyPlan = append(bodyPlan, fld)
			}
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
