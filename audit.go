package repomap

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// AuditIssue is a deterministic lead for a human or LLM audit pass. Issues are
// evidence, not final findings; callers should promote them only after checking
// source, docs, runtime behavior, or command output.
type AuditIssue struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
	Lane     string `json:"lane"`
	Path     string `json:"path,omitempty"`
	Evidence string `json:"evidence"`
}

// AuditHygieneReport captures git/source-discovery facts that are cheap to
// compute but easy for a model to miss, especially tracked-vs-worktree drift.
type AuditHygieneReport struct {
	SchemaVersion int          `json:"schema_version"`
	Root          string       `json:"root"`
	GitAvailable  bool         `json:"git_available"`
	Counts        AuditCounts  `json:"counts"`
	IgnoredSource []string     `json:"ignored_source,omitempty"`
	UntrackedCode []string     `json:"untracked_code,omitempty"`
	Issues        []AuditIssue `json:"issues,omitempty"`
}

// AuditCounts records the path counts behind an AuditHygieneReport.
type AuditCounts struct {
	Tracked       int `json:"tracked"`
	TrackedSource int `json:"tracked_source"`
	Untracked     int `json:"untracked"`
	UntrackedCode int `json:"untracked_code"`
	Ignored       int `json:"ignored"`
	IgnoredSource int `json:"ignored_source"`
}

// AuditRiskReport is a compact packet for selecting deep-audit lanes before
// spending model context on full source reads.
type AuditRiskReport struct {
	SchemaVersion      int             `json:"schema_version"`
	Root               string          `json:"root"`
	Files              []AuditFileRisk `json:"files"`
	FilesOmittedReason string          `json:"files_omitted_reason,omitempty"`
	Lanes              []AuditLane     `json:"lanes"`
}

// AuditFileRisk summarizes why one file deserves audit attention.
type AuditFileRisk struct {
	ID            string   `json:"id"`
	Path          string   `json:"path"`
	Language      string   `json:"language,omitempty"`
	Package       string   `json:"package,omitempty"`
	Score         int      `json:"score"`
	AuditScore    int      `json:"audit_score"`
	EvidenceClass string   `json:"evidence_class,omitempty"`
	Confidence    string   `json:"confidence,omitempty"`
	Lanes         []string `json:"lanes,omitempty"`
	Reasons       []string `json:"reasons,omitempty"`
	Caveat        string   `json:"caveat,omitempty"`
	VerifyCmd     string   `json:"verify_cmd,omitempty"`
	Boundaries    []string `json:"boundaries,omitempty"`
	ImportedBy    int      `json:"imported_by,omitempty"`
	DependsOn     int      `json:"depends_on,omitempty"`
	Symbols       []string `json:"symbols,omitempty"`
}

// AuditLane groups the files that triggered one repo-audit lane.
type AuditLane struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Reason        string   `json:"reason"`
	Files         []string `json:"files"`
	Caveat        string   `json:"caveat,omitempty"`
	Command       string   `json:"command,omitempty"`
	OmittedReason string   `json:"omitted_reason,omitempty"`
}

// AuditHygiene inspects tracked, untracked, and ignored source files. It uses
// git when available so ignored source files remain visible to release audits.
func AuditHygiene(ctx context.Context, root string) (AuditHygieneReport, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return AuditHygieneReport{}, fmt.Errorf("resolve root: %w", err)
	}

	report := AuditHygieneReport{
		SchemaVersion: 1,
		Root:          root,
	}

	tracked, err := gitList(ctx, root, "ls-files", "--cached", "-z")
	if err != nil {
		files, walkErr := scanWalk(ctx, root, nil)
		if walkErr != nil {
			return AuditHygieneReport{}, walkErr
		}
		report.Counts.TrackedSource = len(files)
		return report, nil
	}
	report.GitAvailable = true

	untracked, _ := gitList(ctx, root, "ls-files", "--others", "--exclude-standard", "-z")
	ignored, _ := gitList(ctx, root, "ls-files", "--others", "--ignored", "--exclude-standard", "-z")

	report.Counts.Tracked = len(tracked)
	report.Counts.Untracked = len(untracked)
	report.Counts.Ignored = len(ignored)

	report.Counts.TrackedSource = countSourcePaths(tracked)
	report.UntrackedCode = sourcePaths(untracked)
	report.IgnoredSource = sourcePaths(ignored)
	report.Counts.UntrackedCode = len(report.UntrackedCode)
	report.Counts.IgnoredSource = len(report.IgnoredSource)

	if len(report.IgnoredSource) > 0 {
		for _, path := range report.IgnoredSource {
			report.Issues = append(report.Issues, AuditIssue{
				ID:       "ignored_source_file",
				Severity: "high",
				Lane:     "cleanup/release",
				Path:     path,
				Evidence: "source file is ignored by git and will be absent from a tracked-only checkout",
			})
		}
	}
	if len(report.UntrackedCode) > 0 {
		for _, path := range report.UntrackedCode {
			report.Issues = append(report.Issues, AuditIssue{
				ID:       "untracked_source_file",
				Severity: "medium",
				Lane:     "cleanup/release",
				Path:     path,
				Evidence: "source file is untracked; verify whether audit/build behavior depends on local-only code",
			})
		}
	}

	return report, nil
}

