package repomap

// commit_finding_apply.go — Apply default_action=fix findings and LLM review decisions.
//
// Two entry points:
//   - ApplyFixFindings: deterministic; applies the substitution table to all
//     findings with default_action="fix". Idempotent.
//   - ApplyReviewDecisions: applies LLM-adjudicated verdicts for default_action="review"
//     findings. Verdict "unsafe" → apply replacement; "safe" → no-op.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// fixRule maps a finding kind to its safe-placeholder substitution.
// The replacement is applied to the matched line while preserving
// surrounding syntax (quotes, separators, indentation).
type fixRule struct {
	kind        string
	placeholder string
}

// fixRules is the immutable substitution table for default_action=fix findings.
// Order matters: more-specific kinds first.
var fixRules = []fixRule{
	{kind: "secret_api_key", placeholder: "YOUR_API_KEY"},
	{kind: "secret_password", placeholder: "YOUR_PASSWORD"},
	{kind: "secret_token", placeholder: "YOUR_TOKEN"},
	// gitleaks "secret" maps to our FLAG kind; use generic placeholder.
	{kind: "secret", placeholder: "REDACTED"},
	// PII path rules.
	{kind: "path_user_home", placeholder: "/path/to/project"},
	{kind: "path_machine_specific", placeholder: "/path/to/project"},
	// Generic pii catch-all (e.g. /Users/<name>/ regex hits from commit_secrets.go).
	{kind: "pii", placeholder: "/path/to/project"},
}

// ReviewDecision is one LLM verdict for a REVIEW finding.
type ReviewDecision struct {
	ID          string `json:"id"`          // matches Finding identity (file+line)
	Verdict     string `json:"verdict"`     // "safe" | "unsafe"
	Replacement string `json:"replacement"` // applied when verdict="unsafe"
}

// ApplyFixFindings applies the substitution table to all findings whose
// DefaultAction is "fix". Each line is rewritten in place using an atomic
// temp+rename write. Idempotent: if the placeholder is already present at
// that line, the finding is marked skipped.
//
// Returns the applied and skipped findings, or an error on I/O failure.
func ApplyFixFindings(ctx context.Context, repoRoot string, findings []Finding) (applied, skipped []Finding, err error) {
	// Group by file so we do one read+write per file.
	byFile := groupFindingsByFile(findings, ActionFix)

	for path, group := range byFile {
		abs := filepath.Join(repoRoot, path)
		data, readErr := os.ReadFile(abs)
		if readErr != nil {
			for _, f := range group {
				skipped = append(skipped, f)
			}
			continue
		}

		lines := strings.Split(string(data), "\n")
		dirty := false

		for _, f := range group {
			idx := f.Line - 1 // 1-indexed
			if idx < 0 || idx >= len(lines) {
				skipped = append(skipped, f)
				continue
			}

			placeholder := placeholderFor(f.Kind)
			if placeholder == "" {
				skipped = append(skipped, f)
				continue
			}

			newLine, changed := applyFindingPlaceholder(lines[idx], f.Snippet, placeholder)
			if !changed {
				// Already substituted — idempotent skip.
				skipped = append(skipped, f)
				continue
			}

			lines[idx] = newLine
			dirty = true
			applied = append(applied, f)
		}

		if dirty {
			newContent := []byte(strings.Join(lines, "\n"))
			if writeErr := atomicWriteFile(abs, newContent, 0o644); writeErr != nil {
				return applied, skipped, fmt.Errorf("write %s: %w", path, writeErr)
			}
		}
	}

	// Findings that were not in the "fix" bucket go straight to skipped.
	for _, f := range findings {
		if f.DefaultAction != ActionFix {
			skipped = append(skipped, f)
		}
	}

	return applied, skipped, nil
}

// ReviewFindingCount returns the number of findings requiring judgment.
func ReviewFindingCount(findings []Finding) int {
	count := 0
	for _, f := range findings {
		if f.DefaultAction == ActionReview {
			count++
		}
	}
	return count
}

