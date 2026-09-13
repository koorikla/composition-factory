package examples

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

func TestAllExamplesAreValidBlueprints(t *testing.T) {
	exs := All()
	if len(exs) < 3 {
		t.Fatalf("expected at least 3 starter examples, got %d", len(exs))
	}

	for _, ex := range exs {
		t.Run(ex.ID, func(t *testing.T) {
			if ex.ID == "" {
				t.Error("example ID is empty")
			}
			if ex.Name == "" {
				t.Error("example Name is empty")
			}
			if ex.Description == "" {
				t.Error("example Description is empty")
			}
			if len(ex.Tags) == 0 {
				t.Error("example Tags is empty")
			}
			if ex.YAML == "" {
				t.Fatal("example YAML is empty")
			}

			b, err := blueprint.Parse([]byte(ex.YAML))
			if err != nil {
				t.Fatalf("failed to parse blueprint YAML: %v", err)
			}
			if err := b.Validate(); err != nil {
				t.Fatalf("blueprint validation failed: %v", err)
			}
			if len(b.Spec.Resources) == 0 {
				t.Errorf("expected resources in blueprint, got 0")
			}
		})
	}
}

func TestGetExample(t *testing.T) {
	for _, id := range []string{"irsa", "rds-postgres", "k8s-app", "k8s-workload", "k8s-cronjob", "s3-bucket", "sqs-queue", "gcp-storage", "azure-postgres", "cloud-database", "external-secrets"} {
		ex, err := Get(id)
		if err != nil {
			t.Errorf("Get(%q) returned error: %v", id, err)
			continue
		}
		if ex.ID != id {
			t.Errorf("Get(%q).ID = %q, want %q", id, ex.ID, id)
		}
		if ex.ResourceCount == 0 {
			t.Errorf("Get(%q).ResourceCount = 0, want > 0", id)
		}
	}

	if _, err := Get("non-existent"); err == nil {
		t.Error("Get(\"non-existent\") expected error, got nil")
	}
}

func TestListExamples(t *testing.T) {
	list := List()
	if len(list) != len(All()) {
		t.Errorf("List() len = %d, want %d", len(list), len(All()))
	}
	for _, ex := range list {
		if ex.ResourceCount == 0 {
			t.Errorf("List() example %q has ResourceCount 0", ex.ID)
		}
	}
}

func TestExampleIconColorDesignTokenHygiene(t *testing.T) {
	for _, ex := range All() {
		if ex.Icon.Color == "#7c3aed" {
			t.Errorf("example %q uses rogue violet #7c3aed breaching palette rules", ex.ID)
		}
	}
	cron, err := Get("k8s-cronjob")
	if err != nil {
		t.Fatalf("failed to get k8s-cronjob: %v", err)
	}
	if cron.Icon.Color != "var(--wire-status)" {
		t.Errorf("k8s-cronjob color = %q, want %q", cron.Icon.Color, "var(--wire-status)")
	}
}

func TestStarterExamplesHaveNoLiteralCredentialDefaults(t *testing.T) {
	for _, ex := range All() {
		b, err := blueprint.Parse([]byte(ex.YAML))
		if err != nil {
			t.Fatalf("%s: parse failed: %v", ex.ID, err)
		}
		for paramName, p := range b.Spec.XRD.Parameters {
			nameLower := strings.ToLower(paramName)
			if strings.Contains(nameLower, "password") ||
				strings.Contains(nameLower, "credential") ||
				nameLower == "secret" ||
				nameLower == "token" {
				if p.Default != "" {
					t.Errorf("starter %q declares parameter %q with literal credential default %q; credentials must not have XRD defaults", ex.ID, paramName, p.Default)
				}
				t.Errorf("starter %q declares credential parameter %q in XR spec; credentials must come from somewhere other than the XR's own spec", ex.ID, paramName)
			}
		}
		for envKey, k := range b.Spec.Environment {
			nameLower := strings.ToLower(envKey)
			if (strings.Contains(nameLower, "password") ||
				strings.Contains(nameLower, "credential") ||
				nameLower == "secret" ||
				nameLower == "token") && k.Default != "" {
				t.Errorf("starter %q declares environment key %q with literal credential default %q; credentials must not have defaults", ex.ID, envKey, k.Default)
			}
		}
	}
}

func TestStarterExamplesHaveNoPlaintextCredentialsInXRD(t *testing.T) {
	for _, ex := range All() {
		b, err := blueprint.Parse([]byte(ex.YAML))
		if err != nil {
			t.Fatalf("%s: parse failed: %v", ex.ID, err)
		}
		for paramName := range b.Spec.XRD.Parameters {
			nameLower := strings.ToLower(paramName)
			if strings.Contains(nameLower, "password") ||
				strings.Contains(nameLower, "credential") ||
				nameLower == "token" {
				t.Errorf("starter %q declares credential parameter %q in XRD parameters; credentials must not be a plaintext property of the XR spec", ex.ID, paramName)
			}
		}
	}
}

