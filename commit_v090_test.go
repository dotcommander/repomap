package repomap

import (
	"context"
	"strings"
	"testing"
)

// --- D. Per-edge evidence ---

// Test_D_Evidence_MultiFile verifies that a multi-file group populates the
// Evidence array with at least one entry matching the expected edge weight.
func Test_D_Evidence_MultiFile(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	root := initTestRepo(t,
		map[string]string{
			"go.mod":          "module fixture\ngo 1.22\n",
			"pkg/foo.go":      "package pkg\nfunc Foo() {}\n",
			"pkg/foo_test.go": "package pkg\nimport \"testing\"\nfunc TestFoo(t *testing.T) {}\n",
		},
		map[string]string{
			"pkg/foo.go":      "package pkg\nfunc Foo() {}\nfunc Bar() {}\n",
			"pkg/foo_test.go": "package pkg\nimport \"testing\"\nfunc TestFoo(t *testing.T) {}\nfunc TestBar(t *testing.T) {}\n",
		},
	)
	got, err := AnalyzeCommit(context.Background(), AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatalf("AnalyzeCommit: %v", err)
	}
	// Find the group containing the test-pair.
	var group *CommitGroup
	for i := range got.Groups {
		if containsAll(got.Groups[i].Files, "pkg/foo.go", "pkg/foo_test.go") {
			group = &got.Groups[i]
			break
		}
	}
	if group == nil {
		t.Fatalf("test-pair group not found; groups=%+v", got.Groups)
	}
	if len(group.Evidence) == 0 {
		t.Fatalf("Evidence is empty for multi-file group; want at least one entry")
	}
	// At least one evidence entry should be a test-pair with weight 1.0.
	foundTestPair := false
	for _, ev := range group.Evidence {
		if ev.Reason == "test-pair" && ev.Weight == 1.0 {
			foundTestPair = true
		}
		// Both files in evidence must be valid paths.
		if ev.A == "" || ev.B == "" {
			t.Errorf("evidence entry has empty path: %+v", ev)
		}
	}
	if !foundTestPair {
		t.Errorf("no test-pair evidence with weight 1.0 found; evidence=%+v", group.Evidence)
	}
}

// Test_D_Evidence_Singleton verifies that singleton groups have empty Evidence
// (no edges connect a group of one file).
func Test_D_Evidence_Singleton(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	root := initTestRepo(t,
		map[string]string{
			"go.mod":    "module fixture\ngo 1.22\n",
			"README.md": "# readme\n",
		},
		map[string]string{
			"README.md": "# readme\n\nupdated\n",
		},
	)
	got, err := AnalyzeCommit(context.Background(), AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatalf("AnalyzeCommit: %v", err)
	}
	for _, g := range got.Groups {
		if len(g.Files) == 1 && len(g.Evidence) > 0 {
			t.Errorf("singleton group %q has non-empty evidence: %+v", g.ID, g.Evidence)
		}
	}
}

// --- A. Multi-language ImportPath derivation ---

// Test_A_ImportPath_PHP verifies PHP namespace extraction from regex parser.
func Test_A_ImportPath_PHP(t *testing.T) {
	t.Parallel()
	src := []byte("<?php\nnamespace App\\Http\\Controllers;\nclass UserController {}\n")
	ip := derivePHPNamespace(strings.Split(string(src), "\n"))
	if ip != `App\Http\Controllers` {
		t.Errorf("PHP namespace = %q, want %q", ip, `App\Http\Controllers`)
	}
}

// Test_A_ImportPath_Java verifies Java package extraction from source lines.
func Test_A_ImportPath_Java(t *testing.T) {
	t.Parallel()
	lines := []string{
		"package com.example.service;",
		"public class UserService {}",
	}
	ip := deriveJavaPackage(lines)
	if ip != "com.example.service" {
		t.Errorf("Java package = %q, want %q", ip, "com.example.service")
	}
}

