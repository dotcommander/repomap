package repomap

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const maxAuditLineBytes = 1024 * 1024

// AuditSurfaceReport captures deterministic user-facing contracts that are
// useful audit entrypoints before a model starts reading source broadly.
type AuditSurfaceReport struct {
	SchemaVersion int                `json:"schema_version"`
	Root          string             `json:"root"`
	Files         []AuditSurfaceFile `json:"files"`
	Commands      []AuditSurfaceHit  `json:"commands,omitempty"`
	Flags         []AuditSurfaceHit  `json:"flags,omitempty"`
	EnvVars       []AuditSurfaceHit  `json:"env_vars,omitempty"`
	ConfigKeys    []AuditSurfaceHit  `json:"config_keys,omitempty"`
	SchemaFields  []AuditSurfaceHit  `json:"schema_fields,omitempty"`
	Routes        []AuditSurfaceHit  `json:"routes,omitempty"`
	Outputs       []AuditSurfaceHit  `json:"outputs,omitempty"`
}

// AuditSurfaceFile groups user-facing contract hits by source file.
type AuditSurfaceFile struct {
	Path  string            `json:"path"`
	Score int               `json:"score"`
	Kinds []string          `json:"kinds"`
	Hits  []AuditSurfaceHit `json:"hits"`
}

// AuditSurfaceHit is one static surface lead.
type AuditSurfaceHit struct {
	Kind     string `json:"kind"`
	Name     string `json:"name,omitempty"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Lane     string `json:"lane"`
	Evidence string `json:"evidence"`
}

// AuditEffectReport captures files with side effects and trust boundaries.
type AuditEffectReport struct {
	SchemaVersion int               `json:"schema_version"`
	Root          string            `json:"root"`
	Files         []AuditEffectFile `json:"files"`
	Kinds         []AuditEffectKind `json:"kinds"`
}

// AuditEffectFile groups side-effect leads by source file.
type AuditEffectFile struct {
	Path    string        `json:"path"`
	Score   int           `json:"score"`
	Lanes   []string      `json:"lanes"`
	Effects []AuditEffect `json:"effects"`
}

// AuditEffect is one static side-effect lead.
type AuditEffect struct {
	Kind     string `json:"kind"`
	Op       string `json:"op"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Lane     string `json:"lane"`
	Evidence string `json:"evidence"`
}

// AuditEffectKind groups files that share a side-effect kind.
type AuditEffectKind struct {
	Name    string   `json:"name"`
	Reason  string   `json:"reason"`
	Lane    string   `json:"lane"`
	Files   []string `json:"files"`
	Command string   `json:"command,omitempty"`
}

type auditStaticFile struct {
	path  string
	score int
	lines []auditLine
}

type auditLine struct {
	number int
	text   string
}

type auditPattern struct {
	kind   string
	lane   string
	weight int
	re     *regexp.Regexp
	name   func([]string) string
}

var surfacePatterns = []auditPattern{
	{kind: "command", lane: "cli-ux", weight: 8, re: regexp.MustCompile(`\bUse:\s*"([^"]+)"`), name: groupName(1)},
	{kind: "flag", lane: "cli-ux", weight: 7, re: regexp.MustCompile(`\.(?:String|StringP|StringVar|StringVarP|Bool|BoolP|BoolVar|BoolVarP|Int|IntP|IntVar|IntVarP|StringSlice|StringSliceVar)\(\s*"([^"]+)"`), name: groupName(1)},
	{kind: "flag", lane: "cli-ux", weight: 7, re: regexp.MustCompile(`\bflag\.(?:String|Bool|Int|Duration|Float64)\(\s*"([^"]+)"`), name: groupName(1)},
	{kind: "env-var", lane: "config", weight: 7, re: regexp.MustCompile(`\b(?:os\.)?(?:Getenv|LookupEnv)\(\s*"([^"]+)"`), name: groupName(1)},
	{kind: "config-key", lane: "config", weight: 5, re: regexp.MustCompile("`[^`]*(?:yaml|toml):\"([^\" ,]+)[^\"`]*\"[^`]*`"), name: groupName(1)},
	{kind: "schema-field", lane: "api-contracts", weight: 3, re: regexp.MustCompile("`[^`]*json:\"([^\" ,]+)[^\"`]*\"[^`]*`"), name: groupName(1)},
	{kind: "route", lane: "api-contracts", weight: 8, re: regexp.MustCompile(`\b(?:http\.)?(?:Handle|HandleFunc)\(\s*"([^"]+)"`), name: groupName(1)},
	{kind: "output", lane: "cli-ux", weight: 5, re: regexp.MustCompile(`\b(?:OutOrStdout|OutOrStderr|os\.Stdout|os\.Stderr|fmt\.Fprint|fmt\.Fprintf|json\.NewEncoder|Encoder\()`)},
}

