package blueprint

import (
	"strings"
	"testing"
)

func TestRenameResourceRewritesStatusWires(t *testing.T) {
	b := wiredBlueprint()
	if err := b.RenameResource("main-queue", "primary-queue"); err != nil {
		t.Fatalf("RenameResource: %v", err)
	}
	if b.Spec.Resources[0].Name != "primary-queue" {
		t.Errorf("resource name = %q, want primary-queue", b.Spec.Resources[0].Name)
	}
	got := b.Spec.Resources[1].Fields["queueUrl"].From
	if got != "resources.primary-queue.status.atProvider.url" {
		t.Errorf("wire From = %q, want it rewritten to the new resource name", got)
	}
}

// The rewrite is prefix-exact: renaming "main" must not touch a wire into
// "main-queue" whose name merely shares a prefix.
func TestRenameResourceDoesNotRewritePrefixSharingNames(t *testing.T) {
	b := wiredBlueprint(func(b *Blueprint) {
		b.Spec.Resources = append(b.Spec.Resources, Resource{
			Name: "main", Kind: "Queue", Fields: map[string]Field{},
		})
	})
	if err := b.RenameResource("main", "primary"); err != nil {
		t.Fatalf("RenameResource: %v", err)
	}
	got := b.Spec.Resources[1].Fields["queueUrl"].From
	if got != "resources.main-queue.status.atProvider.url" {
		t.Errorf("wire From = %q, want the main-queue wire left untouched", got)
	}
}

// Params references are a different namespace: renaming a resource must
// never rewrite a params.<name> reference, even one spelled identically.
func TestRenameResourceLeavesParamsRefsAlone(t *testing.T) {
	b := wiredBlueprint(func(b *Blueprint) {
		b.Spec.XRD.Parameters["mainQueue"] = Parameter{Type: "string"}
		b.Spec.Resources[1].Fields["policy"] = Field{From: "params.mainQueue"}
	})
	if err := b.RenameResource("main-queue", "primary-queue"); err != nil {
		t.Fatalf("RenameResource: %v", err)
	}
	if got := b.Spec.Resources[1].Fields["policy"].From; got != "params.mainQueue" {
		t.Errorf("params ref = %q, want it untouched by a resource rename", got)
	}
}

func TestRenameResourceUnknownFromErrors(t *testing.T) {
	b := wiredBlueprint()
	if err := b.RenameResource("nope", "other"); err == nil ||
		!strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v, want an error naming the unknown resource", err)
	}
}

// from == to is a no-op success, matching RenameParameter: a blur-submit UI
// resubmits unchanged names routinely.
func TestRenameResourceSameNameIsANoOp(t *testing.T) {
	b := wiredBlueprint()
	if err := b.RenameResource("main-queue", "main-queue"); err != nil {
		t.Fatalf("RenameResource(same, same) = %v, want nil", err)
	}
	if err := b.RenameResource("absent", "absent"); err == nil {
		t.Fatal("renaming an unknown resource to itself must still error: there is nothing to rename")
	}
}

func TestRenameResourceCollisionErrors(t *testing.T) {
	b := wiredBlueprint()
	err := b.RenameResource("main-queue", "queue-policy")
	if err == nil || !strings.Contains(err.Error(), "queue-policy") {
		t.Fatalf("err = %v, want a collision error naming the existing resource", err)
	}
	// The receiver is untouched on failure.
	if b.Spec.Resources[0].Name != "main-queue" {
		t.Errorf("resource name = %q after a failed rename, want main-queue", b.Spec.Resources[0].Name)
	}
}

func TestRenameResourceToInvalidNameLeavesReceiverUntouched(t *testing.T) {
	b := wiredBlueprint()
	if err := b.RenameResource("main-queue", "Not_A_DNS_Label"); err == nil {
		t.Fatal("RenameResource to a non-DNS-label name must fail Validate")
	}
	if b.Spec.Resources[0].Name != "main-queue" {
		t.Errorf("resource name = %q after a failed rename, want main-queue", b.Spec.Resources[0].Name)
	}
	if got := b.Spec.Resources[1].Fields["queueUrl"].From; got != "resources.main-queue.status.atProvider.url" {
		t.Errorf("wire From = %q after a failed rename, want it untouched", got)
	}
}

func TestDeleteResourceRefusesWhileStatusWiresReferenceIt(t *testing.T) {
	b := wiredBlueprint()
	err := b.DeleteResource("main-queue")
	if err == nil {
		t.Fatal("DeleteResource = nil, want a refusal while queue-policy still wires from its status")
	}
	if !strings.Contains(err.Error(), "queue-policy") {
		t.Errorf("err = %v, want it to name every referencing resource", err)
	}
	if len(b.Spec.Resources) != 2 {
		t.Errorf("resources = %d after a refused delete, want 2", len(b.Spec.Resources))
	}
}

