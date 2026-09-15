package emit

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// requireDocker verifies that the docker binary exists on PATH and that the Docker
// daemon is reachable. If either check fails, the calling test is skipped.
// If available, it returns the path to the docker binary.
func requireDocker(t *testing.T) string {
	t.Helper()
	dockerBin, err := exec.LookPath("docker")
	if err != nil {
		t.Skipf("docker CLI not found on PATH: %v", err)
	}
	if err := exec.Command(dockerBin, "info").Run(); err != nil {
		t.Skipf("Docker daemon unavailable: %v", err)
	}
	return dockerBin
}

// isDockerDaemonError checks if command output or error indicates that the
// Docker daemon was unreachable or connection to the daemon failed.
func isDockerDaemonError(out string, err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(out)
	return strings.Contains(lower, "failed to connect to the docker api") ||
		strings.Contains(lower, "cannot connect to the docker daemon") ||
		strings.Contains(lower, "dial unix") ||
		strings.Contains(lower, "is the docker daemon running") ||
		strings.Contains(lower, "error during connect")
}

func TestCF495_KCLTestsSkipWhenDaemonUnreachable(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go binary not found on PATH: %v", err)
	}

	tests := []string{
		"TestCF415_KCLRuntimeExecution",
		"TestCF430_KCLArrayEmissionSuppressesEmptyElements",
		"TestCF433_KCLAnnotationsOptionalGuard",
		"TestCF427_KCLOptionalMetadataNameFallback",
		"TestMetadataNameByteTarget_KCL",
	}

	pattern := "^(" + strings.Join(tests, "|") + ")$"
	cmd := exec.Command("go", "test", "-short", "-count=1", "-v", "-run", pattern, ".")
	cmd.Env = append(os.Environ(), "DOCKER_HOST=unix:///tmp/nonexistent-cf495.sock")
	out, err := cmd.CombinedOutput()
	outStr := string(out)

	if err != nil {
		t.Fatalf("expected go test to succeed (tests should skip, not fail) with unreachable docker daemon: %v\nOutput:\n%s", err, outStr)
	}

	for _, name := range tests {
		if !strings.Contains(outStr, "--- SKIP: "+name) {
			t.Errorf("expected test %s to be skipped, got output:\n%s", name, outStr)
		}
	}
	if !strings.Contains(outStr, "Docker daemon unavailable") {
		t.Errorf("expected skip reason to mention Docker daemon unavailable, got:\n%s", outStr)
	}
}
