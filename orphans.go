package repomap

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dotcommander/repomap/internal/lsp"
)

// OrphanCandidate is one exported symbol with zero inbound non-test references.
type OrphanCandidate struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Receiver string `json:"receiver,omitempty"`
	File     string `json:"file"`
	Line     int    `json:"line"`
}

// OrphanReport buckets exported symbols by inbound-reference status.
type OrphanReport struct {
	Caveat       string            `json:"caveat"`
	ZeroRefs     []OrphanCandidate `json:"zero_refs"`
	TestOnlyRefs []OrphanCandidate `json:"test_only_refs"`
}

const orphanCaveat = "Candidates only — repomap sees one repo. Verify external/library consumers before deleting."

// OrphanCandidates returns exported symbols with zero inbound references (ZeroRefs)
// and exported symbols referenced only by *_test.go files (TestOnlyRefs).
// Entry points (RankedFile.Tag == "entry") are excluded. The caller owns the LSP lifecycle.
func (m *Map) OrphanCandidates(ctx context.Context, q RefsQuerier) (OrphanReport, error) {
	ranked := m.Ranked()
	report := OrphanReport{Caveat: orphanCaveat}
	for _, rf := range ranked {
		if rf.Tag == "entry" || rf.FileSymbols == nil || strings.HasSuffix(rf.Path, "_test.go") {
			continue
		}
		absFile := filepath.Join(m.root, rf.Path)
		for _, sym := range rf.Symbols {
			if !sym.Exported || sym.Line == 0 {
				continue
			}
			locs, err := q.Refs(ctx, absFile, sym.Line, sym.Name)
			if err != nil {
				continue // skip unresolvable symbols rather than failing the whole run
			}
			nonTest, test := classifyRefs(locs, m.root, rf.Path, sym.Line)
			cand := OrphanCandidate{Name: sym.Name, Kind: sym.Kind, Receiver: sym.Receiver, File: rf.Path, Line: sym.Line}
			switch {
			case nonTest == 0 && test == 0:
				report.ZeroRefs = append(report.ZeroRefs, cand)
			case nonTest == 0 && test > 0:
				report.TestOnlyRefs = append(report.TestOnlyRefs, cand)
			}
		}
	}
	sortOrphans(report.ZeroRefs)
	sortOrphans(report.TestOnlyRefs)
	return report, nil
}

// classifyRefs counts inbound references excluding the definition site, split into non-test and test.
func classifyRefs(locs []Location, root, defRel string, defLine int) (nonTest, test int) {
	for _, loc := range locs {
		if loc.Line == defLine && relPathForOrphan(root, loc.File) == filepath.ToSlash(defRel) {
			continue
		}
		if strings.Contains(loc.File, "_test.go") {
			test++
			continue
		}
		nonTest++
	}
	return nonTest, test
}

func relPathForOrphan(root, path string) string {
	if !filepath.IsAbs(path) {
		return filepath.ToSlash(path)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func sortOrphans(c []OrphanCandidate) {
	sort.SliceStable(c, func(i, j int) bool {
		if c[i].File != c[j].File {
			return c[i].File < c[j].File
		}
		return c[i].Line < c[j].Line
	})
}

// OrphanQuerier constructs an in-process gopls querier and its Manager.
// The caller MUST call shutdown(ctx) when done. Errors if gopls is absent.
func OrphanQuerier(root string) (q RefsQuerier, shutdown func(context.Context), err error) {
	if err := CheckGopls(); err != nil {
		return nil, nil, err
	}
	mgr := lsp.NewManager(root)
	return NewInProcessQuerier(mgr), func(c context.Context) { mgr.Shutdown(c) }, nil
}