func TestDeleteResourceSucceedsOnceUnreferenced(t *testing.T) {
	b := wiredBlueprint(func(b *Blueprint) {
		b.Spec.Resources[1].Fields["queueUrl"] = Field{Value: "https://example.test/q"}
	})
	if err := b.DeleteResource("main-queue"); err != nil {
		t.Fatalf("DeleteResource: %v", err)
	}
	if len(b.Spec.Resources) != 1 || b.Spec.Resources[0].Name != "queue-policy" {
		t.Errorf("resources = %+v, want only queue-policy left", b.Spec.Resources)
	}
}

func TestDeleteResourceUnknownNameErrors(t *testing.T) {
	b := wiredBlueprint()
	if err := b.DeleteResource("nope"); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v, want an error naming the unknown resource", err)
	}
}

func TestRenameResourceRewritesRawReferences(t *testing.T) {
	b := wiredBlueprint(func(b *Blueprint) {
		b.Spec.Resources[1].Fields["rawField"] = Field{
			Raw: `{{ index $.observed.resources "main-queue" "resource" "status" "atProvider" "url" }}`,
		}
		if b.Spec.Templates == nil {
			b.Spec.Templates = make(map[string]string)
		}
		b.Spec.Templates["helper"] = `{{ index .observed.resources "main-queue" "status" "arn" }}`
	})

	if err := b.RenameResource("main-queue", "primary-queue"); err != nil {
		t.Fatalf("RenameResource: %v", err)
	}

	rawGot := b.Spec.Resources[1].Fields["rawField"].Raw
	wantRaw := `{{ index $.observed.resources "primary-queue" "resource" "status" "atProvider" "url" }}`
	if rawGot != wantRaw {
		t.Errorf("raw field = %q, want %q", rawGot, wantRaw)
	}

	tmplGot := b.Spec.Templates["helper"]
	wantTmpl := `{{ index .observed.resources "primary-queue" "status" "arn" }}`
	if tmplGot != wantTmpl {
		t.Errorf("template = %q, want %q", tmplGot, wantTmpl)
	}
}

func TestDeleteResourceRefusesWhileRawReferencesExist(t *testing.T) {
	b := wiredBlueprint(func(b *Blueprint) {
		b.Spec.Resources[1].Fields["queueUrl"] = Field{Value: "static"}
		b.Spec.Resources[1].Fields["rawField"] = Field{
			Raw: `{{ index $.observed.resources "main-queue" }}`,
		}
	})

	err := b.DeleteResource("main-queue")
	if err == nil {
		t.Fatal("expected DeleteResource to fail with raw reference")
	}
	if !strings.Contains(err.Error(), "queue-policy") {
		t.Errorf("error %q should mention queue-policy", err.Error())
	}
}

func TestDeleteResourceAllowsSelfRawReference(t *testing.T) {
	b := wiredBlueprint(func(b *Blueprint) {
		b.Spec.Resources[1].Fields["queueUrl"] = Field{Value: "static"}
		b.Spec.Resources[0].Fields["selfTag"] = Field{
			Raw: `"main-queue"`,
		}
	})

	refs := b.StatusReferencingResources("main-queue")
	for _, r := range refs {
		if r == "main-queue" {
			t.Errorf("StatusReferencingResources returned target resource itself: %v", refs)
		}
	}

	if err := b.DeleteResource("main-queue"); err != nil {
		t.Fatalf("DeleteResource failed: %v", err)
	}
}

func TestRenameResourceDoesNotRewritePrefixSharingRawReferences(t *testing.T) {
	b := wiredBlueprint(func(b *Blueprint) {
		b.Spec.Resources = append(b.Spec.Resources, Resource{
			Name: "main", Kind: "Queue", Fields: map[string]Field{},
		})
		b.Spec.Resources[1].Fields["rawField"] = Field{
			Raw: `{{ .observed.resources.main-queue.resource.status.url }}`,
		}
	})

	if err := b.RenameResource("main", "primary"); err != nil {
		t.Fatalf("RenameResource: %v", err)
	}

	got := b.Spec.Resources[1].Fields["rawField"].Raw
	want := `{{ .observed.resources.main-queue.resource.status.url }}`
	if got != want {
		t.Errorf("raw field = %q, want %q", got, want)
	}
}

