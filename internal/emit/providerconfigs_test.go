package emit

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// --- providerFamily -----------------------------------------------------

func TestProviderFamilySplitsUpjetFamilyNames(t *testing.T) {
	for _, tc := range []struct {
		ref, wantFamily string
		wantSplit       bool
	}{
		{"ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0", "aws", true},
		{"xpkg.upbound.io/upbound/provider-aws-s3:v2", "aws", true},
		{"xpkg.upbound.io/upbound/provider-gcp-storage:v2.6.0", "gcp", true},
		// The family package itself: same family as its service packages, and
		// still recognized as a real split (it names the actual family pkg).
		{"xpkg.upbound.io/upbound/provider-family-aws:v2.4.0", "aws", true},
		// Single-package providers: no split, own family, repo name verbatim
		// (provider- prefix stripped).
		{"ghcr.io/crossplane-contrib/provider-kubernetes:v1.0.0", "kubernetes", false},
		{"xpkg.upbound.io/crossplane-contrib/provider-helm:v0.20.3", "helm", false},
		// Not even provider-shaped: the repo name itself, as-is.
		{"example.org/not-a-provider:v1", "not-a-provider", false},
	} {
		fam, split, err := providerFamily(tc.ref)
		if err != nil {
			t.Errorf("providerFamily(%q): %v", tc.ref, err)
			continue
		}
		if fam != tc.wantFamily || split != tc.wantSplit {
			t.Errorf("providerFamily(%q) = (%q, %v), want (%q, %v)", tc.ref, fam, split, tc.wantFamily, tc.wantSplit)
		}
	}
}

func TestProviderFamilyRejectsUnparseableRef(t *testing.T) {
	if _, _, err := providerFamily("this is not a reference"); err == nil {
		t.Fatal("providerFamily accepted a string that is not a parseable OCI reference")
	}
}

// --- ProviderConfigs -----------------------------------------------------

// awsFamilyBlueprint is a blueprint sourcing provider-aws-sqs, the exact
// package this project's research warmed in the local xpkg cache
// (docs/research/raw/schema-sourcing.md §5). Its own base layer -- verified
// directly against the cached provider-aws-sqs@v2/package.yaml on this
// machine -- carries eight Queue*-family CRDs (queues, queuepolicies,
// queueredrivepolicies, queueredriveallowpolicies, each in both the
// sqs.aws.m.upbound.io and sqs.aws.upbound.io groups) and ZERO
// ProviderConfig or ClusterProviderConfig CRDs of any kind: the
// ClusterProviderConfig CRD ships only in the separate provider-family-aws
// package, which nothing in spec.sources here names. So the real,
// pinned-by-inspection branch for this fixture is the ASSUMPTION fallback,
// not a resolved-from-schema apiVersion/kind -- testCRDs (this package's
// shared fixture, composition_test.go) mirrors that same shape (a Queue CRD
// in both groups, no ProviderConfig CRD), so it doubles as the input here.
func awsFamilyBlueprint() *blueprint.Blueprint {
	return &blueprint.Blueprint{
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{Provider: "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0"},
			},
		},
	}
}

func TestProviderConfigsAWSFamilyGolden(t *testing.T) {
	out, err := ProviderConfigs(awsFamilyBlueprint(), testCRDs(t))
	if err != nil {
		t.Fatalf("ProviderConfigs: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("got %d families, want exactly 1 (aws): %v", len(out), keysOf(out))
	}
	got, ok := out["aws"]
	if !ok {
		t.Fatalf("families = %v, want key %q", keysOf(out), "aws")
	}
	assertGolden(t, "providerconfig_aws.golden.yaml", got)
}

// TestProviderConfigsDedupeSameFamily is the multi-source dedupe case: two
// service packages of the same upjet family (sqs, s3 -- both provider-aws-*)
// must produce exactly ONE providerconfigs/aws.yaml, not two, because
// installing the ProviderConfig is a once-per-family operation (see
// ProviderConfigs' doc comment) -- and its header must name BOTH sources, in
// spec.sources order, so an operator can see everything that landed in this
// one file.
func TestProviderConfigsDedupeSameFamily(t *testing.T) {
	b := &blueprint.Blueprint{
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{Provider: "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0"},
				{Provider: "ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0"},
			},
		},
	}
	out, err := ProviderConfigs(b, testCRDs(t))
	if err != nil {
		t.Fatalf("ProviderConfigs: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("two aws-family sources produced %d files, want exactly 1: %v", len(out), keysOf(out))
	}
	s := string(out["aws"])
	for _, want := range []string{
		"provider-aws-sqs:v2.7.0",
		"provider-aws-s3:v2.7.0",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("providerconfigs/aws.yaml header does not name source %q:\n%s", want, s)
		}
	}
}

