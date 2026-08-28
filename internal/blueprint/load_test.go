package blueprint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const valid = `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue
spec:
  sources:
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
  xrd:
    group: platform.hooli.tech
    kind: XQueue
    plural: xqueues
    version: v1alpha1
    scope: Namespaced
    parameters:
      location: {type: string, required: true, enum: [EU, US]}
      providerName: {type: string, required: true}
      maxMessageSize: {type: integer}
  resources:
    - name: main-queue
      kind: Queue
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
      fields:
        maxMessageSize: {from: params.maxMessageSize}
`

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "bp.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadValidBlueprint(t *testing.T) {
	b, err := Load(write(t, valid))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if b.Spec.XRD.Kind != "XQueue" {
		t.Errorf("Kind = %q, want XQueue", b.Spec.XRD.Kind)
	}
	if got := b.Spec.XRD.Parameters["location"]; !got.Required || len(got.Enum) != 2 {
		t.Errorf("location = %+v, want required with 2 enum values", got)
	}
	if len(b.Spec.Resources) != 1 || b.Spec.Resources[0].Name != "main-queue" {
		t.Errorf("resources = %+v, want one named main-queue", b.Spec.Resources)
	}
}

func TestValidateRejectsMissingScope(t *testing.T) {
	body := strings.Replace(valid, "    scope: Namespaced\n", "", 1)
	_, err := Load(write(t, body))
	if err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("err = %v, want a complaint about scope", err)
	}
}

func TestValidateRejectsLegacyClusterScope(t *testing.T) {
	body := strings.Replace(valid, "scope: Namespaced", "scope: LegacyCluster", 1)
	_, err := Load(write(t, body))
	if err == nil || !strings.Contains(err.Error(), "LegacyCluster") {
		t.Fatalf("err = %v, want LegacyCluster to be refused", err)
	}
}

func TestValidateRejectsUnknownParameterType(t *testing.T) {
	body := strings.Replace(valid, "maxMessageSize: {type: integer}", "maxMessageSize: {type: int}", 1)
	_, err := Load(write(t, body))
	if err == nil || !strings.Contains(err.Error(), "int") {
		t.Fatalf("err = %v, want an unknown-type error naming int", err)
	}
}

func TestValidateRejectsFieldWithTwoSources(t *testing.T) {
	body := strings.Replace(valid,
		"maxMessageSize: {from: params.maxMessageSize}",
		"maxMessageSize: {from: params.maxMessageSize, value: \"1024\"}", 1)
	_, err := Load(write(t, body))
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("err = %v, want a complaint that a field has more than one source", err)
	}
}

func TestValidateRejectsUnknownParameterReference(t *testing.T) {
	body := strings.Replace(valid, "params.maxMessageSize}", "params.nope}", 1)
	_, err := Load(write(t, body))
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v, want an error naming the unknown parameter", err)
	}
}

// --- Fix round 1 additions ---

// TestValidateRejectsMissingRequiredXRDFields covers each of group, kind,
// plural and version being absent individually, asserting on the substring
// that names the specific missing field (not merely that an error occurred).
func TestValidateRejectsMissingRequiredXRDFields(t *testing.T) {
	tests := []struct {
		name       string
		removeLine string
		wantSubstr string
	}{
		{"missing group", "    group: platform.hooli.tech\n", "spec.xrd.group is required"},
		{"missing kind", "    kind: XQueue\n", "spec.xrd.kind is required"},
		{"missing plural", "    plural: xqueues\n", "spec.xrd.plural is required"},
		{"missing version", "    version: v1alpha1\n", "spec.xrd.version is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := strings.Replace(valid, tt.removeLine, "", 1)
			if body == valid {
				t.Fatalf("removeLine %q did not match anything in the fixture", tt.removeLine)
			}
			_, err := Load(write(t, body))
			if err == nil || !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Fatalf("err = %v, want substring %q", err, tt.wantSubstr)
			}
		})
	}
}

// TestValidateMissingPluralNamesOnlyPlural pins the precise-naming behavior:
// when exactly one required XRD field is absent, the error names only that
// field, not the other three that are present.
func TestValidateMissingPluralNamesOnlyPlural(t *testing.T) {
	body := strings.Replace(valid, "    plural: xqueues\n", "", 1)
	_, err := Load(write(t, body))
	if err == nil {
		t.Fatalf("err = nil, want a complaint about the missing plural field")
	}
	if !strings.Contains(err.Error(), "plural") {
		t.Errorf("err = %v, want it to name plural", err)
	}
	if strings.Contains(err.Error(), "group") {
		t.Errorf("err = %v, wrongly names group even though group is present", err)
	}
}

