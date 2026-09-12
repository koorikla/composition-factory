package adopt

import (
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/emit"
	"github.com/koorikla/compositionfactory/internal/schema/k8s"
)

func TestCF414_BracketQuotedMapPatches(t *testing.T) {
	nativeCRDs, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds failed: %v", err)
	}
	allCRDs := append(testCRDs(t), nativeCRDs...)

	t.Run("NativeConfigMapLabels_SingleAndDoubleQuotes", func(t *testing.T) {
		manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-cm-labels
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
  resources:
    - name: sa
      base:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          labels:
            tier: backend
            app: original
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.appName
          toFieldPath: metadata.labels['app']
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.tierName
          toFieldPath: metadata.labels["tier"]
`
		bp, _, err := Adopt([]byte(manifest), Options{})
		if err != nil {
			t.Fatalf("Adopt failed: %v", err)
		}
		res := bp.ResourceNamed("sa")
		if res == nil {
			t.Fatalf("resource sa not found")
		}

		if _, ok := res.Fields["metadata.labels['app']"]; ok {
			t.Errorf("res.Fields contains unnormalized metadata.labels['app']")
		}
		if _, ok := res.Fields[`metadata.labels["tier"]`]; ok {
			t.Errorf(`res.Fields contains unnormalized metadata.labels["tier"]`)
		}

		fldApp, ok := res.Fields["metadata.labels[app]"]
		if !ok {
			t.Fatalf("missing metadata.labels[app] in res.Fields")
		}
		if fldApp.From != "params.appName" {
			t.Errorf("expected metadata.labels[app] From == params.appName, got %+v", fldApp)
		}

		fldTier, ok := res.Fields["metadata.labels[tier]"]
		if !ok {
			t.Fatalf("missing metadata.labels[tier] in res.Fields")
		}
		if fldTier.From != "params.tierName" {
			t.Errorf("expected metadata.labels[tier] From == params.tierName, got %+v", fldTier)
		}

		compYAML, err := emit.Composition(bp, allCRDs)
		if err != nil {
			t.Fatalf("emit.Composition failed: %v", err)
		}
		compStr := string(compYAML)
		if strings.Contains(compStr, "app: 'original'") {
			t.Errorf("emitted composition still contains original static value for app")
		}
		if strings.Contains(compStr, "tier: 'backend'") {
			t.Errorf("emitted composition still contains original static value for tier")
		}
		if !strings.Contains(compStr, "appName") || !strings.Contains(compStr, "tierName") {
			t.Errorf("emitted composition missing parameter wires:\n%s", compStr)
		}
	})

	t.Run("NativeDeploymentTemplateLabels_SingleAndDoubleQuotes", func(t *testing.T) {
		manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-deploy-labels
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
  resources:
    - name: app
      base:
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: my-app
        spec:
          template:
            metadata:
              labels:
                app: original
                env: dev
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.appName
          toFieldPath: spec.template.metadata.labels['app']
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.envName
          toFieldPath: spec.template.metadata.labels["env"]
`
		bp, _, err := Adopt([]byte(manifest), Options{})
		if err != nil {
			t.Fatalf("Adopt failed: %v", err)
		}
		res := bp.ResourceNamed("app")
		if res == nil {
			t.Fatalf("resource app not found")
		}

		if _, ok := res.Fields["spec.template.metadata.labels['app']"]; ok {
			t.Errorf("res.Fields contains unnormalized spec.template.metadata.labels['app']")
		}
		if _, ok := res.Fields[`spec.template.metadata.labels["env"]`]; ok {
			t.Errorf(`res.Fields contains unnormalized spec.template.metadata.labels["env"]`)
		}

		fldApp, ok := res.Fields["spec.template.metadata.labels[app]"]
		if !ok {
			t.Fatalf("missing spec.template.metadata.labels[app] in res.Fields")
		}
		if fldApp.From != "params.appName" {
			t.Errorf("expected spec.template.metadata.labels[app] From == params.appName, got %+v", fldApp)
		}

		fldEnv, ok := res.Fields["spec.template.metadata.labels[env]"]
		if !ok {
			t.Fatalf("missing spec.template.metadata.labels[env] in res.Fields")
		}
		if fldEnv.From != "params.envName" {
			t.Errorf("expected spec.template.metadata.labels[env] From == params.envName, got %+v", fldEnv)
		}

		compYAML, err := emit.Composition(bp, allCRDs)
		if err != nil {
			t.Fatalf("emit.Composition failed: %v", err)
		}
		compStr := string(compYAML)
		if strings.Contains(compStr, "app: 'original'") {
			t.Errorf("emitted composition still contains original static value for app")
		}
		if strings.Contains(compStr, "env: 'dev'") {
			t.Errorf("emitted composition still contains original static value for env")
		}
		if !strings.Contains(compStr, "appName") || !strings.Contains(compStr, "envName") {
			t.Errorf("emitted composition missing parameter wires:\n%s", compStr)
		}
	})

	t.Run("ManagedResourceTags_BracketQuoted", func(t *testing.T) {
		manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-managed-tags
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
  resources:
    - name: test-queue
      base:
        apiVersion: sqs.aws.upbound.io/v1beta1
        kind: Queue
        spec:
          forProvider:
            region: us-east-1
            tags:
              Environment: original
              Project: legacy
              Team: platform
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.env
          toFieldPath: spec.forProvider.tags['Environment']
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.project
          toFieldPath: spec.forProvider.tags["Project"]
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.team
          toFieldPath: tags['Team']
`
		bp, _, err := Adopt([]byte(manifest), Options{
			DefaultProviderRef: "xpkg.upbound.io/upbound/provider-aws-sqs:v2",
		})
		if err != nil {
			t.Fatalf("Adopt failed: %v", err)
		}
		res := bp.ResourceNamed("test-queue")
		if res == nil {
			t.Fatalf("resource test-queue not found")
		}

		if _, ok := res.Fields["tags['Environment']"]; ok {
			t.Errorf("res.Fields contains unnormalized tags['Environment']")
		}
		if _, ok := res.Fields[`tags["Project"]`]; ok {
			t.Errorf(`res.Fields contains unnormalized tags["Project"]`)
		}
		if _, ok := res.Fields["tags['Team']"]; ok {
			t.Errorf("res.Fields contains unnormalized tags['Team']")
		}

		fldEnv, ok := res.Fields["tags[Environment]"]
		if !ok {
			t.Fatalf("missing tags[Environment] in res.Fields")
		}
		if fldEnv.From != "params.env" {
			t.Errorf("expected tags[Environment] From == params.env, got %+v", fldEnv)
		}

		fldProj, ok := res.Fields["tags[Project]"]
		if !ok {
			t.Fatalf("missing tags[Project] in res.Fields")
		}
		if fldProj.From != "params.project" {
			t.Errorf("expected tags[Project] From == params.project, got %+v", fldProj)
		}

		fldTeam, ok := res.Fields["tags[Team]"]
		if !ok {
			t.Fatalf("missing tags[Team] in res.Fields")
		}
		if fldTeam.From != "params.team" {
			t.Errorf("expected tags[Team] From == params.team, got %+v", fldTeam)
		}

		compYAML, err := emit.Composition(bp, allCRDs)
		if err != nil {
			t.Fatalf("emit.Composition failed: %v", err)
		}
		compStr := string(compYAML)
		if strings.Contains(compStr, "Environment: 'original'") {
			t.Errorf("emitted composition still contains original static value for Environment")
		}
		if strings.Contains(compStr, "Project: 'legacy'") {
			t.Errorf("emitted composition still contains original static value for Project")
		}
		if strings.Contains(compStr, "Team: 'platform'") {
			t.Errorf("emitted composition still contains original static value for Team")
		}
		if !strings.Contains(compStr, "env") || !strings.Contains(compStr, "project") || !strings.Contains(compStr, "team") {
			t.Errorf("emitted composition missing parameter wires:\n%s", compStr)
		}
	})

	t.Run("NativeConfigMapData_BracketQuoted", func(t *testing.T) {
		manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-cm-data
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
  resources:
    - name: cfg
      base:
        apiVersion: v1
        kind: ConfigMap
        data:
          app: original
          tier: backend
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.appName
          toFieldPath: data['app']
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.tierName
          toFieldPath: data["tier"]
`
		bp, _, err := Adopt([]byte(manifest), Options{})
		if err != nil {
			t.Fatalf("Adopt failed: %v", err)
		}
		res := bp.ResourceNamed("cfg")
		if res == nil {
			t.Fatalf("resource cfg not found")
		}

		if _, ok := res.Fields["data['app']"]; ok {
			t.Errorf("res.Fields contains unnormalized data['app']")
		}
		if _, ok := res.Fields[`data["tier"]`]; ok {
			t.Errorf(`res.Fields contains unnormalized data["tier"]`)
		}

		fldApp, ok := res.Fields["data[app]"]
		if !ok {
			t.Fatalf("missing data[app] in res.Fields")
		}
		if fldApp.From != "params.appName" {
			t.Errorf("expected data[app] From == params.appName, got %+v", fldApp)
		}

		fldTier, ok := res.Fields["data[tier]"]
		if !ok {
			t.Fatalf("missing data[tier] in res.Fields")
		}
		if fldTier.From != "params.tierName" {
			t.Errorf("expected data[tier] From == params.tierName, got %+v", fldTier)
		}

		compYAML, err := emit.Composition(bp, allCRDs)
		if err != nil {
			t.Fatalf("emit.Composition failed: %v", err)
		}
		compStr := string(compYAML)
		if strings.Contains(compStr, "app: 'original'") {
			t.Errorf("emitted composition still contains original static value for app data")
		}
		if strings.Contains(compStr, "tier: 'backend'") {
			t.Errorf("emitted composition still contains original static value for tier data")
		}
		if !strings.Contains(compStr, "appName") || !strings.Contains(compStr, "tierName") {
			t.Errorf("emitted composition missing parameter wires:\n%s", compStr)
		}
	})

	t.Run("EmitDeduplication_CoexistingBracketQuotedAndUnquoted", func(t *testing.T) {
		bp := &blueprint.Blueprint{
			APIVersion: "factory.crossplane.io/v1alpha1",
			Kind:       "Blueprint",
			Metadata: blueprint.Metadata{
				Name: "test-dedup",
			},
			Spec: blueprint.Spec{
				XRD: blueprint.XRD{
					Group:   "example.org",
					Version: "v1alpha1",
					Kind:    "XApp",
					Plural:  "xapps",
					Scope:   "Namespaced",
					Parameters: map[string]blueprint.Parameter{
						"providerName": {Type: "string", Required: true},
						"env":          {Type: "string"},
					},
				},
				Sources: []blueprint.Source{
					{Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v2"},
				},
				Resources: []blueprint.Resource{
					{
						Name:     "test-queue",
						Kind:     "Queue",
						Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v2",
						Fields: map[string]blueprint.Field{
							"region":              {Value: "us-east-1"},
							"tags['Environment']": {From: "params.env"},
							"tags[Environment]":   {Value: "original"},
						},
					},
				},
			},
		}

		compYAML, err := emit.Composition(bp, allCRDs)
		if err != nil {
			t.Fatalf("emit.Composition failed: %v", err)
		}
		compStr := string(compYAML)
		if strings.Contains(compStr, "Environment: 'original'") {
			t.Errorf("emitted composition still contains static value 'original' instead of wire: %s", compStr)
		}
		if !strings.Contains(compStr, "Environment: {{ $spec.env | quote }}") {
			t.Errorf("emitted composition missing wire for Environment: %s", compStr)
		}
	})
}
