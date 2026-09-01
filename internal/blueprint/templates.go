// This file validates spec.templates bodies by parsing them under the real
// rendering contract: text/template with missingkey=error and
// function-go-templating's function set. A body that cannot parse at render
// time — or that would leak content outside its define block — is refused at
// the source, before an emitter can ship it.
package blueprint

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"text/template"
	"text/template/parse"
	"unicode"

	sprig "github.com/Masterminds/sprig/v3"
)

// templateNameRE is the shape of a user template name. The name reaches the
// emitted Composition inside quoted strings ({{- define "<name>" }},
// include "<name>"), so it is pinned to characters that need no escaping in
// either Go template syntax or YAML: letters, digits, '.', '_' and '-',
// starting with a letter — the shape of the spec's own example, cf.stdLabels.
var templateNameRE = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9._-]*$`)

// conventionMatchRE is the shape of a convention's match: a case-sensitive
// SUFFIX of a camelCase field name, so letters and digits only ("tags",
// "Name"). Anything else could never match a real forProvider property and
// would be a convention that silently never applies.
var conventionMatchRE = regexp.MustCompile(`^[a-zA-Z0-9]+$`)

// engineExtraFuncs are the functions function-go-templating v0.12.0 adds on
// top of sprig (see its function_maps.go getFunctions + initInclude). Only
// the NAMES matter here: text/template checks a called function exists at
// parse time but applies arity and types at execution, so a zero stub is
// enough to validate a body the real engine will accept.
var engineExtraFuncs = []string{
	"randomChoice",
	"toYaml",
	"fromYaml",
	"getResourceCondition",
	"setResourceNameAnnotation",
	"getComposedResource",
	"getCompositeResource",
	"getExtraResources",
	"getExtraResourcesFromContext",
	"getCredentialData",
	"include",
}

// engineFuncs mirrors the function set the emitted template is parsed with
// at render time: sprig minus env/expandenv (function-go-templating deletes
// both for the same information-leakage reason Helm and ArgoCD do — so a
// body calling env must FAIL validation here, exactly as it would fail the
// render), plus the function's own additions as parse-only stubs.
func engineFuncs() template.FuncMap {
	funcs := sprig.TxtFuncMap()
	delete(funcs, "env")
	delete(funcs, "expandenv")
	for _, name := range engineExtraFuncs {
		funcs[name] = func(...any) (string, error) { return "", nil }
	}
	return funcs
}

// TemplateBlockLines is THE assembly of a user template into its define
// block: the exact lines the emitter writes into the Composition's template
// body, and the exact lines validateTemplateBody parses. One function so the
// two can never drift — what was validated is what ships, byte for byte.
// Trailing whitespace is stripped per line (Doc.Line does the same on emit,
// and a stray trailing space in a text node would otherwise make the
// validated body differ from the emitted one), and a trailing newline on the
// body adds no empty line.
func TemplateBlockLines(name, body string) []string {
	lines := []string{fmt.Sprintf("{{- define %q }}", name)}
	for _, l := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		lines = append(lines, strings.TrimRight(l, " \t"))
	}
	lines = append(lines, "{{- end }}")
	return lines
}

// validateTemplates checks spec.templates and spec.conventions. Called from
// Validate before the resources loop, because a resource field's
// template: <name> reference is checked against the set validated here.
// Template names are visited in sorted order so the first error reported is
// deterministic — Validate's contract everywhere else.
func (b *Blueprint) validateTemplates() error {
	names := make([]string, 0, len(b.Spec.Templates))
	for n := range b.Spec.Templates {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if !templateNameRE.MatchString(n) {
			return fmt.Errorf("spec.templates.%s: invalid template name (must start with a letter and "+
				"contain only letters, digits, '.', '_' and '-', e.g. cf.stdTags -- the name is written "+
				"into the emitted template inside quoted strings)", n)
		}
		body := b.Spec.Templates[n]
		if body == "" {
			return fmt.Errorf("spec.templates.%s: body is required", n)
		}
		if err := checkTemplateBody("spec.templates."+n, body); err != nil {
			return err
		}
		if err := validateTemplateBody(n, body); err != nil {
			return err
		}
	}

	for i, c := range b.Spec.Conventions {
		if c.Match == "" {
			return fmt.Errorf("spec.conventions[%d].match is required", i)
		}
		if !conventionMatchRE.MatchString(c.Match) {
			return fmt.Errorf("spec.conventions[%d].match: %q is not a valid field-name suffix "+
				"(letters and digits only, e.g. tags or Name -- anything else can never match a "+
				"forProvider property, a convention that silently never applies)", i, c.Match)
		}
		if c.Template == "" {
			return fmt.Errorf("spec.conventions[%d].template is required", i)
		}
		if _, ok := b.Spec.Templates[c.Template]; !ok {
			return fmt.Errorf("spec.conventions[%d]: references unknown template %q", i, c.Template)
		}
	}
	return nil
}

// checkTemplateBody rejects control characters in a template body the way
// checkScalar does for single-line scalars, EXCEPT the two runes a template
// body legitimately carries: '\n' (bodies are multi-line by nature; the
// emitter writes them line by line inside the block scalar, so a newline
// cannot escape it) and '\t' (ordinary inside template actions and text).
// '\r' stays rejected — emit normalizes CRLF, which would silently change
// the body — as do NEL, U+2028/U+2029 (YAML 1.1 line breaks that are NOT
// newlines to the template engine) and the rest of C0/C1/DEL.
func checkTemplateBody(fieldPath, body string) error {
	for i, r := range body {
		if r == '\n' || r == '\t' {
			continue
		}
		// unicode.IsControl covers C0, C1 (including NEL, U+0085) and DEL;
		// U+2028/U+2029 are Zl/Zp line breaks, added explicitly exactly as
		// checkScalar does.
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return fmt.Errorf("%s: contains the control character %q at byte %d; "+
				"only newlines and tabs are allowed in a template body", fieldPath, r, i)
		}
	}
	return nil
}

// validateTemplateBody parses the assembled define block under the real
// engine contract and then checks that NOTHING escaped the block:
//
//   - the body must parse with the render-time function set (an unknown
//     function, unbalanced action or bad pipeline fails here instead of at
//     render time);
//   - the body must not define additional templates (a helper belongs in
//     spec.templates under its own name, where it gets the same validation);
//   - the root template around the define must be empty apart from
//     whitespace. This is the load-bearing check: a body containing its own
//     "{{- end }}" would close the define early and turn the rest of the
//     body into TOP-LEVEL template text of the Composition — content
//     injected into every rendered document, silently, with balanced
//     variants parsing cleanly. Anything outside the define block is
//     therefore refused, not just the unbalanced cases the parser catches.
func validateTemplateBody(name, body string) error {
	assembled := strings.Join(TemplateBlockLines(name, body), "\n")
	t, err := template.New("cf-validate").Option("missingkey=error").Funcs(engineFuncs()).Parse(assembled)
	if err != nil {
		return fmt.Errorf("spec.templates.%s: does not parse under the rendering engine "+
			"(text/template, missingkey=error, function-go-templating's function set): %w", name, err)
	}
	for _, tt := range t.Templates() {
		switch tt.Name() {
		case "cf-validate", name:
		default:
			return fmt.Errorf("spec.templates.%s: body defines an extra template %q -- "+
				"declare it as its own spec.templates entry instead, so it gets the same validation", name, tt.Name())
		}
	}
	root := t.Tree.Root
	if root == nil {
		return nil
	}
	for _, n := range root.Nodes {
		text, isText := n.(*parse.TextNode)
		if isText && strings.TrimSpace(string(text.Text)) == "" {
			continue
		}
		return fmt.Errorf("spec.templates.%s: content escapes the define block (near %q) -- "+
			"a body must not close its own block; anything outside it would be injected verbatim "+
			"into every rendered document", name, strings.TrimSpace(n.String()))
	}
	return nil
}