var effectPatterns = []auditPattern{
	{kind: "filesystem-write", lane: "data-integrity", weight: 10, re: regexp.MustCompile(`\b(?:os\.)?(?:WriteFile|OpenFile|Create|MkdirAll|Rename|Remove|RemoveAll)\(`), name: firstToken},
	{kind: "filesystem-read", lane: "data-integrity", weight: 4, re: regexp.MustCompile(`\bos\.(?:ReadFile|Open)\(`), name: firstToken},
	{kind: "subprocess", lane: "error-handling", weight: 10, re: regexp.MustCompile(`\bexec\.Command(?:Context)?\(`), name: literalName("exec.Command")},
	{kind: "process-exit", lane: "error-handling", weight: 8, re: regexp.MustCompile(`\b(?:os\.Exit|log\.Fatal|panic)\(`), name: firstToken},
	{kind: "http", lane: "api-contracts", weight: 8, re: regexp.MustCompile(`\b(?:http\.Client|http\.NewRequest|http\.Get|http\.Post|http\.Handle|http\.HandleFunc|ListenAndServe)\b`), name: firstToken},
	{kind: "database", lane: "data-integrity", weight: 10, re: regexp.MustCompile(`\b(?:sql\.Open|QueryContext|ExecContext|BeginTx|Commit|Rollback|sqlite|pgx|database/sql)\b`), name: firstToken},
	{kind: "serialization", lane: "api-contracts", weight: 5, re: regexp.MustCompile(`\b(?:json|yaml|toml|xml)\.(?:Marshal|Unmarshal|NewEncoder|NewDecoder)\b`), name: firstToken},
	{kind: "secret", lane: "security", weight: 9, re: regexp.MustCompile(`(?i)\b(?:api[_-]?key|token|secret|password|credential)\b`), name: literalName("secret-like identifier")},
	{kind: "crypto", lane: "security", weight: 8, re: regexp.MustCompile(`\b(?:crypto/|x/crypto|bcrypt|sha256|sha512|hmac|cipher)\b`), name: firstToken},
	{kind: "time", lane: "data-integrity", weight: 3, re: regexp.MustCompile(`\btime\.(?:Now|Since|After|NewTicker|NewTimer)\(`), name: firstToken},
	{kind: "randomness", lane: "security", weight: 5, re: regexp.MustCompile(`\b(?:rand\.|crypto/rand)\b`), name: firstToken},
}

// AuditSurface extracts command, flag, env, config, route, and output surfaces.
func (m *Map) AuditSurface(ctx context.Context, limit int) (AuditSurfaceReport, error) {
	files := m.auditStaticFiles()
	report := AuditSurfaceReport{SchemaVersion: 1, Root: m.root}
	for _, file := range files {
		lines, err := readAuditLines(ctx, filepath.Join(m.root, filepath.FromSlash(file.path)))
		if err != nil {
			return AuditSurfaceReport{}, err
		}
		file.lines = lines
		hits, score := scanSurfaceFile(file)
		if len(hits) == 0 {
			continue
		}
		report.Files = append(report.Files, AuditSurfaceFile{
			Path:  file.path,
			Score: score,
			Kinds: hitKinds(hits),
			Hits:  capSurfaceHits(hits, 12),
		})
		for _, hit := range hits {
			switch hit.Kind {
			case "command":
				report.Commands = appendCapped(report.Commands, hit, 80)
			case "flag":
				report.Flags = appendCapped(report.Flags, hit, 120)
			case "env-var":
				report.EnvVars = appendCapped(report.EnvVars, hit, 120)
			case "config-key":
				report.ConfigKeys = appendCapped(report.ConfigKeys, hit, 120)
			case "schema-field":
				report.SchemaFields = appendCapped(report.SchemaFields, hit, 120)
			case "route":
				report.Routes = appendCapped(report.Routes, hit, 120)
			case "output":
				report.Outputs = appendCapped(report.Outputs, hit, 120)
			}
		}
	}
	sortSurfaceFiles(report.Files)
	if limit > 0 && len(report.Files) > limit {
		report.Files = report.Files[:limit]
	}
	return report, nil
}

