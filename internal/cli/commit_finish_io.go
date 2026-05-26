package cli

// commit_finish_io.go — I/O and release helpers for commit finish.
// Extracted from commit_finish.go to stay under the 300-line limit.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/dotcommander/repomap"
)

const (
	finishStatusPassed = "passed"
	finishStatusFailed = "failed"
)

// emitFinishResult writes JSON to stdout (or a terse message) then exits with code.
func emitFinishResult(jsonOut bool, exitCode int, fr *finishResult) error {
	if jsonOut {
		data, err := json.MarshalIndent(fr, "", "  ")
		if err != nil {
			return fmt.Errorf("encode result: %w", err)
		}
		if _, err := os.Stdout.Write(data); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout)
	} else {
		fmt.Printf("status: %s\n", fr.Status)
		if fr.FailureDetail != "" {
			fmt.Printf("failure: %s\n", fr.FailureDetail)
		}
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return nil
}

// finishFatal emits a minimal JSON error payload and exits.
func finishFatal(jsonOut bool, exitCode int, detail string) error {
	return emitFinishResult(jsonOut, exitCode, &finishResult{
		Status:        finishStatusFailed,
		FailureDetail: detail,
	})
}

// buildFinishResult converts an ExecuteResult to a finishResult for exit-4 partial failures.
func buildFinishResult(status string, r *repomap.ExecuteResult, detail string) *finishResult {
	fr := &finishResult{
		Status:        status,
		Commits:       r.Commits,
		FailureDetail: detail,
	}
	if r.Tag != nil {
		fr.Tag = *r.Tag
	}
	if r.ReleaseURL != nil {
		fr.ReleaseURL = *r.ReleaseURL
	}
	return fr
}

// parseDecisions reads decisions from an inline JSON string or @path file.
func parseDecisions(arg string) (*finishDecisions, error) {
	var raw []byte
	if strings.HasPrefix(arg, "@") {
		data, err := os.ReadFile(arg[1:])
		if err != nil {
			return nil, fmt.Errorf("read decisions file: %w", err)
		}
		raw = data
	} else {
		raw = []byte(arg)
	}
	var d finishDecisions
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// bumpLevel derives the semver bump level from group metadata.
// If tag looks like vX.Y.Z it is returned verbatim (caller passes it to just release).
// Otherwise: any group with Breaking → "major"; any feat → "minor"; else "patch".
func bumpLevel(groups []repomap.CommitGroup, tag string) string {
	// Explicit semver tag: pass through verbatim.
	if strings.HasPrefix(tag, "v") && strings.Count(tag, ".") == 2 {
		return tag
	}
	hasMinor := false
	for _, g := range groups {
		if g.Breaking {
			return "major"
		}
		if g.Type == "feat" {
			hasMinor = true
		}
	}
	if hasMinor {
		return "minor"
	}
	return "patch"
}

// runJustRelease executes `just release <arg>` streaming output to stderr.
func runJustRelease(ctx context.Context, repoRoot, arg string) error {
	cmd := exec.CommandContext(ctx, "just", "release", arg) //nolint:gosec // arg is bumpLevel output: "major"|"minor"|"patch"|"vX.Y.Z"
	cmd.Dir = repoRoot
	cmd.Stdout = os.Stderr // stream to stderr; stdout reserved for JSON
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