// Test_A_ImportPath_Python_Script verifies that a Python file with no
// __init__.py ancestor returns "" (script file).
func Test_A_ImportPath_Python_Script(t *testing.T) {
	t.Parallel()
	// tmpDir has no __init__.py so derivation must return "".
	dir := t.TempDir()
	ip := derivePythonPackage(dir+"/script.py", dir)
	if ip != "" {
		t.Errorf("Python script ImportPath = %q, want empty", ip)
	}
}

// Test_A_ImportPath_Python_Package verifies that a Python file in a package
// (with __init__.py) gets the dotted module path.
func Test_A_ImportPath_Python_Package(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "myapp/__init__.py", "")
	writeFixture(t, root, "myapp/services/__init__.py", "")
	writeFixture(t, root, "myapp/services/user.py", "")
	ip := derivePythonPackage(root+"/myapp/services/user.py", root)
	// Should be "myapp.services" (directory containing the file, which has __init__.py)
	if ip == "" {
		t.Errorf("Python package ImportPath is empty, want non-empty dotted path")
	}
	if !strings.Contains(ip, "myapp") {
		t.Errorf("Python package ImportPath = %q, want it to contain 'myapp'", ip)
	}
}

// Test_A_ImportPath_Rust verifies Rust crate path derivation from Cargo.toml.
func Test_A_ImportPath_Rust(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "Cargo.toml", "[package]\nname = \"mylib\"\nversion = \"0.1.0\"\n")
	writeFixture(t, root, "src/lib.rs", "pub fn hello() {}")
	ip := deriveRustCratePath(root+"/src/lib.rs", root)
	if ip == "" {
		t.Errorf("Rust crate path is empty, want non-empty")
	}
	if !strings.HasPrefix(ip, "mylib") {
		t.Errorf("Rust crate path = %q, want prefix 'mylib'", ip)
	}
}

// Test_A_ImportPath_TypeScript verifies TS package-relative path derivation.
func Test_A_ImportPath_TypeScript(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "package.json", `{"name":"my-app","version":"1.0.0"}`)
	writeFixture(t, root, "src/utils/helper.ts", "export function help() {}")
	ip := deriveTSPackagePath(root+"/src/utils/helper.ts", root)
	if ip == "" {
		t.Errorf("TS package path is empty, want non-empty")
	}
	if !strings.HasPrefix(ip, "my-app") {
		t.Errorf("TS package path = %q, want prefix 'my-app'", ip)
	}
}

// Test_A_SymbolDepEdge_NonGo verifies that two PHP files where one declares
// a namespace and the other has that namespace in its import path produce a
// symbol-dep edge in buildEdges.
func Test_A_SymbolDepEdge_NonGo(t *testing.T) {
	t.Parallel()
	// Build a minimal gitState with two PHP files that share an import.
	// fileA declares "App\Services" and fileB imports it.
	fsA := &FileSymbols{
		Path:       "app/Services/UserService.php",
		Language:   "php",
		ImportPath: `App\Services`,
	}
	fsB := &FileSymbols{
		Path:     "app/Http/UserController.php",
		Language: "php",
		Imports:  []string{`App\Services`},
	}
	symbols := map[string]*FileSymbols{
		fsA.Path: fsA,
		fsB.Path: fsB,
	}
	gs := &gitState{
		Files: []fileChange{
			{Path: fsA.Path, Language: "php", Type: "feat"},
			{Path: fsB.Path, Language: "php", Type: "feat"},
		},
	}
	edges := buildEdges(gs, symbols)
	found := false
	for _, e := range edges {
		if e.Reason == "symbol-dep" {
			found = true
			if e.Weight != WeightSymbolDep {
				t.Errorf("symbol-dep weight for PHP = %v, want %v (WeightSymbolDep)", e.Weight, WeightSymbolDep)
			}
		}
	}
	if !found {
		t.Errorf("no symbol-dep edge found for PHP files with matching import paths; edges=%+v", edges)
	}
}

