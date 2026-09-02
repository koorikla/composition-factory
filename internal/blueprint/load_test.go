package blueprint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"sigs.k8s.io/yaml"
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
    group: platform.sparky.ee
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
		{"missing group", "    group: platform.sparky.ee\n", "spec.xrd.group is required"},
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
	if err == nil || !strings.Contains(err.Error(), "params.<name>, params.<name>.<member> or resources.<name>.status.<path>") {
		t.Fatalf("err = %v, want a complaint naming all three accepted from grammars", err)
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
    group: platform.sparky.ee
    kind: XQueue
    plural: xqueues
    version: v1alpha1
    scope: Namespaced
    parameters:
      location: {type: string, required: true, enum: [EU, US]}
      # Mandatory for a Namespaced XRD: the Composition dereferences
      # $spec.providerName unguarded for every composed resource.
      providerName: {type: string, required: true}
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
// providerName is present in both fixtures below because a Namespaced XRD
// now requires it (the Composition dereferences $spec.providerName unguarded
// for every composed resource). It is written after the parameter under
// test, with an explicit assignment rather than a second map-literal entry,
// so that a test naming "providerName" as the parameter under test still
// ends up with the valid declaration and is not silently testing a
// different rule.
func validParamBlueprint(paramName string) *Blueprint {
	params := map[string]Parameter{paramName: {Type: "string"}}
	params["providerName"] = Parameter{Type: "string", Required: true}
	return &Blueprint{
		APIVersion: APIVersion,
		Kind:       Kind,
		Spec: Spec{
			XRD: XRD{
				Group: "platform.sparky.ee", Kind: "XQueue", Plural: "xqueues",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: params,
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
		{"bad group: uppercase is not a valid DNS subdomain", "group: platform.sparky.ee", "group: Platform.sparky.ee", "spec.xrd.group"},
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
			old:  "group: platform.sparky.ee", new: `group: "no"`,
			wantReject: true, wantSubstr: "spec.xrd.group",
		},
		{
			name: `group "platform.sparky.ee" is still accepted`,
			old:  "group: platform.sparky.ee", new: "group: platform.sparky.ee",
			wantReject: false,
		},
		{
			name: `group "no.example.com" is accepted: a legitimate multi-label group whose first label reads as a keyword must not be over-rejected`,
			old:  "group: platform.sparky.ee", new: "group: no.example.com",
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
		APIVersion: APIVersion,
		Kind:       Kind,
		Spec: Spec{
			XRD: XRD{
				Group: "platform.sparky.ee", Kind: "XQueue", Plural: "xqueues",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]Parameter{
					"value":        p,
					"providerName": {Type: "string", Required: true},
				},
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
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: "xqueue"},
		Spec: Spec{
			XRD: XRD{
				Group: "platform.sparky.ee", Kind: "XQueue", Plural: "xqueues",
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

// --- Final review, I1: providerName is not optional for a Namespaced XRD ---

func TestValidateRequiresProviderNameForNamespacedScope(t *testing.T) {
	tests := []struct {
		name  string
		param *Parameter // nil means: remove it entirely
	}{
		{"absent entirely", nil},
		{"declared but not required", &Parameter{Type: "string"}},
		{"declared with the wrong type", &Parameter{Type: "integer", Required: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := scalarBlueprint(func(b *Blueprint) {
				if tt.param == nil {
					delete(b.Spec.XRD.Parameters, "providerName")
					return
				}
				b.Spec.XRD.Parameters["providerName"] = *tt.param
			})
			err := b.Validate()
			if err == nil {
				t.Fatal("Validate() = nil, want providerName to be required for a Namespaced XRD: " +
					"the Composition dereferences $spec.providerName unguarded for every composed resource")
			}
			if !strings.Contains(err.Error(), "providerName") {
				t.Errorf("err = %v, want it to name providerName", err)
			}
			if tt.name == "absent entirely" {
				wantMsg := "spec.xrd.parameters.providerName is required for a Namespaced XRD: run cf serve without --blueprint to scaffold one, or add: providerName: {type: string, required: true}"
				if err.Error() != wantMsg {
					t.Errorf("err = %q, want %q", err.Error(), wantMsg)
				}
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

// --- Follow-up: PUT /api/blueprint made spec.sources and the previously
// unchecked resource.kind/resource.provider client-writable ---
//
// Before PUT /api/blueprint existed, no HTTP route made the whole document
// client-writable, so Validate() never checked spec.sources[*].provider at
// all, and skipped checkScalar (the control-character rejection every other
// user-controlled scalar gets) on Resource.Kind and Resource.Provider. Both
// are persisted verbatim by writeBlueprintFile and reloaded on the next
// request, and Resource.Provider/Source.Provider both reach
// cache.Store.Load (cmd/cf/gen.go, cmd/cf/serve.go,
// internal/api/generate.go), so a value with a control character or a
// nonsense shape could silently corrupt the stored document or misdirect a
// cache lookup. These tests pin the closed gap.

// TestValidateRejectsInvalidSourceProvider covers spec.sources[*].provider:
// empty, a control character (the same defect class checkScalar closes
// everywhere else), and a shape no OCI reference can take.
func TestValidateRejectsInvalidSourceProvider(t *testing.T) {
	tests := []struct {
		name       string
		provider   string
		wantSubstr string
	}{
		{"empty is rejected", "", "one of provider (a package ref) or crds"},
		{"newline is a control character", "ghcr.io/x/y:v1\ninjected: true", "spec.sources[0].provider"},
		{"NUL is a control character", "ghcr.io/x/y\x00:v1", "spec.sources[0].provider"},
		{"space is not a valid reference character", "ghcr.io/x y:v1", "spec.sources[0].provider"},
		{"internal quote is not a valid reference character", `ghcr.io/x"y:v1`, "spec.sources[0].provider"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := scalarBlueprint(func(b *Blueprint) {
				b.Spec.Sources = []Source{{Provider: tt.provider}}
			})
			err := b.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Fatalf("err = %v, want substring %q", err, tt.wantSubstr)
			}
		})
	}
}

// TestValidateAcceptsValidSourceProviderForms covers the realistic reference
// shapes providerRefRE must not reject: a plain tag, a digest-pinned
// reference (which contains '@' and ':'), and a registry with an explicit
// port (which contains a second ':').
func TestValidateAcceptsValidSourceProviderForms(t *testing.T) {
	for _, ref := range []string{
		"ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0",
		"ghcr.io/x/y@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"reg:5000/x",
	} {
		t.Run(ref, func(t *testing.T) {
			b := scalarBlueprint(func(b *Blueprint) {
				b.Spec.Sources = []Source{{Provider: ref}}
			})
			if err := b.Validate(); err != nil {
				t.Errorf("Validate() = %v, want provider ref %q to be accepted", err, ref)
			}
		})
	}
}

// TestValidateRejectsInvalidResourceKind covers spec.resources[*].kind: a
// control character (Resource.Kind previously skipped checkScalar entirely,
// unlike Resource.Name and every field value), and a shape that cannot be a
// real Kubernetes Kind (kindRE, the same rule already applied to
// spec.xrd.kind).
func TestValidateRejectsInvalidResourceKind(t *testing.T) {
	tests := []struct {
		name       string
		kind       string
		wantSubstr string
	}{
		{"newline is a control character", "Queue\ninjected: true", "spec.resources[0].kind"},
		{"NUL is a control character", "Queue\x00", "spec.resources[0].kind"},
		{"lowercase initial is not a valid Kind", "queue", "spec.resources[0].kind"},
		{"internal space is not a valid Kind", "Message Queue", "spec.resources[0].kind"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := scalarBlueprint(func(b *Blueprint) {
				b.Spec.Resources[0].Kind = tt.kind
			})
			err := b.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Fatalf("err = %v, want substring %q", err, tt.wantSubstr)
			}
		})
	}
}

// TestValidateRejectsInvalidResourceProvider covers spec.resources[*].provider,
// which -- unlike Source.Provider -- is optional: unset must still be
// accepted (TestValidateStillAcceptsTheValidFixture and
// TestValidateAcceptsValidResourceProviderForms below both exercise both the
// unset and set-and-valid cases), but a set-and-bad value gets exactly the
// same checks as Source.Provider.
func TestValidateRejectsInvalidResourceProvider(t *testing.T) {
	tests := []struct {
		name       string
		provider   string
		wantSubstr string
	}{
		{"newline is a control character", "ghcr.io/x/y:v1\ninjected: true", "spec.resources[0].provider"},
		{"NUL is a control character", "ghcr.io/x/y\x00:v1", "spec.resources[0].provider"},
		{"space is not a valid reference character", "ghcr.io/x y:v1", "spec.resources[0].provider"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := scalarBlueprint(func(b *Blueprint) {
				b.Spec.Resources[0].Provider = tt.provider
			})
			err := b.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Fatalf("err = %v, want substring %q", err, tt.wantSubstr)
			}
		})
	}
}

// TestValidateAcceptsValidResourceProviderForms covers the accepted side: an
// unset provider (optional field, no regression) and the same realistic
// reference shapes as TestValidateAcceptsValidSourceProviderForms, since
// resource.provider is checked identically to source.provider.
func TestValidateAcceptsValidResourceProviderForms(t *testing.T) {
	for _, ref := range []string{
		"",
		"ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0",
		"ghcr.io/x/y@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"reg:5000/x",
	} {
		name := ref
		if name == "" {
			name = "(unset)"
		}
		t.Run(name, func(t *testing.T) {
			b := scalarBlueprint(func(b *Blueprint) {
				b.Spec.Resources[0].Provider = ref
				// this test is about ref FORMAT; the manifest rule (provider
				// must be declared in sources) is covered separately.
				if ref != "" {
					b.Spec.Sources = append(b.Spec.Sources, Source{Provider: ref})
				}
			})
			if err := b.Validate(); err != nil {
				t.Errorf("Validate() = %v, want provider %q to be accepted", err, name)
			}
		})
	}
}

// TestValidateStillAcceptsTheOnDiskFixtureWithSourcesAndProvider is a final
// end-to-end regression check: testdata/xqueue.cf.yaml -- the fixture the
// acceptance test drives the real binary against -- declares both a
// spec.sources entry and a spec.resources[*].provider, and must still load
// cleanly now that both are checked.
func TestValidateStillAcceptsTheOnDiskFixtureWithSourcesAndProvider(t *testing.T) {
	b, err := Load("../../testdata/xqueue.cf.yaml")
	if err != nil {
		t.Fatalf("Load(testdata/xqueue.cf.yaml) = %v, want no error", err)
	}
	if len(b.Spec.Sources) != 1 || b.Spec.Sources[0].Provider == "" {
		t.Fatalf("Spec.Sources = %+v, want one source with a provider", b.Spec.Sources)
	}
	if len(b.Spec.Resources) == 0 {
		t.Fatal("Spec.Resources is empty, want at least one resource")
	}
	// Every resource in the fixture pins its provider explicitly; the count
	// is deliberately NOT pinned here, so growing the acceptance scenario
	// (it gained a status-wired queue-policy for E1) does not break a test
	// whose subject is validation, not the scenario's size.
	for i, r := range b.Spec.Resources {
		if r.Provider == "" {
			t.Errorf("Spec.Resources[%d] (%s) has no provider; the fixture pins every resource's provider", i, r.Name)
		}
	}
}

// --- forEach ---

// forEachBlueprint is a valid blueprint whose only resource is repeated by
// forEach over an integer parameter with a default. Mutations build every
// rejection case from this known-good baseline, so each test pins exactly
// one rule.
func forEachBlueprint(mutate func(*Blueprint)) *Blueprint {
	b := &Blueprint{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: "xqueue"},
		Spec: Spec{
			XRD: XRD{
				Group: "platform.sparky.ee", Kind: "XQueue", Plural: "xqueues",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]Parameter{
					"providerName":  {Type: "string", Required: true},
					"instanceCount": {Type: "integer", Default: "2"},
				},
			},
			Resources: []Resource{{
				Name: "replica-queue", Kind: "Queue",
				ForEach: "params.instanceCount",
				Fields:  map[string]Field{"region": {Value: "eu-north-1"}},
			}},
		},
	}
	mutate(b)
	return b
}

const validForEach = `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue
spec:
  xrd:
    group: platform.sparky.ee
    kind: XQueue
    plural: xqueues
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName: {type: string, required: true}
      instanceCount: {type: integer, default: "2"}
  resources:
    - name: replica-queue
      kind: Queue
      forEach: params.instanceCount
      fields:
        region: {value: eu-north-1}
`

func TestLoadValidForEachBlueprint(t *testing.T) {
	b, err := Load(write(t, validForEach))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := b.Spec.Resources[0].ForEach; got != "params.instanceCount" {
		t.Errorf("ForEach = %q, want params.instanceCount", got)
	}
}

// The forEach key must survive a marshal/unmarshal round trip exactly: the
// HTTP API persists the whole document by re-marshaling the Go struct
// (internal/api's writeBlueprintFile), so a key the struct dropped or
// renamed would be silently erased from the file on the first edit anyone
// makes through the API.
func TestForEachRoundTripsExactly(t *testing.T) {
	b, err := Load(write(t, validForEach))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	body, err := yaml.Marshal(b)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var reloaded Blueprint
	if err := yaml.Unmarshal(body, &reloaded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if diff := cmp.Diff(b, &reloaded); diff != "" {
		t.Errorf("blueprint changed across a marshal/unmarshal round trip (-loaded +reloaded):\n%s", diff)
	}
	if got := reloaded.Spec.Resources[0].ForEach; got != "params.instanceCount" {
		t.Errorf("ForEach after round trip = %q, want params.instanceCount", got)
	}
}

func TestValidateRejectsForEachWithoutParamsPrefix(t *testing.T) {
	b := forEachBlueprint(func(b *Blueprint) {
		b.Spec.Resources[0].ForEach = "instanceCount"
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "params.") {
		t.Fatalf("err = %v, want a complaint that forEach must reference params.<name>", err)
	}
	if !strings.Contains(err.Error(), "replica-queue") {
		t.Errorf("err = %v, want it to name the offending resource", err)
	}
}

func TestValidateRejectsForEachUnknownParameter(t *testing.T) {
	b := forEachBlueprint(func(b *Blueprint) {
		b.Spec.Resources[0].ForEach = "params.nope"
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Fatalf("err = %v, want an error naming the unknown parameter", err)
	}
	if !strings.Contains(err.Error(), "replica-queue") {
		t.Errorf("err = %v, want it to name the offending resource", err)
	}
}

func TestValidateRejectsForEachOnNonIntegerParameter(t *testing.T) {
	for _, typ := range []string{"string", "number", "boolean", "object"} {
		t.Run(typ, func(t *testing.T) {
			b := forEachBlueprint(func(b *Blueprint) {
				// Required with no default, so nothing but the type is wrong:
				// a default of "2" would trip the default-vs-type rule first
				// on boolean and object, and this test pins the type rule.
				b.Spec.XRD.Parameters["instanceCount"] = Parameter{Type: typ, Required: true}
			})
			err := b.Validate()
			if err == nil || !strings.Contains(err.Error(), "integer") {
				t.Fatalf("err = %v, want a complaint that the forEach parameter must be an integer", err)
			}
			if !strings.Contains(err.Error(), "replica-queue") || !strings.Contains(err.Error(), "instanceCount") {
				t.Errorf("err = %v, want it to name both the resource and the parameter", err)
			}
		})
	}
}

// An optional forEach parameter with no default can be genuinely absent from
// the observed composite's spec, and the loop bound is dereferenced
// unguarded: under options: ["missingkey=error"] that absence hard-fails the
// whole render. Only the XRD's required gate or its schema default makes the
// key's presence unconditional.
func TestValidateRejectsForEachOnOptionalParameterWithoutDefault(t *testing.T) {
	b := forEachBlueprint(func(b *Blueprint) {
		b.Spec.XRD.Parameters["instanceCount"] = Parameter{Type: "integer"}
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "required") || !strings.Contains(err.Error(), "default") {
		t.Fatalf("err = %v, want a complaint that the forEach parameter must be required or carry a default", err)
	}
	if !strings.Contains(err.Error(), "replica-queue") || !strings.Contains(err.Error(), "instanceCount") {
		t.Errorf("err = %v, want it to name both the resource and the parameter", err)
	}
}

func TestValidateAcceptsForEachOnRequiredParameterWithoutDefault(t *testing.T) {
	b := forEachBlueprint(func(b *Blueprint) {
		b.Spec.XRD.Parameters["instanceCount"] = Parameter{Type: "integer", Required: true}
	})
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want a required integer parameter to be a valid forEach bound", err)
	}
}

func TestValidateRejectsControlCharacterInForEach(t *testing.T) {
	b := forEachBlueprint(func(b *Blueprint) {
		b.Spec.Resources[0].ForEach = "params.instanceCount\nbogus: injected"
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "control character") {
		t.Fatalf("err = %v, want the checkScalar control-character rejection", err)
	}
	if !strings.Contains(err.Error(), "forEach") {
		t.Errorf("err = %v, want it to name the forEach field", err)
	}
}

// "k8s" is a resource-level label (compose this native kind), never a source
// package: a source entry named "k8s" would reach cache.Store.Load and fail
// with a misleading "run: cf provider add k8s", so Validate refuses it with
// the real explanation instead.
func TestValidateRefusesK8sAsASource(t *testing.T) {
	doc := strings.Replace(valid,
		"- provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2\n", "- provider: k8s\n", 1)
	_, err := Load(write(t, doc))
	if err == nil {
		t.Fatal("Validate accepted spec.sources[0].provider: k8s; native kinds are vendored, not a source package")
	}
	if !strings.Contains(err.Error(), "vendored") {
		t.Errorf("err = %v, want it to explain that native kinds are vendored and always available", err)
	}
}

// A resource composing a native kind carries provider: k8s — that spelling
// must validate.
func TestValidateAcceptsK8sAsAResourceProvider(t *testing.T) {
	b := validParamBlueprint("providerName")
	b.Spec.Resources = append(b.Spec.Resources, Resource{
		Name:     "web",
		Kind:     "Deployment",
		Provider: NativeProvider,
		Fields:   map[string]Field{"spec.replicas": {Raw: "2"}},
	})
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate refused a native resource with provider: k8s: %v", err)
	}
}

// A resource whose provider is not declared in spec.sources generates fine
// on a warm server but cannot survive a restart: startup loads providers
// from sources alone, so generate 400s with "kind not found in any cached
// provider" — hours after the mistake was made. Catch it at the source.
func TestValidateRejectsUndeclaredResourceProvider(t *testing.T) {
	body := strings.Replace(valid, "  resources:\n", "  resources:\n"+
		"    - name: stray-bucket\n"+
		"      kind: Bucket\n"+
		"      provider: ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0\n"+
		"      fields: {}\n", 1)
	_, err := Load(write(t, body))
	if err == nil {
		t.Fatal("Validate accepted a resource provider absent from spec.sources")
	}
	for _, want := range []string{"stray-bucket", "provider-aws-s3", "spec.sources"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// --- when conditionals ---

// whenBlueprint is a valid blueprint whose only resource is gated on a when
// condition over a defaulted string parameter with an enum. Mutations build
// every rejection case from this known-good baseline.
func whenBlueprint(mutate func(*Blueprint)) *Blueprint {
	b := &Blueprint{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: "xqueue"},
		Spec: Spec{
			XRD: XRD{
				Group: "platform.sparky.ee", Kind: "XQueue", Plural: "xqueues",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]Parameter{
					"providerName": {Type: "string", Required: true},
					"tier":         {Type: "string", Default: "standard", Enum: []string{"standard", "pro"}},
					"auditEnabled": {Type: "boolean", Default: "false"},
				},
			},
			Resources: []Resource{{
				Name: "audit-queue", Kind: "Queue",
				When:   `params.tier == "pro"`,
				Fields: map[string]Field{"region": {Value: "eu-north-1"}},
			}},
		},
	}
	mutate(b)
	return b
}

func TestParseWhen(t *testing.T) {
	cases := []struct {
		expr               string
		param, op, literal string
		wantErr            bool
	}{
		{expr: "params.auditEnabled", param: "auditEnabled"},
		{expr: `params.tier == "pro"`, param: "tier", op: "==", literal: "pro"},
		{expr: `params.tier != "standard"`, param: "tier", op: "!=", literal: "standard"},
		{expr: `params.tier == ""`, param: "tier", op: "==", literal: ""},
		{expr: "tier", wantErr: true},                   // no params. prefix
		{expr: `params.tier=="pro"`, wantErr: true},     // no spaces
		{expr: `params.tier ==  "pro"`, wantErr: true},  // two spaces
		{expr: `params.tier == 'pro'`, wantErr: true},   // single quotes
		{expr: `params.tier == pro`, wantErr: true},     // unquoted literal
		{expr: `params.tier == "p\"ro"`, wantErr: true}, // embedded quote/backslash
		{expr: `params.tier < "pro"`, wantErr: true},    // unsupported operator
		{expr: `params.ti-er == "pro"`, wantErr: true},  // non-camelCase parameter
		{expr: "", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			param, op, literal, err := ParseWhen(tc.expr)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseWhen(%q) = (%q, %q, %q), want an error", tc.expr, param, op, literal)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseWhen(%q): %v", tc.expr, err)
			}
			if param != tc.param || op != tc.op || literal != tc.literal {
				t.Errorf("ParseWhen(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tc.expr, param, op, literal, tc.param, tc.op, tc.literal)
			}
		})
	}
}

func TestValidateAcceptsWhenForms(t *testing.T) {
	for _, when := range []string{
		`params.tier == "pro"`,
		`params.tier != "pro"`,
		"params.auditEnabled",
	} {
		t.Run(when, func(t *testing.T) {
			b := whenBlueprint(func(b *Blueprint) { b.Spec.Resources[0].When = when })
			if err := b.Validate(); err != nil {
				t.Fatalf("Validate() = %v, want %q accepted", err, when)
			}
		})
	}
}

func TestValidateRejectsWhenUnknownParameter(t *testing.T) {
	b := whenBlueprint(func(b *Blueprint) { b.Spec.Resources[0].When = `params.nope == "pro"` })
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Fatalf("err = %v, want an error naming the unknown parameter", err)
	}
	if !strings.Contains(err.Error(), "audit-queue") {
		t.Errorf("err = %v, want it to name the offending resource", err)
	}
}

// The condition dereferences its parameter unguarded, exactly like a forEach
// loop bound: only required-or-defaulted parameters are safe under
// missingkey=error.
func TestValidateRejectsWhenOnOptionalParameterWithoutDefault(t *testing.T) {
	b := whenBlueprint(func(b *Blueprint) {
		b.Spec.XRD.Parameters["tier"] = Parameter{Type: "string"}
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "required") || !strings.Contains(err.Error(), "default") {
		t.Fatalf("err = %v, want the required-or-default rule", err)
	}
}

func TestValidateRejectsBareWhenOnNonBooleanParameter(t *testing.T) {
	b := whenBlueprint(func(b *Blueprint) { b.Spec.Resources[0].When = "params.tier" })
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "boolean") {
		t.Fatalf("err = %v, want the bare form to require a boolean parameter", err)
	}
}

func TestValidateRejectsWhenComparisonOnNonStringParameter(t *testing.T) {
	b := whenBlueprint(func(b *Blueprint) { b.Spec.Resources[0].When = `params.auditEnabled == "true"` })
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "string") {
		t.Fatalf("err = %v, want the comparison form to require a string parameter", err)
	}
}

// A literal the enum excludes makes the condition constant: the XRD admits
// no XR that could ever satisfy (or, for !=, fail) it — a resource that
// silently never or always exists, with every gate green.
func TestValidateRejectsWhenLiteralOutsideEnum(t *testing.T) {
	b := whenBlueprint(func(b *Blueprint) { b.Spec.Resources[0].When = `params.tier == "gold"` })
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), `"gold"`) || !strings.Contains(err.Error(), "enum") {
		t.Fatalf("err = %v, want the out-of-enum literal named and refused", err)
	}
}

func TestValidateRejectsMalformedWhen(t *testing.T) {
	b := whenBlueprint(func(b *Blueprint) { b.Spec.Resources[0].When = `params.tier == 'pro'` })
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "when must be") {
		t.Fatalf("err = %v, want the grammar spelled out", err)
	}
}

