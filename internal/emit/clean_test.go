package emit

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestFindExistingManagedFiles(t *testing.T) {
	out := t.TempDir()

	managed := []string{
		filepath.Join(out, "compositions", "c1.yaml"),
		filepath.Join(out, "xrds", "x1.yaml"),
		filepath.Join(out, "providerconfigs", "aws.yaml"),
		filepath.Join(out, "environmentconfigs", "env.yaml"),
		filepath.Join(out, "runtime", "rt.yaml"),
		filepath.Join(out, "templates", "tpl", "001.yaml"),
		filepath.Join(out, "functions.yaml"),
		filepath.Join(out, "rbac.yaml"),
	}
	for _, f := range managed {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	unmanaged := []string{
		filepath.Join(out, "README.md"),
		filepath.Join(out, "kustomization.yaml"),
		filepath.Join(out, "custom", "app.yaml"),
		filepath.Join(out, "other_dir", "nested", "file.yaml"),
	}
	for _, f := range unmanaged {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	found, err := FindExistingManagedFiles(out)
	if err != nil {
		t.Fatalf("FindExistingManagedFiles: %v", err)
	}

	want := make([]string, len(managed))
	for i, f := range managed {
		want[i] = filepath.Clean(f)
	}
	sort.Strings(want)
	// Sort order is deterministic (lexicographical)
	if diff := cmp.Diff(want, found); diff != "" {
		t.Fatalf("FindExistingManagedFiles mismatch (-want +got):\n%s", diff)
	}
}

func TestPruneOrphanedFilesPrunesAndRemovesEmptyDirs(t *testing.T) {
	out := t.TempDir()

	keepFile := filepath.Join(out, "compositions", "keep.yaml")
	staleFile1 := filepath.Join(out, "compositions", "stale.yaml")
	staleFile2 := filepath.Join(out, "templates", "my-tpl", "stale.yaml")
	unmanagedFile := filepath.Join(out, "README.md")

	for _, f := range []string{keepFile, staleFile1, staleFile2, unmanagedFile} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	removed, err := PruneOrphanedFiles(out, map[string]bool{keepFile: true})
	if err != nil {
		t.Fatalf("PruneOrphanedFiles: %v", err)
	}

	wantRemoved := []string{filepath.Clean(staleFile1), filepath.Clean(staleFile2)}
	if diff := cmp.Diff(wantRemoved, removed); diff != "" {
		t.Fatalf("removed mismatch (-want +got):\n%s", diff)
	}

	// keepFile must exist
	if _, err := os.Stat(keepFile); err != nil {
		t.Errorf("keepFile missing: %v", err)
	}
	// unmanagedFile must exist
	if _, err := os.Stat(unmanagedFile); err != nil {
		t.Errorf("unmanagedFile missing: %v", err)
	}
	// staleFiles must not exist
	if _, err := os.Stat(staleFile1); !os.IsNotExist(err) {
		t.Errorf("staleFile1 still exists")
	}
	if _, err := os.Stat(staleFile2); !os.IsNotExist(err) {
		t.Errorf("staleFile2 still exists")
	}

	// templates/my-tpl and templates/ directory should have been pruned because it became empty
	tplDir := filepath.Join(out, "templates", "my-tpl")
	if _, err := os.Stat(tplDir); !os.IsNotExist(err) {
		t.Errorf("empty dir %s was not pruned", tplDir)
	}
	topTplDir := filepath.Join(out, "templates")
	if _, err := os.Stat(topTplDir); !os.IsNotExist(err) {
		t.Errorf("empty dir %s was not pruned", topTplDir)
	}

	// compositions dir still contains keepFile, so it must still exist
	compDir := filepath.Join(out, "compositions")
	if _, err := os.Stat(compDir); err != nil {
		t.Errorf("compDir missing: %v", err)
	}
	// outDir itself must still exist
	if _, err := os.Stat(out); err != nil {
		t.Errorf("outDir missing: %v", err)
	}
}
