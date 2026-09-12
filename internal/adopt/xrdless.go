package adopt

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/schema/k8s"
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
	refs       int    // value references (renders or patches)
	quoted     int    // renders that are string-typed by construction
	unquoted   int    // renders that emit a bare scalar
	guarded    bool   // at least one hasKey guard or an optional patch
	required   bool   // a patch with policy.fromFieldPath: Required
	boolean    bool   // bare truthiness condition (e.g. {{- if $spec.foo }})
	integer    bool   // integer repetition count (e.g. until (int $spec.foo))
	schemaType string // CRD schema type from wired fields (e.g. "number", "integer", "boolean", "string")
}

var (
	reEvidenceRef               = reParamVar
	reEvidenceQuote             = regexp.MustCompile(`\|\s*quote\b`)
	reEvidenceGuard             = regexp.MustCompile(`hasKey\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s+["']([a-zA-Z0-9_.-]+)["']`)
	reEvidenceGuardDefault      = regexp.MustCompile(`default\s+(?:\([^)]+\)|["'][^"']*["']|\S+)\s+(?:\(?\s*(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)|\(?\s*index\s+\(?\s*(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s*\)?\s+["']([a-zA-Z0-9_.-]+)["']\s*\)?)`)
	reEvidenceGuardPipedDefault = regexp.MustCompile(`(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)\s*\|\s*default\b`)
	reSpecRoot                  = regexp.MustCompile(`(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)(?:$|[^a-zA-Z0-9_])`)
	reSpecDotted                = regexp.MustCompile(`(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)`)
	reTrailingDotted            = regexp.MustCompile(`\)\.([a-zA-Z0-9_.-]+)`)
	reQuotedKey                 = regexp.MustCompile(`["'` + "`" + `]([a-zA-Z0-9_.-]+)["'` + "`" + `]`)
	reHasKeyWord                = regexp.MustCompile(`\bhasKey\b`)
	reEvidenceIfSimple          = regexp.MustCompile(`\{\{-?\s*if\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)\s*-?\}\}`)
	reEvidenceIfEq              = regexp.MustCompile(`\{\{-?\s*if\s+(?:eq|ne)\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)\s*"[^"]*"\s*-?\}\}`)
	reEvidenceIfEqRev           = regexp.MustCompile(`\{\{-?\s*if\s+(?:eq|ne)\s+"[^"]*"\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)\s*-?\}\}`)
	reEvidenceLoop              = regexp.MustCompile(`\{\{-?\s*range\s+\$i\s*:=\s*until\s+\(int\s*(?:\(?\s*(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)\s*\)?|\(?\s*index\s+\(?\s*(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s*\)?\s+["']([a-zA-Z0-9_.-]+)["']\s*\)?)\s*\)\s*-?\}\}`)
	reTemplateAction            = regexp.MustCompile(`\{\{-?(.*?)-?\}\}`)
	reEvidenceAnySpec           = regexp.MustCompile(`(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)`)
	reArrayIdx                  = regexp.MustCompile(`\[\d+\]`)
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
		for _, m := range reEvidenceIndexSpec.FindAllStringSubmatch(body, -1) {
			get(m[1]).refs++
		}
	}
	for _, m := range reEvidenceRef.FindAllStringSubmatchIndex(tmpl, -1) {
		var name string
		if m[2] >= 0 {
			name = tmpl[m[2]:m[3]]
		} else if len(m) >= 6 && m[4] >= 0 {
			name = tmpl[m[4]:m[5]]
		}
		if name == "" {
			continue
		}
		e := get(name)
		full := tmpl[m[0]:m[1]]
		quoted := reEvidenceQuote.MatchString(full)
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
		if strings.Contains(full, "default") {
			e.guarded = true
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
		pName := m[1]
		if pName == "" && len(m) >= 3 {
			pName = m[2]
		}
		if pName != "" {
			get(pName).integer = true
		}
	}
	for _, m := range reEvidenceGuard.FindAllStringSubmatch(tmpl, -1) {
		get(m[1]).guarded = true
	}
	scanHasKeyGuards(tmpl, func(pName string) {
		get(pName).guarded = true
	})
	for _, m := range reEvidenceGuardDefault.FindAllStringSubmatch(tmpl, -1) {
		pName := m[1]
		if pName == "" && len(m) >= 3 {
			pName = m[2]
		}
		if pName != "" {
			get(pName).guarded = true
		}
	}
	for _, m := range reEvidenceGuardPipedDefault.FindAllStringSubmatch(tmpl, -1) {
		get(m[1]).guarded = true
	}
}

