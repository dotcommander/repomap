//go:build probe

package repomap

import (
	"context"
	"fmt"
	"os"
	"testing"
)

// TestProbeFormatTS_PostSymbols inspects what parseNonGoFile returns for the
// committed dc-hooks/format.ts in /path/to/project/.pi to validate the
// hypothesis that the post-parse returns far fewer symbols than HEAD has,
// which causes computeSymbolDeltas to flag every "missing" symbol as
// "removed" and any exported one as breaking.
func TestProbeFormatTS_PostSymbols(t *testing.T) {
	root := "/path/to/project/.pi"
	path := "agent/extensions/dc-hooks/security.ts"
	abs := root + "/" + path
	if _, err := os.Stat(abs); err != nil {
		t.Skipf("file unavailable: %v", err)
	}
	sym := parseNonGoFile(abs, root, "typescript")
	if sym == nil {
		t.Fatalf("parseNonGoFile returned nil for %s", path)
	}
	out, _ := os.Create("/tmp/repomap-probe.out")
	defer out.Close()
	fmt.Fprintf(out, "POST: %d symbols\n", len(sym.Symbols))
	for _, s := range sym.Symbols {
		fmt.Fprintf(out, "  POST  %-40s  exported=%v  sig=%q\n", s.Name, s.Exported, s.Signature)
	}

	// Now diff against HEAD~1 (the parent of 446c344) to mirror what
	// computeSymbolDeltas does.
	ctx := context.Background()
	preSrc, err := gitShowAt(ctx, root, "HEAD~1", path)
	if err != nil {
		t.Fatalf("gitShowAt HEAD~1: %v", err)
	}
	preFS := parseFileSymbolsFromSource(root, path, "typescript", preSrc)
	if preFS == nil {
		t.Fatalf("parseFileSymbolsFromSource returned nil for HEAD~1:%s", path)
	}
	fmt.Fprintf(out, "PRE:  %d symbols\n", len(preFS.Symbols))
	for _, s := range preFS.Symbols {
		fmt.Fprintf(out, "  PRE   %-40s  exported=%v  sig=%q\n", s.Name, s.Exported, s.Signature)
	}

	postSigs := symbolSigMap(sym)
	preSigs := symbolSigMap(preFS)
	var added, removed, modified []string
	for name, postSig := range postSigs {
		if pre, ok := preSigs[name]; !ok {
			added = append(added, name)
		} else if pre != postSig {
			modified = append(modified, name)
		}
	}
	for name := range preSigs {
		if _, ok := postSigs[name]; !ok {
			removed = append(removed, name)
		}
	}
	fmt.Fprintf(out, "DELTA: added=%d removed=%d modified=%d\n", len(added), len(removed), len(modified))
	fmt.Fprintf(out, "  ADDED:    %v\n", added)
	fmt.Fprintf(out, "  REMOVED:  %v\n", removed)
	fmt.Fprintf(out, "  MODIFIED: %v\n", modified)
}