// Test_A_SymbolDepEdge_Go verifies Go files still get WeightSymbolDep (0.8).
func Test_A_SymbolDepEdge_Go(t *testing.T) {
	t.Parallel()
	fsA := &FileSymbols{
		Path:       "internal/db/db.go",
		Language:   "go",
		ImportPath: "github.com/example/app/internal/db",
	}
	fsB := &FileSymbols{
		Path:     "cmd/server/main.go",
		Language: "go",
		Imports:  []string{"github.com/example/app/internal/db"},
	}
	symbols := map[string]*FileSymbols{
		fsA.Path: fsA,
		fsB.Path: fsB,
	}
	gs := &gitState{
		Files: []fileChange{
			{Path: fsA.Path, Language: "go", Type: "feat"},
			{Path: fsB.Path, Language: "go", Type: "feat"},
		},
	}
	edges := buildEdges(gs, symbols)
	found := false
	for _, e := range edges {
		if e.Reason == "symbol-dep" {
			found = true
			if e.Weight != WeightSymbolDep {
				t.Errorf("symbol-dep weight for Go = %v, want %v (WeightSymbolDep)", e.Weight, WeightSymbolDep)
			}
		}
	}
	if !found {
		t.Errorf("no symbol-dep edge found for Go files with matching import paths; edges=%+v", edges)
	}
}

// --- B. Signature-aware symbol deltas ---

// Test_B_Modified_DetectedOnSigChange verifies that a function whose signature
// changes (but name stays the same) appears in Modified, not Added/Removed.
func Test_B_Modified_DetectedOnSigChange(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	root := initTestRepo(t,
		map[string]string{
			"go.mod":  "module fixture\ngo 1.22\n",
			"util.go": "package main\nfunc Process(x int) int { return x }\n",
		},
		map[string]string{
			// Signature changed: added a second parameter.
			"util.go": "package main\nfunc Process(x int, y int) int { return x + y }\n",
		},
	)
	got, err := AnalyzeCommit(context.Background(), AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatalf("AnalyzeCommit: %v", err)
	}
	// Find the group for util.go.
	for _, g := range got.Groups {
		if containsAll(g.Files, "util.go") {
			// The suggested message should mention "modify" for Process.
			if !strings.Contains(g.SuggestedMsg, "modify") {
				t.Errorf("SuggestedMsg = %q, want it to contain 'modify'", g.SuggestedMsg)
			}
			return
		}
	}
	t.Errorf("no group found for util.go; groups=%+v", got.Groups)
}

// Test_B_BulletList_WhenMoreThanThreeDeltas verifies multi-line bullet format.
func Test_B_BulletList_WhenMoreThanThreeDeltas(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	root := initTestRepo(t,
		map[string]string{
			"go.mod": "module fixture\ngo 1.22\n",
			"svc.go": "package main\nfunc A() {}\nfunc B() {}\n",
		},
		map[string]string{
			// 4 new symbols (C, D, E, F) → total=4 > 3, triggers bullet list.
			"svc.go": "package main\nfunc A() {}\nfunc B() {}\nfunc C() {}\nfunc D() {}\nfunc E() {}\nfunc F() {}\n",
		},
	)
	got, err := AnalyzeCommit(context.Background(), AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatalf("AnalyzeCommit: %v", err)
	}
	for _, g := range got.Groups {
		if containsAll(g.Files, "svc.go") {
			if !strings.Contains(g.SuggestedMsg, "\n- ") {
				t.Errorf("SuggestedMsg = %q, want bullet-list format (containing '\\n- ')", g.SuggestedMsg)
			}
			return
		}
	}
	t.Errorf("no group for svc.go; groups=%+v", got.Groups)
}

// --- C. Breaking-change detection ---

