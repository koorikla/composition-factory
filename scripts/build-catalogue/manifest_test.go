package main

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadManifestParsesReposAndTags(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	writeFile(t, path, `{
		"repos": [
			{"name": "function-go-templating", "description": "go templates", "html_url": "https://github.com/crossplane-contrib/function-go-templating", "license_spdx_id": "Apache-2.0"},
			{"name": "provider-upjet-aws", "description": "aws generator", "html_url": "https://github.com/crossplane-contrib/provider-upjet-aws", "license_spdx_id": ""}
		],
		"tags": {
			"function-go-templating": ["v0.9.2", "v0.10.0", "v0.11.0"]
		}
	}`)

	m, err := loadManifest(path)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}

	repos := m.repos("crossplane-contrib")
	want := []repo{
		{Name: "function-go-templating", Owner: "crossplane-contrib", Description: "go templates", SourceURL: "https://github.com/crossplane-contrib/function-go-templating", LicenseSPDX: "Apache-2.0"},
		{Name: "provider-upjet-aws", Owner: "crossplane-contrib", Description: "aws generator", SourceURL: "https://github.com/crossplane-contrib/provider-upjet-aws", LicenseSPDX: ""},
	}
	if !reflect.DeepEqual(repos, want) {
		t.Errorf("repos = %+v, want %+v", repos, want)
	}

	wantTags := map[string][]string{"function-go-templating": {"v0.9.2", "v0.10.0", "v0.11.0"}}
	if !reflect.DeepEqual(m.Tags, wantTags) {
		t.Errorf("tags = %+v, want %+v", m.Tags, wantTags)
	}
}

func TestLoadManifestErrorsOnMissingFile(t *testing.T) {
	if _, err := loadManifest(filepath.Join(t.TempDir(), "does-not-exist.json")); err == nil {
		t.Fatal("loadManifest on a missing file = nil error, want one")
	}
}

func TestLoadManifestErrorsOnInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	writeFile(t, path, `not json`)
	if _, err := loadManifest(path); err == nil {
		t.Fatal("loadManifest on invalid JSON = nil error, want one")
	}
}
