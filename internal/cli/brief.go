package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/dotcommander/repomap"
	"github.com/spf13/cobra"
)

// verifyCmds holds the detected build and test commands for a project.
type verifyCmds struct {
	build string
	test  string
	vet   string
	lint  string
}

// briefMapFiles caps how many top-ranked files the brief embeds. A boot digest
// wants the spine, not every file — the full map is one `repomap` call away.
const briefMapFiles = 20

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
			if _, err := fmt.Fprintf(out, "%s, agent — here's your briefing.\n\n", greeting(time.Now().Hour())); err != nil {
				return err
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
			if vc.vet != "" {
				if _, err := fmt.Fprintf(out, "  vet:   %s\n", vc.vet); err != nil {
					return err
				}
			}
			if vc.lint != "" {
				if _, err := fmt.Fprintf(out, "  lint:  %s\n", vc.lint); err != nil {
					return err
				}
			}

			branch := runTrimmed(ctx, "git", "-C", absDir, "branch", "--show-current")
			if branch == "" {
				branch = "(none)"
			}
			dirtyLines := runLines(ctx, "git", "-C", absDir, "status", "--short")
			recent := runLines(ctx, "git", "-C", absDir, "log", "-3", "--format=%s")
			if _, err := fmt.Fprint(out, briefState(branch, dirtyLines, recent)); err != nil {
				return err
			}

			if rules := briefRules(absDir); rules != "" {
				if _, err := fmt.Fprint(out, rules); err != nil {
					return err
				}
			}

			if note := briefConfigNote(absDir); note != "" {
				if _, err := fmt.Fprint(out, note); err != nil {
					return err
				}
			}
			mapBody, total := m.StringBriefMap(briefMapFiles)
			if _, err := fmt.Fprintf(out, "\n## Map\n%s", mapBody); err != nil {
				return err
			}
			if total > briefMapFiles {
				if _, err := fmt.Fprintf(out, "  +%d more files — run `repomap` for the full map\n", total-briefMapFiles); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

// briefState renders the "## State" section: branch, dirty file count plus up
// to 8 changed paths (with their git status codes), then up to 3 recent commit
// subjects. The path and recent blocks are skipped when empty so a clean tree
// or a commit-less repo still render cleanly.
func briefState(branch string, dirty, recent []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n## State\n  branch: %s   dirty: %d file(s)\n", branch, len(dirty))
	const maxPaths = 8
	for i, line := range dirty {
		if i == maxPaths {
			fmt.Fprintf(&b, "    +%d more\n", len(dirty)-maxPaths)
			break
		}
		fmt.Fprintf(&b, "    %s\n", line)
	}
	if len(recent) > 0 {
		b.WriteString("  recent:\n")
		for _, subj := range recent {
			fmt.Fprintf(&b, "    %s\n", subj)
		}
	}
	return b.String()
}

// briefRules renders a "## Rules" section pointing at any agent-convention docs
// present at the repo root (banned libs, "don't touch X" — constraints not
// inferrable from reading code). Returns "" when none exist so the section is
// omitted entirely.
func briefRules(dir string) string {
	candidates := []string{"CLAUDE.md", "AGENTS.md", ".cursorrules", ".github/copilot-instructions.md"}
	var found []string
	for _, name := range candidates {
		if fileExists(dir, name) {
			found = append(found, name)
		}
	}
	if len(found) == 0 {
		return ""
	}
	return fmt.Sprintf("\n## Rules\n  conventions: %s — read before editing\n", strings.Join(found, ", "))
}

// briefConfigNote warns that repomap's own config is active so a reader does
// not mistake a filtered Map for the complete symbol set. Returns "" when no
// .repomap.yaml/.yml is present.
func briefConfigNote(dir string) string {
	if fileExists(dir, ".repomap.yaml") || fileExists(dir, ".repomap.yml") {
		return "\n## Config\n  .repomap.yaml active — Map may omit filtered symbols\n"
	}
	return ""
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

// golangciCmd returns the golangci-lint command when a config file is present,
// else "" so no lint line is advertised for a repo with no linter configured.
func golangciCmd(dir string) string {
	if fileExists(dir, ".golangci.yml") || fileExists(dir, ".golangci.yaml") {
		return "golangci-lint run ./..."
	}
	return ""
}

// detectVerify infers build/test commands from project manifests in priority
// order; first match wins, graceful "(unknown)" default.
func detectVerify(dir string) verifyCmds {
	switch {
	case fileExists(dir, "go.mod"):
		return verifyCmds{build: "go build ./...", test: "go test ./...", vet: "go vet ./...", lint: golangciCmd(dir)}
	case fileExists(dir, "package.json"):
		b, t := pkgScripts(dir)
		return verifyCmds{build: orDefault(b, "npm run build"), test: orDefault(t, "npm test")}
	case fileExists(dir, "justfile") || fileExists(dir, "Justfile"):
		return verifyCmds{build: recipeCmd(dir, "just", "build", "justfile", "Justfile"), test: recipeCmd(dir, "just", "test", "justfile", "Justfile")}
	case fileExists(dir, "Makefile"):
		return verifyCmds{build: recipeCmd(dir, "make", "build", "Makefile"), test: recipeCmd(dir, "make", "test", "Makefile")}
	default:
		return verifyCmds{build: "(unknown)", test: "(unknown)"}
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

// recipeCmd returns "<tool> <name>" when a target or recipe named <name> is
// defined in one of files (checked in order), else "(unknown)". Headers sit at
// column 0 as "<name>:" or "<name> <args>:"; we match either. Best-effort: it
// avoids advertising a verify command the build tool does not actually define.
func recipeCmd(dir, tool, name string, files ...string) string {
	for _, fn := range files {
		data, err := os.ReadFile(filepath.Join(dir, fn))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, name+":") || strings.HasPrefix(trimmed, name+" ") {
				return tool + " " + name
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

// greeting returns a time-of-day salutation for the given 24-hour clock hour.
func greeting(hour int) string {
	switch {
	case hour < 12:
		return "Good morning"
	case hour < 18:
		return "Good afternoon"
	default:
		return "Good evening"
	}
}

