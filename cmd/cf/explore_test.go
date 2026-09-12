package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKindsCommand(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	// Add real parsed CRD
	addCmd := &ProviderAddCmd{
		Ref:      "example.org/provider-test:v2",
		CacheDir: cacheDir,
		Lock:     filepath.Join(dir, ".cf.lock"),
		fetch:    fakeFetch,
	}
	var addOut bytes.Buffer
	if err := addCmd.Run(&addOut); err != nil {
		t.Fatalf("ProviderAddCmd.Run: %v", err)
	}

	// 1. List all kinds (cached Widget + native kinds like Deployment)
	kindsCmd := &KindsCmd{
		CacheDir: cacheDir,
	}
	var out bytes.Buffer
	if err := kindsCmd.Run(&out); err != nil {
		t.Fatalf("KindsCmd.Run: %v", err)
	}
	outStr := out.String()
	if !strings.Contains(outStr, "Widget") {
		t.Errorf("expected output to contain Widget, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "Deployment") {
		t.Errorf("expected output to contain Deployment, got:\n%s", outStr)
	}

	// 2. Filter kinds by query
	kindsCmdQ := &KindsCmd{
		Q:        "Widget",
		CacheDir: cacheDir,
	}
	out.Reset()
	if err := kindsCmdQ.Run(&out); err != nil {
		t.Fatalf("KindsCmd.Run with Q=Widget: %v", err)
	}
	outStr = out.String()
	if !strings.Contains(outStr, "Widget") {
		t.Errorf("expected output to contain Widget, got:\n%s", outStr)
	}
	if strings.Contains(outStr, "Deployment") {
		t.Errorf("expected output to NOT contain Deployment when filtered by Widget, got:\n%s", outStr)
	}

	// 3. Filter kinds by non-matching query
	kindsCmdNone := &KindsCmd{
		Q:        "NonExistentKind123",
		CacheDir: cacheDir,
	}
	out.Reset()
	if err := kindsCmdNone.Run(&out); err != nil {
		t.Fatalf("KindsCmd.Run with non-matching Q: %v", err)
	}
	if !strings.Contains(out.String(), "No kinds found matching") {
		t.Errorf("expected 'No kinds found matching', got:\n%s", out.String())
	}
}

func TestFieldsCommand(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")

	// 1. Fields for Deployment (native kind)
	fieldsCmd := &FieldsCmd{
		Kind:     "Deployment",
		CacheDir: cacheDir,
	}
	var out bytes.Buffer
	if err := fieldsCmd.Run(&out); err != nil {
		t.Fatalf("FieldsCmd.Run: %v", err)
	}
	outStr := out.String()
	if !strings.Contains(outStr, "KIND:       Deployment") {
		t.Errorf("expected KIND: Deployment, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "spec.template.spec.containers") {
		t.Errorf("expected spec.template.spec.containers field, got:\n%s", outStr)
	}

	// 2. Fields with --required
	fieldsReqCmd := &FieldsCmd{
		Kind:     "Deployment",
		Required: true,
		CacheDir: cacheDir,
	}
	out.Reset()
	if err := fieldsReqCmd.Run(&out); err != nil {
		t.Fatalf("FieldsCmd.Run with --required: %v", err)
	}
	outStr = out.String()
	if !strings.Contains(outStr, "spec.selector") {
		t.Errorf("expected required field/branch spec.selector, got:\n%s", outStr)
	}

	// 3. Fields with --status
	fieldsStatusCmd := &FieldsCmd{
		Kind:     "Deployment",
		Status:   true,
		CacheDir: cacheDir,
	}
	out.Reset()
	if err := fieldsStatusCmd.Run(&out); err != nil {
		t.Fatalf("FieldsCmd.Run with --status: %v", err)
	}
	outStr = out.String()
	if !strings.Contains(outStr, "Deployment (Status)") {
		t.Errorf("expected Deployment (Status), got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "readyReplicas") && !strings.Contains(outStr, "replicas") {
		t.Errorf("expected status fields like readyReplicas or replicas, got:\n%s", outStr)
	}

	// 4. Unknown kind returns error
	unknownCmd := &FieldsCmd{
		Kind:     "NoSuchResourceKind",
		CacheDir: cacheDir,
	}
	if err := unknownCmd.Run(&out); err == nil {
		t.Fatal("expected error for unknown kind, got nil")
	}

	// 5. Inexact/fuzzy kind match is refused with "did you mean" suggestion (CF-150)
	fuzzyCmd := &FieldsCmd{
		Kind:     "Deploymen",
		CacheDir: cacheDir,
	}
	out.Reset()
	if err := fuzzyCmd.Run(&out); err == nil {
		t.Fatal("expected error for fuzzy kind 'Deploymen', got nil")
	} else if !strings.Contains(err.Error(), `did you mean "Deployment"?`) {
		t.Errorf("expected error to suggest 'did you mean \"Deployment\"?', got: %v", err)
	}
}

func TestCatalogueCommand(t *testing.T) {
	// 1. All catalogue packages
	cmd := &CatalogueCmd{}
	var out bytes.Buffer
	if err := cmd.Run(&out); err != nil {
		t.Fatalf("CatalogueCmd.Run: %v", err)
	}
	if !strings.Contains(out.String(), "provider-aws-rds") {
		t.Errorf("expected catalogue to contain provider-aws-rds, got:\n%s", out.String())
	}

	// 2. Search by kind: DatabaseInstance -> provider-gcp-sql
	cmdQ := &CatalogueCmd{Q: "DatabaseInstance"}
	out.Reset()
	if err := cmdQ.Run(&out); err != nil {
		t.Fatalf("CatalogueCmd.Run with Q=DatabaseInstance: %v", err)
	}
	if !strings.Contains(out.String(), "provider-gcp-sql") {
		t.Errorf("expected search for DatabaseInstance to find provider-gcp-sql, got:\n%s", out.String())
	}

	// 3. Search by kind: Bucket -> provider-aws-s3 and provider-gcp-storage
	cmdBucket := &CatalogueCmd{Q: "Bucket"}
	out.Reset()
	if err := cmdBucket.Run(&out); err != nil {
		t.Fatalf("CatalogueCmd.Run with Q=Bucket: %v", err)
	}
	outStr := out.String()
	if !strings.Contains(outStr, "provider-aws-s3") || !strings.Contains(outStr, "provider-gcp-storage") {
		t.Errorf("expected search for Bucket to find provider-aws-s3 and provider-gcp-storage, got:\n%s", outStr)
	}

	// 4. Filter by type=function
	cmdFn := &CatalogueCmd{Type: "function"}
	out.Reset()
	if err := cmdFn.Run(&out); err != nil {
		t.Fatalf("CatalogueCmd.Run with Type=function: %v", err)
	}
	outStr = out.String()
	if !strings.Contains(outStr, "function-go-templating") {
		t.Errorf("expected function-go-templating, got:\n%s", outStr)
	}
	if strings.Contains(outStr, "provider-aws-rds") {
		t.Errorf("expected no providers in type=function listing, got:\n%s", outStr)
	}

	// 5. Filter by --kind=DatabaseInstance
	cmdKind := &CatalogueCmd{Kind: "DatabaseInstance"}
	out.Reset()
	if err := cmdKind.Run(&out); err != nil {
		t.Fatalf("CatalogueCmd.Run with Kind=DatabaseInstance: %v", err)
	}
	outStr = out.String()
	if !strings.Contains(outStr, "provider-gcp-sql") {
		t.Errorf("expected provider-gcp-sql for --kind=DatabaseInstance, got:\n%s", outStr)
	}

	// 6. Filter by --kind=Bucket must match s3/gcp-storage and NOT bitbucket-server
	cmdBucketKind := &CatalogueCmd{Kind: "Bucket"}
	out.Reset()
	if err := cmdBucketKind.Run(&out); err != nil {
		t.Fatalf("CatalogueCmd.Run with Kind=Bucket: %v", err)
	}
	outStr = out.String()
	if !strings.Contains(outStr, "provider-aws-s3") || !strings.Contains(outStr, "provider-gcp-storage") {
		t.Errorf("expected provider-aws-s3 and provider-gcp-storage for --kind=Bucket, got:\n%s", outStr)
	}
	if strings.Contains(outStr, "provider-bitbucket-server") {
		t.Errorf("expected provider-bitbucket-server to be excluded for --kind=Bucket, got:\n%s", outStr)
	}
}

func TestCF102KindsAndFieldsErrorOnInvalidBlueprint(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	badBP := filepath.Join(dir, "bad.cf.yaml")
	if err := os.WriteFile(badBP, []byte("spec: [invalid yaml"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	kindsCmd := &KindsCmd{
		CacheDir:  cacheDir,
		Blueprint: badBP,
	}
	var out bytes.Buffer
	if err := kindsCmd.Run(&out); err == nil {
		t.Fatal("expected kindsCmd.Run to fail on invalid blueprint, got nil")
	}

	fieldsCmd := &FieldsCmd{
		Kind:      "Deployment",
		CacheDir:  cacheDir,
		Blueprint: badBP,
	}
	out.Reset()
	if err := fieldsCmd.Run(&out); err == nil {
		t.Fatal("expected fieldsCmd.Run to fail on invalid blueprint, got nil")
	}
}

func TestCF102KindsAndFieldsWarnOnUncachedSource(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	bpFile := filepath.Join(dir, "doc.cf.yaml")
	bpContent := `apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: test
spec:
  sources:
    - provider: ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0
  xrd:
    group: platform.example.org
    version: v1alpha1
    kind: XApp
    plural: xapps
    scope: Namespaced
    parameters:
      providerName:
        type: string
        required: true
`
	if err := os.WriteFile(bpFile, []byte(bpContent), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	kindsCmd := &KindsCmd{
		CacheDir:  cacheDir,
		Blueprint: bpFile,
	}
	var out bytes.Buffer
	// Should succeed (partial index with native kinds) but warn on stderr about uncached provider
	if err := kindsCmd.Run(&out); err != nil {
		t.Fatalf("kindsCmd.Run failed: %v", err)
	}

	fieldsCmd := &FieldsCmd{
		Kind:      "Deployment",
		CacheDir:  cacheDir,
		Blueprint: bpFile,
	}
	out.Reset()
	if err := fieldsCmd.Run(&out); err != nil {
		t.Fatalf("fieldsCmd.Run failed: %v", err)
	}
}