// AuditEffects extracts side-effect and trust-boundary packets from source.
func (m *Map) AuditEffects(ctx context.Context, limit int) (AuditEffectReport, error) {
	files := m.auditStaticFiles()
	report := AuditEffectReport{SchemaVersion: 1, Root: m.root}
	kindFiles := map[string][]string{}
	for _, file := range files {
		lines, err := readAuditLines(ctx, filepath.Join(m.root, filepath.FromSlash(file.path)))
		if err != nil {
			return AuditEffectReport{}, err
		}
		file.lines = lines
		effects, score := scanEffectFile(file)
		if len(effects) == 0 {
			continue
		}
		lanes := effectLanes(effects)
		report.Files = append(report.Files, AuditEffectFile{
			Path:    file.path,
			Score:   score,
			Lanes:   lanes,
			Effects: capEffectHits(effects, 12),
		})
		for _, effect := range effects {
			kindFiles[effect.Kind] = append(kindFiles[effect.Kind], effect.Path)
		}
	}
	sortEffectFiles(report.Files)
	if limit > 0 && len(report.Files) > limit {
		report.Files = report.Files[:limit]
	}
	report.Kinds = buildEffectKinds(kindFiles)
	return report, nil
}

func (m *Map) auditStaticFiles() []auditStaticFile {
	m.mu.RLock()
	ranked := cloneRanked(m.ranked)
	m.mu.RUnlock()

	files := make([]auditStaticFile, 0, len(ranked))
	for _, f := range ranked {
		path := filepath.ToSlash(f.Path)
		if path == "" || isTestPath(path) {
			continue
		}
		files = append(files, auditStaticFile{path: path, score: f.Score})
	}
	return files
}

func readAuditLines(ctx context.Context, path string) ([]auditLine, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open audit source %s: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), maxAuditLineBytes)
	var lines []auditLine
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lines = append(lines, auditLine{number: len(lines) + 1, text: strings.TrimSpace(scanner.Text())})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan audit source %s: %w", path, err)
	}
	return lines, nil
}

func scanSurfaceFile(file auditStaticFile) ([]AuditSurfaceHit, int) {
	var hits []AuditSurfaceHit
	score := 0
	for _, line := range file.lines {
		for _, pattern := range surfacePatterns {
			match := pattern.re.FindStringSubmatch(line.text)
			if match == nil {
				continue
			}
			name := patternName(pattern, match)
			if name == "-" {
				continue
			}
			hits = append(hits, AuditSurfaceHit{
				Kind:     pattern.kind,
				Name:     name,
				Path:     file.path,
				Line:     line.number,
				Lane:     pattern.lane,
				Evidence: auditEvidence(line.text),
			})
			score += pattern.weight
		}
	}
	return hits, score + min(file.score/20, 10)
}

func scanEffectFile(file auditStaticFile) ([]AuditEffect, int) {
	var effects []AuditEffect
	score := 0
	for _, line := range file.lines {
		for _, pattern := range effectPatterns {
			match := pattern.re.FindStringSubmatch(line.text)
			if match == nil {
				continue
			}
			effects = append(effects, AuditEffect{
				Kind:     pattern.kind,
				Op:       patternName(pattern, match),
				Path:     file.path,
				Line:     line.number,
				Lane:     pattern.lane,
				Evidence: auditEvidence(line.text),
			})
			score += pattern.weight
		}
	}
	return effects, score + min(file.score/20, 10)
}

func patternName(pattern auditPattern, match []string) string {
	if pattern.name == nil {
		return ""
	}
	return pattern.name(match)
}

