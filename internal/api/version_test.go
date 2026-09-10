package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/index"
	"github.com/koorikla/compositionfactory/internal/schema"
)

func TestVersionResponseIncludesCleanWorkspaceRelativeBlueprint(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name          string
		blueprintPath string
		wantBlueprint string
	}{
		{
			name:          "relative path with leading dot-slash",
			blueprintPath: "./test-blueprint.cf.yaml",
			wantBlueprint: "test-blueprint.cf.yaml",
		},
		{
			name:          "nested relative path",
			blueprintPath: "sub/nested/doc.cf.yaml",
			wantBlueprint: "sub/nested/doc.cf.yaml",
		},
		{
			name:          "absolute path inside cwd",
			blueprintPath: filepath.Join(cwd, "my-dir", "doc.cf.yaml"),
			wantBlueprint: "my-dir/doc.cf.yaml",
		},
		{
			name:          "absolute path outside cwd",
			blueprintPath: filepath.Join(os.TempDir(), "other-outside-dir", "doc.cf.yaml"),
			wantBlueprint: filepath.ToSlash(filepath.Clean(filepath.Join(os.TempDir(), "other-outside-dir", "doc.cf.yaml"))),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			idx, err := index.Build(map[string][]schema.CRD{})
			if err != nil {
				t.Fatal(err)
			}
			handler, err := New(Options{
				Version:   "v1.0.0",
				Index:     idx,
				Store:     cache.New(tmp),
				Blueprint: tt.blueprintPath,
				OutDir:    tmp,
				Lock:      filepath.Join(tmp, ".cf.lock"),
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}

			var res versionResponse
			if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
				t.Fatalf("decode: %v", err)
			}

			if res.Blueprint != tt.wantBlueprint {
				t.Errorf("Blueprint = %q, want %q", res.Blueprint, tt.wantBlueprint)
			}
			if res.Version != "v1.0.0" {
				t.Errorf("Version = %q, want %q", res.Version, "v1.0.0")
			}
			if res.OutDir != tmp {
				t.Errorf("OutDir = %q, want %q", res.OutDir, tmp)
			}
		})
	}
}
