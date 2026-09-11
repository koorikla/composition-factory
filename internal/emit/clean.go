package emit

import (
	"os"
	"path/filepath"
	"sort"
)

// ManagedDirs lists the directory names within the output root that emit manages.
var ManagedDirs = []string{
	"compositions",
	"xrds",
	"providerconfigs",
	"runtime",
	"templates",
	"environmentconfigs",
}

// ManagedTopFiles lists the root-level files that emit manages.
var ManagedTopFiles = []string{
	"functions.yaml",
	"rbac.yaml",
}

// FindExistingManagedFiles returns all existing files under outDir that fall within
// the managed directories or match top-level managed files. Paths are returned cleaned and sorted.
func FindExistingManagedFiles(outDir string) ([]string, error) {
	cleanOut := filepath.Clean(outDir)
	var found []string

	for _, d := range ManagedDirs {
		dir := filepath.Join(cleanOut, d)
		if _, err := os.Stat(dir); err == nil {
			_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				found = append(found, filepath.Clean(path))
				return nil
			})
		}
	}
	for _, f := range ManagedTopFiles {
		file := filepath.Join(cleanOut, f)
		if _, err := os.Stat(file); err == nil {
			found = append(found, filepath.Clean(file))
		}
	}
	sort.Strings(found)
	return found, nil
}

// PruneOrphanedOutputs removes any files under outDir that fall within managed
// scopes but are not present in outputs. Empty parent directories within managed
// scopes up to outDir are also removed. Returns the list of pruned file paths, sorted.
func PruneOrphanedOutputs(outDir string, outputs []Output) ([]string, error) {
	expected := make(map[string]bool, len(outputs))
	for _, o := range outputs {
		expected[filepath.Clean(o.Path)] = true
	}
	return PruneOrphanedFiles(outDir, expected)
}

// PruneOrphanedFiles removes any files under outDir that fall within managed scopes
// but are not present in expected. Empty parent directories within managed scopes
// up to outDir are also removed. Returns the list of pruned file paths, sorted.
func PruneOrphanedFiles(outDir string, expected map[string]bool) ([]string, error) {
	cleanOut := filepath.Clean(outDir)
	existingFiles, err := FindExistingManagedFiles(cleanOut)
	if err != nil {
		return nil, err
	}

	cleanedExpected := make(map[string]bool, len(expected))
	for k, v := range expected {
		if v {
			cleanedExpected[filepath.Clean(k)] = true
		}
	}

	var removed []string
	for _, path := range existingFiles {
		if !cleanedExpected[path] {
			if err := os.Remove(path); err == nil {
				removed = append(removed, path)
				// Prune empty parent directory if inside managed scope
				dir := filepath.Dir(path)
				for dir != "." && dir != cleanOut && dir != string(filepath.Separator) && filepath.Dir(dir) != dir {
					if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
						_ = os.Remove(dir)
						dir = filepath.Dir(dir)
					} else {
						break
					}
				}
			} else if !os.IsNotExist(err) {
				return removed, err
			}
		}
	}
	sort.Strings(removed)
	return removed, nil
}
