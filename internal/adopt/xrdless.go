package adopt

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

// Adopting a Composition without its XRD loses the parameter schema: only the
// XRD declares a parameter's type, required, default, enum and description.
// The Composition still proves some of it on its own. A go-templating render
// that is quoted (`| quote`, or the expression sitting inside YAML quotes) is
// a string; one that is unquoted is not. A reference wrapped in
// `{{- if hasKey $spec "x" }}` is optional; a bare dereference only renders
// safely if the XRD requires the value. A patch-and-transform patch is
// optional unless its policy.fromFieldPath is Required. Everything else is
// reported, per parameter, as not recovered — never silently written as
// `type: string`.

// paramEvidence is what one Composition proves about one parameter.
type paramEvidence struct {
	refs     int  // value references (renders or patches)
	quoted   int  // renders that are string-typed by construction
	unquoted int  // renders that emit a bare scalar
	guarded  bool // at least one hasKey guard or an optional patch
	required bool // a patch with policy.fromFieldPath: Required
	boolean  bool // bare truthiness condition (e.g. {{- if $spec.foo }})
	integer  bool // integer repetition count (e.g. until (int $spec.foo))
}

var (
	reEvidenceRef      = regexp.MustCompile(`\{\{-?\s*(?:\$spec|\.spec|\.observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+?)\s*(\|\s*quote\s*)?-?\}\}`)
	reEvidenceGuard    = regexp.MustCompile(`hasKey\s+(?:\$spec|\.spec|\.observed\.composite\.resource\.spec)\s+["']([a-zA-Z0-9_.-]+)["']`)
	reEvidenceIfSimple = regexp.MustCompile(`\{\{-?\s*if\s+(?:\$spec|\.spec|\.observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)\s*-?\}\}`)
	reEvidenceIfEq     = regexp.MustCompile(`\{\{-?\s*if\s+(?:eq|ne)\s+(?:\$spec|\.spec|\.observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)\s+"[^"]*"\s*-?\}\}`)
	reEvidenceIfEqRev  = regexp.MustCompile(`\{\{-?\s*if\s+(?:eq|ne)\s+"[^"]*"\s+(?:\$spec|\.spec|\.observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)\s*-?\}\}`)
	reEvidenceLoop     = regexp.MustCompile(`\{\{-?\s*range\s+\$i\s*:=\s*until\s+\(int\s+(?:\$spec|\.spec|\.observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)\)\s*-?\}\}`)
	reTemplateAction   = regexp.MustCompile(`\{\{-?(.*?)-?\}\}`)
	reEvidenceAnySpec  = regexp.MustCompile(`(?:\$spec|\.spec|\.observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)`)
)

// collectTemplateEvidence scans one go-templating template body.
func collectTemplateEvidence(tmpl string, ev map[string]*paramEvidence) {
	get := func(name string) *paramEvidence {
		e, ok := ev[name]
		if !ok {
			e = &paramEvidence{}
			ev[name] = e
		}
		return e
	}
	for _, action := range reTemplateAction.FindAllStringSubmatch(tmpl, -1) {
		body := action[1]
		if strings.HasPrefix(strings.TrimSpace(body), "/*") {
			continue
		}
		for _, m := range reEvidenceAnySpec.FindAllStringSubmatch(body, -1) {
			get(m[1]).refs++
		}
	}
	for _, m := range reEvidenceRef.FindAllStringSubmatchIndex(tmpl, -1) {
		name := tmpl[m[2]:m[3]]
		e := get(name)
		quoted := m[4] >= 0
		if !quoted {
			before := byte(0)
			after := byte(0)
			if m[0] > 0 {
				before = tmpl[m[0]-1]
			}
			if m[1] < len(tmpl) {
				after = tmpl[m[1]]
			}
			quoted = (before == '\'' && after == '\'') || (before == '"' && after == '"')
		}
		if quoted {
			e.quoted++
		} else {
			e.unquoted++
		}
	}
	for _, m := range reEvidenceIfSimple.FindAllStringSubmatch(tmpl, -1) {
		get(m[1]).boolean = true
	}
	for _, m := range reEvidenceIfEq.FindAllStringSubmatch(tmpl, -1) {
		get(m[1]).quoted++
	}
	for _, m := range reEvidenceIfEqRev.FindAllStringSubmatch(tmpl, -1) {
		get(m[1]).quoted++
	}
	for _, m := range reEvidenceLoop.FindAllStringSubmatch(tmpl, -1) {
		get(m[1]).integer = true
	}
	for _, m := range reEvidenceGuard.FindAllStringSubmatch(tmpl, -1) {
		get(m[1]).guarded = true
	}
}

// collectPatchEvidence scans classic and patch-and-transform patches.
func collectPatchEvidence(patches []any, ev map[string]*paramEvidence) {
	for _, raw := range patches {
		patch, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		pType, _ := patch["type"].(string)
		if pType != "" && pType != "FromCompositeFieldPath" {
			continue
		}
		fromPath, _ := patch["fromFieldPath"].(string)
		var name string
		switch {
		case strings.HasPrefix(fromPath, "spec.parameters."):
			name = strings.TrimPrefix(fromPath, "spec.parameters.")
		case strings.HasPrefix(fromPath, "spec."):
			name = strings.TrimPrefix(fromPath, "spec.")
		default:
			continue
		}
		e, ok := ev[name]
		if !ok {
			e = &paramEvidence{}
			ev[name] = e
		}
		e.refs++
		required := false
		if policy, ok := patch["policy"].(map[string]any); ok {
			if fp, ok := policy["fromFieldPath"].(string); ok && strings.EqualFold(fp, "Required") {
				required = true
			}
		}
		if required {
			e.required = true
		} else {
			e.guarded = true
		}
	}
}