func groupName(index int) func([]string) string {
	return func(match []string) string {
		if len(match) <= index {
			return ""
		}
		return match[index]
	}
}

func literalName(name string) func([]string) string {
	return func([]string) string {
		return name
	}
}

func firstToken(match []string) string {
	if len(match) == 0 {
		return ""
	}
	token := strings.Trim(match[0], " \t\n\r({")
	if idx := strings.Index(token, "("); idx >= 0 {
		token = token[:idx]
	}
	return token
}

func auditEvidence(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= 160 {
		return text
	}
	return text[:157] + "..."
}

func hitKinds(hits []AuditSurfaceHit) []string {
	seen := map[string]bool{}
	var out []string
	for _, hit := range hits {
		if seen[hit.Kind] {
			continue
		}
		seen[hit.Kind] = true
		out = append(out, hit.Kind)
	}
	slices.Sort(out)
	return out
}

func effectLanes(effects []AuditEffect) []string {
	seen := map[string]bool{}
	var out []string
	for _, effect := range effects {
		if seen[effect.Lane] {
			continue
		}
		seen[effect.Lane] = true
		out = append(out, effect.Lane)
	}
	slices.Sort(out)
	return out
}

func appendCapped[T any](items []T, item T, cap int) []T {
	if cap > 0 && len(items) >= cap {
		return items
	}
	return append(items, item)
}

func capSurfaceHits(hits []AuditSurfaceHit, cap int) []AuditSurfaceHit {
	if cap > 0 && len(hits) > cap {
		return hits[:cap]
	}
	return hits
}

func capEffectHits(hits []AuditEffect, cap int) []AuditEffect {
	if cap > 0 && len(hits) > cap {
		return hits[:cap]
	}
	return hits
}

func sortSurfaceFiles(files []AuditSurfaceFile) {
	slices.SortFunc(files, func(a, b AuditSurfaceFile) int {
		if a.Score != b.Score {
			return b.Score - a.Score
		}
		return strings.Compare(a.Path, b.Path)
	})
}

func sortEffectFiles(files []AuditEffectFile) {
	slices.SortFunc(files, func(a, b AuditEffectFile) int {
		if a.Score != b.Score {
			return b.Score - a.Score
		}
		return strings.Compare(a.Path, b.Path)
	})
}

func buildEffectKinds(kindFiles map[string][]string) []AuditEffectKind {
	names := make([]string, 0, len(kindFiles))
	for name := range kindFiles {
		names = append(names, name)
	}
	slices.Sort(names)

	out := make([]AuditEffectKind, 0, len(names))
	for _, name := range names {
		out = append(out, AuditEffectKind{
			Name:    name,
			Reason:  effectKindReason(name),
			Lane:    effectKindLane(name),
			Files:   dedupeAndSort(kindFiles[name]),
			Command: "repomap audit effects --json",
		})
	}
	return out
}

func effectKindReason(name string) string {
	switch name {
	case "filesystem-write":
		return "writes, renames, or deletes can affect data integrity and rollback behavior"
	case "filesystem-read":
		return "file reads can affect config, import, and input validation behavior"
	case "subprocess":
		return "subprocess boundaries need timeout, stderr, and exit-code handling"
	case "process-exit":
		return "process termination paths affect cleanup and user-facing errors"
	case "http":
		return "HTTP boundaries need request, response, timeout, and contract checks"
	case "database":
		return "database calls need transaction, migration, and error-path checks"
	case "serialization":
		return "serialization boundaries define API, file, and persistence contracts"
	case "secret":
		return "secret-like identifiers need storage, logging, and config review"
	case "crypto":
		return "crypto boundaries need algorithm and key-handling review"
	case "time":
		return "time-dependent logic can affect ordering, expiry, and reproducibility"
	case "randomness":
		return "randomness can affect security, determinism, and reproducibility"
	default:
		return "static side-effect signal"
	}
}

func effectKindLane(name string) string {
	switch name {
	case "filesystem-write", "filesystem-read", "database", "time":
		return "data-integrity"
	case "subprocess", "process-exit":
		return "error-handling"
	case "http", "serialization":
		return "api-contracts"
	case "secret", "crypto", "randomness":
		return "security"
	default:
		return "best-practices"
	}
}
