package repomap

// commit_prep_helpers.go — Types and pure helpers for the commit prep pipeline.
// The CLI (internal/cli/commit_prep.go) imports these; keeping them in the root
// package avoids re-exporting through the cli package and keeps the logic testable
// without Cobra.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PrepPayload is the JSON document emitted by `repomap commit prep --json`.
type PrepPayload struct {
	Preflight       PrepPreflight    `json:"preflight"`
	ModeHint        string           `json:"mode_hint"` // "FULL" | "LOCAL"
	PrepToken       string           `json:"prep_token"`
	Status          string           `json:"status"` // "ready" | "needs_judgment" | "abort"
	AbortReason     string           `json:"abort_reason,omitempty"`
	Plan            []PrepPlanGroup  `json:"plan"`
	Review          []PrepReviewItem `json:"review"`
	LowConfSubjects []PrepLowConf    `json:"low_conf_subjects"`
	ReleaseRecipe   bool             `json:"release_recipe"`
	SessionRepos    []string         `json:"session_repos"`
	ReleaseGate     *PrepReleaseGate `json:"release_gate,omitempty"`
}

// PrepPreflight mirrors the cpt.md context block fields.
type PrepPreflight struct {
	Branch    string `json:"branch"`
	Working   string `json:"working"`
	Remote    string `json:"remote"`
	Unpushed  string `json:"unpushed"`
	LatestTag string `json:"latest_tag"`
	GHAuth    string `json:"gh_auth"`
}

// PrepPlanGroup is one consolidated commit in the plan.
type PrepPlanGroup struct {
	Type       string   `json:"type"`
	Scope      string   `json:"scope"`
	Subject    string   `json:"subject"`
	Files      []string `json:"files"`
	Confidence float64  `json:"confidence"`
}

// PrepReviewItem is one finding that needs LLM judgment (capped at 5; snippet ≤200 chars).
type PrepReviewItem struct {
	ID            string `json:"id"`
	File          string `json:"file"`
	Line          int    `json:"line"`
	Snippet       string `json:"snippet"`
	Detail        string `json:"detail"`
	DefaultAction string `json:"default_action"`
	ByteOffset    int    `json:"byte_offset"`
	ByteLength    int    `json:"byte_length"`
}

// PrepLowConf is one group requiring LLM subject polish (capped at 3; diff_slice ≤500 chars).
type PrepLowConf struct {
	GroupID   string   `json:"group_id"`
	Files     []string `json:"files"`
	DiffSlice string   `json:"diff_slice"`
}

// PrepReleaseGate holds the result of running the release gate.
type PrepReleaseGate struct {
	Applied []any `json:"applied"`
	BuildOK bool  `json:"build_ok"`
}

// PrepState is persisted to tmpdir and loaded by `commit finish`.
type PrepState struct {
	Analysis      *CommitAnalysis  `json:"analysis"`
	Plan          []CommitGroup    `json:"plan"`
	SessionRepos  []string         `json:"session_repos"`
	ReleaseRecipe bool             `json:"release_recipe"`
	ReleaseGate   *PrepReleaseGate `json:"release_gate,omitempty"`
	RepoRoot      string           `json:"repo_root"`
}

// LoadPrepState reads a persisted PrepState from tmpdir by token.
func LoadPrepState(token string) (*PrepState, error) {
	path := filepath.Join(os.TempDir(), "prep-"+token+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("prep state not found for token %q (looked at %s): %w", token, path, err)
	}
	var s PrepState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse prep state: %w", err)
	}
	return &s, nil
}

// PersistPrepState writes state to tmpdir and returns the prep_token.
func PersistPrepState(state *PrepState) (string, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	token := hex.EncodeToString(sum[:8])
	path := filepath.Join(os.TempDir(), "prep-"+token+".json")
	if err := atomicWriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("write state: %w", err)
	}
	return token, nil
}

// BuildReviewItems extracts REVIEW findings into PrepReviewItems, capped at maxItems.
func BuildReviewItems(findings []Finding, maxItems int) []PrepReviewItem {
	var out []PrepReviewItem
	for _, f := range findings {
		if f.DefaultAction != ActionReview {
			continue
		}
		snippet := f.Snippet
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		out = append(out, PrepReviewItem{
			ID:            fmt.Sprintf("%s:%d", f.File, f.Line),
			File:          f.File,
			Line:          f.Line,
			Snippet:       snippet,
			Detail:        f.Detail,
			DefaultAction: f.DefaultAction,
		})
		if len(out) >= maxItems {
			break
		}
	}
	return out
}

// GroupsToPlan converts CommitGroups to PrepPlanGroups for the payload.
func GroupsToPlan(groups []CommitGroup) []PrepPlanGroup {
	out := make([]PrepPlanGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, PrepPlanGroup{
			Type:       g.Type,
			Scope:      g.Scope,
			Subject:    g.SuggestedMsg,
			Files:      g.Files,
			Confidence: g.Confidence,
		})
	}
	return out
}

// LoadDiffSlice extracts the diff slice for a group's files, capped at maxChars.
func LoadDiffSlice(diffsPath string, g CommitGroup, maxChars int) string {
	if diffsPath == "" || len(g.DiffOffsets) == 0 {
		return ""
	}
	data, err := os.ReadFile(diffsPath)
	if err != nil {
		return ""
	}
	var sb strings.Builder
	for _, f := range g.Files {
		offsets, ok := g.DiffOffsets[f]
		if !ok {
			continue
		}
		start, end := offsets[0], offsets[1]
		if start < 0 || end > len(data) || start >= end {
			continue
		}
		sb.WriteString(string(data[start:end]))
		if sb.Len() >= maxChars {
			break
		}
	}
	s := sb.String()
	if len(s) > maxChars {
		s = s[:maxChars]
	}
	return s
}

// DetectSessionRepos returns repos likely touched in this session.
// Checks known companion repos; always includes repoRoot itself.
func DetectSessionRepos(repoRoot string) []string {
	repos := []string{repoRoot}
	home, err := os.UserHomeDir()
	if err != nil {
		return repos
	}
	for _, c := range []string{
		filepath.Join(home, ".claude"),
		filepath.Join(home, "go", "src", "dotcommander-plugins"),
	} {
		if c == repoRoot {
			continue
		}
		if _, err := os.Stat(filepath.Join(c, ".git")); err == nil {
			repos = append(repos, c)
		}
	}
	return repos
}

// DetectJustfileRelease returns true when a Justfile with a `release` recipe exists.
func DetectJustfileRelease(repoRoot string) bool {
	data, err := os.ReadFile(filepath.Join(repoRoot, "Justfile"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "release ") || line == "release:" {
			return true
		}
	}
	return false
}

// LoadFindings reads a findings JSON file. Returns nil, nil when absent.
func LoadFindings(path string) ([]Finding, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var findings []Finding
	if err := json.Unmarshal(data, &findings); err != nil {
		return nil, err
	}
	return findings, nil
}

// ModeHint derives FULL/LOCAL from preflight signals.
//
//	FULL  = remote present AND gh auth logged in → push + tag
//	LOCAL = anything else                        → no push, no tag
//
// Permissive on auth string format: any "logged in" substring is truthy
// unless the line also says "not logged in" (gh's own negative phrasing).
func ModeHint(p PrepPreflight) string {
	hasRemote := p.Remote != "" && p.Remote != "(none)"
	auth := strings.ToLower(p.GHAuth)
	hasAuth := strings.Contains(auth, "logged in") && !strings.Contains(auth, "not logged in")
	if hasRemote && hasAuth {
		return "FULL"
	}
	return "LOCAL"
}
