package repomap

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

type taskCandidate struct {
	file      RankedFile
	evidence  []TaskEvidence
	relevance int
	fallback  bool
}

func taskCandidates(ranked []RankedFile, goal string) []taskCandidate {
	positive := []taskCandidate{}
	for _, file := range ranked {
		evidence, score := taskFieldEvidence(file, goal)
		if isTestFile(file.Path) && !taskTestEligible(file, goal, evidence) {
			continue
		}
		if score > 0 {
			positive = append(positive, taskCandidate{file: file, evidence: evidence, relevance: score})
		}
	}
	if len(positive) > 0 {
		sortTaskCandidates(positive)
		return positive
	}
	fallback := []taskCandidate{}
	for _, file := range ranked {
		if !isTestFile(file.Path) {
			fallback = append(fallback, taskCandidate{file: file, evidence: []TaskEvidence{{Field: "fallback", Value: "no positive task evidence; structural fallback"}}, fallback: true})
		}
	}
	sortTaskCandidates(fallback)
	return fallback
}
func sortTaskCandidates(values []taskCandidate) {
	slices.SortFunc(values, func(a, b taskCandidate) int {
		if a.relevance != b.relevance {
			return b.relevance - a.relevance
		}
		if a.file.Score != b.file.Score {
			return b.file.Score - a.file.Score
		}
		return strings.Compare(a.file.Path, b.file.Path)
	})
}
func taskTestEligible(file RankedFile, goal string, evidence []TaskEvidence) bool {
	lower := strings.ToLower(goal)
	if strings.Contains(lower, "test") {
		return true
	}
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(file.Path), filepath.Ext(file.Path)))
	for _, term := range taskGoalTerms(goal) {
		if strings.Contains(base, term) {
			return true
		}
		for _, symbol := range file.Symbols {
			if strings.Contains(strings.ToLower(symbol.Name), term) {
				return true
			}
		}
	}
	return false
}
func taskFieldEvidence(file RankedFile, goal string) ([]TaskEvidence, int) {
	terms := taskGoalTerms(goal)
	if len(terms) == 0 {
		return nil, 0
	}
	values := []struct {
		field, value string
		weight       int
	}{{"path", file.Path, 8}, {"package", file.Package, 5}, {"import", strings.Join(file.Imports, " "), 3}}
	for _, s := range file.Symbols {
		values = append(values, struct {
			field, value string
			weight       int
		}{"symbol", s.Name, 8}, struct {
			field, value string
			weight       int
		}{"signature", s.Signature, 4}, struct {
			field, value string
			weight       int
		}{"doc", s.Doc, 3})
	}
	seen := map[string]bool{}
	out := []TaskEvidence{}
	score := 0
	for _, value := range values {
		lower := strings.ToLower(value.value)
		matches := 0
		for _, term := range terms {
			if strings.Contains(lower, term) {
				matches++
				key := value.field + "\x00" + value.value
				if !seen[key] {
					seen[key] = true
					out = append(out, TaskEvidence{Field: value.field, Value: value.value})
				}
			}
		}
		score += value.weight * matches
	}
	return out, score
}
func taskGoalTerms(goal string) []string {
	fields := strings.FieldsFunc(strings.ToLower(goal), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != '-'
	})
	seen := map[string]bool{}
	out := []string{}
	for _, term := range fields {
		if len(term) >= 2 && !seen[term] {
			seen[term] = true
			out = append(out, term)
		}
	}
	return out
}

