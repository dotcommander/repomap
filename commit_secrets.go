package repomap

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dotcommander/repomap/internal/safefs"
)

// scanSecrets runs all content-review passes over the changeset and returns
// a Findings slice + SecretsSummary. Mirrors commit-content-review.sh — port
// is verbatim on pattern set, optimized by scanning files once per pass.
//
// visibility drives Finding.DefaultAction: "none" (personal repo, no remote)
// is lenient — PII paths and dev-history markers auto-resolve to "safe". Any
// other visibility is strict — REVIEW findings default to "review" so the
// agent adjudicates them.
func scanSecrets(ctx context.Context, root string, files []fileChange, visibility string) ([]Finding, SecretsSummary) {
	scanFiles := filterScannable(root, files)
	var findings []Finding

	// Gitleaks pre-pass (single exec over all files).
	findings = append(findings, runGitleaks(ctx, scanFiles)...)

	// Regex passes (one compiled regex per category, one file walk per category).
	findings = append(findings, runRegexPass(scanFiles, secretFlagRules, "FLAG", "secret", "secret")...)
	findings = append(findings, runRegexPass(scanFiles, secretReviewRules, "REVIEW", "secret", "credential (review)")...)
	findings = append(findings, runRegexPass(scanFiles, piiFlagRules, "FLAG", "pii", "personal path")...)
	findings = append(findings, runRegexPass(scanFiles, piiReviewRules, "REVIEW", "pii", "personal info (review)")...)
	findings = append(findings, runRegexPass(scanFiles, devHistoryFlagRules, "FLAG", "dev_history", "dev history")...)
	findings = append(findings, runRegexPass(scanFiles, devHistoryReviewRules, "REVIEW", "dev_history", "dev history (review)")...)

	// Adjudicate each finding with a deterministic DefaultAction so the agent
	// can process them without a per-finding LLM round-trip.
	sum := SecretsSummary{Clean: true}
	for i := range findings {
		findings[i].DefaultAction = defaultAction(findings[i], visibility)
		f := findings[i]
		switch f.Class {
		case "FLAG":
			sum.FlagCount++
			sum.Clean = false
			if strings.HasPrefix(f.Detail, "gitleaks:") {
				sum.GitleaksFindings++
			}
		case "REVIEW":
			sum.ReviewCount++
		}
		switch f.DefaultAction {
		case "fix":
			sum.FixCount++
		case "safe":
			sum.SafeCount++
		case "review":
			sum.AmbiguousCount++
		}
	}
	return findings, sum
}

// defaultAction adjudicates a single finding into one of:
//
//	fix    — auto-replace with a safe placeholder (all FLAGs + public-repo PII REVIEWs)
//	safe   — leave as-is (lenient defaults on personal/private repos)
//	review — needs LLM judgment (fallback for anything we can't adjudicate)
//
// Policy by (class, kind, visibility):
//
//	FLAG/*                   → fix       (always; deterministic secret/PII hits)
//	REVIEW/secret            → review    (regex credentials are ambiguous everywhere)
//	REVIEW/pii    + public   → review    (emails/IPs on public repos are risky)
//	REVIEW/pii    + private  → safe      (team-internal PII is fine)
//	REVIEW/pii    + none     → safe      (personal repo; paths/emails expected)
//	REVIEW/pii    + unknown  → review    (strict fallback)
//	REVIEW/dev_history + *   → safe      (TODOs/draft markers are not leaks)
func defaultAction(f Finding, visibility string) string {
	if f.Class == "FLAG" {
		return "fix"
	}
	// class == REVIEW
	switch f.Kind {
	case "dev_history":
		return "safe"
	case "secret":
		return "review"
	case "pii":
		switch visibility {
		case "none", "private":
			return "safe"
		case "public":
			return "review"
		default:
			return "review"
		}
	}
	return "review"
}

// filterScannable drops files that shouldn't be grep'd: deletions, binaries,
// files larger than 1MB, directories, symlinks, and other non-regular files.
type scannableFile struct {
	Path string
	Data []byte
}

func filterScannable(root string, files []fileChange) []scannableFile {
	var out []scannableFile
	for _, f := range files {
		if f.Status == "D" || f.IndexStatus == "D" {
			continue
		}
		if f.IsArtifact {
			continue
		}
		_, data, err := safefs.ReadRepositoryRegularFileLimit(root, f.Path, 1<<20)
		if err != nil {
			continue
		}
		out = append(out, scannableFile{Path: f.Path, Data: data})
	}
	return out
}

