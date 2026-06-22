package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/dotcommander/repomap"
	"github.com/spf13/cobra"
)

// verifyCmds holds the detected build and test commands for a project.
type verifyCmds struct {
	build string
	test  string
}

// newBriefCmd builds the `repomap brief` subcommand: an agent boot digest that
// answers identity + how-to-verify + current git state in one call, then
// appends the standard enriched repo map. Task surfacing is intentionally
// omitted — there is no reliable in-repo task source.
func newBriefCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "brief [directory]",
		Short: "Print an agent boot digest (identity + verify + state) followed by the repo map",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}
			absDir, err := filepath.Abs(dir)
			if err != nil {
				return fmt.Errorf("resolve path: %w", err)
			}

			cfg := repomap.Config{MaxTokens: 2048, MaxTokensNoCtx: 2048}
			m := repomap.New(absDir, cfg)
			if err := m.Build(cmd.Context()); err != nil {
				return err
			}

			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			modulePath := readModulePath(absDir)
			lang, kind := briefIdentity(absDir, m)

			title := path.Base(absDir)
			if modulePath != "" {
				title = path.Base(modulePath)
			}
			if _, err := fmt.Fprintf(out, "# %s — %s %s\n", title, lang, kind); err != nil {
				return err
			}
			if modulePath != "" {
				if _, err := fmt.Fprintf(out, "  module %s\n", modulePath); err != nil {
					return err
				}
			}

			vc := detectVerify(absDir)
			if _, err := fmt.Fprintf(out, "\n## Verify\n  build: %s\n  test:  %s\n", vc.build, vc.test); err != nil {
				return err
			}

			branch := runTrimmed(ctx, "git", "-C", absDir, "branch", "--show-current")
			if branch == "" {
				branch = "(none)"
			}
			dirty := len(runLines(ctx, "git", "-C", absDir, "status", "--short"))
			if _, err := fmt.Fprintf(out, "\n## State\n  branch: %s   dirty: %d file(s)\n", branch, dirty); err != nil {
				return err
			}

			if _, err := fmt.Fprintf(out, "\n## Map\n%s", m.String()); err != nil {
				return err
			}
			return nil
		},
	}
}

// readModulePath returns the module path from <dir>/go.mod, or "" when absent.
func readModulePath(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// fileExists reports whether <dir>/<name> exists as a regular file.
func fileExists(dir, name string) bool {
	info, err := os.Stat(filepath.Join(dir, name))
	return err == nil && !info.IsDir()
}

// briefIdentity returns a language label and project-kind word for the title,
// keyed on the dominant manifest, falling back to the most common parsed
// language across ranked files.
func briefIdentity(dir string, m *repomap.Map) (lang, kind string) {
	switch {
	case fileExists(dir, "go.mod"):
		return "Go", "module"
	case fileExists(dir, "package.json"):
		return "JavaScript/TypeScript", "package"
	default:
		return dominantLanguage(m.Ranked()), "project"
	}
}

// dominantLanguage returns the most common non-empty RankedFile.Language,
// breaking ties lexicographically for deterministic output.
func dominantLanguage(ranked []repomap.RankedFile) string {
	counts := map[string]int{}
	for _, rf := range ranked {
		if rf.FileSymbols != nil && rf.Language != "" {
			counts[rf.Language]++
		}
	}
	best, bestN := "unknown", 0
	for l, n := range counts {
		if n > bestN || (n == bestN && l < best) {
			best, bestN = l, n
		}
	}
	return best
}

// detectVerify infers build/test commands from project manifests in priority
// order; first match wins, graceful "(unknown)" default.
func detectVerify(dir string) verifyCmds {
	switch {
	case fileExists(dir, "go.mod"):
		return verifyCmds{"go build ./...", "go test ./..."}
	case fileExists(dir, "package.json"):
		b, t := pkgScripts(dir)
		return verifyCmds{orDefault(b, "npm run build"), orDefault(t, "npm test")}
	case fileExists(dir, "justfile") || fileExists(dir, "Justfile"):
		return verifyCmds{justRecipe(dir, "build"), justRecipe(dir, "test")}
	case fileExists(dir, "Makefile"):
		return verifyCmds{"make build", "make test"}
	default:
		return verifyCmds{"(unknown)", "(unknown)"}
	}
}

// pkgScripts returns the npm build/test commands when those scripts exist in
// <dir>/package.json; empty strings otherwise.
func pkgScripts(dir string) (build, test string) {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return "", ""
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return "", ""
	}
	if _, ok := pkg.Scripts["build"]; ok {
		build = "npm run build"
	}
	if _, ok := pkg.Scripts["test"]; ok {
		test = "npm test"
	}
	return build, test
}

// justRecipe returns "just <name>" when a recipe with that name is defined in a
// justfile, else "(unknown)".
func justRecipe(dir, name string) string {
	for _, fn := range []string{"justfile", "Justfile"} {
		data, err := os.ReadFile(filepath.Join(dir, fn))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, name+":") || strings.HasPrefix(trimmed, name+" ") {
				return "just " + name
			}
		}
	}
	return "(unknown)"
}

// orDefault returns v, or def when v is empty.
func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

