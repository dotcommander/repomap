package repomap

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// parseFiles parses all discovered files in parallel and returns the symbols
// and a path→mtime map for stale checking.
// Uses ctags for non-Go files when available, falling back to regex.
func (m *Map) parseFiles(ctx context.Context, files []FileInfo) ([]*FileSymbols, map[string]time.Time, error) {
	mtimes := make(map[string]time.Time, len(files))
	for _, fi := range files {
		absPath := filepath.Join(m.root, fi.Path)
		info, err := os.Stat(absPath)
		if err != nil {
			continue
		}
		mtimes[absPath] = info.ModTime()
	}

	var (
		goParsed    []*FileSymbols
		nonGoParsed []*FileSymbols
		wg          sync.WaitGroup
	)

	wg.Add(2)

	go func() {
		defer wg.Done()
		goFiles := make([]FileInfo, 0, len(files))
		for _, fi := range files {
			if fi.Language == "go" {
				goFiles = append(goFiles, fi)
			}
		}
		goResults := make([]*FileSymbols, len(goFiles))
		g, _ := errgroup.WithContext(ctx)
		g.SetLimit(runtime.NumCPU())

		for i, fi := range goFiles {
			g.Go(func() error {
				absPath := filepath.Join(m.root, fi.Path)
				sym, err := ParseGoFile(absPath, m.root)
				if err != nil {
					return nil //nolint:nilerr // skip parse errors
				}
				goResults[i] = sym
				return nil
			})
		}
		_ = g.Wait()
		for _, sym := range goResults {
			if sym != nil {
				goParsed = append(goParsed, sym)
			}
		}
	}()

	go func() {
		defer wg.Done()
		if CtagsAvailable() {
			ctagsParsed, err := ParseWithCtags(ctx, m.root, files)
			if err == nil {
				nonGoParsed = ctagsParsed
			} else {
				nonGoParsed = m.parseGenericFiles(files)
			}
		} else {
			nonGoParsed = m.parseGenericFiles(files)
		}
	}()

	wg.Wait()

	parsed := make([]*FileSymbols, 0, len(goParsed)+len(nonGoParsed))
	parsed = append(parsed, goParsed...)
	parsed = append(parsed, nonGoParsed...)

	return parsed, mtimes, nil
}

// parseGenericFiles parses non-Go files using regex patterns in parallel.
func (m *Map) parseGenericFiles(files []FileInfo) []*FileSymbols {
	results := make([]*FileSymbols, len(files))
	g := new(errgroup.Group)
	g.SetLimit(runtime.NumCPU())

	for i, fi := range files {
		if fi.Language == "go" {
			continue
		}
		g.Go(func() error {
			absPath := filepath.Join(m.root, fi.Path)
			sym, err := ParseGenericFile(absPath, m.root, fi.Language)
			if err != nil {
				return nil //nolint:nilerr // skip parse errors
			}
			results[i] = sym
			return nil
		})
	}
	_ = g.Wait()

	var parsed []*FileSymbols
	for _, sym := range results {
		if sym != nil {
			parsed = append(parsed, sym)
		}
	}
	return parsed
}
