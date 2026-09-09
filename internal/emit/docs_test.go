package emit

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find repo root containing go.mod from %s", dir)
		}
		dir = parent
	}
}

func defaultAutoReadyPackage(t *testing.T) string {
	t.Helper()
	fnsDoc, err := Functions(&blueprint.Blueprint{})
	if err != nil {
		t.Fatalf("Functions: %v", err)
	}
	re := regexp.MustCompile(`name:\s*function-auto-ready[\s\S]*?package:\s*([^\s\n]+)`)
	matches := re.FindStringSubmatch(string(fnsDoc))
	if len(matches) < 2 {
		t.Fatalf("could not find function-auto-ready package in Functions doc:\n%s", string(fnsDoc))
	}
	return matches[1]
}

// TestDefaultFunctionsInDocsMatchEmitter asserts that documentation in docs/dsl.md
// and docs/cli.md matches the exact package emitted by the generator for default functions
// (specifically function-auto-ready), ensuring docs and emitter never drift (CF-078).
func TestDefaultFunctionsInDocsMatchEmitter(t *testing.T) {
	root := repoRoot(t)
	wantPkg := defaultAutoReadyPackage(t)

	// Ensure emitter default pipeline agrees with Functions doc
	if defaultPipeline[0].Package != wantPkg {
		t.Fatalf("defaultPipeline package %q != Functions doc package %q", defaultPipeline[0].Package, wantPkg)
	}

	docFiles := []string{
		filepath.Join(root, "docs", "dsl.md"),
		filepath.Join(root, "docs", "cli.md"),
	}

	// Matches package references like "xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0"
	pkgRefRegex := regexp.MustCompile(`([a-zA-Z0-9.\-_/]+function-auto-ready:[a-zA-Z0-9.\-_]+)`)

	for _, docFile := range docFiles {
		relPath, _ := filepath.Rel(root, docFile)
		data, err := os.ReadFile(docFile)
		if err != nil {
			t.Fatalf("failed to read %s: %v", relPath, err)
		}
		content := string(data)

		matches := pkgRefRegex.FindAllString(content, -1)
		if len(matches) == 0 {
			t.Errorf("%s: expected at least one function-auto-ready package reference, found none", relPath)
		}

		for _, match := range matches {
			if match != wantPkg {
				t.Errorf("%s: documented package reference %q does not match emitted %q", relPath, match, wantPkg)
			}
		}

		if strings.Contains(content, "function-auto-ready:v0.5.1") {
			t.Errorf("%s: contains outdated function-auto-ready:v0.5.1 reference", relPath)
		}
	}
}

// TestRBACDocumentationTrigger verifies that docs/cli.md correctly documents the trigger
// for rbac.yaml: emitted only when composed native Kubernetes kinds require permissions
// not pre-granted to Crossplane (e.g. HorizontalPodAutoscaler), rather than claiming it
// is emitted for Deployment or Service (CF-079).
func TestRBACDocumentationTrigger(t *testing.T) {
	root := repoRoot(t)
	cliDoc := filepath.Join(root, "docs", "cli.md")
	data, err := os.ReadFile(cliDoc)
	if err != nil {
		t.Fatalf("failed to read docs/cli.md: %v", err)
	}
	content := string(data)

	// Must not say Deployment or Service trigger rbac.yaml
	if strings.Contains(content, "When native Kubernetes kinds like Deployment or Service are composed") {
		t.Errorf("docs/cli.md incorrectly states rbac.yaml is emitted when Deployment or Service are composed (both are pre-granted)")
	}

	if strings.Contains(content, "(emitted when native Kubernetes kinds are composed)") {
		t.Errorf("docs/cli.md file tree diagram incorrectly states rbac.yaml is emitted when native Kubernetes kinds are composed")
	}

	// Must explain that rbac.yaml is emitted for native kinds not pre-granted to Crossplane
	if !strings.Contains(content, "not pre-granted to Crossplane") {
		t.Errorf("docs/cli.md should clarify that rbac.yaml is emitted for permissions not pre-granted to Crossplane")
	}

	if !strings.Contains(content, "HorizontalPodAutoscaler") {
		t.Errorf("docs/cli.md should cite HorizontalPodAutoscaler as an example of a non-pre-granted kind")
	}
}