// TestValidateRejectsFieldWithZeroSources covers the zero-of-{from,value,raw}
// branch, distinct from TestValidateRejectsFieldWithTwoSources which only
// covers the two-sources branch of the same "set != 1" check.
func TestValidateRejectsFieldWithZeroSources(t *testing.T) {
	body := strings.Replace(valid, "maxMessageSize: {from: params.maxMessageSize}", "maxMessageSize: {}", 1)
	_, err := Load(write(t, body))
	if err == nil {
		t.Fatalf("err = nil, want a complaint that no source was set")
	}
	if !strings.Contains(err.Error(), "exactly one") || !strings.Contains(err.Error(), "got 0") {
		t.Fatalf("err = %v, want a complaint identifying zero sources set (got 0)", err)
	}
}

// TestValidateRejectsFromWithoutParamsPrefix covers the "from must start
// with params." branch, which none of the original six tests exercised.
func TestValidateRejectsFromWithoutParamsPrefix(t *testing.T) {
	body := strings.Replace(valid, "maxMessageSize: {from: params.maxMessageSize}", "maxMessageSize: {from: maxMessageSize}", 1)
	_, err := Load(write(t, body))
	if err == nil || !strings.Contains(err.Error(), "must start with params.") {
		t.Fatalf("err = %v, want a complaint that from lacks the params. prefix", err)
	}
}

// multiResource is a fixture with three resources: two valid, and a third
// with neither name nor kind set, at index 2.
const multiResource = `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue
spec:
  sources:
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
  xrd:
    group: platform.hooli.tech
    kind: XQueue
    plural: xqueues
    version: v1alpha1
    scope: Namespaced
    parameters:
      location: {type: string, required: true, enum: [EU, US]}
  resources:
    - name: main-queue
      kind: Queue
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
      fields:
        maxMessageSize: {value: "1024"}
    - name: second-queue
      kind: Queue
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
      fields:
        maxMessageSize: {value: "2048"}
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
      fields:
        maxMessageSize: {value: "4096"}
`

// TestValidateResourceErrorIdentifiesOffendingEntry covers the discriminator
// added to the "needs a name and a kind" error: a blank entry among several
// resources must be locatable by its index (and, when only one of name/kind
// is missing, by whichever one is present).
func TestValidateResourceErrorIdentifiesOffendingEntry(t *testing.T) {
	t.Run("blank third resource names index 2", func(t *testing.T) {
		_, err := Load(write(t, multiResource))
		if err == nil || !strings.Contains(err.Error(), "spec.resources[2]") {
			t.Fatalf("err = %v, want it to identify spec.resources[2]", err)
		}
	})

	t.Run("kind present but name missing names the kind for context", func(t *testing.T) {
		body := strings.Replace(multiResource,
			"    - provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2\n      fields:\n        maxMessageSize: {value: \"4096\"}\n",
			"    - kind: Queue\n      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2\n      fields:\n        maxMessageSize: {value: \"4096\"}\n",
			1)
		_, err := Load(write(t, body))
		if err == nil || !strings.Contains(err.Error(), "spec.resources[2]") || !strings.Contains(err.Error(), "Queue") {
			t.Fatalf("err = %v, want it to identify spec.resources[2] and name kind Queue", err)
		}
	})

	t.Run("name present but kind missing names the resource for context", func(t *testing.T) {
		body := strings.Replace(multiResource,
			"    - provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2\n      fields:\n        maxMessageSize: {value: \"4096\"}\n",
			"    - name: third-queue\n      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2\n      fields:\n        maxMessageSize: {value: \"4096\"}\n",
			1)
		_, err := Load(write(t, body))
		if err == nil || !strings.Contains(err.Error(), "spec.resources[2]") || !strings.Contains(err.Error(), "third-queue") {
			t.Fatalf("err = %v, want it to identify spec.resources[2] and name third-queue", err)
		}
	})
}

