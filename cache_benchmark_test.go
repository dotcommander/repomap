package repomap

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/trace"
	"strings"
	"testing"
)

var benchmarkCacheStartupResult *Map

// BenchmarkCacheStartupCleanCold measures a cache-disabled startup against an
// isolated clean clone of a generated medium-sized repository.
func BenchmarkCacheStartupCleanCold(b *testing.B) {
	repo := cacheStartupBenchmarkClone(b, cacheStartupBenchmarkSource(b))
	cacheStartupBenchmark(b, repo, "")
}

// BenchmarkCacheStartupCleanDiskWarm measures a startup that hydrates an
// unchanged cache from disk.
func BenchmarkCacheStartupCleanDiskWarm(b *testing.B) {
	repo := cacheStartupBenchmarkClone(b, cacheStartupBenchmarkSource(b))
	cacheDir := b.TempDir()
	cacheStartupBenchmarkSeed(b, repo, cacheDir)
	cacheStartupBenchmark(b, repo, cacheDir)
}

// BenchmarkCacheStartupDirtyCold measures a cache-disabled startup after an
// uncommitted tracked TypeScript change.
func BenchmarkCacheStartupDirtyCold(b *testing.B) {
	repo := cacheStartupBenchmarkClone(b, cacheStartupBenchmarkSource(b))
	cacheStartupBenchmarkDirty(b, repo)
	cacheStartupBenchmark(b, repo, "")
}

// BenchmarkCacheStartupDirtyUnchangedDiskWarm measures the incremental
// disk-cache path when the worktree is dirty but unchanged since cache seeding.
func BenchmarkCacheStartupDirtyUnchangedDiskWarm(b *testing.B) {
	repo := cacheStartupBenchmarkClone(b, cacheStartupBenchmarkSource(b))
	cacheStartupBenchmarkDirty(b, repo)
	cacheDir := b.TempDir()
	cacheStartupBenchmarkSeed(b, repo, cacheDir)
	cacheStartupBenchmark(b, repo, cacheDir)
}

func cacheStartupBenchmarkSource(b *testing.B) string {
	b.Helper()

	repo := filepath.Join(b.TempDir(), "source")
	cacheStartupBenchmarkWriteFile(b, filepath.Join(repo, "go.mod"), "module example.com/cache-startup-benchmark\n\ngo 1.26\n")

	for pkg := range 6 {
		for file := range 10 {
			name := fmt.Sprintf("Record%02d%02d", pkg, file)
			source := fmt.Sprintf(`package p%02d

type %s struct {
	ID int
}

func New%s(id int) %s {
	return %s{ID: id}
}

func (r %s) Next() int {
	return r.ID + 1
}
`, pkg, name, name, name, name, name)
			path := filepath.Join(repo, fmt.Sprintf("pkg%02d", pkg), fmt.Sprintf("file%02d.go", file))
			cacheStartupBenchmarkWriteFile(b, path, source)
		}
	}

	cacheStartupBenchmarkWriteFile(b, filepath.Join(repo, "web", "dashboard.ts"), `export function dashboardName(): string {
	return "cache-startup";
}
`)
	cacheStartupBenchmarkGit(b, repo, "init")
	cacheStartupBenchmarkGit(b, repo, "add", ".")
	cacheStartupBenchmarkGit(b, repo, "commit", "-m", "initial fixture")
	return repo
}

func cacheStartupBenchmarkClone(b *testing.B, source string) string {
	b.Helper()

	clone := filepath.Join(b.TempDir(), "clone")
	cacheStartupBenchmarkGit(b, "", "clone", "--quiet", source, clone)
	return clone
}

func cacheStartupBenchmarkDirty(b *testing.B, repo string) {
	b.Helper()

	path := filepath.Join(repo, "web", "dashboard.ts")
	data, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	cacheStartupBenchmarkWriteFile(b, path, string(data)+"\nexport const dirtyCacheStartup = true;\n")
}

func cacheStartupBenchmarkSeed(b *testing.B, repo, cacheDir string) {
	b.Helper()

	m := New(repo, DefaultConfig())
	m.SetCacheDir(cacheDir)
	if err := m.Build(context.Background()); err != nil {
		b.Fatal(err)
	}
	if !New(repo, DefaultConfig()).LoadCache(cacheDir) {
		b.Fatal("seeded cache is not loadable")
	}
}

func cacheStartupBenchmark(b *testing.B, repo, cacheDir string) {
	b.Helper()
	b.ResetTimer()
	for b.Loop() {
		trace.WithRegion(b.Context(), "cache-startup-build", func() {
			m := New(repo, DefaultConfig())
			if cacheDir != "" {
				m.SetCacheDir(cacheDir)
			}
			if err := m.Build(context.Background()); err != nil {
				b.Fatal(err)
			}
			benchmarkCacheStartupResult = m
		})
	}
}

func cacheStartupBenchmarkWriteFile(b *testing.B, path, content string) {
	b.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		b.Fatal(err)
	}
}

func cacheStartupBenchmarkGit(b *testing.B, dir string, args ...string) {
	b.Helper()
	gitArgs := append([]string{"-c", "user.email=benchmark@example.com", "-c", "user.name=Cache Benchmark"}, args...)
	cmd := exec.CommandContext(b.Context(), "git", gitArgs...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		b.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
