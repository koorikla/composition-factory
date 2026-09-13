package examples

import (
	"strings"
	"testing"

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