func TestValidateRejectsControlCharacterInWhen(t *testing.T) {
	b := whenBlueprint(func(b *Blueprint) {
		b.Spec.Resources[0].When = "params.tier\nbogus: injected"
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "control character") {
		t.Fatalf("err = %v, want the checkScalar control-character rejection", err)
	}
}

// The when key must survive a marshal/unmarshal round trip exactly: the HTTP
// API persists the whole document by re-marshaling the Go struct.
func TestWhenRoundTripsExactly(t *testing.T) {
	b := whenBlueprint(func(*Blueprint) {})
	body, err := yaml.Marshal(b)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var reloaded Blueprint
	if err := yaml.Unmarshal(body, &reloaded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if diff := cmp.Diff(b, &reloaded); diff != "" {
		t.Errorf("blueprint changed across a marshal/unmarshal round trip (-original +reloaded):\n%s", diff)
	}
	if got := reloaded.Spec.Resources[0].When; got != `params.tier == "pro"` {
		t.Errorf("When after round trip = %q, want the expression unchanged", got)
	}
}

// --- cross-resource status references ---

// statusRefBlueprint is a valid blueprint whose second resource wires a
// field from the first resource's observed status. Mutations build every
// rejection case from this known-good baseline, so each test pins exactly
// one rule.
func statusRefBlueprint(mutate func(*Blueprint)) *Blueprint {
	b := &Blueprint{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: "xqueue"},
		Spec: Spec{
			XRD: XRD{
				Group: "platform.sparky.ee", Kind: "XQueuePair", Plural: "xqueuepairs",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]Parameter{
					"providerName": {Type: "string", Required: true},
				},
			},
			Resources: []Resource{{
				Name: "main-queue", Kind: "Queue",
				Fields: map[string]Field{"region": {Value: "eu-north-1"}},
			}, {
				Name: "queue-policy", Kind: "QueuePolicy",
				Fields: map[string]Field{
					"region":   {Value: "eu-north-1"},
					"queueUrl": {From: "resources.main-queue.status.atProvider.url"},
				},
			}},
		},
	}
	mutate(b)
	return b
}

