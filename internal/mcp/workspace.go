// This file is the write gate: the check that confines every generated-file
// write to the --out workspace the operator declared at launch.
package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// workspace is the one directory generate {"write":true} may write into,
// held absolute and cleaned so every containment check below is a plain
// string-prefix comparison with no ".." or "." segments left to reason
// about.
type workspace struct {
	out string
}

// newWorkspace resolves outDir to its absolute, cleaned form once, at server
// construction — not per check, so a working-directory change mid-run (this
// process never makes one, but nothing here should depend on that) cannot
// move the boundary.
func newWorkspace(outDir string) (workspace, error) {
	abs, err := filepath.Abs(outDir)
	if err != nil {
		return workspace{}, fmt.Errorf("resolve --out workspace %q: %w", outDir, err)
	}
	return workspace{out: abs}, nil
}

// check reports whether path may be written: it must resolve — absolute,
// cleaned — to the workspace directory or somewhere strictly inside it.
// filepath.Abs both absolutizes and cleans, so a traversal like
// "out/../../etc/cron.d/x" is compared by where it actually lands, never by
// what its spelling suggests. The prefix comparison appends the separator so
// a sibling directory sharing the workspace's name as a prefix ("/ws-evil"
// against workspace "/ws") does not pass.
//
// Symlinks are deliberately not resolved (no filepath.EvalSymlinks): the
// workspace may not exist yet on the first generate, and both sides of the
// comparison derive from the same --out flag, so they agree about any
// symlinked ancestry. A symlink placed INSIDE the workspace pointing out of
// it could still redirect a write — but planting that link already requires
// write access beyond what this server grants, so it is outside this gate's
// threat model (the same stance cmd/cf/gen.go takes for the CLI).
func (w workspace) check(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("refusing to write %q: cannot resolve it to an absolute path: %v", path, err)
	}
	if abs != w.out && !strings.HasPrefix(abs, w.out+string(os.PathSeparator)) {
		return fmt.Errorf("refusing to write %q: it resolves to %q, outside the declared --out workspace %q; "+
			"the MCP server only ever writes generated output inside that directory", path, abs, w.out)
	}
	return nil
}