// runGitleaks shells `gitleaks detect --no-git --source <tmp>` where <tmp> is
// a private snapshot of the scannable files. Returns FLAG findings.
// Silently skips if gitleaks is missing.
func runGitleaks(ctx context.Context, files []scannableFile) []Finding {
	if _, err := exec.LookPath("gitleaks"); err != nil {
		return nil
	}
	if len(files) == 0 {
		return nil
	}
	tmp, err := os.MkdirTemp("", "commit-gitleaks-")
	if err != nil {
		return nil
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	// Build a flat snapshot so gitleaks never reopens repository paths after
	// rooted validation.
	basenameToOrig := make(map[string]string, len(files))
	for _, file := range files {
		bn := filepath.Base(file.Path)
		// Dedupe by appending an index if necessary.
		snapshot := filepath.Join(tmp, bn)
		for i := 1; ; i++ {
			if _, exists := basenameToOrig[filepath.Base(snapshot)]; !exists {
				break
			}
			snapshot = filepath.Join(tmp, fmt.Sprintf("%d_%s", i, bn))
		}
		if err := os.WriteFile(snapshot, file.Data, 0o600); err != nil {
			continue
		}
		basenameToOrig[filepath.Base(snapshot)] = file.Path
	}

	report := filepath.Join(tmp, "report.json")
	cmd := exec.CommandContext(ctx, "gitleaks", "detect",
		"--no-git", "--no-banner",
		"--source", tmp,
		"--report-format", "json",
		"--report-path", report,
	)
	_ = cmd.Run() // gitleaks exits non-zero on findings; we ignore exit code.

	data, err := os.ReadFile(report)
	if err != nil || len(data) == 0 {
		return nil
	}
	var raw []struct {
		File      string `json:"File"`
		RuleID    string `json:"RuleID"`
		StartLine int    `json:"StartLine"`
		Match     string `json:"Match"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	var out []Finding
	for _, r := range raw {
		orig := basenameToOrig[filepath.Base(r.File)]
		if orig == "" {
			orig = r.File
		}
		out = append(out, Finding{
			Class:   "FLAG",
			Kind:    "secret",
			File:    orig,
			Line:    r.StartLine,
			Snippet: truncSnippet(r.Match),
			Detail:  "gitleaks: " + r.RuleID,
		})
	}
	return out
}

// scanFileForSecrets scans one rooted file snapshot and returns all regex findings within it.
func scanFileForSecrets(data []byte, rel string, rules []*regexp.Regexp, class, kind, detail string) []Finding {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var out []Finding
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		for _, re := range rules {
			if re.MatchString(line) {
				if kind == "pii" && isPlaceholderPath(line) {
					break
				}
				out = append(out, Finding{
					Class:   class,
					Kind:    kind,
					File:    rel,
					Line:    lineNo,
					Snippet: truncSnippet(line),
					Detail:  detail,
				})
				break // one finding per line is enough
			}
		}
	}
	return out
}

// runRegexPass scans every file in `files` for any of the `rules`. One
// compiled regex per category (OR'd) keeps this O(files × bytes).
func runRegexPass(files []scannableFile, rules []*regexp.Regexp, class, kind, detail string) []Finding {
	if len(rules) == 0 || len(files) == 0 {
		return nil
	}
	var out []Finding
	for _, file := range files {
		out = append(out, scanFileForSecrets(file.Data, file.Path, rules, class, kind, detail)...)
	}
	return out
}

// isPlaceholderPath returns true for obvious doc-placeholder paths
// ("/Users/you/", "/home/user/", etc.) that should not flag as PII.
// These appear in READMEs and example output, not in real code.
func isPlaceholderPath(line string) bool {
	for _, p := range placeholderPathFragments {
		if strings.Contains(line, p) {
			return true
		}
	}
	return false
}

var placeholderPathFragments = []string{
	"/Users/you/", "/Users/user/", "/Users/username/", "/Users/name/",
	"/home/you/", "/home/user/", "/home/username/", "/home/name/",
	"/Users/<", "/home/<", // angle-bracket placeholders
}

// truncSnippet caps a snippet at 100 runes to keep findings.json small.
func truncSnippet(s string) string {
	const max = 100
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

// writeFindings writes findings to refs.findings as a JSON array.
func writeFindings(path string, findings []Finding) error {
	data, err := json.MarshalIndent(findings, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path, data)
}

// --- Pattern sets (verbatim from commit-content-review.sh) ---

var (
	secretFlagRules = mustCompileAll(
		`AKIA[0-9A-Z]{16}`,
		`-----BEGIN\s+.*(PRIVATE KEY|CERTIFICATE)\s*-----`,
		`(postgres|mysql|redis|mongodb|amqp)://[^\s"']+@`,
		`(sk-[A-Za-z0-9_-]{20,}|sk-ant-[A-Za-z0-9_-]{20,}|pk_live_[A-Za-z0-9]{20,})`,
		`gh[pousr]_[A-Za-z0-9_]{36,}`,
	)
	secretReviewRules = mustCompileAll(
		`(?i)(api_key|apikey|api-key|token|secret|password|passwd|credentials)\s*[:=]\s*[A-Za-z0-9_/+.-]{8,}`,
	)

	piiFlagRules = mustCompileAll(
		`/Users/[A-Za-z0-9_.-]+/`,
		`/home/[A-Za-z0-9_.-]+/`,
	)
	piiReviewRules = mustCompileAll(
		`(~/|\$HOME[/\\]|%USERPROFILE%[/\\])`,
		`[A-Za-z0-9._%+/-]{1,64}@[A-Za-z0-9.-]{1,253}\.[A-Za-z]{2,}`,
		`(192\.168\.[0-9]{1,3}\.[0-9]{1,3}|10\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3})(:[0-9]+)?`,
		`localhost:[0-9]{2,5}`,
	)

	devHistoryFlagRules = mustCompileAll(
		`(?i)TODO\s*[:.-]?\s*(session|conversation|context|chat)`,
		`(?i)(<!--\s*removed\s*-->|//\s*removed|#\s*removed)`,
		`(?i)(previously this was|we tried|changed from|used to be|before this was)`,
	)
	devHistoryReviewRules = mustCompileAll(
		`console\.(log|debug|warn|error)\(`,
		`fmt\.(Println|Printf)\(\s*("|')?(DEBUG|debug|TRACE|trace|TODO|todo)`,
		`(## (WIP|Draft|DRAFT|Unreleased)|<!-- (draft|WIP) -->)`,
	)
)

func mustCompileAll(patterns ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		out = append(out, regexp.MustCompile(p))
	}
	return out
}