// --- Task 7b: identifier-format validation (closes the gap the Task 8
// review surfaced: Validate() never format-checked identifiers, and they
// reach emitted output as raw YAML map keys and structural values) ---

// validParamBlueprint returns a minimally-valid Blueprint (built directly as
// a Go value, not through YAML) with exactly one parameter, named paramName,
// of type string. Building the struct directly -- rather than round-tripping
// through a YAML fixture, as the rest of this file does -- is deliberate:
// several of the pathological names under test (a colon-space, a leading
// '#', the empty string) either cannot appear as an unquoted YAML mapping
// key at all, or can only appear via a quoted key, which would conflate
// "does my YAML fixture parse" with "does Validate() reject this
// identifier." Constructing the Blueprint directly isolates the function
// actually under test.
func validParamBlueprint(paramName string) *Blueprint {
	return &Blueprint{
		Spec: Spec{
			XRD: XRD{
				Group: "platform.hooli.tech", Kind: "XQueue", Plural: "xqueues",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]Parameter{paramName: {Type: "string"}},
			},
		},
	}
}

// TestValidateRejectsInvalidParameterNames covers every pathological
// parameter name the Task 8 review found reaches emitted output unchecked:
// a colon-space (breaks YAML as an unquoted key: "mapping values are not
// allowed in this context"), a leading '#' (the rest of the line is read as
// a comment, silently eating the key), the empty string (invalid YAML key),
// and "yes"/"1.0" (parse fine but the KEY silently becomes a bool/number
// under YAML 1.1 keyword rules, not the literal string the user wrote).
//
// (b) "a b" (a name containing an internal space) is deliberately included
// here as a REJECTED case, not an accepted one. It does not break YAML the
// way the others do, but it is not camelCase either -- and camelCase is the
// shape of every real CRD spec property (forProvider, maxMessageSize), so
// rejecting it keeps parameter names consistent with the properties they
// sit beside once emitted. This is a deliberate policy choice, not a parser
// limitation.
func TestValidateRejectsInvalidParameterNames(t *testing.T) {
	tests := []struct {
		name      string // subtest name
		paramName string
	}{
		{"colon-space breaks YAML as an unquoted key", "foo: bar"},
		{"leading hash is read as a comment", "#lead"},
		{"empty string is not valid YAML key content", ""},
		{"yes is a YAML 1.1 boolean keyword", "yes"},
		{"1.0 is a YAML number, not a string key", "1.0"},
		{"internal space -- rejected by policy, see (b) above", "a b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validParamBlueprint(tt.paramName).Validate()
			if err == nil {
				t.Fatalf("Validate() = nil, want a complaint about parameter name %q", tt.paramName)
			}
			if !strings.Contains(err.Error(), "invalid parameter name") {
				t.Errorf("err = %v, want it to say \"invalid parameter name\"", err)
			}
			if !strings.Contains(err.Error(), "spec.xrd.parameters.") {
				t.Errorf("err = %v, want it to name the field path spec.xrd.parameters.*", err)
			}
		})
	}
}

// TestValidateAcceptsLegitimateParameterNames covers (c): realistic
// camelCase parameter names, including one with digits directly after the
// leading letter (x509Mode), must not be rejected by the new format check.
func TestValidateAcceptsLegitimateParameterNames(t *testing.T) {
	for _, n := range []string{"location", "maxMessageSize", "providerName", "x509Mode"} {
		t.Run(n, func(t *testing.T) {
			if err := validParamBlueprint(n).Validate(); err != nil {
				t.Errorf("Validate() = %v, want %q to be accepted", err, n)
			}
		})
	}
}

// TestValidateRejectsInvalidXRDIdentifiers covers (d): group, kind, plural
// and version are each individually format-checked, and the error names the
// specific field. The fixture's existing valid forms (exercised by
// TestLoadValidBlueprint, unaffected by these changes) establish that a
// well-formed blueprint is still accepted -- this test only adds the
// rejection side.
func TestValidateRejectsInvalidXRDIdentifiers(t *testing.T) {
	tests := []struct {
		name       string
		old, new   string
		wantSubstr string
	}{
		{"bad group: uppercase is not a valid DNS subdomain", "group: platform.hooli.tech", "group: Platform.Hooli.Tech", "spec.xrd.group"},
		{"bad kind: must start uppercase", "kind: XQueue", "kind: xQueue", "spec.xrd.kind"},
		{"bad plural: must be all lowercase", "plural: xqueues", "plural: XQueues", "spec.xrd.plural"},
		{"bad version: missing v prefix", "version: v1alpha1", "version: version1", "spec.xrd.version"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := strings.Replace(valid, tt.old, tt.new, 1)
			if body == valid {
				t.Fatalf("replacement %q did not match anything in the fixture", tt.old)
			}
			_, err := Load(write(t, body))
			if err == nil || !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Fatalf("err = %v, want substring %q", err, tt.wantSubstr)
			}
		})
	}
}