// AuditRisks converts a built map into deterministic audit-lane packets.
func (m *Map) AuditRisks(limit int) AuditRiskReport {
	m.mu.RLock()
	root := m.root
	ranked := cloneRanked(m.ranked)
	m.mu.RUnlock()

	files := make([]AuditFileRisk, 0, len(ranked))
	laneFiles := map[string][]string{}
	for _, f := range ranked {
		risk := auditRiskForFile(f)
		if risk.AuditScore == 0 {
			continue
		}
		files = append(files, risk)
		for _, lane := range risk.Lanes {
			laneFiles[lane] = append(laneFiles[lane], risk.Path)
		}
	}

	slices.SortFunc(files, func(a, b AuditFileRisk) int {
		if a.AuditScore != b.AuditScore {
			return b.AuditScore - a.AuditScore
		}
		return strings.Compare(a.Path, b.Path)
	})
	totalScored := len(files)
	if limit > 0 && len(files) > limit {
		files = files[:limit]
	}

	lanes := buildAuditLanes(laneFiles)
	report := AuditRiskReport{
		SchemaVersion: 2,
		Root:          root,
		Files:         files,
		Lanes:         lanes,
	}
	if len(files) == 0 {
		report.FilesOmittedReason = "no files scored above the audit threshold"
	} else if len(files) < totalScored {
		report.FilesOmittedReason = fmt.Sprintf("showing %d of %d scored files; truncated by --limit", len(files), totalScored)
	}
	return report
}

func auditRiskForFile(f RankedFile) AuditFileRisk {
	path := filepath.ToSlash(f.Path)
	if isTestPath(path) {
		return AuditFileRisk{Path: path}
	}

	risk := AuditFileRisk{
		Path:       path,
		Language:   f.Language,
		Package:    f.Package,
		Score:      f.Score,
		Boundaries: append([]string(nil), f.Boundaries...),
		ImportedBy: f.ImportedBy,
		DependsOn:  f.DependsOn,
		Symbols:    auditSymbolNames(f.Symbols),
	}

	add := func(points int, lane, reason string) {
		risk.AuditScore += points
		risk.Lanes = appendUnique(risk.Lanes, lane)
		risk.Reasons = append(risk.Reasons, reason)
	}

	if strings.HasPrefix(path, "cmd/") || strings.HasSuffix(path, "/main.go") || path == "main.go" {
		add(20, "cli-ux", "entrypoint or command surface")
	}
	if f.ImportedBy >= 5 {
		add(15, "architecture", fmt.Sprintf("central dependency imported by %d files", f.ImportedBy))
	}
	if f.DependsOn >= 4 {
		add(10, "coupling", fmt.Sprintf("imports %d internal dependencies", f.DependsOn))
	}
	if f.Untested {
		add(10, "test-risk", "exported symbols without package test coverage")
	}
	if len(f.Boundaries) > 0 {
		for _, boundary := range f.Boundaries {
			switch boundary {
			case "HTTP", "gRPC":
				add(14, "api-contracts", "network boundary: "+boundary)
			case "Postgres", "Redis", "Kafka", "DB":
				add(14, "data-integrity", "data boundary: "+boundary)
			case "Shell":
				add(12, "error-handling", "subprocess boundary")
			case "Crypto":
				add(12, "security", "crypto boundary")
			default:
				add(8, "best-practices", "boundary: "+boundary)
			}
		}
	}
	if fileHasLargeSymbol(f.Symbols) {
		add(8, "large-functions", "symbol spans at least 80 lines")
	}
	if fileAllDead(f.Symbols) {
		add(8, "dead-code", "all exported symbols have no in-repo importers")
	}
	if f.ParseMethod != "" && f.ParseMethod != "go_ast" && f.ParseMethod != "tree_sitter" {
		add(6, "parse-fidelity", "low-fidelity parser: "+f.ParseMethod)
	}

	risk.ID = "repomap:risk:" + auditSlug(path)
	risk.Caveat = auditExternalCaveat(risk.Lanes)
	risk.EvidenceClass = auditEvidenceForLanes(risk.Lanes)
	risk.Confidence = auditConfidence(risk.EvidenceClass, risk.Caveat != "")
	risk.VerifyCmd = auditVerifyCmd(path, f.Language)

	return risk
}

