package emit

import (
	"bytes"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

func configFixture() *blueprint.Blueprint {
	return &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xqueue"},
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{Provider: "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0"},
			},
			XRD: blueprint.XRD{
				Group: "platform.example.org", Kind: "XQueue", Plural: "xqueues",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
				},
			},
			Resources: []blueprint.Resource{{
				Name: "q", Kind: "Queue",
				Provider: "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0",
				Fields:   map[string]blueprint.Field{"fifoQueue": {Value: "true"}},
			}},
		},
	}
}

func TestConfigurationMetaShape(t *testing.T) {
	src := []byte("apiVersion: factory.crossplane.io/v1alpha1\nkind: Blueprint\n")
	got, err := ConfigurationMeta(configFixture(), src)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)

	for _, want := range []string{
		"apiVersion: meta.pkg.crossplane.io/v1",
		"kind: Configuration",
		"name: xqueue",
		"version: '>=v2.0.0'",
		"kind: Provider",
		"package: ghcr.io/crossplane-contrib/provider-aws-sqs",
		"version: '=v2.7.0'",
		"kind: Function",
		"package: xpkg.upbound.io/crossplane-contrib/function-go-templating",
		"version: '=v0.12.0'",
		"package: xpkg.upbound.io/crossplane-contrib/function-auto-ready",
		"version: '=v0.5.0'",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("crossplane.yaml missing %q:\n%s", want, s)
		}
	}

	// the blueprint source is embedded verbatim, line-indented under a block scalar
	if !strings.Contains(s, "factory.crossplane.io/blueprint: |") {
		t.Errorf("missing embedded-source annotation:\n%s", s)
	}
	if !strings.Contains(s, "kind: Blueprint") {
		t.Errorf("embedded source lines missing:\n%s", s)
	}
}

func TestConfigurationMetaDeterministic(t *testing.T) {
	src := []byte("kind: Blueprint\n")
	a, err := ConfigurationMeta(configFixture(), src)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ConfigurationMeta(configFixture(), src)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("two runs differ")
	}
}

func TestConfigurationMetaDigestPinnedSource(t *testing.T) {
	bp := configFixture()
	bp.Spec.Sources = []blueprint.Source{
		{Provider: "ghcr.io/crossplane-contrib/provider-aws-sqs@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
	}
	bp.Spec.Resources[0].Provider = bp.Spec.Sources[0].Provider
	got, err := ConfigurationMeta(bp, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	// a digest pin stays on the package ref verbatim; no semver constraint is invented
	if !strings.Contains(s, "package: ghcr.io/crossplane-contrib/provider-aws-sqs@sha256:0123456789abcdef") {
		t.Errorf("digest ref not kept verbatim:\n%s", s)
	}
	if strings.Contains(s, "version: '=sha256") {
		t.Errorf("digest must not become a version constraint:\n%s", s)
	}
}

func TestConfigurationMetaPipelineFunctions(t *testing.T) {
	bp := configFixture()
	bp.Spec.Pipeline = []blueprint.PipelineStep{{
		Name:        "envcfg",
		FunctionRef: "function-environment-configs",
		Package:     "xpkg.upbound.io/crossplane-contrib/function-environment-configs:v0.4.0",
		Position:    "before-templating",
	}}
	got, err := ConfigurationMeta(bp, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "package: xpkg.upbound.io/crossplane-contrib/function-environment-configs") {
		t.Errorf("pipeline function missing from dependsOn:\n%s", got)
	}
}