func TestPortableDatabaseStarterObtainsPasswordFromEnvironmentNotXRSpec(t *testing.T) {
	ex, err := Get("cloud-database")
	if err != nil {
		t.Fatalf("failed to get cloud-database example: %v", err)
	}
	b, err := blueprint.Parse([]byte(ex.YAML))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if _, ok := b.Spec.XRD.Parameters["password"]; ok {
		t.Errorf("cloud-database starter must not declare password in XRD parameters (XR spec)")
	}
	envKey, ok := b.Spec.Environment["password"]
	if !ok {
		t.Fatalf("cloud-database starter must declare password in spec.environment")
	}
	if envKey.Default != "" {
		t.Errorf("cloud-database starter must not declare a literal default for environment password, got %q", envKey.Default)
	}
	secretRes := b.ResourceNamed("db-secret")
	if secretRes == nil {
		t.Fatalf("cloud-database missing db-secret resource")
	}
	passField, ok := secretRes.Fields["stringData[POSTGRES_PASSWORD]"]
	if !ok {
		t.Fatalf("db-secret missing stringData[POSTGRES_PASSWORD] field")
	}
	if passField.From != "env.password" {
		t.Errorf("db-secret stringData[POSTGRES_PASSWORD] from = %q, want %q", passField.From, "env.password")
	}
}

func TestRDSStarterConfiguresUsernameAndPasswordSecretRef(t *testing.T) {
	ex, err := Get("rds-postgres")
	if err != nil {
		t.Fatalf("failed to get rds-postgres example: %v", err)
	}
	b, err := blueprint.Parse([]byte(ex.YAML))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	res := b.ResourceNamed("db-instance")
	if res == nil {
		t.Fatalf("rds-postgres missing db-instance resource")
	}

	usernameField, ok := res.Fields["username"]
	if !ok {
		t.Errorf("db-instance missing username field")
	} else {
		if usernameField.From != "" {
			if usernameField.From != "params.username" {
				t.Errorf("db-instance username from = %q, want %q", usernameField.From, "params.username")
			}
			param, ok := b.Spec.XRD.Parameters["username"]
			if !ok {
				t.Errorf("rds-postgres missing username XRD parameter")
			} else if param.Default != "postgres" {
				t.Errorf("rds-postgres username parameter default = %q, want %q", param.Default, "postgres")
			}
		} else if usernameField.Value != "postgres" {
			t.Errorf("db-instance username value = %v, want %q", usernameField.Value, "postgres")
		}
	}

	nameField, ok := res.Fields["passwordSecretRef.name"]
	if !ok {
		t.Errorf("db-instance missing passwordSecretRef.name field")
	} else if nameField.Value == "" && nameField.From == "" {
		t.Errorf("db-instance passwordSecretRef.name must be non-empty")
	}

	keyField, ok := res.Fields["passwordSecretRef.key"]
	if !ok {
		t.Errorf("db-instance missing passwordSecretRef.key field")
	} else if keyField.Value == "" && keyField.From == "" {
		t.Errorf("db-instance passwordSecretRef.key must be non-empty")
	}
}

func TestConfigMapRDSBlueprintHasUsernameAndPasswordSecretRef(t *testing.T) {
	cmPath := filepath.Join("..", "..", "deploy", "k8s", "configmap.yaml")
	data, err := os.ReadFile(cmPath)
	if err != nil {
		t.Fatalf("failed to read deploy/k8s/configmap.yaml: %v", err)
	}

	var cm struct {
		Data map[string]string `yaml:"data"`
	}
	if err := yaml.Unmarshal(data, &cm); err != nil {
		t.Fatalf("failed to unmarshal configmap: %v", err)
	}

	rdsYAML, ok := cm.Data["rds.cf.yaml"]
	if !ok {
		t.Fatalf("deploy/k8s/configmap.yaml missing rds.cf.yaml entry")
	}

	b, err := blueprint.Parse([]byte(rdsYAML))
	if err != nil {
		t.Fatalf("failed to parse rds.cf.yaml in configmap: %v", err)
	}
	res := b.ResourceNamed("db-instance")
	if res == nil {
		t.Fatalf("configmap rds.cf.yaml missing db-instance resource")
	}

	usernameField, ok := res.Fields["username"]
	if !ok {
		t.Errorf("configmap db-instance missing username field")
	} else {
		if usernameField.From != "" {
			if usernameField.From != "params.username" {
				t.Errorf("configmap db-instance username from = %q, want %q", usernameField.From, "params.username")
			}
			param, ok := b.Spec.XRD.Parameters["username"]
			if !ok {
				t.Errorf("configmap rds.cf.yaml missing username XRD parameter")
			} else if param.Default != "postgres" {
				t.Errorf("configmap rds.cf.yaml username parameter default = %q, want %q", param.Default, "postgres")
			}
		} else if usernameField.Value != "postgres" {
			t.Errorf("configmap db-instance username value = %v, want %q", usernameField.Value, "postgres")
		}
	}

	nameField, ok := res.Fields["passwordSecretRef.name"]
	if !ok {
		t.Errorf("configmap db-instance missing passwordSecretRef.name field")
	} else if nameField.Value == "" && nameField.From == "" {
		t.Errorf("configmap db-instance passwordSecretRef.name must be non-empty")
	}

	keyField, ok := res.Fields["passwordSecretRef.key"]
	if !ok {
		t.Errorf("configmap db-instance missing passwordSecretRef.key field")
	} else if keyField.Value == "" && keyField.From == "" {
		t.Errorf("configmap db-instance passwordSecretRef.key must be non-empty")
	}
}
