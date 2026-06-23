package cli

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/dotcommander/repomap"
)

// ownershipClusters caps how many "## Likely ownership" rows the brief shows.
const ownershipClusters = 5

// genericSegments are path segments too generic to serve as a concern label;
// when a cluster directory ends in one, the label walks up to the nearest
// meaningful ancestor segment.
var genericSegments = map[string]bool{
	"lib": true, "src": true, "internal": true, "pkg": true,
	"dist": true, "build": true, "cmd": true, "app": true, "core": true,
}

// ownerCluster is a group of ranked files sharing a parent directory, with a
// derived concern label and aggregate signal used for routing hints.
type ownerCluster struct {
	dir     string
	label   string
	files   int
	score   int
	symbols []string
}

// briefOwnership renders the "## Likely ownership" routing section from the
// already-ranked file set: it clusters ranked files by parent directory, keeps
// directories owning >=2 files, labels each by its deepest meaningful segment,
// and lists file count plus a few top exported symbols. Returns "" (no header)
// when fewer than 2 clusters qualify — a flat or single-area repo gets nothing
// rather than noise.
func briefOwnership(ranked []repomap.RankedFile) string {
	clusters := clusterByOwner(ranked)
	if len(clusters) < 2 {
		return ""
	}
	sort.Slice(clusters, func(i, j int) bool {
		if clusters[i].score != clusters[j].score {
			return clusters[i].score > clusters[j].score
		}
		return clusters[i].dir < clusters[j].dir
	})
	if len(clusters) > ownershipClusters {
		clusters = clusters[:ownershipClusters]
	}
	var b strings.Builder
	b.WriteString("\n## Likely ownership\n")
	for _, c := range clusters {
		fmt.Fprintf(&b, "  %s/ — %s (%d files", c.dir, c.label, c.files)
		if len(c.symbols) > 0 {
			fmt.Fprintf(&b, ": %s", strings.Join(c.symbols, ", "))
		}
		b.WriteString(")\n")
	}
	return b.String()
}

// clusterByOwner buckets ranked files by parent directory, keeping only
// directories that own >=2 ranked files. Files at the repo root (dir ".") are
// dropped — a root bucket is not a routable concern.
func clusterByOwner(ranked []repomap.RankedFile) []ownerCluster {
	type bucket struct {
		score int
		files []repomap.RankedFile
	}
	buckets := map[string]*bucket{}
	var order []string
	for _, rf := range ranked {
		if rf.FileSymbols == nil || rf.Path == "" {
			continue
		}
		dir := path.Dir(rf.Path)
		if dir == "." || dir == "" {
			continue
		}
		bk, ok := buckets[dir]
		if !ok {
			bk = &bucket{}
			buckets[dir] = bk
			order = append(order, dir)
		}
		bk.score += rf.Score
		bk.files = append(bk.files, rf)
	}
	var clusters []ownerCluster
	for _, dir := range order {
		bk := buckets[dir]
		if len(bk.files) < 2 {
			continue
		}
		clusters = append(clusters, ownerCluster{
			dir:     dir,
			label:   ownerLabel(dir),
			files:   len(bk.files),
			score:   bk.score,
			symbols: topClusterSymbols(bk.files),
		})
	}
	return clusters
}

// ownerLabel derives a concern word from a cluster directory: its last path
// segment, walking up past generic segments (lib, src, internal, ...) to the
// nearest meaningful ancestor. Falls back to the raw last segment if every
// segment is generic.
func ownerLabel(dir string) string {
	segs := strings.Split(dir, "/")
	for i := len(segs) - 1; i >= 0; i-- {
		if segs[i] != "" && !genericSegments[segs[i]] {
			return segs[i]
		}
	}
	return segs[len(segs)-1]
}

// topClusterSymbols returns up to 3 exported symbol names from a cluster,
// drawn from the highest-scored files first, deduplicated, preserving that
// order. Routing-useful: names a reader keys on to confirm the area.
func topClusterSymbols(files []repomap.RankedFile) []string {
	ordered := make([]repomap.RankedFile, len(files))
	copy(ordered, files)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Score > ordered[j].Score
	})
	var out []string
	seen := map[string]bool{}
	for _, rf := range ordered {
		for _, s := range rf.Symbols {
			if !s.Exported || seen[s.Name] {
				continue
			}
			seen[s.Name] = true
			out = append(out, s.Name)
			if len(out) == 3 {
				return out
			}
		}
	}
	return out
}