func TestValidateAcceptsStatusReference(t *testing.T) {
	b := statusRefBlueprint(func(*Blueprint) {})
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want a well-formed cross-resource status reference to be accepted", err)
	}
}

func TestValidateRejectsStatusReferenceToUnknownResource(t *testing.T) {
	b := statusRefBlueprint(func(b *Blueprint) {
		b.Spec.Resources[1].Fields["queueUrl"] = Field{From: "resources.no-such-queue.status.atProvider.url"}
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), `"no-such-queue"`) {
		t.Fatalf("err = %v, want an error naming the unknown resource", err)
	}
	if !strings.Contains(err.Error(), "queue-policy") || !strings.Contains(err.Error(), "queueUrl") {
		t.Errorf("err = %v, want it to name the referencing resource and field", err)
	}
}

func TestValidateRejectsStatusReferenceToSelf(t *testing.T) {
	b := statusRefBlueprint(func(b *Blueprint) {
		b.Spec.Resources[1].Fields["queueUrl"] = Field{From: "resources.queue-policy.status.atProvider.id"}
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "own status") {
		t.Fatalf("err = %v, want a self-reference to be refused", err)
	}
}

// A looped resource's composed documents are named <name>-0, <name>-1, ...
// (the indexed setResourceNameAnnotation), so the un-indexed key a status
// reference names never appears in $.observed.resources: the wire could
// never carry a value, silently.
func TestValidateRejectsStatusReferenceToLoopedResource(t *testing.T) {
	b := statusRefBlueprint(func(b *Blueprint) {
		b.Spec.XRD.Parameters["instanceCount"] = Parameter{Type: "integer", Default: "2"}
		b.Spec.Resources[0].ForEach = "params.instanceCount"
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "looped") {
		t.Fatalf("err = %v, want a reference to a forEach resource to be refused", err)
	}
	if !strings.Contains(err.Error(), "main-queue") {
		t.Errorf("err = %v, want it to name the looped target", err)
	}
}

func TestValidateRejectsMalformedStatusReference(t *testing.T) {
	for _, from := range []string{
		"resources.main-queue",                     // no .status. at all
		"resources.main-queue.status.",             // empty path
		"resources..status.atProvider.url",         // empty name
		"resources.main-queue.spec.forProvider.id", // not a status path
	} {
		t.Run(from, func(t *testing.T) {
			b := statusRefBlueprint(func(b *Blueprint) {
				b.Spec.Resources[1].Fields["queueUrl"] = Field{From: from}
			})
			err := b.Validate()
			if err == nil || !strings.Contains(err.Error(), "resources.<name>.status.<path>") {
				t.Fatalf("err = %v, want the grammar spelled out", err)
			}
		})
	}
}

// Every status path segment reaches the emitted template inside a hasKey
// guard and a dereference expression, so anything that is not a clean
// camelCase identifier is a structural risk, not a style complaint.
func TestValidateRejectsStatusReferencePathWithInvalidSegment(t *testing.T) {
	for _, from := range []string{
		"resources.main-queue.status.atProvider..url",
		`resources.main-queue.status.atProvider.u"rl`,
		"resources.main-queue.status.atProvider.a b",
	} {
		t.Run(from, func(t *testing.T) {
			b := statusRefBlueprint(func(b *Blueprint) {
				b.Spec.Resources[1].Fields["queueUrl"] = Field{From: from}
			})
			err := b.Validate()
			if err == nil {
				t.Fatalf("Validate accepted %q, want a rejection of the malformed path segment", from)
			}
		})
	}
}

func TestValidateRejectsDuplicateResourceNames(t *testing.T) {
	b := statusRefBlueprint(func(b *Blueprint) {
		b.Spec.Resources[1].Name = "main-queue"
		// Drop the reference: with both resources named main-queue the
		// self-reference rule would fire first and mask the duplicate check.
		b.Spec.Resources[1].Fields = map[string]Field{"region": {Value: "eu-north-1"}}
	})
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "duplicate resource name") {
		t.Fatalf("err = %v, want the duplicate name to be refused (it collapses two composed "+
			"resources into one composition-resource-name)", err)
	}
}

// A forward reference — the target declared after the referencing resource —
// is legitimate: emission order is not dependency order.
func TestValidateAcceptsForwardStatusReference(t *testing.T) {
	b := statusRefBlueprint(func(b *Blueprint) {
		b.Spec.Resources[0], b.Spec.Resources[1] = b.Spec.Resources[1], b.Spec.Resources[0]
	})
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want a forward reference to be accepted", err)
	}
}

