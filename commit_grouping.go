package repomap

import (
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// classifyFiles annotates each fileChange with derived metadata: language, type,
// IsConfig/IsArtifact/IsTest/IsDep flags. Call once after collectGitState so all
// downstream phases see the same classification.
func classifyFiles(files []fileChange) {
	for i := range files {
		f := &files[i]
		f.Language = LanguageFor(filepath.Ext(f.Path))
		f.IsTest = isTestFile(f.Path)
		f.IsConfig = isConfigFile(f.Path)
		f.IsArtifact = isArtifactFile(f.Path)
		f.IsDep = depManager(f.Path) != ""
		f.Type = inferType(f)
	}
}

// isConfigFile matches .md / .yaml / .yml / .json / .toml / .env* / .cfg / .ini
// / .conf — anything that flows through the BLOCKING content-review gate.
func isConfigFile(path string) bool {
	base := filepath.Base(path)
	if strings.HasPrefix(base, ".env") {
		return true
	}
	switch filepath.Ext(path) {
	case ".md", ".yaml", ".yml", ".json", ".toml", ".cfg", ".ini", ".conf":
		return true
	}
	return false
}

// isArtifactFile matches generated/session-artifact paths we'd rather gitignore
// than commit: AUDIT.md, REVIEW.md, *-output.md, *.log, *.tmp, dist/, build/.
var artifactPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^(audit|review|plan|notes|scratch)\.md$`),
	regexp.MustCompile(`(?i)-output\.md$`),
	regexp.MustCompile(`\.(log|tmp|bak|swp)$`),
	regexp.MustCompile(`^(dist|build|coverage|node_modules)/`),
}

func isArtifactFile(path string) bool {
	base := filepath.Base(path)
	for _, re := range artifactPatterns {
		if re.MatchString(base) || re.MatchString(path) {
			return true
		}
	}
	return false
}

// inferType maps a fileChange to a conventional-commit type. Priority:
// artifact > deps > test > docs/chore > feat (default).
func inferType(f *fileChange) string {
	if f.IsArtifact {
		return "artifact"
	}
	if f.IsDep {
		return "deps"
	}
	if f.IsTest {
		return "test"
	}
	if strings.HasSuffix(f.Path, ".md") {
		// README, CHANGELOG, docs/, agents/, skills/ — all docs.
		return "docs"
	}
	base := filepath.Base(f.Path)
	if base == "Makefile" || base == "Dockerfile" || strings.HasPrefix(base, ".github") {
		return "chore"
	}
	if strings.HasPrefix(f.Path, ".github/") || strings.HasPrefix(f.Path, "ci/") {
		return "chore"
	}
	if f.IsConfig {
		return "chore"
	}
	return "feat"
}

// buildGroups is the grouping entrypoint: builds the weighted edge graph,
// runs connected-components at the given threshold, and assembles CommitGroup
// records with rationale + confidence. `symbols` maps path → parsed symbols
// (may be nil if repomap couldn't parse that language).
func buildGroups(gs *gitState, symbols map[string]*FileSymbols, threshold float64) []CommitGroup {
	if len(gs.Files) == 0 {
		return nil
	}
	edges := buildEdges(gs, symbols)
	clusters := connectedComponents(gs.Files, edges, threshold)

	// Singletons are already their own clusters; the loop below turns each
	// cluster into one CommitGroup.
	groups := make([]CommitGroup, 0, len(clusters))
	for i, cluster := range clusters {
		grp := assembleGroup(cluster, gs, edges)
		grp.ID = groupID(i)
		groups = append(groups, grp)
	}
	// Deterministic order: by first-file path.
	slices.SortFunc(groups, func(a, b CommitGroup) int {
		if len(a.Files) == 0 {
			return -1
		}
		if len(b.Files) == 0 {
			return 1
		}
		return strings.Compare(a.Files[0], b.Files[0])
	})
	// Re-ID after sort so g1..gN matches display order.
	for i := range groups {
		groups[i].ID = groupID(i)
	}
	return groups
}

func groupID(i int) string {
	return "g" + strconv.Itoa(i+1)
}

// buildEdges computes weighted edges between dirty files. Edge weight is the
// max across reasons (test-pair dominates co-change, etc.).
func buildEdges(gs *gitState, symbols map[string]*FileSymbols) []edge {
	paths := make([]string, 0, len(gs.Files))
	byPath := make(map[string]*fileChange, len(gs.Files))
	for i := range gs.Files {
		f := &gs.Files[i]
		paths = append(paths, f.Path)
		byPath[f.Path] = f
	}

	// Index imports by internal package → files that import it; plus internal
	// packages → files that define them. A file imports another if their
	// ImportPaths overlap.
	pkgFiles := make(map[string][]string) // ImportPath -> paths
	fileImports := make(map[string][]string)
	for path, fs := range symbols {
		if fs == nil {
			continue
		}
		if fs.ImportPath != "" {
			pkgFiles[fs.ImportPath] = append(pkgFiles[fs.ImportPath], path)
		}
		fileImports[path] = fs.Imports
	}

	seen := make(map[string]edge) // key "A|B" with A<B
	add := func(a, b string, weight float64, reason string) {
		if a == b {
			return
		}
		if a > b {
			a, b = b, a
		}
		k := a + "|" + b
		cur, ok := seen[k]
		if !ok || weight > cur.Weight {
			seen[k] = edge{A: a, B: b, Weight: weight, Reason: reason}
		}
	}

	// Edge 1: test-pair (weight 1.0). foo.go ↔ foo_test.go, foo.ts ↔ foo.test.ts,
	// src/bar.py ↔ tests/test_bar.py.
	for _, a := range paths {
		for _, b := range paths {
			if a >= b {
				continue
			}
			if isTestPair(a, b) {
				add(a, b, 1.0, "test-pair")
			}
		}
	}

	// Edge 2: symbol-dep via import-path overlap (weight 0.8). If file A imports
	// package P and file B lives in P, they co-change logically.
	for path, imports := range fileImports {
		for _, imp := range imports {
			targets := pkgFiles[imp]
			for _, t := range targets {
				add(path, t, 0.8, "symbol-dep")
			}
		}
	}

	// Edge 3: co-change (weight 0.5). gitState.CoChange[a][b] counts co-commits
	// in the last 500. Threshold of 3 filters out incidental pairings.
	for a, inner := range gs.CoChange {
		if byPath[a] == nil {
			continue
		}
		for b, count := range inner {
			if byPath[b] == nil || count < 3 {
				continue
			}
			add(a, b, 0.5, "co-change")
		}
	}

	// Edge 4: path sibling (weight 0.3). Same directory AND same inferred type
	// — tie-breaker when nothing else links two files.
	for _, a := range paths {
		for _, b := range paths {
			if a >= b {
				continue
			}
			fa, fb := byPath[a], byPath[b]
			if filepath.Dir(a) != filepath.Dir(b) {
				continue
			}
			if fa.Type != fb.Type {
				continue
			}
			add(a, b, 0.3, "sibling")
		}
	}

	edges := make([]edge, 0, len(seen))
	for _, e := range seen {
		edges = append(edges, e)
	}
	return edges
}

// isTestPair detects canonical test/source pairings across languages.
func isTestPair(a, b string) bool {
	ax, bx := filepath.Ext(a), filepath.Ext(b)
	if ax != bx {
		// Python: src/foo.py ↔ tests/test_foo.py still shares .py.
		return false
	}
	// Normalize: one is the test, one is the source.
	testA, testB := isTestFile(a), isTestFile(b)
	if testA == testB {
		return false
	}
	test, src := a, b
	if testB {
		test, src = b, a
	}
	baseT := strings.TrimSuffix(filepath.Base(test), ax)
	baseS := strings.TrimSuffix(filepath.Base(src), ax)

	// Go: foo_test.go ↔ foo.go
	if ax == ".go" {
		return strings.TrimSuffix(baseT, "_test") == baseS
	}
	// TS/JS: foo.test.ts / foo.spec.ts ↔ foo.ts
	if ax == ".ts" || ax == ".tsx" || ax == ".js" || ax == ".jsx" {
		for _, suf := range []string{".test", ".spec"} {
			if strings.TrimSuffix(baseT, suf) == baseS {
				return true
			}
		}
	}
	// Python: test_foo.py / foo_test.py ↔ foo.py
	if ax == ".py" {
		if strings.HasPrefix(baseT, "test_") && baseT[5:] == baseS {
			return true
		}
		if strings.HasSuffix(baseT, "_test") && strings.TrimSuffix(baseT, "_test") == baseS {
			return true
		}
	}
	// Rust: tests/foo.rs ↔ src/foo.rs
	if ax == ".rs" && baseT == baseS {
		return true
	}
	return false
}

// connectedComponents runs union-find over edges above threshold and returns
// clusters (each cluster is a sorted slice of paths). All dirty files appear
// in exactly one cluster; files with no qualifying edges are singletons.
func connectedComponents(files []fileChange, edges []edge, threshold float64) [][]string {
	parent := make(map[string]string, len(files))
	for _, f := range files {
		parent[f.Path] = f.Path
	}
	var find func(string) string
	find = func(x string) string {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(a, b string) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}
	for _, e := range edges {
		if e.Weight < threshold {
			continue
		}
		if _, ok := parent[e.A]; !ok {
			continue
		}
		if _, ok := parent[e.B]; !ok {
			continue
		}
		union(e.A, e.B)
	}
	buckets := make(map[string][]string)
	for _, f := range files {
		root := find(f.Path)
		buckets[root] = append(buckets[root], f.Path)
	}
	// Deterministic cluster order: sort each cluster, then sort clusters by
	// their first element.
	clusters := make([][]string, 0, len(buckets))
	for _, paths := range buckets {
		slices.Sort(paths)
		clusters = append(clusters, paths)
	}
	slices.SortFunc(clusters, func(a, b []string) int {
		return strings.Compare(a[0], b[0])
	})
	return clusters
}

// assembleGroup picks the dominant type + scope for a cluster, drafts a
// rationale string from matching edges, and scores confidence.
func assembleGroup(paths []string, gs *gitState, edges []edge) CommitGroup {
	byPath := make(map[string]*fileChange, len(gs.Files))
	for i := range gs.Files {
		byPath[gs.Files[i].Path] = &gs.Files[i]
	}

	// Dominant type by file count (tests roll into their dominant-type partner
	// via the test-pair edge, so we count them as their source's type where possible).
	typeCounts := make(map[string]int)
	for _, p := range paths {
		if f := byPath[p]; f != nil {
			typeCounts[f.Type]++
		}
	}
	domType := dominantType(typeCounts)

	// Scope: deepest common directory among non-test files (so feat(search) not
	// feat(internal)).
	scope := commonScope(paths, byPath)

	// Rationale: list reasons that connected this cluster (distinct).
	reasons := clusterReasons(paths, edges)

	// Confidence heuristic:
	//   1.0 base
	//   -0.2 if mixed types
	//   -0.1 per 5 files beyond 3
	//   -0.15 if cluster has no non-sibling edge (singletons sometimes land here)
	//   floor at 0.3
	conf := 1.0
	if len(typeCounts) > 1 {
		conf -= 0.2
	}
	if len(paths) > 3 {
		conf -= 0.1 * float64((len(paths)-3)/5+1)
	}
	if len(paths) > 1 && !hasStrongEdge(paths, edges) {
		conf -= 0.15
	}
	if conf < 0.3 {
		conf = 0.3
	}

	rationale := "singleton"
	if len(reasons) > 0 {
		rationale = strings.Join(reasons, "; ")
	}

	return CommitGroup{
		Type:       domType,
		Scope:      scope,
		Files:      paths,
		Rationale:  rationale,
		Confidence: conf,
		// SuggestedMsg filled in by commit_messages.go.
	}
}

func dominantType(counts map[string]int) string {
	best, bestN := "chore", -1
	// Preference order when tied: feat > fix > refactor > test > docs > deps > chore > artifact.
	priority := map[string]int{
		"feat": 7, "fix": 6, "refactor": 5, "test": 4, "docs": 3, "deps": 2, "chore": 1, "artifact": 0,
	}
	for t, n := range counts {
		if n > bestN || (n == bestN && priority[t] > priority[best]) {
			best, bestN = t, n
		}
	}
	return best
}

// commonScope returns the deepest directory shared by all non-test files in
// the cluster, trimmed to a short conventional-commit scope (last segment).
func commonScope(paths []string, byPath map[string]*fileChange) string {
	var nonTest []string
	for _, p := range paths {
		if f := byPath[p]; f != nil && !f.IsTest {
			nonTest = append(nonTest, p)
		}
	}
	if len(nonTest) == 0 {
		nonTest = paths
	}
	dirs := make([]string, len(nonTest))
	for i, p := range nonTest {
		dirs[i] = filepath.Dir(p)
	}
	cp := commonDirPrefix(dirs)
	if cp == "" || cp == "." {
		return ""
	}
	// Conventional commits favor a short scope — use the deepest directory
	// segment.
	last := filepath.Base(cp)
	// Skip uninformative scopes.
	switch last {
	case ".", "/", "src", "internal", "pkg", "lib", "cmd":
		return ""
	}
	return last
}

// commonDirPrefix returns the deepest directory that prefixes every input
// directory.
func commonDirPrefix(dirs []string) string {
	if len(dirs) == 0 {
		return ""
	}
	prefix := strings.Split(dirs[0], string(filepath.Separator))
	for _, d := range dirs[1:] {
		parts := strings.Split(d, string(filepath.Separator))
		n := len(prefix)
		if len(parts) < n {
			n = len(parts)
		}
		i := 0
		for i < n && prefix[i] == parts[i] {
			i++
		}
		prefix = prefix[:i]
		if len(prefix) == 0 {
			return ""
		}
	}
	return strings.Join(prefix, string(filepath.Separator))
}

// clusterReasons returns the distinct edge reasons that connect files in the
// cluster, in a stable order.
func clusterReasons(paths []string, edges []edge) []string {
	inCluster := make(map[string]bool, len(paths))
	for _, p := range paths {
		inCluster[p] = true
	}
	seen := make(map[string]bool)
	var out []string
	order := []string{"test-pair", "symbol-dep", "co-change", "sibling"}
	for _, e := range edges {
		if inCluster[e.A] && inCluster[e.B] && !seen[e.Reason] {
			seen[e.Reason] = true
		}
	}
	for _, r := range order {
		if seen[r] {
			out = append(out, r)
		}
	}
	return out
}

// hasStrongEdge is true if any in-cluster edge has weight > 0.3 (i.e. at least
// one non-sibling reason).
func hasStrongEdge(paths []string, edges []edge) bool {
	inCluster := make(map[string]bool, len(paths))
	for _, p := range paths {
		inCluster[p] = true
	}
	for _, e := range edges {
		if inCluster[e.A] && inCluster[e.B] && e.Weight > 0.3 {
			return true
		}
	}
	return false
}
