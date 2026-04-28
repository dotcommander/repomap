package repomap

// CommitAnalysis is the JSON document `repomap commit analyze` writes to stdout.
// Designed to stay under ~5KB for typical changesets — large blobs (full diffs,
// untracked content, plan text, full findings) are written to disk and
// referenced by Refs paths so the agent can read them on demand.
type CommitAnalysis struct {
	Version        int            `json:"version"`
	Tmpdir         string         `json:"tmpdir"`
	EarlyExit      bool           `json:"early_exit"`
	EarlyReason    string         `json:"early_reason,omitempty"`
	Complexity     string         `json:"complexity"` // simple | medium | complex
	Counts         CommitCounts   `json:"counts"`
	HistoryStyle   string         `json:"history_style"`             // conventional | mixed | freeform
	LatestTag      string         `json:"latest_tag,omitempty"`      // most recent semver tag; empty if none
	RecentSubjects []string       `json:"recent_subjects,omitempty"` // top-5 commit subjects from HEAD (style sample)
	Remote         RemoteInfo     `json:"remote"`                    // origin URL + visibility class; drives finding default_action
	Secrets        SecretsSummary `json:"secrets"`
	Artifacts      []string       `json:"artifacts"`    // paths recommended for .gitignore
	ConfigFiles    []string       `json:"config_files"` // .md/.yaml/.toml/.json/.env*/.cfg/.ini/.conf in changeset
	DepBumps       []DepBump      `json:"dep_bumps"`
	Groups         []CommitGroup  `json:"groups"`
	BreakingCount  int            `json:"breaking_count,omitempty"` // count of groups with Breaking=true
	PlanHash       string         `json:"plan_hash"`
	Refs           CommitRefs     `json:"refs"`
	Diagnostics    []string       `json:"diagnostics,omitempty"` // non-fatal warnings
}

// CommitCounts gives a per-status file tally.
type CommitCounts struct {
	Total     int `json:"total"`
	Staged    int `json:"staged"`
	Unstaged  int `json:"unstaged"`
	Untracked int `json:"untracked"`
}

// SecretsSummary is a compact gate for Phase 1 in the agent: if Clean is true
// and AmbiguousCount is zero, the agent can skip LLM review entirely —
// default_action handles every finding deterministically.
type SecretsSummary struct {
	Clean            bool `json:"clean"`             // no FLAG findings (deterministic)
	GitleaksFindings int  `json:"gitleaks_findings"` // total flagged by gitleaks
	ReviewCount      int  `json:"review_count"`      // findings originally classed REVIEW (pre-adjudication)
	FlagCount        int  `json:"flag_count"`        // deterministic findings (auto-fixable)
	FixCount         int  `json:"fix_count"`         // findings with default_action=fix (auto-handled)
	SafeCount        int  `json:"safe_count"`        // findings with default_action=safe (auto-cleared)
	AmbiguousCount   int  `json:"ambiguous_count"`   // findings with default_action=review (need LLM judgment)
}

// RemoteInfo records the origin remote and its visibility class. Used to pick
// the strictness of default_action on REVIEW findings: a personal repo with no
// remote gets lenient defaults; github.com/* public repos get strict defaults.
type RemoteInfo struct {
	OriginURL  string `json:"origin_url,omitempty"`
	Visibility string `json:"visibility"` // public | private | none | unknown
}

// DepBump captures a recognized dependency version change in package manifests.
type DepBump struct {
	File    string   `json:"file"`              // go.mod, package.json, plugin.json, etc.
	Manager string   `json:"manager,omitempty"` // go | npm | cargo | dc-plugin
	Changes []string `json:"changes"`           // human-readable: "name v1 -> v2"
}

// EdgeEvidence records one weighted edge that contributed to a group's
// clustering, so agents can inspect or override the grouping logic without
// pattern-matching on the Rationale string.
type EdgeEvidence struct {
	A      string  `json:"a"`      // first file path
	B      string  `json:"b"`      // second file path
	Weight float64 `json:"weight"` // edge weight (1.0 test-pair, 0.8/0.6 symbol-dep, 0.5 co-change, 0.3 sibling)
	Reason string  `json:"reason"` // "test-pair" | "symbol-dep" | "co-change" | "sibling"
}

// CommitGroup is one proposed commit. The agent ratifies (confidence >= 0.75)
// or inspects refs.diffs at DiffOffsets to refine grouping/messaging.
type CommitGroup struct {
	ID           string            `json:"id"`
	Type         string            `json:"type"`           // feat | fix | docs | chore | test | refactor | deps
	Scope        string            `json:"scope"`          // empty for top-level changes
	Verb         string            `json:"verb,omitempty"` // "add" | "remove" | "update" — dominant git operation
	SuggestedMsg string            `json:"suggested_msg"`
	Files        []string          `json:"files"`
	Rationale    string            `json:"rationale"`              // why these files cluster (backward-compat string)
	Confidence   float64           `json:"confidence"`             // 0.0–1.0
	Breaking     bool              `json:"breaking,omitempty"`     // true if any constituent file has a breaking-change delta
	DiffOffsets  map[string][2]int `json:"diff_offsets,omitempty"` // file -> [byte_start, byte_end] in refs.diffs
	Evidence     []EdgeEvidence    `json:"evidence,omitempty"`     // per-edge clustering evidence
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

// Prep status values emitted in PrepPayload.Status.
const (
	PrepStatusReady         = "ready"
	PrepStatusNeedsJudgment = "needs_judgment"
	PrepStatusAbort         = "abort"
)

// Review verdict values emitted in ReviewDecision.Verdict.
const (
	VerdictSafe   = "safe"
	VerdictUnsafe = "unsafe"
)

// DefaultAction values emitted in Finding.DefaultAction.
const (
	ActionFix    = "fix"
	ActionSafe   = "safe"
	ActionReview = "review"
)

// Finding is one secret/PII/dev-history hit emitted by commit_secrets.go.
// DefaultAction is the deterministic adjudication the agent should follow
// unless it has a reason to override — it collapses the per-finding LLM loop
// that previously re-examined every REVIEW hit.
type Finding struct {
	Class         string `json:"class"` // FLAG | REVIEW | CLEAR
	Kind          string `json:"kind"`  // secret | pii | dev_history | path | email | etc.
	File          string `json:"file"`
	Line          int    `json:"line,omitempty"`
	Snippet       string `json:"snippet,omitempty"`
	Detail        string `json:"detail,omitempty"`
	DefaultAction string `json:"default_action"` // fix | safe | review
}

// symbolDelta captures additions/removals/renames detected by diffing pre- and
// post-change parser output for a file. Drives commit-message templating.
type symbolDelta struct {
	Path     string
	Added    []string // symbol names
	Removed  []string
	Modified []string // signature changed but name kept
	Breaking bool     // true if any removed/modified symbol was publicly exported
}
