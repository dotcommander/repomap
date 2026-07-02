package repomap

import (
	"regexp"
	"strings"
)

// userPathRe matches /Users/<name>/... or /home/<name>/... paths.
var userPathRe = regexp.MustCompile(`(/Users/[A-Za-z0-9_.-]+|/home/[A-Za-z0-9_.-]+)/`)

var secretAssignRe = regexp.MustCompile(
	`(?i)((?:api_key|apikey|api-key|token|secret|password|passwd|credentials)\s*[:=]\s*)(["']?)([^"'\s]+)(["']?)`,
)

var secretTokenRe = regexp.MustCompile(
	`(AKIA[0-9A-Z]{16}|sk-[A-Za-z0-9_-]{20,}|sk-ant-[A-Za-z0-9_-]{20,}|pk_live_[A-Za-z0-9]{20,}|gh[pousr]_[A-Za-z0-9_]{36,})`,
)

var placeholderSecretRe = regexp.MustCompile(
	`YOUR_[A-Z0-9_]*(?:KEY|TOKEN|PASSWORD|SECRET)[A-Z0-9_]*`,
)

var projectPathRe = regexp.MustCompile(`/path/to/project(?:/[A-Za-z0-9_.-]+)+`)

// applyPlaceholder rewrites a line to replace detected sensitive content with
// the placeholder. It preserves the surrounding line syntax:
//   - For path PII (/path/to/project/): replaces the path prefix with the placeholder.
//   - For secrets (API keys, passwords, tokens): replaces the value portion
//     after the separator (: = -) while keeping the key name and quote style.
//
// Returns (newLine, changed).
func applyFindingPlaceholder(line, snippet, placeholder string) (string, bool) {
	if placeholder == "/path/to/project" {
		return applyPlaceholder(line, placeholder)
	}
	// Fail closed: a secret substitution requires the finding's snippet to still
	// be present on the line. The old pattern-based fallback rewrote unrelated
	// lines when the finding was stale (e.g. `token := lexer.Next()`).
	if snippet == "" || snippet == placeholder || !strings.Contains(line, snippet) {
		return line, false
	}
	newLine := strings.ReplaceAll(line, snippet, placeholder)
	return newLine, newLine != line
}

func applyPlaceholder(line, placeholder string) (string, bool) {
	// Idempotency guard: if placeholder already present and there is no more
	// specific path/secret pattern to collapse, don't re-apply.
	if strings.Contains(line, placeholder) && placeholder != "/path/to/project" {
		return line, false
	}

	// Path replacement: /Users/<name>/, /home/<name>/, or an already-generic
	// project path that still carries machine-specific suffixes.
	if (userPathRe.MatchString(line) || projectPathRe.MatchString(line)) && (placeholder == "/path/to/project" ||
		placeholder == "REDACTED") {
		newLine := userPathRe.ReplaceAllString(line, "/path/to/project/")
		newLine = projectPathRe.ReplaceAllString(newLine, "/path/to/project")
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

	// Fallback: if the line contains a recognizable secret pattern (AKIA..., sk-...)
	// or placeholder-shaped test credential (YOUR_API_KEY), replace the matched
	// token directly.
	if secretTokenRe.MatchString(line) || placeholderSecretRe.MatchString(line) {
		newLine := secretTokenRe.ReplaceAllString(line, placeholder)
		newLine = placeholderSecretRe.ReplaceAllString(newLine, placeholder)
		if newLine != line {
			return newLine, true
		}
	}

	// No pattern matched; return unchanged (caller marks as skipped).
	return line, false
}