func buildAuditLanes(laneFiles map[string][]string) []AuditLane {
	names := make([]string, 0, len(laneFiles))
	for name := range laneFiles {
		names = append(names, name)
	}
	slices.Sort(names)

	lanes := make([]AuditLane, 0, len(names))
	for _, name := range names {
		files := dedupeAndSort(laneFiles[name])
		lanes = append(lanes, AuditLane{
			ID:      "repomap:lane:" + auditSlug(name),
			Name:    name,
			Reason:  auditLaneReason(name),
			Files:   files,
			Caveat:  auditExternalCaveat([]string{name}),
			Command: auditLaneCommand(name),
		})
	}
	return lanes
}

func auditLaneReason(name string) string {
	switch name {
	case "api-contracts":
		return "HTTP/gRPC or schema-facing code needs docs/runtime contract checks"
	case "architecture":
		return "central files have broad blast radius"
	case "cli-ux":
		return "command entrypoints and user-visible flags/output need smoke checks"
	case "cleanup/release":
		return "git hygiene can change what ships or builds from a clean checkout"
	case "data-integrity":
		return "database, queue, or persistence boundaries need correctness checks"
	case "dead-code":
		return "orphaned exported symbols need caller and API-intent checks"
	case "coupling":
		return "high fan-out files need package-boundary and change-amplification checks"
	case "dependency-policy":
		return "dependency manifests need policy, vulnerability, and replacement checks"
	case "error-handling":
		return "subprocess or failure boundaries need actionable errors and cleanup checks"
	case "large-functions":
		return "large symbols are harder to review and change safely"
	case "lifecycle-concurrency":
		return "goroutines and cancellation boundaries need lifecycle checks"
	case "parse-fidelity":
		return "low-fidelity parser output can hide symbols and audit surface"
	case "performance":
		return "unbounded reads and hot-path allocations need resource-bound checks"
	case "security":
		return "security-sensitive boundaries need explicit review before promotion"
	case "test-risk":
		return "public behavior without detected test coverage needs verification checks"
	default:
		return "deterministic rank or boundary signal"
	}
}

func auditLaneCommand(name string) string {
	switch name {
	case "cleanup/release":
		return "repomap audit hygiene --json"
	case "cli-ux":
		return "repomap audit risks --json --limit 20"
	case "dependency-policy":
		return "repomap audit surface --json"
	default:
		return "repomap audit risks --json"
	}
}