// TestValidateRejectsInvalidResourceName covers the resource-name format
// check: a resource's name becomes a composition-resource-name annotation
// value, so it must be a DNS label like the rest of Kubernetes' naming
// rules, not merely non-empty.
func TestValidateRejectsInvalidResourceName(t *testing.T) {
	body := strings.Replace(valid, "name: main-queue", "name: Main_Queue", 1)
	_, err := Load(write(t, body))
	if err == nil || !strings.Contains(err.Error(), "spec.resources[0]") || !strings.Contains(err.Error(), "invalid resource name") {
		t.Fatalf("err = %v, want it to name spec.resources[0] as an invalid resource name", err)
	}
}

// TestValidateStillAcceptsTheValidFixture is (e): the existing valid
// blueprint fixture -- whose group, kind, plural, version, parameter names
// and resource name all now pass through the new format checks -- must
// still load cleanly. TestLoadValidBlueprint already covers this and
// continues to pass unmodified; this test exists to make the "no
// regression" requirement an explicit, separately-named assertion.
func TestValidateStillAcceptsTheValidFixture(t *testing.T) {
	if _, err := Load(write(t, valid)); err != nil {
		t.Fatalf("Load(valid) = %v, want no error", err)
	}
}

// --- Follow-up: yamlKeywords applied to plural and group ---
//
// The class TestValidateRejectsInvalidParameterNames closed for parameter
// names was only half-closed: pluralRE (^[a-z][a-z0-9]*$) and groupRE both
// admit bare keyword strings too ("yes"/"no" are plain lowercase letters),
// and plural/group both reach emitted output as unquoted YAML scalar
// VALUES (internal/emit/xrd.go's `d.Line(2, "plural: %s", ...)` and
// `d.Line(1, "group: %s", ...)`), the same failure mode as a parameter name
// reaching it as an unquoted map KEY. kind and version need no such check:
// kindRE requires an uppercase initial letter and versionRE requires a `v`
// followed by digits, so no string satisfying either can also be a YAML 1.1
// keyword (yes/no/true/false/on/off/null/y/n are all lowercase-initial).
//
// A wrinkle surfaced while writing these tests: sigs.k8s.io/yaml coerces an
// *unquoted* keyword-shaped scalar during parsing of the blueprint source
// file itself, before Validate() ever runs -- `plural: yes` unmarshals to
// the Go string "true", and `plural: no` to "false" (verified with a throwaway
// script against this exact dependency; not asserted here since it is
// YAML-library behavior, not this package's logic). That's a related but
// distinct layer of the same defect class: even a user who never intended a
// boolean has their source file's plain-looking "yes" quietly turned into
// a different string during parsing. It doesn't change what needs testing
// here, but it does mean the fixtures below use an explicitly quoted
// "yes"/"no" ("plural: \"yes\"", "group: \"no\"") to land the literal
// string in x.Plural/x.Group, exercising Validate()'s check specifically --
// an unquoted `plural: yes` would test yamlKeywords indirectly, by way of
// coercing to "true", which is also a keyword and would pass for the wrong
// reason.
func TestValidateGroupAndPluralKeywordCheck(t *testing.T) {
	tests := []struct {
		name       string
		old, new   string
		wantReject bool
		wantSubstr string // checked only when wantReject
	}{
		{
			name: `plural "yes" is a YAML keyword, rejected`,
			old:  "plural: xqueues", new: `plural: "yes"`,
			wantReject: true, wantSubstr: "spec.xrd.plural",
		},
		{
			name: `plural "xqueues" is still accepted`,
			old:  "plural: xqueues", new: "plural: xqueues",
			wantReject: false,
		},
		{
			name: `bare group "no" is a YAML keyword, rejected`,
			old:  "group: platform.hooli.tech", new: `group: "no"`,
			wantReject: true, wantSubstr: "spec.xrd.group",
		},
		{
			name: `group "platform.hooli.tech" is still accepted`,
			old:  "group: platform.hooli.tech", new: "group: platform.hooli.tech",
			wantReject: false,
		},
		{
			name: `group "no.example.com" is accepted: a legitimate multi-label group whose first label reads as a keyword must not be over-rejected`,
			old:  "group: platform.hooli.tech", new: "group: no.example.com",
			wantReject: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := strings.Replace(valid, tt.old, tt.new, 1)
			_, err := Load(write(t, body))
			if tt.wantReject {
				if err == nil || !strings.Contains(err.Error(), tt.wantSubstr) {
					t.Fatalf("err = %v, want substring %q", err, tt.wantSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v, want no error", err)
			}
		})
	}
}

// --- Cleanup task, item 2: default validated against parameter type ---
//
// The XRD emitter honours Parameter.Default, emitting it quoted for
// type: string and unquoted for integer/number/boolean. It has no sensible
// handling for a default on type: object or array, and there is nothing
// stopping a boolean default of "notabool" or an integer default of "abc"
// from being written out unquoted and unvalidated either -- any of these
// would produce an invalid CRD schema. Validate() is the right place to
// stop that, not the emitter guessing.

// blueprintWithParam returns a minimally-valid Blueprint with exactly one
// parameter, named "value", set to p. Used by the default-vs-type tests
// below, which need control over both Type and Default together -- unlike
// validParamBlueprint above, which only varies the parameter's name.
func blueprintWithParam(p Parameter) *Blueprint {
	return &Blueprint{
		Spec: Spec{
			XRD: XRD{
				Group: "platform.hooli.tech", Kind: "XQueue", Plural: "xqueues",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]Parameter{"value": p},
			},
		},
	}
}

