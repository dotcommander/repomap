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
	"regexp"
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

			newLine, changed := applyPlaceholder(lines[idx], placeholder)
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

// ApplyReviewDecisions applies LLM-adjudicated verdicts to REVIEW findings.
// For verdict="unsafe": applies the decision's Replacement at the finding's
// file+line. For verdict="safe": no-op (finding is cleared without edit).
// Decisions referencing unknown findings are silently skipped.
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

// userPathRe matches /Users/<name>/... or /home/<name>/... paths.
var userPathRe = regexp.MustCompile(`(/Users/[A-Za-z0-9_.-]+|/home/[A-Za-z0-9_.-]+)/`)

var secretAssignRe = regexp.MustCompile(
	`(?i)((?:api_key|apikey|api-key|token|secret|password|passwd|credentials)\s*[:=]\s*)(["']?)([^"'\s]+)(["']?)`,
)

var secretTokenRe = regexp.MustCompile(
	`(AKIA[0-9A-Z]{16}|sk-[A-Za-z0-9_-]{20,}|sk-ant-[A-Za-z0-9_-]{20,}|pk_live_[A-Za-z0-9]{20,}|gh[pousr]_[A-Za-z0-9_]{36,})`,
)

// applyPlaceholder rewrites a line to replace detected sensitive content with
// the placeholder. It preserves the surrounding line syntax:
//   - For path PII (/path/to/project/): replaces the path prefix with the placeholder.
//   - For secrets (API keys, passwords, tokens): replaces the value portion
//     after the separator (: = -) while keeping the key name and quote style.
//
// Returns (newLine, changed).
func applyPlaceholder(line, placeholder string) (string, bool) {
	// Idempotency guard: if placeholder already present, don't re-apply.
	if strings.Contains(line, placeholder) {
		return line, false
	}

	// Path replacement: /Users/<name>/ or /home/<name>/
	if userPathRe.MatchString(line) && (placeholder == "/path/to/project" ||
		placeholder == "REDACTED") {
		newLine := userPathRe.ReplaceAllString(line, "/path/to/project/")
		if newLine != line {
			return newLine, true
		}
	}

	// Secret replacement: look for key=value or key: value patterns.
	// Preserve everything up to and including the separator+whitespace+open-quote,
	// then substitute the value with the placeholder.
	if m := secretAssignRe.FindStringSubmatchIndex(line); m != nil {
		// Groups: 0=full, 1=key+sep, 2=open-quote, 3=value, 4=close-quote
		prefix := line[m[2]:m[3]] // key+separator
		openQ := line[m[4]:m[5]]  // open quote (may be empty)
		closeQ := line[m[8]:m[9]] // close quote
		replacement := line[:m[2]] + prefix + openQ + placeholder + closeQ + line[m[9]:]
		if replacement != line {
			return replacement, true
		}
	}

	// Fallback: if the line contains a recognizable secret pattern (AKIA..., sk-...),
	// replace the matched token directly.
	if secretTokenRe.MatchString(line) {
		newLine := secretTokenRe.ReplaceAllString(line, placeholder)
		if newLine != line {
			return newLine, true
		}
	}

	// No pattern matched; return unchanged (caller marks as skipped).
	return line, false
}

// findingID returns a stable string identity for a finding used as a map key.
// Format: "file:line" (line 0 when absent).
func findingID(f Finding) string {
	return fmt.Sprintf("%s:%d", f.File, f.Line)
}
