package cli

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestPreflightProbeTimeout verifies the bounded-execution guard: a hung
// `git`/`gh` subprocess must surface as an empty/fallback string within the
// timeout, not block the preflight indefinitely.
func TestPreflightProbeTimeout(t *testing.T) {
	// Not parallel: mutates process-global preflightProbeTimeout and PATH.

	if runtime.GOOS == "windows" {
		t.Skip("fake-git shim assumes POSIX shell")
	}

	dir := t.TempDir()
	// Shim for `git` — sleeps far longer than the test's timeout.
	// `exec sleep` replaces the shell so SIGKILL hits one PID cleanly.
	shim := filepath.Join(dir, "git")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatalf("write shim: %v", err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+origPath)

	orig := preflightProbeTimeout
	preflightProbeTimeout = 200 * time.Millisecond
	t.Cleanup(func() { preflightProbeTimeout = orig })

	start := time.Now()
	got := runTrimmed(context.Background(), "git", "branch", "--show-current")
	elapsed := time.Since(start)

	// Soft-failure contract: timeout produces empty string, not a hang.
	if got != "" {
		t.Fatalf("expected empty string on timeout, got %q", got)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("runTrimmed did not honor timeout: elapsed=%s", elapsed)
	}
}