// Test_C_Breaking_ExportedRemoval verifies feat! promotion when an exported
// function is removed.
func Test_C_Breaking_ExportedRemoval(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	root := initTestRepo(t,
		map[string]string{
			"go.mod": "module fixture\ngo 1.22\n",
			"api.go": "package main\nfunc PublicAPI() {}\nfunc Helper() {}\n",
		},
		map[string]string{
			// PublicAPI removed — should trigger breaking.
			"api.go": "package main\nfunc Helper() {}\n",
		},
	)
	got, err := AnalyzeCommit(context.Background(), AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatalf("AnalyzeCommit: %v", err)
	}
	for _, g := range got.Groups {
		if containsAll(g.Files, "api.go") {
			if !g.Breaking {
				t.Errorf("group.Breaking = false, want true (exported func removed)")
			}
			if !strings.HasPrefix(g.SuggestedMsg, "feat!") && !strings.HasPrefix(g.SuggestedMsg, "fix!") {
				t.Errorf("SuggestedMsg = %q, want feat!/fix! prefix", g.SuggestedMsg)
			}
			return
		}
	}
	t.Errorf("no group for api.go; groups=%+v", got.Groups)
}

// Test_C_Breaking_UnexportedRemoval verifies that removing an unexported
// function does NOT trigger breaking.
func Test_C_Breaking_UnexportedRemoval(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	root := initTestRepo(t,
		map[string]string{
			"go.mod":    "module fixture\ngo 1.22\n",
			"intern.go": "package main\nfunc helper() {}\nfunc anotherHelper() {}\n",
		},
		map[string]string{
			// helper removed — unexported, must NOT trigger breaking.
			"intern.go": "package main\nfunc anotherHelper() {}\n",
		},
	)
	got, err := AnalyzeCommit(context.Background(), AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatalf("AnalyzeCommit: %v", err)
	}
	for _, g := range got.Groups {
		if containsAll(g.Files, "intern.go") {
			if g.Breaking {
				t.Errorf("group.Breaking = true, want false (unexported func removed)")
			}
			if strings.HasPrefix(g.SuggestedMsg, "feat!") || strings.HasPrefix(g.SuggestedMsg, "fix!") {
				t.Errorf("SuggestedMsg = %q has breaking prefix, want plain feat/fix", g.SuggestedMsg)
			}
			return
		}
	}
	t.Errorf("no group for intern.go; groups=%+v", got.Groups)
}

// Test_C_BreakingCount verifies that BreakingCount is incremented per breaking group.
func Test_C_BreakingCount(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	root := initTestRepo(t,
		map[string]string{
			"go.mod": "module fixture\ngo 1.22\n",
			"api.go": "package main\nfunc PublicAPI() {}\n",
		},
		map[string]string{
			"api.go": "package main\n// PublicAPI removed\n",
		},
	)
	got, err := AnalyzeCommit(context.Background(), AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatalf("AnalyzeCommit: %v", err)
	}
	if got.BreakingCount < 1 {
		t.Errorf("BreakingCount = %d, want >= 1", got.BreakingCount)
	}
}

// Test_EdgeWeights_ClusteringContract pins the invariant that every
// cluster-forming edge weight exceeds DefaultConfidenceCutoff, and every
// refine-only weight is below it. Bumping a weight or the cutoff in
// isolation must break this test loudly instead of silently disabling
// clustering for one edge type (as nearly happened with the pre-merge
// WeightSymbolDepDerived = 0.6 variant).
func Test_EdgeWeights_ClusteringContract(t *testing.T) {
	t.Parallel()
	clusterForming := map[string]float64{
		"test-pair":  WeightTestPair,
		"symbol-dep": WeightSymbolDep,
	}
	refineOnly := map[string]float64{
		"co-change": WeightCoChange,
		"sibling":   WeightSibling,
	}
	for name, w := range clusterForming {
		if w < DefaultConfidenceCutoff {
			t.Errorf("%s weight %v < cutoff %v — edge cannot form a cluster", name, w, DefaultConfidenceCutoff)
		}
	}
	for name, w := range refineOnly {
		if w >= DefaultConfidenceCutoff {
			t.Errorf("%s weight %v >= cutoff %v — refine-only edge would form clusters", name, w, DefaultConfidenceCutoff)
		}
	}
}