func taskEffectsByPath(report AuditEffectReport) map[string][]TaskEffect {
	out := map[string][]TaskEffect{}
	for _, file := range report.Files {
		for _, effect := range file.AllEffects() {
			out[file.Path] = append(out[file.Path], TaskEffect{Effect: effect, Provenance: "syntactic"})
		}
	}
	return out
}
func buildTaskTarget(m *Map, candidate taskCandidate, consumed []string, effectsByPath map[string][]TaskEffect) (TaskTarget, []TaskTruncation) {
	file := candidate.file
	ranked := m.Ranked()
	importers := impactImporters(file, ranked)
	heuristicTests := impactTests(file, ranked)
	callers := taskCallers(m.SemanticCallers(), file)
	tests := append([]string(nil), heuristicTests...)
	relationships := []TaskRelationship{}
	for _, path := range importers {
		relationships = append(relationships, TaskRelationship{Kind: "consumer", Path: path, Provenance: "syntactic"})
	}
	for _, path := range heuristicTests {
		relationships = append(relationships, TaskRelationship{Kind: "test", Path: path, Provenance: "heuristic"})
	}
	for _, caller := range callers {
		kind := "caller"
		if isTestFile(caller.File) {
			kind = "test"
			tests = append(tests, caller.File)
		}
		relationships = append(relationships, TaskRelationship{Kind: kind, Path: caller.File, Provenance: "exact"})
	}
	truncations := []TaskTruncation{}
	symbols := append([]Symbol(nil), file.Symbols...)
	slices.SortFunc(symbols, func(a, b Symbol) int {
		if a.Exported != b.Exported {
			if a.Exported {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	symbols = capTaskSymbols(symbols, 2, "targets["+file.Path+"].symbols", &truncations)
	allEffects := append(append([]TaskEffect(nil), effectsByPath[file.Path]...), taskCallSiteEffects(file)...)
	allEffects = append(allEffects, taskLexicalEffects(m, file)...)
	allEffects = dedupeTaskEffects(allEffects)
	effects := capTaskEffects(allEffects, 4, "targets["+file.Path+"].effects", &truncations)
	consumers := capTaskStrings(importers, 5, "targets["+file.Path+"].consumers", &truncations)
	tests = capTaskStrings(tests, 5, "targets["+file.Path+"].tests", &truncations)
	imports := capTaskStrings(file.Imports, 5, "targets["+file.Path+"].imports", &truncations)
	callers = capTaskLocations(callers, 5, "targets["+file.Path+"].callers", &truncations)
	relationships = capTaskRelationships(relationships, 19, "targets["+file.Path+"].relationships", &truncations)
	affected := affectedPackages(file, importers, ranked)
	target := TaskTarget{Path: file.Path, Package: file.Package, AffectedPackages: affected, Confidence: taskConfidence(candidate), Symbols: symbols, Evidence: candidate.evidence, Relationships: relationships, Consumers: consumers, Callers: callers, Tests: tests, Imports: imports, Effects: effects, Boundaries: append([]string(nil), file.Boundaries...), Risk: impactRiskLevel(file, importers, tests), Parse: file.ParseMethod, Consumed: slices.Contains(consumed, file.Path)}
	if !target.Consumed {
		target.Source = taskSource(m, file.Path, symbols, &truncations)
	}
	return target, truncations
}

func taskConfidence(candidate taskCandidate) string {
	switch {
	case candidate.fallback:
		return "fallback"
	case candidate.relevance >= 16:
		return "high"
	case candidate.relevance >= 8:
		return "medium"
	default:
		return "low"
	}
}

func taskCallSiteEffects(file RankedFile) []TaskEffect {
	var out []TaskEffect
	for _, call := range file.CallSites {
		name := strings.ToLower(call.Name)
		kind, lane := "", ""
		switch {
		case strings.HasSuffix(name, "fetch"), strings.HasSuffix(name, "axios"), strings.Contains(name, "http."):
			kind, lane = "http", "api-contracts"
		case strings.HasSuffix(name, "mail"), strings.HasSuffix(name, "sendmail"):
			kind, lane = "external-message", "data-integrity"
		case strings.HasSuffix(name, "file_put_contents"), strings.HasSuffix(name, "writefile"):
			kind, lane = "filesystem-write", "data-integrity"
		}
		if kind == "" {
			continue
		}
		out = append(out, TaskEffect{
			Effect: AuditEffect{
				Kind: kind, Op: call.Name, Path: file.Path, Line: call.Line,
				Lane: lane, Evidence: "parsed call expression",
			},
			Provenance: "heuristic",
		})
	}
	return out
}

func taskLexicalEffects(m *Map, file RankedFile) []TaskEffect {
	m.mu.RLock()
	root := m.root
	m.mu.RUnlock()
	source, err := os.Open(filepath.Join(root, filepath.FromSlash(file.Path)))
	if err != nil {
		return nil
	}
	defer func() { _ = source.Close() }()
	data, err := io.ReadAll(io.LimitReader(source, defaultMaxFileSize+1))
	if err != nil || len(data) > defaultMaxFileSize {
		return nil
	}
	var out []TaskEffect
	for index, line := range strings.Split(string(data), "\n") {
		lower := strings.ToLower(line)
		kind, op, lane := "", "", ""
		switch {
		case strings.Contains(lower, "fetch("):
			kind, op, lane = "http", "fetch", "api-contracts"
		case strings.Contains(lower, "mail("):
			kind, op, lane = "external-message", "mail", "data-integrity"
		case strings.Contains(lower, "file_put_contents("):
			kind, op, lane = "filesystem-write", "file_put_contents", "data-integrity"
		}
		if kind == "" {
			continue
		}
		out = append(out, TaskEffect{
			Effect: AuditEffect{
				Kind: kind, Op: op, Path: file.Path, Line: index + 1,
				Lane: lane, Evidence: "lexical side-effect marker",
			},
			Provenance: "heuristic",
		})
	}
	return out
}

func dedupeTaskEffects(effects []TaskEffect) []TaskEffect {
	seen := map[string]bool{}
	out := effects[:0]
	for _, effect := range effects {
		key := effect.Effect.Kind + "\x00" + effect.Effect.Path + "\x00" + strconv.Itoa(effect.Effect.Line)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, effect)
	}
	slices.SortFunc(out, func(a, b TaskEffect) int {
		if a.Effect.Line != b.Effect.Line {
			return a.Effect.Line - b.Effect.Line
		}
		return strings.Compare(a.Effect.Kind, b.Effect.Kind)
	})
	return out
}
func taskCallers(callers SymbolCallers, file RankedFile) []Location {
	seen := map[string]bool{}
	out := []Location{}
	for _, s := range file.Symbols {
		for _, loc := range callers.CallersForSymbol(file.Path, s) {
			key := loc.File + "\x00" + strconv.Itoa(loc.Line)
			if !seen[key] && loc.File != file.Path {
				seen[key] = true
				out = append(out, loc)
			}
		}
	}
	slices.SortFunc(out, func(a, b Location) int {
		if a.File != b.File {
			return strings.Compare(a.File, b.File)
		}
		return a.Line - b.Line
	})
	return out
}
func taskSource(m *Map, path string, symbols []Symbol, truncations *[]TaskTruncation) []TaskSource {
	m.mu.RLock()
	root := m.root
	m.mu.RUnlock()
	out := []TaskSource{}
	for _, s := range symbols {
		if s.Line <= 0 {
			continue
		}
		lines, truncated, _ := readSymbolSource(filepath.Join(root, filepath.FromSlash(path)), s, 60)
		if len(lines) > 0 {
			out = append(out, TaskSource{Symbol: s.Name, Lines: lines})
		}
		if truncated {
			*truncations = append(*truncations, TaskTruncation{Field: "targets[" + path + "].source[" + s.Name + "]", Shown: len(lines), Total: s.LineSpan(), Reason: "60 lines per symbol cap"})
		}
	}
	return out
}
func capTaskStrings(v []string, max int, field string, t *[]TaskTruncation) []string {
	v = dedupeTaskStrings(append([]string(nil), v...))
	if len(v) > max {
		*t = append(*t, TaskTruncation{Field: field, Shown: max, Total: len(v), Reason: "relationship cap"})
		return v[:max]
	}
	return v
}
func capTaskSymbols(v []Symbol, max int, field string, t *[]TaskTruncation) []Symbol {
	if len(v) > max {
		*t = append(*t, TaskTruncation{Field: field, Shown: max, Total: len(v), Reason: "symbol cap"})
		return v[:max]
	}
	return v
}
func capTaskEffects(v []TaskEffect, max int, field string, t *[]TaskTruncation) []TaskEffect {
	if len(v) > max {
		*t = append(*t, TaskTruncation{Field: field, Shown: max, Total: len(v), Reason: "effect cap"})
		return v[:max]
	}
	return v
}
func capTaskLocations(v []Location, max int, field string, t *[]TaskTruncation) []Location {
	if len(v) > max {
		*t = append(*t, TaskTruncation{Field: field, Shown: max, Total: len(v), Reason: "relationship cap"})
		return v[:max]
	}
	return v
}
func capTaskRelationships(v []TaskRelationship, max int, field string, t *[]TaskTruncation) []TaskRelationship {
	if len(v) > max {
		*t = append(*t, TaskTruncation{Field: field, Shown: max, Total: len(v), Reason: "relationship cap"})
		return v[:max]
	}
	return v
}
func dedupeTaskStrings(v []string) []string {
	seen := map[string]bool{}
	out := v[:0]
	for _, s := range v {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	slices.Sort(out)
	return out
}
func taskEffectString(effect TaskEffect) string {
	return fmt.Sprintf("%s %s:%d", effect.Effect.Kind, effect.Effect.Path, effect.Effect.Line)
}