func TestValidateAPIVersionAndKind(t *testing.T) {
	b := &Blueprint{
		APIVersion: "bogus/v1",
		Kind:       "Blueprint",
		Metadata:   Metadata{Name: "xqueue"},
		Spec: Spec{
			XRD: XRD{
				Group: "platform.sparky.ee", Kind: "XQueue", Plural: "xqueues",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]Parameter{
					"providerName": {Type: "string", Required: true},
				},
			},
		},
	}
	if err := b.Validate(); err == nil || !strings.Contains(err.Error(), "apiVersion") {
		t.Errorf("expected apiVersion validation error, got: %v", err)
	}

	b.APIVersion = APIVersion
	b.Kind = "OtherKind"
	if err := b.Validate(); err == nil || !strings.Contains(err.Error(), "kind") {
		t.Errorf("expected kind validation error, got: %v", err)
	}
}

func TestValidateAllowsOmittingProviderNameForNativeOnlyCompositions(t *testing.T) {
	tests := []struct {
		name       string
		parameters map[string]Parameter
		resources  []Resource
		wantErr    bool
	}{
		{
			name: "pure native with no providerName parameter",
			parameters: map[string]Parameter{
				"image": {Type: "string", Required: true},
			},
			resources: []Resource{
				{
					Name: "web", Kind: "Deployment", Provider: NativeProvider,
					Fields: map[string]Field{
						"spec.template.spec.containers[0].name":  {Value: "web"},
						"spec.template.spec.containers[0].image": {From: "params.image"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "pure native with optional providerName parameter",
			parameters: map[string]Parameter{
				"providerName": {Type: "string"},
				"image":        {Type: "string", Required: true},
			},
			resources: []Resource{
				{
					Name: "web", Kind: "Deployment", Provider: NativeProvider,
					Fields: map[string]Field{
						"spec.template.spec.containers[0].name":  {Value: "web"},
						"spec.template.spec.containers[0].image": {From: "params.image"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "mixed native and managed without providerName parameter",
			parameters: map[string]Parameter{
				"image": {Type: "string", Required: true},
			},
			resources: []Resource{
				{
					Name: "web", Kind: "Deployment", Provider: NativeProvider,
					Fields: map[string]Field{
						"spec.template.spec.containers[0].name":  {Value: "web"},
						"spec.template.spec.containers[0].image": {From: "params.image"},
					},
				},
				{
					Name: "main-queue", Kind: "Queue",
					Fields: map[string]Field{"region": {Value: "eu-north-1"}},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &Blueprint{
				APIVersion: APIVersion,
				Kind:       Kind,
				Metadata:   Metadata{Name: "xapp"},
				Spec: Spec{
					XRD: XRD{
						Group: "platform.sparky.ee", Kind: "XApp", Plural: "xapps",
						Version: "v1alpha1", Scope: "Namespaced",
						Parameters: tt.parameters,
					},
					Resources: tt.resources,
				},
			}
			err := b.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("Validate() = nil, want error requiring providerName for blueprint with managed resource")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() = %v, want valid for native-only blueprint", err)
			}
		})
	}
}
