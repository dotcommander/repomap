package repomap

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultConfidenceCutoff is the minimum per-edge weight required to form
// a cluster. Set slightly below WeightSymbolDep so that symbol-dep is the
// lowest cluster-forming edge type; co-change (0.5) and sibling (0.3) are
// below the cutoff by design — they refine an already-clustered group but
// never pull files together on their own.
//
// Invariant enforced by Test_EdgeWeights_ClusteringContract: every
// cluster-forming weight (test-pair, symbol-dep) must exceed this cutoff;
// every refine-only weight (co-change, sibling) must be below it.
const DefaultConfidenceCutoff = WeightSymbolDep - 0.05

// AnalyzeOptions configures a commit-analyze run.
type AnalyzeOptions struct {
	Root             string  // repo root (usually ".")
	Tag              bool    // --tag: activate release gate (go.mod bump before commit)
	ConfidenceCutoff float64 // default 0.75
	Tmpdir           string  // override temp directory (tests)
}

// AnalyzeCommit is the public entrypoint called by the CLI. Collects git
// state, parses all dirty files, runs grouping + messages + secrets, writes
// side files, returns the CommitAnalysis ready for JSON emit.
func AnalyzeCommit(ctx context.Context, opts AnalyzeOptions) (*CommitAnalysis, error) {
	root := opts.Root
	if root == "" {
		root = "."
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve root: %w", err)
	}
	root = abs
	if opts.ConfidenceCutoff == 0 {
		opts.ConfidenceCutoff = DefaultConfidenceCutoff
	}

	tmpdir := opts.Tmpdir
	if tmpdir == "" {
		tmpdir, err = makeTmpdir()
		if err != nil {
			return nil, fmt.Errorf("tmpdir: %w", err)
		}
	} else if err := os.MkdirAll(tmpdir, 0o700); err != nil {
		return nil, fmt.Errorf("tmpdir mkdir: %w", err)
	}

	gs, err := collectGitState(ctx, root)
	if err != nil {
		return nil, err
	}
	classifyFiles(gs.Files)

	refs := CommitRefs{
		Plan:      filepath.Join(tmpdir, "plan.txt"),
		Diffs:     filepath.Join(tmpdir, "diffs.patch"),
		Untracked: filepath.Join(tmpdir, "untracked.txt"),
		History:   filepath.Join(tmpdir, "history.txt"),
		Findings:  filepath.Join(tmpdir, "findings.json"),
	}

	// Early-exit: nothing to commit.
	if len(gs.Files) == 0 {
		return &CommitAnalysis{
			Version:     1,
			Tmpdir:      tmpdir,
			EarlyExit:   true,
			EarlyReason: "no changes in working tree",
			Complexity:  "simple",
			Remote:      RemoteInfo{OriginURL: gs.OriginURL, Visibility: gs.Visibility},
			Secrets:     SecretsSummary{Clean: true},
			Refs:        refs,
		}, nil
	}

	// Write reference files in parallel-safe order. History is always useful;
	// diffs + untracked are written unconditionally so agents can read on demand.
	if err := writeFile(refs.History, []byte(gs.HistoryRaw)); err != nil {
		return nil, fmt.Errorf("write history: %w", err)
	}
	if err := collectFullDiff(ctx, root, refs.Diffs); err != nil {
		return nil, fmt.Errorf("write diffs: %w", err)
	}
	if err := untrackedConfigContent(ctx, root, gs.Files, refs.Untracked); err != nil {
		return nil, fmt.Errorf("write untracked: %w", err)
	}

	// Parse all dirty source files (skipping deletions). repomap's parser
	// dispatch handles language routing.
	postSymbols := parseDirtyFiles(root, gs.Files)

	// Symbol deltas for message generation.
	deltas := computeSymbolDeltas(ctx, root, gs.Files, postSymbols)

	// Dep bumps.
	bumps := detectDepBumps(ctx, root, gs.Files)

	// Secrets scan. Visibility is passed in so findings get a deterministic
	// default_action that the agent can act on without per-finding LLM calls.
	findings, summary := scanSecrets(ctx, root, gs.Files, gs.Visibility)
	if err := writeFindings(refs.Findings, findings); err != nil {
		return nil, fmt.Errorf("write findings: %w", err)
	}

	// Groups.
	groups := buildGroups(gs, postSymbols, opts.ConfidenceCutoff)
	suggestMessages(groups, gs, deltas, bumps)

	// Count groups flagged as breaking changes.
	breakingCount := 0
	for _, g := range groups {
		if g.Breaking {
			breakingCount++
		}
	}

	// Config files / artifacts / history style (top-level summary fields).
	configFiles := collectConfigFiles(gs.Files)
	artifacts := collectArtifacts(gs.Files)
	histStyle := classifyHistoryStyle(gs.HistoryRaw)

	// Surface tag + recent subjects so the agent never needs to re-run
	// `git log` / `git tag` just for style inference or version lookup.
	var latestTag string
	if len(gs.Tags) > 0 {
		latestTag = gs.Tags[0]
	}
	recentSubjects := splitLines(gs.HistoryRaw)
	if len(recentSubjects) > 5 {
		recentSubjects = recentSubjects[:5]
	}

	// Plan file.
	plan := renderPlan(groups)
	if err := writeFile(refs.Plan, []byte(plan)); err != nil {
		return nil, fmt.Errorf("write plan: %w", err)
	}

	analysis := &CommitAnalysis{
		Version:        1,
		Tmpdir:         tmpdir,
		EarlyExit:      false,
		Complexity:     classifyComplexity(gs.Files, groups),
		Counts:         countFiles(gs.Files),
		HistoryStyle:   histStyle,
		LatestTag:      latestTag,
		RecentSubjects: recentSubjects,
		Remote:         RemoteInfo{OriginURL: gs.OriginURL, Visibility: gs.Visibility},
		Secrets:        summary,
		Artifacts:      artifacts,
		ConfigFiles:    configFiles,
		DepBumps:       bumps,
		Groups:         groups,
		BreakingCount:  breakingCount,
		PlanHash:       planHash(plan),
		Refs:           refs,
	}
	return analysis, nil
}

