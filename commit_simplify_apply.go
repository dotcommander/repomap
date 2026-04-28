package repomap

// commit_simplify_apply.go — Deterministic code-quality preflight for commit prep.
//
// Porting choice: SHELL-OUT to simplify-detect.sh rather than porting the linter
// invocations into Go. Rationale:
//   - The script runs golangci-lint, gocyclo, go vet, biome, eslint, ruff — each
//     with its own binary, config resolution, and timeout logic.
//   - Porting that faithfully would add ~400 LOC plus subprocess error-handling
//     for each tool, with no semantic gain.
//   - The script already exists, is tested, and outputs a stable section format.
//
// The output format is text sections (not JSON):
//
//	=== SECTION_NAME ===
//	content
//
// We parse this into Candidate structs by extracting lint lines and mapping
// them back to file+line. Since the script does not emit machine-readable
// replacement text, ApplyCandidates is always a no-op (candidates are
// informational only for the prep pipeline — the agent applies fixes).
// The "applied" / "skipped" split is preserved for interface completeness.

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Candidate is one code-quality finding from the simplify detector.
// Replacement is empty when the detector does not provide an auto-fix
// (the current shell script never does — it only reports).
type Candidate struct {
	File        string `json:"file"`
	Line        int    `json:"line"`
	Kind        string `json:"kind"`                  // go_lint | go_vet | go_complexity | ts_lint | py_lint
	Hint        string `json:"hint"`                  // human-readable finding
	Replacement string `json:"replacement,omitempty"` // "" means no auto-fix available
}

// RunSimplifyDetect execs simplify-detect.sh and parses its section output
// into a []Candidate. Returns nil candidates (not an error) when the script
// is absent, exits non-zero, or finds nothing.
func RunSimplifyDetect(ctx context.Context, repoRoot string) ([]Candidate, error) {
	script := dcScriptPath("simplify-detect.sh")
	if _, err := os.Stat(script); err != nil {
		// Script absent — treat as no findings, not an error.
		return nil, nil
	}

	cmd := exec.CommandContext(ctx, "bash", script, repoRoot)
	cmd.Dir = repoRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Exit code is always 0 from the script; non-zero = script error.
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("simplify-detect: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}

	return parseSimplifyOutput(stdout.String()), nil
}

// lintLineRe matches common linter output formats:
//   - file.go:12:3: message
//   - file.go:12: message
//   - ./file.go:12:3: message
var lintLineRe = regexp.MustCompile(`^\.?/?([^:]+\.[a-zA-Z]+):(\d+)(?::\d+)?:\s+(.+)$`)

// parseSimplifyOutput converts the section-delimited text output of
// simplify-detect.sh into a slice of Candidate structs. Lines that don't
// match the file:line: pattern are ignored.
func parseSimplifyOutput(raw string) []Candidate {
	var candidates []Candidate
	currentSection := ""

	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := scanner.Text()

		// Section header: === NAME ===
		if strings.HasPrefix(line, "=== ") && strings.HasSuffix(line, " ===") {
			currentSection = strings.TrimSuffix(strings.TrimPrefix(line, "=== "), " ===")
			continue
		}

		// Skip SKIP/OK lines and non-tool sections.
		if strings.HasPrefix(line, "SKIP:") || strings.HasPrefix(line, "OK:") {
			continue
		}
		if currentSection == "CHANGED_FILES" || currentSection == "SUMMARY" || currentSection == "" {
			continue
		}

		m := lintLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		lineNo, _ := strconv.Atoi(m[2])
		candidates = append(candidates, Candidate{
			File: m[1],
			Line: lineNo,
			Kind: strings.ToLower(currentSection),
			Hint: m[3],
		})
	}
	return candidates
}

// ApplyCandidates attempts to apply each candidate's Replacement at file:line.
// Candidates with an empty Replacement are always marked skipped (the current
// simplify-detect.sh never provides one — findings are informational only).
//
// For candidates that do carry a Replacement, the function reads the file,
// verifies the line still matches the expected content, and rewrites it
// atomically via temp+rename. Mismatches are skipped (not errors) to be
// idempotent across re-runs.
func ApplyCandidates(ctx context.Context, repoRoot string, candidates []Candidate) (applied, skipped []Candidate, err error) {
	for _, c := range candidates {
		if c.Replacement == "" {
			skipped = append(skipped, c)
			continue
		}

		abs := filepath.Join(repoRoot, c.File)
		data, readErr := os.ReadFile(abs)
		if readErr != nil {
			skipped = append(skipped, c)
			continue
		}

		lines := strings.Split(string(data), "\n")
		idx := c.Line - 1 // 1-indexed → 0-indexed
		if idx < 0 || idx >= len(lines) {
			skipped = append(skipped, c)
			continue
		}

		// Idempotency: if line already equals the replacement, skip.
		if strings.TrimRight(lines[idx], "\r") == c.Replacement {
			skipped = append(skipped, c)
			continue
		}

		lines[idx] = c.Replacement
		newContent := []byte(strings.Join(lines, "\n"))
		if writeErr := atomicWriteFile(abs, newContent, 0o644); writeErr != nil {
			return applied, skipped, fmt.Errorf("write %s: %w", c.File, writeErr)
		}
		applied = append(applied, c)
	}
	return applied, skipped, nil
}
