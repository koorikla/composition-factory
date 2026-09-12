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
		{
			name:     "dig resources double quoted exact",
			raw:      `hasKey (dig "resources" "main" "resource" "status" dict $.observed) "url"`,
			from:     "main",
			to:       "primary",
			expected: `hasKey (dig "resources" "primary" "resource" "status" dict $.observed) "url"`,
		},
		{
			name:     "dig resources double quoted prefix-sharing left alone",
			raw:      `hasKey (dig "resources" "main-queue" "resource" "status" dict $.observed) "url"`,
			from:     "main",
			to:       "primary",
			expected: `hasKey (dig "resources" "main-queue" "resource" "status" dict $.observed) "url"`,
		},
		{
			name:     "dig resources single quoted exact",
			raw:      `hasKey (dig 'resources' 'main' 'resource' 'status' dict $.observed) 'url'`,
			from:     "main",
			to:       "primary",
			expected: `hasKey (dig 'resources' 'primary' 'resource' 'status' dict $.observed) 'url'`,
		},
		{
			name:     "dig resources backtick exact",
			raw:      "hasKey (dig `resources` `main` `resource` `status` dict $.observed) `url`",
			from:     "main",
			to:       "primary",
			expected: "hasKey (dig `resources` `primary` `resource` `status` dict $.observed) `url`",
		},
		{
			name:     "dig resources mixed quotes",
			raw:      `dig "resources" 'main' "status"`,
			from:     "main",
			to:       "primary",
			expected: `dig "resources" 'primary' "status"`,
		},
		{
			name:     "dig non-resources left alone",
			raw:      `dig "params" "main" "status"`,
			from:     "main",
			to:       "primary",
			expected: `dig "params" "main" "status"`,
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

func TestRawReferencesResource_GetComposedResource(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		resName  string
		expected bool
	}{
		{"getComposedResource dot double quotes", `{{ (getComposedResource . "main").status.id }}`, "main", true},
		{"getComposedResource dollar double quotes", `{{ (getComposedResource $ "main").status.id }}`, "main", true},
		{"getComposedResource dot single quotes", `{{ (getComposedResource . 'main').status.id }}`, "main", true},
		{"getResourceCondition getComposedResource", `{{ (getResourceCondition "Ready" (getComposedResource . "main")).Status }}`, "main", true},
		{"getComposedResource prefix sharing", `{{ (getComposedResource . "main-queue").status.id }}`, "main", false},
		{"getComposedResource dollar dot double quotes", `{{ (getComposedResource $. "main").status.id }}`, "main", true},
		{"getComposedResource variable context", `{{ (getComposedResource $item "main").status.id }}`, "main", true},
		{"getComposedResource backtick quotes", "{{ (getComposedResource . `main`).status.id }}", "main", true},
		{"getComposedResource backtick prefix sharing", "{{ (getComposedResource . `main-queue`).status.id }}", "main", false},
		{"getComposedResource single quote prefix sharing", `{{ (getComposedResource . 'main-queue').status.id }}`, "main", false},
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

func TestDeleteResource_RefusesWhenGetComposedResourceExists(t *testing.T) {
	b := wiredBlueprint(func(b *Blueprint) {
		b.Spec.Resources = append(b.Spec.Resources, Resource{
			Name: "main", Kind: "Queue", Fields: map[string]Field{},
		})
		b.Spec.Resources[1].Fields["rawField"] = Field{
			Raw: `{{ (getComposedResource . "main").status.id }}`,
		}
	})

	err := b.DeleteResource("main")
	if err == nil {
		t.Fatal("DeleteResource = nil, want error when raw getComposedResource reference exists")
	}
}

func TestDeleteResource_RefusesWhenGetComposedResourceInTemplate(t *testing.T) {
	b := wiredBlueprint(func(b *Blueprint) {
		b.Spec.Resources = append(b.Spec.Resources, Resource{
			Name: "main", Kind: "Queue", Fields: map[string]Field{},
		})
		b.Spec.Templates = map[string]string{
			"helper": `{{ (getComposedResource . "main").status.id }}`,
		}
	})

	err := b.DeleteResource("main")
	if err == nil {
		t.Fatal("DeleteResource = nil, want error when template getComposedResource reference exists")
	}
}

func TestRenameResource_RewritesGetComposedResource(t *testing.T) {
	b := wiredBlueprint(func(b *Blueprint) {
		b.Spec.Resources = append(b.Spec.Resources, Resource{
			Name: "main", Kind: "Queue", Fields: map[string]Field{},
		})
		b.Spec.Resources[1].Fields["rawField"] = Field{
			Raw: `{{ (getComposedResource . "main").status.id }}`,
		}
	})

	if err := b.RenameResource("main", "primary"); err != nil {
		t.Fatalf("RenameResource: %v", err)
	}

	got := b.Spec.Resources[1].Fields["rawField"].Raw
	want := `{{ (getComposedResource . "primary").status.id }}`
	if got != want {
		t.Errorf("raw field = %q, want %q", got, want)
	}
}

func TestRenameResource_RewritesGetComposedResource_Variants(t *testing.T) {
	b := wiredBlueprint(func(b *Blueprint) {
		b.Spec.Resources = append(b.Spec.Resources, Resource{
			Name: "main", Kind: "Queue", Fields: map[string]Field{},
		})
		b.Spec.Resources[1].Fields["singleQuoted"] = Field{
			Raw: `{{ (getComposedResource . 'main').status.id }}`,
		}
		b.Spec.Resources[1].Fields["backtickQuoted"] = Field{
			Raw: "{{ (getComposedResource . `main`).status.id }}",
		}
		b.Spec.Resources[1].Fields["prefixSharing"] = Field{
			Raw: `{{ (getComposedResource . "main-queue").status.id }}`,
		}
		b.Spec.Resources[1].Envelope = map[string]Field{
			"envField": {
				Raw: `{{ (getComposedResource $ "main").status.id }}`,
			},
		}
		b.Spec.Resources[1].Annotations = map[string]Field{
			"annField": {
				Raw: `{{ (getComposedResource $. "main").status.id }}`,
			},
		}
		b.Spec.Templates = map[string]string{
			"tmpl": `{{ $item := . }}{{ (getComposedResource $item "main").status.id }}`,
		}
	})

	if err := b.RenameResource("main", "primary"); err != nil {
		t.Fatalf("RenameResource: %v", err)
	}

	gotSingle := b.Spec.Resources[1].Fields["singleQuoted"].Raw
	wantSingle := `{{ (getComposedResource . 'primary').status.id }}`
	if gotSingle != wantSingle {
		t.Errorf("single quoted field = %q, want %q", gotSingle, wantSingle)
	}

	gotBacktick := b.Spec.Resources[1].Fields["backtickQuoted"].Raw
	wantBacktick := "{{ (getComposedResource . `primary`).status.id }}"
	if gotBacktick != wantBacktick {
		t.Errorf("backtick quoted field = %q, want %q", gotBacktick, wantBacktick)
	}

	gotPrefix := b.Spec.Resources[1].Fields["prefixSharing"].Raw
	wantPrefix := `{{ (getComposedResource . "main-queue").status.id }}`
	if gotPrefix != wantPrefix {
		t.Errorf("prefix sharing field = %q, want %q", gotPrefix, wantPrefix)
	}

	gotEnv := b.Spec.Resources[1].Envelope["envField"].Raw
	wantEnv := `{{ (getComposedResource $ "primary").status.id }}`
	if gotEnv != wantEnv {
		t.Errorf("envelope field = %q, want %q", gotEnv, wantEnv)
	}

	gotAnn := b.Spec.Resources[1].Annotations["annField"].Raw
	wantAnn := `{{ (getComposedResource $. "primary").status.id }}`
	if gotAnn != wantAnn {
		t.Errorf("annotation field = %q, want %q", gotAnn, wantAnn)
	}

	gotTmpl := b.Spec.Templates["tmpl"]
	wantTmpl := `{{ $item := . }}{{ (getComposedResource $item "primary").status.id }}`
	if gotTmpl != wantTmpl {
		t.Errorf("template = %q, want %q", gotTmpl, wantTmpl)
	}
}

func TestRawReferencesResource_HasKey(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		resName  string
		expected bool
	}{
		{"hasKey dollar dot observed.resources", `{{- if hasKey $.observed.resources "main-queue" }}ready{{ end }}`, "main-queue", true},
		{"hasKey dot observed.resources", `{{- if hasKey .observed.resources "main-queue" }}ready{{ end }}`, "main-queue", true},
		{"hasKey observed.resources single quote", `{{- if hasKey .observed.resources 'main-queue' }}ready{{ end }}`, "main-queue", true},
		{"hasKey observed.resources backtick", "{{- if hasKey .observed.resources `main-queue` }}ready{{ end }}", "main-queue", true},
		{"hasKey unrelated prefix", `{{- if hasKey $.observed.resources "main-queue-dlq" }}ready{{ end }}`, "main-queue", false},
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

func TestDeleteResource_RefusesWhenHasKeyObservedResources(t *testing.T) {
	b := editable()
	b.Spec.Resources = append(b.Spec.Resources, Resource{
		Name: "worker-queue", Kind: "Queue",
		Fields: map[string]Field{
			"dep": {Raw: `{{- if hasKey $.observed.resources "main-queue" }}ready{{ end }}`},
		},
	})

	err := b.DeleteResource("main-queue")
	if err == nil {
		t.Fatal("DeleteResource = nil, want refusal when raw field references resource via hasKey observed.resources")
	}
}

func TestRenameResource_RewritesHasKeyObservedResources(t *testing.T) {
	b := editable()
	b.Spec.Resources = append(b.Spec.Resources, Resource{
		Name: "worker-queue", Kind: "Queue",
		Fields: map[string]Field{
			"dep": {Raw: `{{- if hasKey $.observed.resources "main-queue" }}ready{{ end }}`},
		},
	})
	b.Spec.Templates = map[string]string{
		"check": `{{- if hasKey .observed.resources "main-queue" }}found{{ end }}`,
	}

	if err := b.RenameResource("main-queue", "primary-queue"); err != nil {
		t.Fatalf("RenameResource: %v", err)
	}

	if got := b.Spec.Resources[1].Fields["dep"].Raw; got != `{{- if hasKey $.observed.resources "primary-queue" }}ready{{ end }}` {
		t.Errorf("raw field = %q, want hasKey primary-queue", got)
	}
	if got := b.Spec.Templates["check"]; got != `{{- if hasKey .observed.resources "primary-queue" }}found{{ end }}` {
		t.Errorf("template = %q, want hasKey primary-queue", got)
	}
}

func TestRawReferencesResource_DigResources(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		resName  string
		expected bool
	}{
		{
			name:     "status guard with dig double quotes",
			raw:      `{{- if hasKey (dig "resources" "main-queue" "resource" "status" "atProvider" dict $.observed) "url" }}ready{{ end }}`,
			resName:  "main-queue",
			expected: true,
		},
		{
			name:     "status guard with dig single quotes",
			raw:      `{{- if hasKey (dig 'resources' 'main-queue' 'resource' 'status' 'atProvider' dict $.observed) 'url' }}ready{{ end }}`,
			resName:  "main-queue",
			expected: true,
		},
		{
			name:     "status guard with dig backticks",
			raw:      "{{- if hasKey (dig `resources` `main-queue` `resource` `status` `atProvider` dict $.observed) `url` }}ready{{ end }}",
			resName:  "main-queue",
			expected: true,
		},
		{
			name:     "dig mixed quotes double and single",
			raw:      `{{ dig "resources" 'main-queue' "status" }}`,
			resName:  "main-queue",
			expected: true,
		},
		{
			name:     "dig mixed quotes single and backtick",
			raw:      "{{ dig 'resources' `main-queue` \"status\" }}",
			resName:  "main-queue",
			expected: true,
		},
		{
			name:     "dig mixed quotes backtick and double",
			raw:      `{{ dig ` + "`resources`" + ` "main-queue" "status" }}`,
			resName:  "main-queue",
			expected: true,
		},
		{
			name:     "dig prefix-sharing double quotes rejected",
			raw:      `{{- if hasKey (dig "resources" "main-queue-dlq" "resource" "status" "atProvider" dict $.observed) "url" }}ready{{ end }}`,
			resName:  "main-queue",
			expected: false,
		},
		{
			name:     "dig prefix-sharing single quotes rejected",
			raw:      `{{ dig 'resources' 'main-queue-dlq' 'status' }}`,
			resName:  "main-queue",
			expected: false,
		},
		{
			name:     "dig prefix-sharing backticks rejected",
			raw:      "{{ dig `resources` `main-queue-dlq` `status` }}",
			resName:  "main-queue",
			expected: false,
		},
		{
			name:     "dig non-resources key rejected",
			raw:      `{{ dig "parameters" "main-queue" "status" }}`,
			resName:  "main-queue",
			expected: false,
		},
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

func TestDeleteResource_RefusesWhenDigResourcesExists(t *testing.T) {
	b := editable()
	b.Spec.Resources = append(b.Spec.Resources, Resource{
		Name: "consumer", Kind: "Queue",
		Fields: map[string]Field{
			"dep": {Raw: `{{- if hasKey (dig "resources" "main-queue" "resource" "status" "atProvider" dict $.observed) "url" }}ready{{ end }}`},
		},
	})

	// 1. DeleteResource succeeds instead of refusing:
	err := b.DeleteResource("main-queue")
	if err == nil {
		t.Fatal("DeleteResource = nil, want refusal naming consumer when raw dig resources reference exists")
	}
	if !strings.Contains(err.Error(), "consumer") {
		t.Errorf("DeleteResource err = %q, want mention of consumer", err.Error())
	}
}

func TestDeleteResource_RefusesWhenDigResourcesInTemplate(t *testing.T) {
	b := editable()
	b.Spec.Templates = map[string]string{
		"tmpl": `{{- if hasKey (dig "resources" "main-queue" "resource" "status" "atProvider" dict $.observed) "url" }}ready{{ end }}`,
	}

	err := b.DeleteResource("main-queue")
	if err == nil {
		t.Fatal("DeleteResource = nil, want refusal when template dig resources reference exists")
	}
	if !strings.Contains(err.Error(), "tmpl") {
		t.Errorf("DeleteResource err = %q, want mention of tmpl", err.Error())
	}
}

func TestRenameResource_RewritesDigResources(t *testing.T) {
	b := editable()
	b.Spec.Resources = append(b.Spec.Resources, Resource{
		Name: "consumer", Kind: "Queue",
		Fields: map[string]Field{
			"dep":           {Raw: `{{- if hasKey (dig "resources" "main-queue" "resource" "status" "atProvider" dict $.observed) "url" }}ready{{ end }}`},
			"single":        {Raw: `{{ dig 'resources' 'main-queue' 'resource' }}`},
			"backtick":      {Raw: "{{ dig `resources` `main-queue` `resource` }}"},
			"mixed":         {Raw: `{{ dig "resources" 'main-queue' "resource" }}`},
			"prefixSharing": {Raw: `{{ dig "resources" "main-queue-dlq" "resource" }}`},
		},
		Envelope: map[string]Field{
			"env": {Raw: `{{ dig "resources" "main-queue" "resource" }}`},
		},
		Annotations: map[string]Field{
			"ann": {Raw: `{{ dig "resources" "main-queue" "resource" }}`},
		},
	})
	b.Spec.Templates = map[string]string{
		"guard": `{{- if hasKey (dig "resources" "main-queue" "resource" "status" "atProvider" dict $.observed) "url" }}ready{{ end }}`,
	}

	// 2. RenameResource fails to rewrite the reference:
	err := b.RenameResource("main-queue", "primary-queue")
	if err != nil {
		t.Fatalf("RenameResource failed: %v", err)
	}

	depGot := b.Spec.Resources[1].Fields["dep"].Raw
	depWant := `{{- if hasKey (dig "resources" "primary-queue" "resource" "status" "atProvider" dict $.observed) "url" }}ready{{ end }}`
	if depGot != depWant {
		t.Errorf("dep field = %q, want %q", depGot, depWant)
	}

	singleGot := b.Spec.Resources[1].Fields["single"].Raw
	singleWant := `{{ dig 'resources' 'primary-queue' 'resource' }}`
	if singleGot != singleWant {
		t.Errorf("single field = %q, want %q", singleGot, singleWant)
	}

	backtickGot := b.Spec.Resources[1].Fields["backtick"].Raw
	backtickWant := "{{ dig `resources` `primary-queue` `resource` }}"
	if backtickGot != backtickWant {
		t.Errorf("backtick field = %q, want %q", backtickGot, backtickWant)
	}

	mixedGot := b.Spec.Resources[1].Fields["mixed"].Raw
	mixedWant := `{{ dig "resources" 'primary-queue' "resource" }}`
	if mixedGot != mixedWant {
		t.Errorf("mixed field = %q, want %q", mixedGot, mixedWant)
	}

	prefixGot := b.Spec.Resources[1].Fields["prefixSharing"].Raw
	prefixWant := `{{ dig "resources" "main-queue-dlq" "resource" }}`
	if prefixGot != prefixWant {
		t.Errorf("prefixSharing field = %q, want %q", prefixGot, prefixWant)
	}

	envGot := b.Spec.Resources[1].Envelope["env"].Raw
	envWant := `{{ dig "resources" "primary-queue" "resource" }}`
	if envGot != envWant {
		t.Errorf("envelope field = %q, want %q", envGot, envWant)
	}

	annGot := b.Spec.Resources[1].Annotations["ann"].Raw
	annWant := `{{ dig "resources" "primary-queue" "resource" }}`
	if annGot != annWant {
		t.Errorf("annotation field = %q, want %q", annGot, annWant)
	}

	tmplGot := b.Spec.Templates["guard"]
	tmplWant := `{{- if hasKey (dig "resources" "primary-queue" "resource" "status" "atProvider" dict $.observed) "url" }}ready{{ end }}`
	if tmplGot != tmplWant {
		t.Errorf("template guard = %q, want %q", tmplGot, tmplWant)
	}
}
