package blueprint

import (
	"fmt"
	"strings"
)

// validateRoot validates the top-level blueprint fields (apiVersion, kind, metadata, emit).
func (b *Blueprint) validateRoot() error {
	if b.APIVersion != APIVersion {
		return fmt.Errorf("apiVersion: %q is not valid (must be %q)", b.APIVersion, APIVersion)
	}
	if b.Kind != Kind {
		return fmt.Errorf("kind: %q is not valid (must be %q)", b.Kind, Kind)
	}

	if b.Metadata.Name == "" {
		return fmt.Errorf("metadata.name is required")
	}
	if err := checkScalar("metadata.name", b.Metadata.Name); err != nil {
		return err
	}
	if len(b.Metadata.Name) > 253 {
		return fmt.Errorf("metadata.name: %q exceeds maximum length of 253 characters", b.Metadata.Name)
	}
	if !groupRE.MatchString(b.Metadata.Name) || yamlKeywords[strings.ToLower(b.Metadata.Name)] {
		return fmt.Errorf("metadata.name: %q is not a valid DNS subdomain name "+
			"(must be lowercase alphanumeric characters, '-' or '.', and start and end with an alphanumeric character)",
			b.Metadata.Name)
	}

	if b.Spec.Emit != nil {
		if b.Spec.Emit.TemplateSource != "" {
			switch b.Spec.Emit.TemplateSource {
			case TemplateSourceInline, TemplateSourceFileSystem:
			default:
				return fmt.Errorf("spec.emit.templateSource: %q is not a valid template source (must be %q or %q)",
					b.Spec.Emit.TemplateSource, TemplateSourceInline, TemplateSourceFileSystem)
			}
		}
		if b.Spec.Emit.Engine != "" {
			switch strings.ToLower(b.Spec.Emit.Engine) {
			case EngineGoTemplating, EngineKCL, EnginePython:
			default:
				return fmt.Errorf("spec.emit.engine: %q is not a valid engine (must be %q, %q, or %q)",
					b.Spec.Emit.Engine, EngineGoTemplating, EngineKCL, EnginePython)
			}
		}
		if b.TemplateSource() == TemplateSourceFileSystem && b.Engine() != EngineGoTemplating {
			return fmt.Errorf("spec.emit.templateSource: %s is only supported with the %s engine (current engine is %q)",
				TemplateSourceFileSystem, EngineGoTemplating, b.Engine())
		}
	}
	return nil
}

// validateSources validates spec.sources entries.
func validateSources(sources []Source) error {
	for i, s := range sources {
		if s.CRDs != "" {
			if s.Provider != "" {
				return fmt.Errorf("spec.sources[%d]: provider and crds are mutually exclusive — "+
					"a source is either a provider package or a CRD manifest file", i)
			}
			if err := checkScalar(fmt.Sprintf("spec.sources[%d].crds", i), s.CRDs); err != nil {
				return err
			}
			if !strings.HasSuffix(s.CRDs, ".yaml") && !strings.HasSuffix(s.CRDs, ".yml") {
				return fmt.Errorf("spec.sources[%d].crds: %q must be a .yaml/.yml file path", i, s.CRDs)
			}
			continue
		}
		if s.Provider == "" {
			return fmt.Errorf("spec.sources[%d]: one of provider (a package ref) or crds (a CRD manifest file) is required", i)
		}
		if s.Provider == NativeProvider {
			return fmt.Errorf("spec.sources[%d].provider: %q is not a package source -- native Kubernetes "+
				"kinds are vendored into cf itself and always available. Delete this source entry and set "+
				"provider: %s on the resources that compose native kinds", i, s.Provider, NativeProvider)
		}
		if s.Provider == ClusterProvider {
			return fmt.Errorf("spec.sources[%d].provider: %q is not a package source -- %q is a live "+
				"discovery pseudo-provider. Delete this source entry and set provider: %s on the "+
				"resources that compose cluster-discovered kinds", i, s.Provider, s.Provider, ClusterProvider)
		}
		if err := checkScalar(fmt.Sprintf("spec.sources[%d].provider", i), s.Provider); err != nil {
			return err
		}
		if !providerRefRE.MatchString(s.Provider) {
			return fmt.Errorf("spec.sources[%d].provider: %q is not a valid provider reference "+
				"(e.g. ghcr.io/org/provider-name:v1.2.3, or ...@sha256:<digest>)", i, s.Provider)
		}
	}
	return nil
}