func TestValidateDefaultAgainstType(t *testing.T) {
	tests := []struct {
		name       string
		param      Parameter
		wantReject bool
		wantSubstr string // checked only when wantReject
	}{
		{
			name:       "(a) default on an object parameter is rejected, naming the parameter",
			param:      Parameter{Type: "object", Default: "{}"},
			wantReject: true, wantSubstr: "spec.xrd.parameters.value",
		},
		{
			name:       "(b) default on an array parameter is rejected",
			param:      Parameter{Type: "array", Default: "[]"},
			wantReject: true, wantSubstr: "spec.xrd.parameters.value",
		},
		{
			name:       `(c) default "notabool" on a boolean parameter is rejected`,
			param:      Parameter{Type: "boolean", Default: "notabool"},
			wantReject: true, wantSubstr: "spec.xrd.parameters.value",
		},
		{
			name:       `(d) default "abc" on an integer parameter is rejected`,
			param:      Parameter{Type: "integer", Default: "abc"},
			wantReject: true, wantSubstr: "spec.xrd.parameters.value",
		},
		{
			name:  `(e) valid string default "sm" is accepted`,
			param: Parameter{Type: "string", Default: "sm"},
		},
		{
			name:  `(e) valid integer default "1024" is accepted`,
			param: Parameter{Type: "integer", Default: "1024"},
		},
		{
			name:  `(e) valid number default "1.5" is accepted`,
			param: Parameter{Type: "number", Default: "1.5"},
		},
		{
			name:  `(e) valid boolean default "true" is accepted`,
			param: Parameter{Type: "boolean", Default: "true"},
		},
		{
			name:  "(f) no default at all is accepted -- no regression, including on object/array types",
			param: Parameter{Type: "object"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := blueprintWithParam(tt.param).Validate()
			if tt.wantReject {
				if err == nil || !strings.Contains(err.Error(), tt.wantSubstr) {
					t.Fatalf("err = %v, want substring %q", err, tt.wantSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v, want no error", err)
			}
		})
	}
}

// TestValidateIntegerDefaultRejectsFraction pins a stricter behavior than
// the brief's letter required: "integer" defaults are parsed with
// strconv.ParseInt, not strconv.ParseFloat, so a fractional string like
// "1.5" -- syntactically "a number" but not a whole one -- is rejected on an
// integer parameter. A type: integer OpenAPI/CRD schema field with a
// non-whole default is itself an invalid schema; parsing leniently enough to
// accept "1.5" here would just move that defect one step downstream, past
// the one place equipped to catch it precisely.
func TestValidateIntegerDefaultRejectsFraction(t *testing.T) {
	err := blueprintWithParam(Parameter{Type: "integer", Default: "1.5"}).Validate()
	if err == nil || !strings.Contains(err.Error(), "spec.xrd.parameters.value") {
		t.Fatalf("err = %v, want a complaint naming spec.xrd.parameters.value", err)
	}
}

// --- Final review, C1: control characters in user-controlled scalars ---
//
// quoteYAML wraps a user scalar in single quotes, which handles ": ", " #"
// and keyword-shaped values. It does nothing about a line break, because
// internal/emit's Doc.Line writes `indent + text + "\n"` verbatim and
// nothing re-indents a continuation line: the rest of the value lands at
// column 0, outside the quotes and outside every indentation context the
// emitter established. The reviewer reproduced both halves:
//
//	Field.Value       = "eu-north-1\nbogus: injected"   -> unparseable Composition
//	Parameter.Description = "line one\nline two: bad"   -> an XRD that PARSES,
//	                                                       with a bogus top-level
//	                                                       key, and `cf gen --check`
//	                                                       reporting "in sync"
//
// internal/emit/composition_test.go proves the artifact-level consequence by
// parsing emitted YAML. This file proves the rule: every reachable scalar is
// checked, and the error names the exact field path.

// scalarBlueprint returns a valid Namespaced blueprint with one resource,
// which mutate then poisons in exactly one place.
func scalarBlueprint(mutate func(*Blueprint)) *Blueprint {
	b := &Blueprint{
		Metadata: Metadata{Name: "xqueue"},
		Spec: Spec{
			XRD: XRD{
				Group: "platform.hooli.tech", Kind: "XQueue", Plural: "xqueues",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]Parameter{
					"providerName": {Type: "string", Required: true},
					"location":     {Type: "string", Required: true},
				},
			},
			Resources: []Resource{{
				Name: "main-queue", Kind: "Queue",
				Fields: map[string]Field{"region": {Value: "eu-north-1"}},
			}},
		},
	}
	mutate(b)
	return b
}

func TestValidateRejectsControlCharactersInUserScalars(t *testing.T) {
	// Every distinct reachable scalar, each poisoned on its own. The
	// injected text is the reviewer's own reproduction where one exists.
	fields := []struct {
		name      string
		poison    func(*Blueprint, string)
		wantField string
	}{
		{"metadata.name", func(b *Blueprint, bad string) {
			b.Metadata.Name = "xqueue" + bad
		}, "metadata.name"},
		{"parameter description", func(b *Blueprint, bad string) {
			p := b.Spec.XRD.Parameters["location"]
			p.Description = "line one" + bad + "line two: injected"
			b.Spec.XRD.Parameters["location"] = p
		}, "spec.xrd.parameters.location.description"},
		{"parameter default", func(b *Blueprint, bad string) {
			p := b.Spec.XRD.Parameters["location"]
			p.Default = "EU" + bad + "injected: true"
			b.Spec.XRD.Parameters["location"] = p
		}, "spec.xrd.parameters.location.default"},
		{"enum entry", func(b *Blueprint, bad string) {
			p := b.Spec.XRD.Parameters["location"]
			p.Enum = []string{"EU", "US" + bad + "injected: true"}
			b.Spec.XRD.Parameters["location"] = p
		}, "spec.xrd.parameters.location.enum[1]"},
		{"field value", func(b *Blueprint, bad string) {
			b.Spec.Resources[0].Fields["region"] = Field{Value: "eu-north-1" + bad + "bogus: injected"}
		}, `field "region": value`},
		{"field raw", func(b *Blueprint, bad string) {
			b.Spec.Resources[0].Fields["region"] = Field{Raw: "eu-north-1" + bad + "bogus: injected"}
		}, `field "region": raw`},
		{"field from", func(b *Blueprint, bad string) {
			b.Spec.Resources[0].Fields["region"] = Field{From: "params.location" + bad + "x"}
		}, `field "region": from`},
	}
	// \n and \r are YAML line breaks. \t is not, but Doc.Line's TrimRight
	// silently eats it at end of line, so the emitted value stops matching
	// the value the user wrote. \x00 stands in for the rest of C0.
	poisons := map[string]string{
		"newline":         "\n",
		"carriage return": "\r",
		"tab":             "\t",
		"NUL":             "\x00",
	}
	for _, f := range fields {
		for pname, bad := range poisons {
			t.Run(f.name+"/"+pname, func(t *testing.T) {
				b := scalarBlueprint(func(b *Blueprint) { f.poison(b, bad) })
				err := b.Validate()
				if err == nil {
					t.Fatalf("Validate() = nil, want %s with a %s to be rejected", f.name, pname)
				}
				if !strings.Contains(err.Error(), f.wantField) {
					t.Errorf("err = %v, want it to name the field path %q", err, f.wantField)
				}
			})
		}
	}
}

// TestValidateAcceptsOrdinaryPunctuationInScalars is the other half: the
// rule must reject control characters, not free text. A description with
// colons, hashes and quotes is ordinary and quoteYAML already handles it.
func TestValidateAcceptsOrdinaryPunctuationInScalars(t *testing.T) {
	b := scalarBlueprint(func(b *Blueprint) {
		p := b.Spec.XRD.Parameters["location"]
		p.Description = `Region: the "place" # where it lives -- naïve, 100% fine`
		p.Enum = []string{"EU", "US"}
		b.Spec.XRD.Parameters["location"] = p
		b.Spec.Resources[0].Fields["region"] = Field{Value: "eu-north-1: not a key # not a comment"}
	})
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want ordinary punctuation and non-ASCII text to be accepted", err)
	}
}

