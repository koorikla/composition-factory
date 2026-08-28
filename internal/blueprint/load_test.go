package blueprint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
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

// TestDereferencedParams is a table-driven test for the function that guards
// the project's central defect class: every parameter it reports gets marked
// required in the generated XRD, so a missing value fails at the XR
// admission gate instead of rendering the literal string "<no value>" into a
// live managed resource. If this function silently regresses, that mitigation
// silently stops working, so every branch is covered here directly against
// Blueprint values (bypassing YAML/Validate, since DereferencedParams does
// not depend on either).
func TestDereferencedParams(t *testing.T) {
	tests := []struct {
		name      string
		resources []Resource
		// want is asserted exactly with cmp.Diff. DereferencedParams never
		// returns a nil slice -- even with zero results it returns a non-nil,
		// empty []string{} (make(..., 0, 0) in Go yields a non-nil slice), so
		// every "nothing collected" case below asserts []string{}, not nil.
		want []string
	}{
		{
			name: "same parameter referenced by two resources dedupes to one entry",
			resources: []Resource{
				{Name: "a", Kind: "A", Fields: map[string]Field{"x": {From: "params.foo"}}},
				{Name: "b", Kind: "B", Fields: map[string]Field{"y": {From: "params.foo"}}},
			},
			want: []string{"foo"},
		},
		{
			name: "several parameters across several resources come back sorted",
			resources: []Resource{
				{Name: "a", Kind: "A", Fields: map[string]Field{
					"x": {From: "params.zebra"},
					"y": {From: "params.apple"},
				}},
				{Name: "b", Kind: "B", Fields: map[string]Field{
					"z": {From: "params.mango"},
				}},
			},
			want: []string{"apple", "mango", "zebra"},
		},
		{
			name: "field with empty From contributes nothing",
			resources: []Resource{
				{Name: "a", Kind: "A", Fields: map[string]Field{"x": {}}},
			},
			want: []string{},
		},
		{
			name: "From lacking the params. prefix contributes nothing",
			resources: []Resource{
				{Name: "a", Kind: "A", Fields: map[string]Field{"x": {From: "context.foo"}}},
			},
			want: []string{},
		},
		{
			name:      "no resources returns empty, not nil",
			resources: nil,
			want:      []string{},
		},
		{
			name: "fields set via value or raw contribute nothing",
			resources: []Resource{
				{Name: "a", Kind: "A", Fields: map[string]Field{
					"x": {Value: "1024"},
					"y": {Raw: "some: yaml"},
				}},
			},
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &Blueprint{Spec: Spec{Resources: tt.resources}}
			got := b.DereferencedParams()
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("DereferencedParams() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

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