// scanHasKeyGuards finds all hasKey guards targeting $spec or its nested properties,
// including top-level (hasKey $spec "key"), dotted (hasKey $spec.cluster "desc"),
// and indexed (hasKey (index $spec "cluster") "desc") expressions, and records the
// qualified parameter path (e.g. "key" or "cluster.desc").
func scanHasKeyGuards(tmpl string, record func(param string)) {
	matches := reHasKeyWord.FindAllStringIndex(tmpl, -1)
	for _, m := range matches {
		idx := m[1]
		// Skip whitespace after hasKey
		for idx < len(tmpl) && (tmpl[idx] == ' ' || tmpl[idx] == '\t' || tmpl[idx] == '\r' || tmpl[idx] == '\n') {
			idx++
		}
		if idx >= len(tmpl) {
			continue
		}

		// Extract target argument (arg1)
		var arg1 string
		if tmpl[idx] == '(' {
			start := idx
			depth := 0
			inQuote := byte(0)
			for idx < len(tmpl) {
				ch := tmpl[idx]
				if inQuote != 0 {
					if ch == inQuote && (idx == 0 || tmpl[idx-1] != '\\') {
						inQuote = 0
					}
				} else if ch == '"' || ch == '\'' || ch == '`' {
					inQuote = ch
				} else if ch == '(' {
					depth++
				} else if ch == ')' {
					depth--
					if depth == 0 {
						idx++
						// Also consume any chained dot accessors after the closing paren, e.g. (index $spec "a").b
						for idx < len(tmpl) && (tmpl[idx] == '.' || (tmpl[idx] >= 'a' && tmpl[idx] <= 'z') || (tmpl[idx] >= 'A' && tmpl[idx] <= 'Z') || (tmpl[idx] >= '0' && tmpl[idx] <= '9') || tmpl[idx] == '_' || tmpl[idx] == '-') {
							idx++
						}
						break
					}
				}
				idx++
			}
			arg1 = tmpl[start:idx]
		} else {
			start := idx
			for idx < len(tmpl) && !(tmpl[idx] == ' ' || tmpl[idx] == '\t' || tmpl[idx] == '\r' || tmpl[idx] == '\n' || tmpl[idx] == ')' || tmpl[idx] == '}' || tmpl[idx] == '|') {
				idx++
			}
			arg1 = tmpl[start:idx]
		}

		// Skip whitespace between arg1 and arg2
		for idx < len(tmpl) && (tmpl[idx] == ' ' || tmpl[idx] == '\t' || tmpl[idx] == '\r' || tmpl[idx] == '\n') {
			idx++
		}
		if idx >= len(tmpl) {
			continue
		}

		// Extract key argument (arg2) - must be a quoted string
		if tmpl[idx] != '"' && tmpl[idx] != '\'' && tmpl[idx] != '`' {
			continue
		}
		quoteCh := tmpl[idx]
		idx++
		startKey := idx
		for idx < len(tmpl) && tmpl[idx] != quoteCh {
			if tmpl[idx] == '\\' && idx+1 < len(tmpl) {
				idx++
			}
			idx++
		}
		if idx >= len(tmpl) {
			continue
		}
		key := tmpl[startKey:idx]

		// Check if arg1 references the composite resource spec
		if !reSpecRoot.MatchString(arg1) {
			continue
		}

		var segments []string

		// Check for dotted path directly following spec root
		if dm := reSpecDotted.FindStringSubmatch(arg1); len(dm) >= 2 && dm[1] != "" {
			parts := strings.Split(dm[1], ".")
			for _, p := range parts {
				if p != "" {
					segments = append(segments, p)
				}
			}
		}

		// Extract any index string literals in arg1
		for _, qm := range reQuotedKey.FindAllStringSubmatch(arg1, -1) {
			if len(qm) >= 2 && qm[1] != "" {
				segments = append(segments, qm[1])
			}
		}

		// Check for chained dotted path after closing paren, e.g. ).b
		if tm := reTrailingDotted.FindStringSubmatch(arg1); len(tm) >= 2 && tm[1] != "" {
			parts := strings.Split(tm[1], ".")
			for _, p := range parts {
				if p != "" {
					segments = append(segments, p)
				}
			}
		}

		// Append the guarded key
		segments = append(segments, key)

		if len(segments) > 0 {
			record(strings.Join(segments, "."))
		}
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

// collectPatchSetEvidence scans patches defined in patchSets.
func collectPatchSetEvidence(patchSets []any, ev map[string]*paramEvidence) {
	for _, raw := range patchSets {
		if ps, ok := raw.(map[string]any); ok {
			if patches, ok := ps["patches"].([]any); ok {
				collectPatchEvidence(patches, ev)
			}
		}
	}
}

// compositionEvidence walks one Composition document for parameter evidence.
func compositionEvidence(compDoc map[string]any, ev map[string]*paramEvidence) {
	spec, _ := compDoc["spec"].(map[string]any)
	if spec == nil {
		return
	}
	if patchSets, ok := spec["patchSets"].([]any); ok {
		collectPatchSetEvidence(patchSets, ev)
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
		if patchSets, ok := input["patchSets"].([]any); ok {
			collectPatchSetEvidence(patchSets, ev)
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
func applyXRDlessEvidence(bp *blueprint.Blueprint, compDocs []map[string]any, synthesized map[string]bool, report *LossReport, baseBP *blueprint.Blueprint, store *cache.Store) {
	ev := make(map[string]*paramEvidence)
	for _, doc := range compDocs {
		compositionEvidence(doc, ev)
	}

	schemaTypes := inferParamSchemaTypes(bp, store)
	for name, st := range schemaTypes {
		e := ev[name]
		if e == nil {
			e = &paramEvidence{}
			ev[name] = e
		}
		e.schemaType = st
	}

	for _, r := range bp.Spec.Resources {
		if r.ForEach != "" {
			if !strings.HasPrefix(r.ForEach, "env.") && !strings.HasPrefix(r.ForEach, "resources.") {
				param := strings.TrimSpace(strings.TrimPrefix(r.ForEach, "params."))
				if param != "" {
					e := ev[param]
					if e == nil {
						e = &paramEvidence{}
						ev[param] = e
					}
					hasDefault := false
					if bpParam, ok := bp.Spec.XRD.Parameters[param]; ok && bpParam.Default != "" {
						hasDefault = true
					} else if baseBP != nil {
						if bpParam, ok := baseBP.Spec.XRD.Parameters[param]; ok && bpParam.Default != "" {
							hasDefault = true
						}
					}
					if !hasDefault {
						e.required = true
					}
					e.integer = true
				}
			}
		}
		if r.When != "" {
			source, param, op, _, err := blueprint.ParseWhen(r.When)
			if err == nil && (source == "params" || source == "") {
				e := ev[param]
				if e == nil {
					e = &paramEvidence{}
					ev[param] = e
				}
				e.required = true
				if op == "" {
					e.boolean = true
				} else {
					e.quoted++
				}
			}
		}
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
			var baseProps map[string]blueprint.Parameter
			if baseBP != nil {
				if bpParent, ok := baseBP.Spec.XRD.Parameters[name]; ok {
					baseProps = bpParent.Properties
				}
			}
			settleProperties(p.Properties, baseProps, name, "xrd.parameters."+name, ev, report)
			report.Record("xrd.parameters."+name, "without the XRD, description could not be recovered (combine XRD and Composition into one file, or import XRD to complement)")
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

// settleProperties recursively settles properties of an object parameter.
func settleProperties(
	props map[string]blueprint.Parameter,
	baseProps map[string]blueprint.Parameter,
	paramPath string,
	reportPath string,
	ev map[string]*paramEvidence,
	report *LossReport,
) {
	members := make([]string, 0, len(props))
	for m := range props {
		members = append(members, m)
	}
	sort.Strings(members)

	for _, m := range members {
		mp := props[m]
		var baseMember *blueprint.Parameter
		if baseProps != nil {
			if bmp, ok := baseProps[m]; ok {
				baseMember = &bmp
			}
		}

		childParamPath := m
		if paramPath != "" {
			childParamPath = paramPath + "." + m
		}
		childReportPath := fmt.Sprintf("%s.properties.%s", reportPath, m)

		if mp.Type == "object" && len(mp.Properties) > 0 {
			var childBaseProps map[string]blueprint.Parameter
			if baseMember != nil {
				childBaseProps = baseMember.Properties
			}
			settleProperties(mp.Properties, childBaseProps, childParamPath, childReportPath, ev, report)
			report.Record(childReportPath, "without the XRD, description could not be recovered (combine XRD and Composition into one file, or import XRD to complement)")
			props[m] = mp
		} else {
			settle(&mp, ev[childParamPath], childReportPath, report, baseMember)
			props[m] = mp
		}
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
	case e.schemaType != "":
		p.Type = e.schemaType
		if baseParam != nil && isCompatibleScalar(e.schemaType, baseParam.Type) {
			p.Type = baseParam.Type
		}
	case e.unquoted > 0:
		if baseParam != nil && (baseParam.Type == "number" || baseParam.Type == "integer" || baseParam.Type == "boolean") {
			p.Type = baseParam.Type
		} else {
			p.Type = "string"
			lost = append(lost, "type (rendered unquoted, so it is not a string; written as string until the XRD or the CRD schema says which scalar it is)")
		}
	default:
		if p.Type == "boolean" || p.Type == "integer" || p.Type == "object" {
			// type was already recovered from template structure (e.g. conditional or loop bound) or is an object
		} else if baseParam != nil && baseParam.Type != "" {
			p.Type = baseParam.Type
		} else {
			p.Type = "string"
			lost = append(lost, "type (written as string)")
		}
	}

	lost = append(lost, "default", "enum", "description")
	report.Record(path, "without the XRD, "+joinLost(lost)+" could not be recovered (combine XRD and Composition into one file, or import XRD to complement)")
}

func walkNodes(nodes []*schema.Node, prefix string, out map[string]*schema.Node) {
	for _, n := range nodes {
		path := n.Name
		if prefix != "" {
			path = prefix + "." + n.Name
		}
		out[path] = n
		if len(n.Children) == 0 {
			continue
		}
		childPrefix := path
		if n.Type == "array" {
			childPrefix += "[0]"
		}
		walkNodes(n.Children, childPrefix, out)
	}
}

func matchesProvider(group, provider string) bool {
	if provider == "" || provider == blueprint.NativeProvider || provider == "cluster" {
		return true
	}
	p := provider
	if idx := strings.LastIndex(p, "/"); idx != -1 {
		p = p[idx+1:]
	}
	if idx := strings.Index(p, "@"); idx != -1 {
		p = p[:idx]
	}
	if idx := strings.Index(p, ":"); idx != -1 {
		p = p[:idx]
	}
	p = strings.TrimPrefix(p, "provider-")
	if p == "test" {
		return true
	}
	parts := strings.Split(p, "-")
	for _, part := range parts {
		if !strings.Contains(group, part) {
			return false
		}
	}
	return true
}

func resolveResourceCRD(crds []schema.CRD, r blueprint.Resource, wantNamespaced bool) *schema.CRD {
	objectRooted := r.Provider == blueprint.NativeProvider ||
		strings.HasSuffix(r.Provider, ".yaml") || strings.HasSuffix(r.Provider, ".yml")
	if objectRooted {
		for i := range crds {
			if crds[i].Native && crds[i].Kind == r.Kind {
				return &crds[i]
			}
		}
		return nil
	}

	var fallback *schema.CRD
	var candidates []*schema.CRD
	var nativeCandidate *schema.CRD

	for i := range crds {
		c := &crds[i]
		if c.Kind != r.Kind {
			continue
		}
		if c.Native {
			if r.Provider == "cluster" && nativeCandidate == nil {
				nativeCandidate = c
			}
			continue
		}
		if !c.IsManaged() {
			continue
		}
		if c.Namespaced() == wantNamespaced {
			candidates = append(candidates, c)
		} else {
			fallback = c
		}
	}

	if len(candidates) == 1 {
		if r.Provider != "" && !matchesProvider(candidates[0].Group, r.Provider) {
			return nil
		}
		return candidates[0]
	}
	if len(candidates) > 1 {
		for _, c := range candidates {
			if r.Provider != "" && matchesProvider(c.Group, r.Provider) {
				return c
			}
		}
		if r.Provider != "" {
			return nil
		}
		return candidates[0]
	}
	if r.Provider == "cluster" && nativeCandidate != nil {
		return nativeCandidate
	}
	if fallback != nil {
		if r.Provider != "" && !matchesProvider(fallback.Group, r.Provider) {
			return nil
		}
		return fallback
	}
	return nil
}

func reconcileSchemaTypes(types []string) string {
	if len(types) == 0 {
		return ""
	}
	hasNumber := false
	hasInteger := false
	hasBoolean := false
	hasString := false
	for _, t := range types {
		switch t {
		case "number":
			hasNumber = true
		case "integer":
			hasInteger = true
		case "boolean":
			hasBoolean = true
		case "string":
			hasString = true
		}
	}
	nonStringCount := 0
	if hasNumber || hasInteger {
		nonStringCount++
	}
	if hasBoolean {
		nonStringCount++
	}
	if nonStringCount > 1 {
		return ""
	}
	if hasInteger {
		return "integer"
	}
	if hasNumber {
		return "number"
	}
	if hasBoolean {
		return "boolean"
	}
	if hasString {
		return "string"
	}
	return ""
}

func isCompatibleScalar(schemaType, paramType string) bool {
	if paramType == "" {
		return false
	}
	if schemaType == paramType {
		return true
	}
	if schemaType == "number" && paramType == "integer" {
		return true
	}
	if schemaType == "string" {
		return paramType == "string" || paramType == "integer" || paramType == "number" || paramType == "boolean"
	}
	return false
}

func inferParamSchemaTypes(bp *blueprint.Blueprint, store *cache.Store) map[string]string {
	if bp == nil {
		return nil
	}
	var crds []schema.CRD
	if store != nil {
		for _, s := range bp.Spec.Sources {
			if s.Provider != "" {
				if got, err := store.Load(s.Provider); err == nil {
					crds = append(crds, got...)
				}
			}
		}
		if len(crds) == 0 {
			if list, err := store.List(); err == nil {
				for _, ref := range list {
					if got, err := store.Load(ref); err == nil {
						crds = append(crds, got...)
					}
				}
			}
		}
	}
	if native, err := k8s.Kinds(); err == nil {
		crds = append(crds, native...)
	}
	if len(crds) == 0 {
		return nil
	}

	wantNamespaced := bp.Spec.XRD.Scope == "Namespaced" || bp.Spec.XRD.Scope == ""
	paramTypes := make(map[string][]string)

	for _, r := range bp.Spec.Resources {
		crd := resolveResourceCRD(crds, r, wantNamespaced)
		if crd == nil {
			continue
		}

		fieldNodes, err := crd.FieldTree()
		knownFields := make(map[string]*schema.Node)
		if err == nil {
			walkNodes(fieldNodes, "", knownFields)
		}

		envNodes, err := crd.Envelope()
		knownEnvelope := make(map[string]*schema.Node)
		if err == nil {
			walkNodes(envNodes, "", knownEnvelope)
		}

		for p, f := range r.Fields {
			if f.From == "" {
				continue
			}
			param, member, ok := blueprint.ParamRef(f.From)
			if !ok {
				continue
			}
			fullName := param
			if member != "" {
				fullName = param + "." + member
			}
			basePath, _, isMap := blueprint.ParseFieldPath(p)
			lookup := reArrayIdx.ReplaceAllString(p, "[0]")
			if isMap {
				lookup = reArrayIdx.ReplaceAllString(basePath, "[0]")
			}
			if isMap {
				paramTypes[fullName] = append(paramTypes[fullName], "string")
			} else if node := knownFields[lookup]; node != nil && node.Type != "" {
				paramTypes[fullName] = append(paramTypes[fullName], node.Type)
			}
		}

		for p, f := range r.Envelope {
			if f.From == "" {
				continue
			}
			param, member, ok := blueprint.ParamRef(f.From)
			if !ok {
				continue
			}
			fullName := param
			if member != "" {
				fullName = param + "." + member
			}
			if node := knownEnvelope[p]; node != nil && node.Type != "" {
				paramTypes[fullName] = append(paramTypes[fullName], node.Type)
			}
		}

		for _, f := range r.Annotations {
			if f.From == "" {
				continue
			}
			param, member, ok := blueprint.ParamRef(f.From)
			if !ok {
				continue
			}
			fullName := param
			if member != "" {
				fullName = param + "." + member
			}
			paramTypes[fullName] = append(paramTypes[fullName], "string")
		}
	}

	res := make(map[string]string)
	for name, types := range paramTypes {
		if reconciled := reconcileSchemaTypes(types); reconciled != "" {
			res[name] = reconciled
		}
	}
	return res
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