// parseDirtyFiles runs repomap's language-appropriate parser on each dirty
// source file and returns path → symbols. Deletions and unknown languages
// are skipped.
func parseDirtyFiles(root string, files []fileChange) map[string]*FileSymbols {
	bl, _ := LoadBlocklistConfig(root)
	out := make(map[string]*FileSymbols, len(files))
	for _, f := range files {
		if f.Status == "D" || f.IndexStatus == "D" {
			continue
		}
		if f.Language == "" {
			continue
		}
		abs := filepath.Join(root, f.Path)
		if f.Language == "go" {
			if sym, err := ParseGoFile(abs, root); err == nil && sym != nil {
				bl.filterSymbols(sym)
				out[f.Path] = sym
			}
			continue
		}
		if sym := parseNonGoFile(abs, root, f.Language); sym != nil {
			bl.filterSymbols(sym)
			out[f.Path] = sym
		}
	}
	return out
}

// countFiles returns the per-status tally.
func countFiles(files []fileChange) CommitCounts {
	var c CommitCounts
	for _, f := range files {
		c.Total++
		switch {
		case f.Status == "?":
			c.Untracked++
		case f.IndexStatus != "." && f.IndexStatus != " ":
			c.Staged++
		default:
			c.Unstaged++
		}
	}
	return c
}

// collectConfigFiles returns paths of all .md/.yaml/.json/.toml/.env/.cfg/.ini
// files in the changeset — the set that BLOCKING content review runs over.
func collectConfigFiles(files []fileChange) []string {
	var out []string
	for _, f := range files {
		if f.IsConfig {
			out = append(out, f.Path)
		}
	}
	return out
}

// collectArtifacts returns paths that match artifact patterns (candidates for
// gitignore rather than commit).
func collectArtifacts(files []fileChange) []string {
	var out []string
	for _, f := range files {
		if f.IsArtifact {
			out = append(out, f.Path)
		}
	}
	return out
}

// classifyHistoryStyle inspects the last 20 commit subjects and returns
// "conventional" | "mixed" | "freeform". ≥80% conventional → conventional,
// ≥20% conventional → mixed, else freeform.
func classifyHistoryStyle(raw string) string {
	lines := splitLines(raw)
	if len(lines) == 0 {
		return "freeform"
	}
	conv := 0
	for _, line := range lines {
		if isConventional(line) {
			conv++
		}
	}
	ratio := float64(conv) / float64(len(lines))
	switch {
	case ratio >= 0.8:
		return "conventional"
	case ratio >= 0.2:
		return "mixed"
	default:
		return "freeform"
	}
}

// isConventional is a permissive "type(scope): subject" matcher.
func isConventional(subject string) bool {
	colon := strings.Index(subject, ":")
	if colon <= 0 || colon >= len(subject)-1 {
		return false
	}
	head := subject[:colon]
	// Strip optional (scope) and breaking-change marker (!).
	if paren := strings.Index(head, "("); paren > 0 {
		head = head[:paren]
	}
	head = strings.TrimRight(head, "!")
	switch strings.TrimSpace(head) {
	case "feat", "fix", "docs", "chore", "test", "refactor", "perf", "style",
		"ci", "build", "revert", "deps":
		return true
	}
	return false
}

// classifyComplexity is a rough heuristic used to tell the agent how much
// care is warranted: simple | medium | complex.
func classifyComplexity(files []fileChange, groups []CommitGroup) string {
	switch {
	case len(files) <= 3 && len(groups) <= 1:
		return "simple"
	case len(files) <= 15 && len(groups) <= 4:
		return "medium"
	default:
		return "complex"
	}
}

// makeTmpdir returns a per-run directory under $TMPDIR with an 8-byte random suffix.
func makeTmpdir() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	name := "commit-" + hex.EncodeToString(buf[:])
	dir := filepath.Join(os.TempDir(), name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// renderPlan emits commit-execute.sh's native format:
//
//	COMMIT
//	MSG: feat(scope): subject
//	FILES: path1
//	FILES: path2
//	COMMIT
//	...
//	END
//
// Release gate (--tag + go.mod bump) is left to the agent to apply before
// invoking commit-execute.sh; the tool reports it via dep_bumps, not the plan.
func renderPlan(groups []CommitGroup) string {
	var b strings.Builder
	for _, g := range groups {
		b.WriteString("COMMIT\n")
		fmt.Fprintf(&b, "MSG: %s\n", g.SuggestedMsg)
		for _, f := range g.Files {
			fmt.Fprintf(&b, "FILES: %s\n", f)
		}
	}
	b.WriteString("END\n")
	return b.String()
}

func planHash(plan string) string {
	sum := sha256.Sum256([]byte(plan))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// EncodeJSON serializes a CommitAnalysis for stdout. Compact by default; set
// pretty=true for human inspection.
func EncodeJSON(a *CommitAnalysis, pretty bool) ([]byte, error) {
	if pretty {
		return json.MarshalIndent(a, "", "  ")
	}
	return json.Marshal(a)
}