// compositionEvidence walks one Composition document for parameter evidence.
func compositionEvidence(compDoc map[string]any, ev map[string]*paramEvidence) {
	spec, _ := compDoc["spec"].(map[string]any)
	if spec == nil {
		return
	}
	if resources, ok := spec["resources"].([]any); ok {
		for _, raw := range resources {
			if res, ok := raw.(map[string]any); ok {
				if patches, ok := res["patches"].([]any); ok {
					collectPatchEvidence(patches, ev)
				}
			}
		}
	}
	pipeline, _ := spec["pipeline"].([]any)
	for _, raw := range pipeline {
		step, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		input, _ := step["input"].(map[string]any)
		if input == nil {
			continue
		}
		if inline, ok := input["inline"].(map[string]any); ok {
			if tmpl, ok := inline["template"].(string); ok {
				collectTemplateEvidence(tmpl, ev)
			}
		}
		if resources, ok := input["resources"].([]any); ok {
			for _, rraw := range resources {
				if res, ok := rraw.(map[string]any); ok {
					if patches, ok := res["patches"].([]any); ok {
						collectPatchEvidence(patches, ev)
					}
				}
			}
		}
	}
}

// applyXRDlessEvidence runs after the parameters were inferred from the
// Composition(s) and no XRD was adopted. It settles required and type where
// the Composition proves them and records, per parameter, what it could not
// recover. synthesized names parameters the adopter added itself (providerName
// for a Namespaced XRD); those are not a loss.
func applyXRDlessEvidence(bp *blueprint.Blueprint, compDocs []map[string]any, synthesized map[string]bool, report *LossReport, baseBP *blueprint.Blueprint) {
	ev := make(map[string]*paramEvidence)
	for _, doc := range compDocs {
		compositionEvidence(doc, ev)
	}

	names := make([]string, 0, len(bp.Spec.XRD.Parameters))
	for n := range bp.Spec.XRD.Parameters {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		if synthesized[name] {
			continue
		}
		p := bp.Spec.XRD.Parameters[name]
		if p.Type == "object" && len(p.Properties) > 0 {
			members := make([]string, 0, len(p.Properties))
			for m := range p.Properties {
				members = append(members, m)
			}
			sort.Strings(members)
			for _, m := range members {
				mp := p.Properties[m]
				var baseMember *blueprint.Parameter
				if baseBP != nil {
					if bpParent, ok := baseBP.Spec.XRD.Parameters[name]; ok && bpParent.Properties != nil {
						if bmp, ok := bpParent.Properties[m]; ok {
							baseMember = &bmp
						}
					}
				}
				settle(&mp, ev[name+"."+m], "xrd.parameters."+name+".properties."+m, report, baseMember)
				p.Properties[m] = mp
			}
			report.Record("xrd.parameters."+name, "without the XRD, description could not be recovered")
			bp.Spec.XRD.Parameters[name] = p
			continue
		}
		var baseParam *blueprint.Parameter
		if baseBP != nil {
			if bpParam, ok := baseBP.Spec.XRD.Parameters[name]; ok {
				baseParam = &bpParam
			}
		}
		settle(&p, ev[name], "xrd.parameters."+name, report, baseParam)
		bp.Spec.XRD.Parameters[name] = p
	}
}

// settle applies the evidence for one parameter and records what is lost.
func settle(p *blueprint.Parameter, e *paramEvidence, path string, report *LossReport, baseParam *blueprint.Parameter) {
	if e == nil {
		e = &paramEvidence{}
	}
	var lost []string

	switch {
	case e.required:
		p.Required = true
	case e.guarded:
		p.Required = false
	case e.refs > 0:
		// Dereferenced with no guard: only a required parameter renders safely.
		p.Required = true
	default:
		lost = append(lost, "required")
	}

	if baseParam != nil && baseParam.Required != p.Required {
		oldFlag := "optional"
		newFlag := "required"
		if baseParam.Required {
			oldFlag = "required"
			newFlag = "optional"
		}
		lost = append(lost, fmt.Sprintf("required (changed from %s to %s)", oldFlag, newFlag))
	}

	switch {
	case e.boolean:
		p.Type = "boolean"
	case e.integer:
		p.Type = "integer"
	case e.quoted > 0 && e.unquoted == 0:
		p.Type = "string"
	case e.unquoted > 0:
		lost = append(lost, "type (rendered unquoted, so it is not a string; written as string until the XRD or the CRD schema says which scalar it is)")
	default:
		if p.Type == "boolean" || p.Type == "integer" {
			// type was already recovered from template structure (e.g. conditional or loop bound)
		} else {
			p.Type = "string"
			lost = append(lost, "type (written as string)")
		}
	}

	lost = append(lost, "default", "enum", "description")
	report.Record(path, "without the XRD, "+joinLost(lost)+" could not be recovered")
}

func joinLost(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}