func auditSymbolNames(symbols []Symbol) []string {
	if len(symbols) == 0 {
		return nil
	}
	out := make([]string, 0, min(len(symbols), 8))
	for _, s := range symbols {
		if !s.Exported {
			continue
		}
		name := s.Name
		if s.Receiver != "" {
			name = s.Receiver + "." + s.Name
		}
		out = append(out, name)
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func fileHasLargeSymbol(symbols []Symbol) bool {
	for _, s := range symbols {
		if s.LineSpan() >= 80 {
			return true
		}
	}
	return false
}

func gitList(ctx context.Context, root string, args ...string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return splitNUL(out.String()), nil
}

func sourcePaths(paths []string) []string {
	var out []string
	for _, path := range paths {
		if isSourcePath(path) {
			out = append(out, filepath.ToSlash(path))
		}
	}
	slices.Sort(out)
	return out
}

func countSourcePaths(paths []string) int {
	return len(sourcePaths(paths))
}

func isSourcePath(path string) bool {
	return LanguageFor(filepath.Ext(path)) != "" && !isBuildArtifact(path)
}

func isTestPath(path string) bool {
	base := filepath.Base(path)
	return strings.Contains(base, "_test.") ||
		strings.Contains(base, ".test.") ||
		strings.Contains(base, ".spec.") ||
		strings.HasPrefix(base, "test_")
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func dedupeAndSort(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	slices.Sort(out)
	return out
}

// Evidence classes for audit packets. gopls_caller is reserved for a future
// caller-graph signal; the deterministic audit producers do not emit it today.
const (
	auditEvidenceImportGraph = "import_graph"
	auditEvidenceAST         = "ast"
	auditEvidenceGitHistory  = "git_history"
	auditEvidenceHeuristic   = "heuristic"
)

// auditLaneEvidence maps an audit lane to the class of signal that produced it.
// Lanes absent here fall back to heuristic.
var auditLaneEvidence = map[string]string{
	"architecture":          auditEvidenceImportGraph,
	"coupling":              auditEvidenceImportGraph,
	"dead-code":             auditEvidenceImportGraph,
	"test-risk":             auditEvidenceAST,
	"large-functions":       auditEvidenceAST,
	"cleanup/release":       auditEvidenceGitHistory,
	"cli-ux":                auditEvidenceHeuristic,
	"api-contracts":         auditEvidenceHeuristic,
	"data-integrity":        auditEvidenceHeuristic,
	"security":              auditEvidenceHeuristic,
	"error-handling":        auditEvidenceHeuristic,
	"best-practices":        auditEvidenceHeuristic,
	"parse-fidelity":        auditEvidenceHeuristic,
	"lifecycle-concurrency": auditEvidenceHeuristic,
	"performance":           auditEvidenceHeuristic,
	"config":                auditEvidenceHeuristic,
	"dependency-policy":     auditEvidenceHeuristic,
}

// auditEvidencePrecedence ranks classes when a packet mixes signals; the
// strongest evidence wins.
var auditEvidencePrecedence = map[string]int{
	auditEvidenceImportGraph: 3,
	auditEvidenceAST:         2,
	auditEvidenceGitHistory:  1,
	auditEvidenceHeuristic:   0,
}

// auditSlug renders a path or name into a stable, readable id fragment:
// lowercased, with every run of non-alphanumeric characters collapsed to a
// single dash and surrounding dashes trimmed.
func auditSlug(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// auditEvidenceForLanes picks the strongest evidence class across a file's lanes.
func auditEvidenceForLanes(lanes []string) string {
	best := auditEvidenceHeuristic
	bestRank := -1
	for _, lane := range lanes {
		class, ok := auditLaneEvidence[lane]
		if !ok {
			class = auditEvidenceHeuristic
		}
		if r := auditEvidencePrecedence[class]; r > bestRank {
			bestRank = r
			best = class
		}
	}
	return best
}

// auditConfidence derives a confidence tier from an evidence class. Packets
// carrying an external-consumer caveat are capped at low: they cannot see
// callers outside this repo.
func auditConfidence(class string, hasCaveat bool) string {
	if hasCaveat {
		return "low"
	}
	switch class {
	case auditEvidenceImportGraph, auditEvidenceAST, auditEvidenceGitHistory:
		return "high"
	default:
		return "medium"
	}
}

// auditExternalCaveat returns a caveat for lanes whose signal cannot see
// out-of-repo callers (dead-code/orphan, untested exports). Empty otherwise.
func auditExternalCaveat(lanes []string) string {
	for _, lane := range lanes {
		switch lane {
		case "dead-code":
			return "may be referenced by external consumers; repomap sees only in-repo importers"
		case "test-risk":
			return "may be exercised by external or integration tests outside this repo"
		}
	}
	return ""
}

// auditVerifyCmd suggests the next command to run for a file. Go files get a
// package-scoped test command; other languages get none.
func auditVerifyCmd(path, language string) string {
	if language != "go" {
		return ""
	}
	i := strings.LastIndex(path, "/")
	if i < 0 {
		return "go test ./..."
	}
	return "go test ./" + path[:i] + "/..."
}
