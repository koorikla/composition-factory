package examples

import (
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
	for _, id := range []string{"irsa", "rds-postgres", "k8s-app", "k8s-workload", "k8s-cronjob", "s3-bucket", "sqs-queue"} {
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
