package emit

import (
	"fmt"
)

// unknownPathError formats a consistent error message when a requested path is not found in a schema,
// suggesting the closest match if one exists.
func unknownPathError(resourceName, prefix, path, schemaDesc string, suggestions []string, extraReason string) error {
	s := closestPath(path, suggestions)
	detail := ""
	if extraReason != "" {
		detail = " (" + extraReason + ")"
	}
	if s != "" {
		return fmt.Errorf("resource %q%s: %q is not in %s; did you mean %q?%s",
			resourceName, prefix, path, schemaDesc, s, detail)
	}
	return fmt.Errorf("resource %q%s: %q is not in %s%s",
		resourceName, prefix, path, schemaDesc, detail)
}