// --- Final review, C2: composite values behind from: ---

func TestValidateRejectsArrayParameterType(t *testing.T) {
	b := scalarBlueprint(func(b *Blueprint) {
		b.Spec.XRD.Parameters["zones"] = Parameter{Type: "array"}
	})
	err := b.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want type: array to be refused in M1")
	}
	if !strings.Contains(err.Error(), "spec.xrd.parameters.zones") {
		t.Errorf("err = %v, want it to name spec.xrd.parameters.zones", err)
	}
	if !strings.Contains(err.Error(), "array") {
		t.Errorf("err = %v, want it to name the array type", err)
	}
}

func TestValidateRejectsFromOnCompositeParameter(t *testing.T) {
	b := scalarBlueprint(func(b *Blueprint) {
		b.Spec.XRD.Parameters["tags"] = Parameter{Type: "object"}
		b.Spec.Resources[0].Fields["tags"] = Field{From: "params.tags"}
	})
	err := b.Validate()
	if err == nil {
		t.Fatal(`Validate() = nil, want a from: mapping onto an object parameter to be refused: ` +
			`it renders Go's fmt of the map ("map[env:prod]"), which is valid YAML and silently wrong`)
	}
	if !strings.Contains(err.Error(), "tags") || !strings.Contains(err.Error(), "object") {
		t.Errorf("err = %v, want it to name the field, the parameter and its type", err)
	}
}