func TestDeleteResourceDoesNotRefuseOnPrefixSharingRawReference(t *testing.T) {
	b := wiredBlueprint(func(b *Blueprint) {
		b.Spec.Resources = append(b.Spec.Resources, Resource{
			Name: "main", Kind: "Queue", Fields: map[string]Field{},
		})
		b.Spec.Resources[1].Fields["queueUrl"] = Field{Value: "static"}
		b.Spec.Resources[1].Fields["rawField"] = Field{
			Raw: `{{ .observed.resources.main-queue.resource.status.url }}`,
		}
	})

	if err := b.DeleteResource("main"); err != nil {
		t.Fatalf("DeleteResource failed: %v", err)
	}
}

func TestRawReferencesResourceBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		resName  string
		expected bool
	}{
		{"bare double quoted exact", `"main"`, "main", false},
		{"bare double quoted prefix-sharing", `"main-queue"`, "main", false},
		{"bare single quoted exact", `'main'`, "main", false},
		{"bare single quoted prefix-sharing", `'main-queue'`, "main", false},
		{"bare backtick exact", "`main`", "main", false},
		{"bare backtick prefix-sharing", "`main-queue`", "main", false},
		{"unanchored quoted in condition", `{{ if eq $spec.tier "main" }}`, "main", false},
		{"index dot observed double quoted exact", `{{ index .observed.resources "main" }}`, "main", true},
		{"index dot observed double quoted prefix-sharing", `{{ index .observed.resources "main-queue" }}`, "main", false},
		{"index dollar dot observed double quoted exact", `{{ index $.observed.resources "main" }}`, "main", true},
		{"index dollar dot observed double quoted prefix-sharing", `{{ index $.observed.resources "main-queue" }}`, "main", false},
		{"index dollar observed double quoted exact", `{{ index $observed.resources "main" }}`, "main", true},
		{"index dollar observed double quoted prefix-sharing", `{{ index $observed.resources "main-queue" }}`, "main", false},
		{"index observed double quoted exact", `{{ index observed.resources "main" }}`, "main", true},
		{"index resources double quoted exact", `{{ index resources "main" }}`, "main", true},
		{"index resources double quoted prefix-sharing", `{{ index resources "main-queue" }}`, "main", false},
		{"index single quoted exact", `{{ index .observed.resources 'main' }}`, "main", true},
		{"index single quoted prefix-sharing", `{{ index .observed.resources 'main-queue' }}`, "main", false},
		{"index backtick exact", "{{ index .observed.resources `main` }}", "main", true},
		{"index backtick prefix-sharing", "{{ index .observed.resources `main-queue` }}", "main", false},
		{"dot observed dot status", ".observed.resources.main.resource.status", "main", true},
		{"dot observed prefix-sharing", ".observed.resources.main-queue.resource.status", "main", false},
		{"dollar dot observed dot status", "$.observed.resources.main.resource.status", "main", true},
		{"dollar dot observed prefix-sharing", "$.observed.resources.main-queue.resource.status", "main", false},
		{"dollar observed dot status", "$observed.resources.main.resource.status", "main", true},
		{"dollar observed prefix-sharing", "$observed.resources.main-queue.resource.status", "main", false},
		{"resources dot", "resources.main.status", "main", true},
		{"resources dot prefix-sharing", "resources.main-queue.status", "main", false},
		{"resources space", "resources.main == true", "main", true},
		{"resources brace", "{{ resources.main }}", "main", true},
		{"resources paren", "(resources.main)", "main", true},
		{"resources end of string", "resources.main", "main", true},
		{"resources prefix-sharing identifier chars", "resources.main2", "main", false},
		{"resources prefix-sharing underscore", "resources.main_service", "main", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rawReferencesResource(tt.raw, tt.resName)
			if got != tt.expected {
				t.Errorf("rawReferencesResource(%q, %q) = %v, want %v", tt.raw, tt.resName, got, tt.expected)
			}
		})
	}
}

func TestRewriteRawResourceBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		from     string
		to       string
		expected string
	}{
		{
			name:     "quoted exact",
			raw:      `{{ index $.observed.resources "main" }}`,
			from:     "main",
			to:       "primary",
			expected: `{{ index $.observed.resources "primary" }}`,
		},
		{
			name:     "quoted prefix-sharing left alone",
			raw:      `{{ index $.observed.resources "main-queue" }}`,
			from:     "main",
			to:       "primary",
			expected: `{{ index $.observed.resources "main-queue" }}`,
		},
		{
			name:     "single quoted index exact",
			raw:      `{{ index $.observed.resources 'main' }}`,
			from:     "main",
			to:       "primary",
			expected: `{{ index $.observed.resources 'primary' }}`,
		},
		{
			name:     "backtick index exact",
			raw:      "{{ index $.observed.resources `main` }}",
			from:     "main",
			to:       "primary",
			expected: "{{ index $.observed.resources `primary` }}",
		},
		{
			name:     "index resources exact",
			raw:      `{{ index resources "main" }}`,
			from:     "main",
			to:       "primary",
			expected: `{{ index resources "primary" }}`,
		},
		{
			name:     "unanchored quoted string left alone",
			raw:      `{{ if eq $spec.tier "main" }}`,
			from:     "main",
			to:       "primary",
			expected: `{{ if eq $spec.tier "main" }}`,
		},
		{
			name:     "dot observed rewrites exact",
			raw:      `{{ .observed.resources.main.resource.status.url }}`,
			from:     "main",
			to:       "primary",
			expected: `{{ .observed.resources.primary.resource.status.url }}`,
		},
		{
			name:     "dot observed leaves prefix-sharing alone",
			raw:      `{{ .observed.resources.main-queue.resource.status.url }}`,
			from:     "main",
			to:       "primary",
			expected: `{{ .observed.resources.main-queue.resource.status.url }}`,
		},
		{
			name:     "dollar dot observed rewrites exact",
			raw:      `{{ $.observed.resources.main.resource.status.url }}`,
			from:     "main",
			to:       "primary",
			expected: `{{ $.observed.resources.primary.resource.status.url }}`,
		},
		{
			name:     "dollar dot observed leaves prefix-sharing alone",
			raw:      `{{ $.observed.resources.main-queue.resource.status.url }}`,
			from:     "main",
			to:       "primary",
			expected: `{{ $.observed.resources.main-queue.resource.status.url }}`,
		},
		{
			name:     "dollar observed rewrites exact",
			raw:      `$observed.resources.main.resource.status.url`,
			from:     "main",
			to:       "primary",
			expected: `$observed.resources.primary.resource.status.url`,
		},
		{
			name:     "dollar observed leaves prefix-sharing alone",
			raw:      `$observed.resources.main-queue.resource.status.url`,
			from:     "main",
			to:       "primary",
			expected: `$observed.resources.main-queue.resource.status.url`,
		},
		{
			name:     "resources dot rewrites exact",
			raw:      `resources.main.status.url`,
			from:     "main",
			to:       "primary",
			expected: `resources.primary.status.url`,
		},
		{
			name:     "resources dot leaves prefix-sharing alone",
			raw:      `resources.main-queue.status.url`,
			from:     "main",
			to:       "primary",
			expected: `resources.main-queue.status.url`,
		},
		{
			name:     "resources with space and brace",
			raw:      `{{ resources.main }}`,
			from:     "main",
			to:       "primary",
			expected: `{{ resources.primary }}`,
		},
		{
			name:     "multiple occurrences in same string",
			raw:      `{{ .observed.resources.main.url }} and {{ .observed.resources.main-queue.url }}`,
			from:     "main",
			to:       "primary",
			expected: `{{ .observed.resources.primary.url }} and {{ .observed.resources.main-queue.url }}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rewriteRawResource(tt.raw, tt.from, tt.to)
			if got != tt.expected {
				t.Errorf("rewriteRawResource(%q, %q, %q) = %q, want %q", tt.raw, tt.from, tt.to, got, tt.expected)
			}
		})
	}
}

func TestDeleteResource_BlockedByUnrelatedQuotedStringInRaw(t *testing.T) {
	b := wiredBlueprint(func(b *Blueprint) {
		b.Spec.Resources = []Resource{
			{
				Name: "db",
				Kind: "Instance",
			},
			{
				Name: "app",
				Kind: "Deployment",
				Fields: map[string]Field{
					"tier": {
						Raw: `{{ if eq $spec.tier "db" }}primary{{ end }}`,
					},
				},
			},
		}
	})

	err := b.DeleteResource("db")
	if err != nil {
		t.Fatalf("DeleteResource(\"db\") failed: %v", err)
	}
}

func TestRenameResource_CorruptsUnrelatedQuotedStringInRaw(t *testing.T) {
	b := wiredBlueprint(func(b *Blueprint) {
		b.Spec.Resources = []Resource{
			{
				Name: "db",
				Kind: "Instance",
			},
			{
				Name: "app",
				Kind: "Deployment",
				Fields: map[string]Field{
					"tier": {
						Raw: `{{ if eq $spec.tier "db" }}primary{{ end }}`,
					},
				},
			},
		}
	})

	err := b.RenameResource("db", "database")
	if err != nil {
		t.Fatalf("RenameResource failed: %v", err)
	}

	gotRaw := b.Spec.Resources[1].Fields["tier"].Raw
	wantRaw := `{{ if eq $spec.tier "db" }}primary{{ end }}`
	if gotRaw != wantRaw {
		t.Errorf("app raw field corrupted by renaming db: got %q, want %q", gotRaw, wantRaw)
	}
}
