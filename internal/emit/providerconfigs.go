package emit

import (
	"fmt"
	"path"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// ProviderConfigs renders one example ProviderConfig manifest per distinct
// provider FAMILY that b.Spec.Sources references -- the cluster-setup
// scaffold a platform team needs sitting right next to the compositions, so
// they are not left to reverse-engineer "what ProviderConfig does this
// Composition's providerConfigRef actually expect" from provider docs. It
// returns family -> rendered document; Generate joins each into
// providerconfigs/<family>.yaml and sorts the set into its output list.
//
// Family, not source. Two service packages of the same upjet family (e.g.
// provider-aws-sqs and provider-aws-s3, both split off provider-family-aws)
// share ONE ProviderConfig -- the family package is what actually ships the
// ProviderConfig/ClusterProviderConfig CRD and what a cluster operator
// installs once per family, not once per service (see
// docs/research/raw/schema-sourcing.md §5: "provider-aws-sqs contains zero
// ProviderConfig CRDs ... provider-family-aws contains exactly 5"). Emitting
// one file per SOURCE would put N near-duplicate aws.yaml-ish files next to
// the compositions for no reason a platform team would want; emitting one
// per family gives them exactly the scaffold they need to apply once.
func ProviderConfigs(b *blueprint.Blueprint, crds []schema.CRD) (map[string][]byte, error) {
	type family struct {
		name  string
		refs  []string // every source ref that maps to this family, in spec.sources order
		split bool     // true once any ref recognizably names an upjet family split (see providerFamily)
	}
	byName := map[string]*family{}
	var order []string // first-seen family name order, for a stable refs slice within each family

	for _, s := range b.Spec.Sources {
		if s.Provider == "" {
			continue // a crds: source has no provider package, so no ProviderConfig family
		}
		fam, split, err := providerFamily(s.Provider)
		if err != nil {
			return nil, fmt.Errorf("spec.sources: deriving a provider family: %w", err)
		}
		f, ok := byName[fam]
		if !ok {
			f = &family{name: fam}
			byName[fam] = f
			order = append(order, fam)
		}
		f.refs = append(f.refs, s.Provider)
		f.split = f.split || split
	}

	out := make(map[string][]byte, len(byName))
	for _, fam := range order {
		f := byName[fam]
		body, err := renderProviderConfig(f.name, f.refs, f.split, crds)
		if err != nil {
			return nil, err
		}
		out[f.name] = body
	}
	return out, nil
}

// providerFamily derives a provider FAMILY label from a source reference's
// repository name -- entirely from the string itself, before any package is
// fetched, because spec.sources is exactly what a not-yet-generated
// blueprint has to work with. The second return value, split, distinguishes
// the two shapes the docstring below describes: true when ref recognizably
// names one member of an upjet-style family split (so there really is a
// separate provider-family-<family> package to point an operator at), false
// when ref itself is being treated as its own, single-package family (so
// suggesting a family package to add would be suggesting one that does not
// exist).
//
// The upjet convention (docs/research/raw/schema-sourcing.md §5, VERIFIED
// against the family package's own meta object) splits one family into many
// same-prefixed service packages: provider-aws-sqs, provider-aws-s3,
// provider-aws-eks, ... all depend on ONE provider-family-aws, which is the
// package that actually ships the ProviderConfig CRD. So "provider-<family>-
// <service>" yields <family> -- the first hyphen-delimited segment after
// "provider-". A source that names the family package itself
// (provider-family-aws) collapses to the same "aws", so a blueprint that
// happens to list both the family and one of its services still produces
// exactly one providerconfigs/aws.yaml, not two.
//
// Anything that does not fit that shape -- a single-package provider such as
// provider-kubernetes or provider-helm, which is never split into a family
// package plus service packages -- is its own family: the repository name
// verbatim (provider- prefix stripped for readability, matching the
// docs/research/2026-08-28-provider-discovery.md §3.6 short-name
// convention), with no further guessing. This is a naming heuristic, not a
// network lookup or a hard-coded provider list, so it is correct by
// construction for the documented upjet families (aws, gcp, azure,
// alibabacloud -- docs/research/2026-08-28-provider-discovery.md §2.1) and
// degrades to "one family per unrecognised provider name" rather than
// silently mis-grouping anything it has not seen before.
func providerFamily(ref string) (fam string, split bool, err error) {
	r, err := name.ParseReference(ref)
	if err != nil {
		return "", false, fmt.Errorf("parse reference %q: %w", ref, err)
	}
	repo := path.Base(r.Context().RepositoryStr())

	short, isProvider := strings.CutPrefix(repo, "provider-")
	if !isProvider {
		// Does not even look like a Crossplane provider package name -- there
		// is nothing to split, so the repo name itself is the family.
		return repo, false, nil
	}
	// provider-family-aws -> aws, and this ref alone proves it is a real
	// split family: naming the family package directly is as certain as this
	// heuristic gets.
	if withoutFamily, isFamilyPkg := strings.CutPrefix(short, "family-"); isFamilyPkg && withoutFamily != "" {
		return withoutFamily, true, nil
	}
	if i := strings.IndexByte(short, '-'); i > 0 {
		return short[:i], true, nil
	}
	return short, false, nil
}

// renderProviderConfig renders the example providerconfigs/<family>.yaml for
// one family, sourced from refs (every spec.sources entry that mapped to
// it, in blueprint order -- for the header's provenance comment). split
// mirrors providerFamily's second return: true when at least one of refs
// recognizably names an upjet family split (so there really is a separate
// provider-family-<fam> package worth pointing an operator at), false when
// fam is being treated as a single-package provider's own family.
func renderProviderConfig(fam string, refs []string, split bool, crds []schema.CRD) ([]byte, error) {
	d := NewDoc()
	header(d, strings.Join(refs, ", "))

	apiVersion, kind, assumed, err := providerConfigKind(fam, crds)
	if err != nil {
		return nil, err
	}
	if assumed {
		// The loud comment: nothing cached told cf what this family's real
		// ProviderConfig CRD looks like, so what follows is a well-known-shape
		// guess, not a read schema. See providerConfigKind's own comment for
		// the research this guess is pinned to. The fix-it advice differs by
		// split: a recognized upjet family really does have a separate
		// provider-family-<fam> package to add and regenerate against, but a
		// single-package provider (kubernetes, helm, ...) does not -- ITS
		// ProviderConfig CRD, if it ships one at all, lives in the very
		// package the blueprint already sources, so suggesting a nonexistent
		// "provider-family-<fam>" would be actively wrong advice.
		if split {
			d.Comment("ASSUMPTION: no ClusterProviderConfig CRD for provider family %q was found among the "+
				"loaded schemas -- only its service package(s) are cached, not provider-family-%s itself. "+
				"What follows is the well-known Upbound v2 family shape (<family>.m.upbound.io/v1beta1, "+
				"kind ClusterProviderConfig), not a value read from a real CRD. Run "+
				"`cf provider add xpkg.upbound.io/upbound/provider-family-%s` and regenerate to replace this "+
				"guess with the real apiVersion/kind.",
				fam, fam, fam)
		} else {
			d.Comment("ASSUMPTION: %q does not look like a split Upbound provider family, and no "+
				"ClusterProviderConfig CRD for it was found among the loaded schemas -- this provider may "+
				"ship no ClusterProviderConfig at all (many single-package providers still only ship the "+
				"pre-v2 ProviderConfig kind), or may ship one this generator's naming heuristic did not "+
				"recognize. What follows is a GUESS at the well-known Upbound v2 shape "+
				"(<name>.m.upbound.io/v1beta1, kind ClusterProviderConfig), not a value read from a real "+
				"CRD -- verify apiVersion/kind against this provider's own CRDs "+
				"(kubectl get crd | grep -i providerconfig) or its documentation, and correct this file by "+
				"hand.", fam)
		}
	}

	d.Line(0, "apiVersion: %s", apiVersion)
	d.Line(0, "kind: %s", kind)
	d.Line(0, "metadata:")
	// A fixed, obviously-a-placeholder name: the Composition's own
	// providerConfigRef.name is {{ $spec.providerName }} (see
	// internal/emit/envelope.go's writeComputedProviderConfigRef), a
	// required XRD parameter every caller of this blueprint sets on their
	// XR -- so "example" is never load-bearing by itself, only whatever the
	// operator renames it to (or the providerName value they pass) has to
	// match.
	d.Line(1, "name: example  # must match this family's XR(s): the value passed for the providerName parameter")
	d.Line(0, "spec:")
	d.Line(1, "credentials:")
	// Every Upbound-family and Upbound-shaped ClusterProviderConfig this
	// project's research surveyed carries a `credentials.source` enum whose
	// members differ per provider (docs/research/raw/cs-gcp-portability.md:
	// AWS ships [None, Secret, IRSA, WebIdentity, PodIdentity, Upbound]; GCP
	// ships a different six) but every one of them includes Secret, and
	// Secret is the only source that composes with a scaffold cf can
	// actually pre-fill -- IRSA/WebIdentity/InjectedIdentity name cluster
	// infrastructure this generator has no way to know about. So Secret is
	// the example; the comment names the escape hatch rather than pretending
	// it is the only option.
	d.Line(2, "source: Secret  # your provider may also support IRSA/WebIdentity/InjectedIdentity/etc; see its docs")
	d.Line(2, "secretRef:")
	d.Line(3, "namespace: crossplane-system  # TODO: namespace holding the credentials Secret")
	d.Line(3, "name: %s-creds  # TODO: name of the credentials Secret", fam)
	d.Line(3, "key: creds  # TODO: key inside that Secret holding the provider's credentials")
	return d.Bytes(), nil
}

// providerConfigKind resolves the (apiVersion, kind) this family's example
// ProviderConfig should carry: the resolved variant's real values whenever a
// ClusterProviderConfig CRD for this family is actually loaded (assumed
// false), otherwise a documented, VERIFIED assumption (assumed true, which
// renderProviderConfig turns into the loud in-file comment).
//
// The assumption branch is real for M1's actual generate path: `cf gen`
// (cmd/cf/gen.go) and both front doors load CRDs from spec.sources alone --
// the SERVICE package(s) a blueprint's resources are composed from -- never
// the family package, which ships no managed resource and so is never a
// reason to add it as a source. schema-sourcing.md §5 confirms this is not
// hypothetical: "provider-aws-sqs contains zero ProviderConfig CRDs";
// provider-family-aws is a SEPARATE package nothing here fetches
// automatically. So crds will, in the common case, simply not carry a
// ClusterProviderConfig for a family whose service package the user added --
// and this function must still produce a usable scaffold rather than erroring.
func providerConfigKind(fam string, crds []schema.CRD) (apiVersion, kind string, assumed bool, err error) {
	if crd, ok := familyProviderConfigCRD(fam, crds); ok {
		v, err := crd.APIVersion()
		if err != nil {
			return "", "", false, fmt.Errorf("family %q: resolved ClusterProviderConfig CRD %s: %w", fam, crd.Kind, err)
		}
		return v, crd.Kind, false, nil
	}
	// ASSUMPTION, not a fact read from any cached schema: docs/research/raw/
	// schema-sourcing.md's own extraction of provider-family-aws:v2.4.0 lists
	// clusterproviderconfigs.aws.m.upbound.io (group aws.m.upbound.io, kind
	// ClusterProviderConfig) -- NOT the legacy aws.upbound.io group, which
	// carries only kind ProviderConfig (Cluster-scoped, no "Cluster" prefix)
	// and has no ClusterProviderConfig at all. docs/research/raw/
	// cs-v2-native.md independently confirms the same shape from a real
	// cluster's own manifest: "apiVersion: aws.m.upbound.io/v1beta1 / kind:
	// ClusterProviderConfig". Since composition.go hard-codes
	// providerConfigRef.kind to ClusterProviderConfig (never the namespaced
	// ProviderConfig), the fallback here must name a group that actually
	// carries that kind -- <family>.m.upbound.io, not <family>.upbound.io --
	// or the scaffold would reference a GVK no such family package serves.
	// v1beta1 is the version every surveyed family package's ProviderConfig
	// CRDs use; there is no evidence of a second, competing version to guard
	// against. This whole branch is exactly the "family package was never
	// added" case, so it is expected to fire for most blueprints today --
	// the comment written into the file (renderProviderConfig) says so.
	return fam + ".m.upbound.io/v1beta1", "ClusterProviderConfig", true, nil
}

// familyProviderConfigCRD finds fam's real ClusterProviderConfig CRD among
// crds, if one happens to be loaded -- e.g. because the blueprint's sources
// (or a schema added at runtime) include the family package itself, not
// just one of its services. crds is Generate's aggregate schema set (every
// spec.sources entry's CRDs, unioned, with no per-source label attached), so
// there is no stronger match than (Kind, Group prefix): a group belongs to
// fam when it is exactly "fam.<anything>" -- the trailing dot in the prefix
// check is load-bearing, so family "aws" never matches a hypothetical
// "awsx.m.upbound.io".
func familyProviderConfigCRD(fam string, crds []schema.CRD) (schema.CRD, bool) {
	prefix := fam + "."
	for _, c := range crds {
		if !c.Native && c.Kind == "ClusterProviderConfig" && strings.HasPrefix(c.Group, prefix) {
			return c, true
		}
	}
	return schema.CRD{}, false
}
