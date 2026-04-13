package repomap

// CommitAnalysis is the JSON document `repomap commit analyze` writes to stdout.
// Designed to stay under ~5KB for typical changesets — large blobs (full diffs,
// untracked content, plan text, full findings) are written to disk and
// referenced by Refs paths so the agent can read them on demand.
type CommitAnalysis struct {
	Version      int            `json:"version"`
	Tmpdir       string         `json:"tmpdir"`
	EarlyExit    bool           `json:"early_exit"`
	EarlyReason  string         `json:"early_reason,omitempty"`
	Complexity   string         `json:"complexity"` // simple | medium | complex
	Counts       CommitCounts   `json:"counts"`
	HistoryStyle string         `json:"history_style"` // conventional | mixed | freeform
	Secrets      SecretsSummary `json:"secrets"`
	Artifacts    []string       `json:"artifacts"`    // paths recommended for .gitignore
	ConfigFiles  []string       `json:"config_files"` // .md/.yaml/.toml/.json/.env*/.cfg/.ini/.conf in changeset
	DepBumps     []DepBump      `json:"dep_bumps"`
	Groups       []CommitGroup  `json:"groups"`
	PlanHash     string         `json:"plan_hash"`
	Refs         CommitRefs     `json:"refs"`
	Diagnostics  []string       `json:"diagnostics,omitempty"` // non-fatal warnings
}

// CommitCounts gives a per-status file tally.
type CommitCounts struct {
	Total     int `json:"total"`
	Staged    int `json:"staged"`
	Unstaged  int `json:"unstaged"`
	Untracked int `json:"untracked"`
}

// SecretsSummary is a compact gate for Phase 1 in the agent: if Clean is true
// and ReviewCount is zero, the agent can skip detailed LLM review.
type SecretsSummary struct {
	Clean            bool `json:"clean"`             // no FLAG findings (deterministic)
	GitleaksFindings int  `json:"gitleaks_findings"` // total flagged by gitleaks
	ReviewCount      int  `json:"review_count"`      // ambiguous findings needing LLM judgment
	FlagCount        int  `json:"flag_count"`        // deterministic findings (auto-fixable)
}

// DepBump captures a recognized dependency version change in package manifests.
type DepBump struct {
	File    string   `json:"file"`              // go.mod, package.json, plugin.json, etc.
	Manager string   `json:"manager,omitempty"` // go | npm | cargo | dc-plugin
	Changes []string `json:"changes"`           // human-readable: "name v1 -> v2"
}

// CommitGroup is one proposed commit. The agent ratifies (confidence >= 0.75)
// or inspects refs.diffs at DiffOffsets to refine grouping/messaging.
type CommitGroup struct {
	ID           string            `json:"id"`
	Type         string            `json:"type"`  // feat | fix | docs | chore | test | refactor | deps
	Scope        string            `json:"scope"` // empty for top-level changes
	SuggestedMsg string            `json:"suggested_msg"`
	Files        []string          `json:"files"`
	Rationale    string            `json:"rationale"`              // why these files cluster
	Confidence   float64           `json:"confidence"`             // 0.0–1.0
	DiffOffsets  map[string][2]int `json:"diff_offsets,omitempty"` // file -> [byte_start, byte_end] in refs.diffs
}

// CommitRefs points at on-disk side files for content too large to inline.
type CommitRefs struct {
	Plan      string `json:"plan"`      // commit-execute.sh format, ready to pipe
	Diffs     string `json:"diffs"`     // full unified diff
	Untracked string `json:"untracked"` // full content of untracked config/md files
	History   string `json:"history"`   // full git log sample
	Findings  string `json:"findings"`  // FLAG/REVIEW/CLEAR JSON array
}

// --- Internal types (not serialized to top-level JSON) ---

// fileChange is one entry from `git status --porcelain=v2`, enriched with
// language, churn, and classification.
type fileChange struct {
	Path        string // path in working tree (post-rename for renamed files)
	OldPath     string // pre-rename path; empty if not renamed
	Status      string // M | A | D | R | ? (worktree status)
	IndexStatus string // M | A | D | R | ? (index status)
	Language    string // repomap language ID; empty for non-source
	Type        string // feat | fix | docs | chore | test | deps | artifact
	Added       int    // lines added
	Removed     int    // lines removed
	IsConfig    bool   // .md/.yaml/.json/.toml/.env/.cfg/.ini/.conf
	IsArtifact  bool   // AUDIT.md, *-output.md, binaries, etc.
	IsTest      bool   // *_test.go, *.test.ts, tests/**
	IsDep       bool   // go.mod/go.sum, package.json, Cargo.toml, plugin.json, marketplace.json
}

// edge is a weighted relationship between two changed files; clustering uses
// these to build commit groups.
type edge struct {
	A, B   string  // file paths
	Weight float64 // 0.0–1.0
	Reason string  // "test-pair" | "symbol-dep" | "co-change" | "sibling"
}

// Finding is one secret/PII/dev-history hit emitted by commit_secrets.go.
type Finding struct {
	Class   string `json:"class"` // FLAG | REVIEW | CLEAR
	Kind    string `json:"kind"`  // secret | pii | dev_history | path | email | etc.
	File    string `json:"file"`
	Line    int    `json:"line,omitempty"`
	Snippet string `json:"snippet,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// symbolDelta captures additions/removals/renames detected by diffing pre- and
// post-change parser output for a file. Drives commit-message templating.
type symbolDelta struct {
	Path     string
	Added    []string // symbol names
	Removed  []string
	Modified []string // signature changed but name kept
}