// TestProviderConfigsDistinctFamiliesProduceDistinctFiles is dedupe's
// complement: two DIFFERENT families must never collapse into one file.
func TestProviderConfigsDistinctFamiliesProduceDistinctFiles(t *testing.T) {
	b := &blueprint.Blueprint{
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{Provider: "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0"},
				{Provider: "xpkg.upbound.io/upbound/provider-gcp-storage:v2.6.0"},
			},
		},
	}
	out, err := ProviderConfigs(b, testCRDs(t))
	if err != nil {
		t.Fatalf("ProviderConfigs: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d families, want 2 (aws, gcp): %v", len(out), keysOf(out))
	}
	if _, ok := out["aws"]; !ok {
		t.Errorf("missing family %q in %v", "aws", keysOf(out))
	}
	if _, ok := out["gcp"]; !ok {
		t.Errorf("missing family %q in %v", "gcp", keysOf(out))
	}
}

// TestProviderConfigsUsesRealCRDWhenLoaded covers the other branch of the
// "VERIFY against the cache" rule: when a ClusterProviderConfig CRD for the
// family IS among the loaded schemas (e.g. the family package was also
// added as a source), the scaffold uses ITS real apiVersion/kind, not the
// well-known-shape guess, and carries no ASSUMPTION comment.
func TestProviderConfigsUsesRealCRDWhenLoaded(t *testing.T) {
	pcCRDs, err := schema.ParseCRDs([][]byte{[]byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: clusterproviderconfigs.aws.m.upbound.io}
spec:
  group: aws.m.upbound.io
  scope: Cluster
  names: {kind: ClusterProviderConfig, plural: clusterproviderconfigs, categories: [providerconfig]}
  versions:
  - {name: v1beta1, served: true, storage: true}
`)})
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}
	crds := append(append([]schema.CRD{}, testCRDs(t)...), pcCRDs...)

	out, err := ProviderConfigs(awsFamilyBlueprint(), crds)
	if err != nil {
		t.Fatalf("ProviderConfigs: %v", err)
	}
	s := string(out["aws"])
	if !strings.Contains(s, "apiVersion: aws.m.upbound.io/v1beta1") {
		t.Errorf("did not use the resolved CRD's real apiVersion:\n%s", s)
	}
	if !strings.Contains(s, "kind: ClusterProviderConfig") {
		t.Errorf("did not carry kind: ClusterProviderConfig:\n%s", s)
	}
	if strings.Contains(s, "ASSUMPTION") {
		t.Errorf("carried an ASSUMPTION comment despite a real CRD being loaded:\n%s", s)
	}
}

// TestProviderConfigsSinglePackageFamilyDoesNotSuggestAFamilyPackage covers
// the honesty requirement on the fallback's own advice: a single-package
// provider (no upjet split detected) has no separate provider-family-<name>
// package for an operator to add, so the ASSUMPTION comment must not tell
// them to run `cf provider add .../provider-family-kubernetes` -- that
// command would fail against a package that does not exist.
func TestProviderConfigsSinglePackageFamilyDoesNotSuggestAFamilyPackage(t *testing.T) {
	b := &blueprint.Blueprint{
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{Provider: "ghcr.io/crossplane-contrib/provider-kubernetes:v1.0.0"},
			},
		},
	}
	out, err := ProviderConfigs(b, nil)
	if err != nil {
		t.Fatalf("ProviderConfigs: %v", err)
	}
	s := string(out["kubernetes"])
	if !strings.Contains(s, "ASSUMPTION") {
		t.Fatalf("expected an ASSUMPTION comment with no cached CRD at all:\n%s", s)
	}
	if strings.Contains(s, "provider-family-kubernetes") {
		t.Errorf("suggested adding a nonexistent provider-family-kubernetes package:\n%s", s)
	}
}

// TestProviderConfigsSourcelessBlueprintProducesNone covers the zero case:
// a blueprint that declares no spec.sources (every existing fixture in this
// package, including testBlueprint) must not gain a providerconfigs file
// from nothing.
func TestProviderConfigsSourcelessBlueprintProducesNone(t *testing.T) {
	out, err := ProviderConfigs(testBlueprint(), testCRDs(t))
	if err != nil {
		t.Fatalf("ProviderConfigs: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("a blueprint with no spec.sources produced %d providerconfig files, want 0: %v", len(out), keysOf(out))
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// --- Generate wiring -------------------------------------------------

func TestGenerateWritesOneProviderConfigPerFamily(t *testing.T) {
	b := testBlueprint()
	b.Spec.Sources = []blueprint.Source{
		{Provider: "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0"},
	}
	outs, err := Generate(b, testCRDs(t), "out")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	var pc *Output
	for i := range outs {
		if strings.HasSuffix(filepath.ToSlash(outs[i].Path), "providerconfigs/aws.yaml") {
			pc = &outs[i]
		}
	}
	if pc == nil {
		var paths []string
		for _, o := range outs {
			paths = append(paths, o.Path)
		}
		t.Fatalf("Generate did not produce providerconfigs/aws.yaml; got %v", paths)
	}
	if len(pc.Body) == 0 {
		t.Error("providerconfigs/aws.yaml has an empty body")
	}

	// Path order: Generate documents "sorted by path", and
	// "providerconfigs" sorts between "functions.yaml" and "xrds" byte-wise.
	// This is the wiring hunk's whole contract -- assert it structurally
	// rather than just "the file exists somewhere in the slice".
	if len(outs) != 4 {
		t.Fatalf("got %d outputs, want 4 (composition, functions.yaml, providerconfigs/aws.yaml, xrd)", len(outs))
	}
	wantSuffixes := []string{
		"compositions/xqueues.platform.hooli.tech.yaml",
		"functions.yaml",
		"providerconfigs/aws.yaml",
		"xrds/xqueues.platform.hooli.tech.yaml",
	}
	for i, want := range wantSuffixes {
		if !strings.HasSuffix(filepath.ToSlash(outs[i].Path), want) {
			t.Errorf("outputs[%d].Path = %q, want a path ending %q", i, outs[i].Path, want)
		}
	}
}

// TestGenerateProviderConfigsIsDeterministic is the double-generate
// determinism check for this feature specifically: byte-identical output,
// same path order, across two independent calls -- the same invariant
// TestGenerateIsDeterministic (emit_test.go) pins for the original three
// outputs, extended to cover the sources-bearing path this feature adds.
func TestGenerateProviderConfigsIsDeterministic(t *testing.T) {
	b := testBlueprint()
	b.Spec.Sources = []blueprint.Source{
		{Provider: "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0"},
		{Provider: "ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0"},
		{Provider: "xpkg.upbound.io/upbound/provider-gcp-storage:v2.6.0"},
	}
	a, err := Generate(b, testCRDs(t), "out")
	if err != nil {
		t.Fatalf("Generate (first run): %v", err)
	}
	c, err := Generate(b, testCRDs(t), "out")
	if err != nil {
		t.Fatalf("Generate (second run): %v", err)
	}
	if len(a) == 0 || len(a) != len(c) {
		t.Fatalf("output counts differ or are empty: %d vs %d", len(a), len(c))
	}
	for i := range a {
		if a[i].Path != c[i].Path || string(a[i].Body) != string(c[i].Body) {
			t.Fatalf("output %q differs between runs", a[i].Path)
		}
	}
}
