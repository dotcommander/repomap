package repomap

// commit_prep_io.go — I/O-side helpers for the commit prep pipeline:
// shell-outs (release gate, git reset) and dc-plugin script resolution.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// dcScriptPath returns the path to a dc-plugin script, honoring CLAUDE_PLUGIN_ROOT
// when set, then falling back to the dc plugin cache. The returned path is not
// stat'd — callers decide how to handle absence.
func dcScriptPath(name string) string {
	if root := os.Getenv("CLAUDE_PLUGIN_ROOT"); root != "" {
		p := filepath.Join(root, "scripts", name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "plugins", "cache", "dotcommander", "dc", "current", "scripts", name)
}

// StashArtifacts adds artifact paths to .gitignore and unstages them.
// Best-effort: I/O failures are swallowed (artifacts are advisory).
func StashArtifacts(repoRoot string, artifacts []string) {
	if len(artifacts) == 0 {
		return
	}
	giPath := filepath.Join(repoRoot, ".gitignore")
	existing, _ := os.ReadFile(giPath)
	existingStr := string(existing)

	var additions []string
	for _, a := range artifacts {
		if !strings.Contains(existingStr, a) {
			additions = append(additions, a)
		}
	}
	if len(additions) > 0 {
		newContent := strings.TrimRight(existingStr, "\n") + "\n" + strings.Join(additions, "\n") + "\n"
		_ = atomicWriteFile(giPath, []byte(newContent), 0o644)
	}
	// Batch all artifacts into a single `git reset HEAD -- a b c` call.
	args := append([]string{"-C", repoRoot, "reset", "HEAD", "--"}, artifacts...)
	_ = exec.Command("git", args...).Run()
}

// RunReleaseGate shells out to release-gate.sh and returns a summary.
// build_ok=true when the script exits 0 or is absent.
func RunReleaseGate(repoRoot string) *PrepReleaseGate {
	script := dcScriptPath("release-gate.sh")
	if _, err := os.Stat(script); err != nil {
		return &PrepReleaseGate{Applied: []any{}, BuildOK: true}
	}
	cmd := exec.Command("bash", script, "--path", repoRoot, "--no-toolchain")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	gate := &PrepReleaseGate{Applied: []any{}, BuildOK: err == nil}
	var result struct {
		Applied []any `json:"applied"`
	}
	if json.Unmarshal(out, &result) == nil && result.Applied != nil {
		gate.Applied = result.Applied
	}
	return gate
}