// ValidateReviewDecisions verifies that all REVIEW findings have one explicit
// verdict before commit finish executes the prepared plan.
func ValidateReviewDecisions(findings []Finding, decisions []ReviewDecision) error {
	reviewByID := make(map[string]Finding)
	for _, f := range findings {
		if f.DefaultAction == ActionReview {
			reviewByID[findingID(f)] = f
		}
	}

	seen := make(map[string]struct{}, len(decisions))
	for _, d := range decisions {
		if d.ID == "" {
			return fmt.Errorf("decision missing id")
		}
		if _, ok := seen[d.ID]; ok {
			return fmt.Errorf("duplicate decision for %s", d.ID)
		}
		seen[d.ID] = struct{}{}

		if _, ok := reviewByID[d.ID]; !ok {
			return fmt.Errorf("decision for unknown review finding %s", d.ID)
		}
		switch d.Verdict {
		case VerdictSafe:
		case VerdictUnsafe:
			if d.Replacement == "" {
				return fmt.Errorf("unsafe decision for %s missing replacement", d.ID)
			}
		default:
			return fmt.Errorf("decision for %s has unsupported verdict %q", d.ID, d.Verdict)
		}
	}

	for id := range reviewByID {
		if _, ok := seen[id]; !ok {
			return fmt.Errorf("missing decision for review finding %s", id)
		}
	}
	return nil
}

// ApplyReviewDecisions applies LLM-adjudicated verdicts to REVIEW findings.
// For verdict="unsafe": applies the decision's Replacement at the finding's
// file+line. For verdict="safe": no-op (finding is cleared without edit).
// Callers should validate decisions before applying them.
func ApplyReviewDecisions(ctx context.Context, repoRoot string, decisions []ReviewDecision, findings []Finding) error {
	// Build finding lookup by ID (file:line).
	findByID := make(map[string]Finding, len(findings))
	for _, f := range findings {
		id := findingID(f)
		findByID[id] = f
	}

	// Group unsafe decisions by file so we do one read+write per file.
	type lineEdit struct {
		lineIdx     int
		replacement string
	}
	fileEdits := make(map[string][]lineEdit)

	for _, d := range decisions {
		if d.Verdict != VerdictUnsafe {
			continue // safe → no-op
		}
		f, ok := findByID[d.ID]
		if !ok {
			continue
		}
		if d.Replacement == "" {
			continue
		}
		fileEdits[f.File] = append(fileEdits[f.File], lineEdit{
			lineIdx:     f.Line - 1,
			replacement: d.Replacement,
		})
	}

	for path, edits := range fileEdits {
		abs := filepath.Join(repoRoot, path)
		data, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}

		lines := strings.Split(string(data), "\n")
		for _, e := range edits {
			if e.lineIdx < 0 || e.lineIdx >= len(lines) {
				continue
			}
			lines[e.lineIdx] = e.replacement
		}

		if err := atomicWriteFile(abs, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}

// --- helpers ---

// groupFindingsByFile returns findings grouped by File path, filtered to
// those matching the given DefaultAction.
func groupFindingsByFile(findings []Finding, action string) map[string][]Finding {
	out := make(map[string][]Finding)
	for _, f := range findings {
		if f.DefaultAction == action {
			out[f.File] = append(out[f.File], f)
		}
	}
	return out
}

// placeholderFor returns the safe placeholder for a finding kind,
// or "" when the kind is not in the substitution table.
func placeholderFor(kind string) string {
	for _, r := range fixRules {
		if r.kind == kind {
			return r.placeholder
		}
	}
	return ""
}

// findingID returns a stable string identity for a finding used as a map key.
// Format: "file:line" (line 0 when absent).
func findingID(f Finding) string {
	return fmt.Sprintf("%s:%d", f.File, f.Line)
}