// A scalar parameter behind from: is the supported case and must still work.
func TestValidateAcceptsFromOnScalarParameter(t *testing.T) {
	for _, typ := range []string{"string", "integer", "number", "boolean"} {
		t.Run(typ, func(t *testing.T) {
			b := scalarBlueprint(func(b *Blueprint) {
				b.Spec.XRD.Parameters["thing"] = Parameter{Type: typ}
				b.Spec.Resources[0].Fields["thing"] = Field{From: "params.thing"}
			})
			if err := b.Validate(); err != nil {
				t.Fatalf("Validate() = %v, want from: onto a %s parameter to be accepted", err, typ)
			}
		})
	}
}

// --- Final review, I3: Cluster scope is refused, not half-composed ---

func TestValidateRejectsClusterScope(t *testing.T) {
	b := scalarBlueprint(func(b *Blueprint) { b.Spec.XRD.Scope = "Cluster" })
	err := b.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want Cluster scope refused: the Composition emitter omits " +
			"providerConfigRef entirely for it, silently binding every composed resource to the " +
			"ProviderConfig named \"default\"")
	}
	if !strings.Contains(err.Error(), "Cluster") || !strings.Contains(err.Error(), "Namespaced") {
		t.Errorf("err = %v, want it to name Cluster and point at Namespaced", err)
	}
}
