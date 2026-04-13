package repomap

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// computeSymbolDeltas returns a per-file diff of symbol names between HEAD
// and the working tree. Only files in `files` with a language supported by
// our parsers are probed. Missing-at-HEAD is treated as an all-added file.
func computeSymbolDeltas(ctx context.Context, root string, files []fileChange, postSymbols map[string]*FileSymbols) map[string]symbolDelta {
	out := make(map[string]symbolDelta, len(files))
	for _, f := range files {
		if f.Language == "" {
			continue
		}
		post := postSymbols[f.Path]
		postNames := symbolNameSet(post)

		// For deletions, `post` is empty; compare against HEAD content.
		var preNames map[string]bool
		if f.IndexStatus == "A" && f.Status == "A" {
			// Pure addition — no HEAD content, skip pre.
			preNames = map[string]bool{}
		} else {
			preSrc, _ := gitShowAt(ctx, root, "HEAD", oldPathOr(f))
			if preSrc != "" {
				preNames = parseSymbolsFromSource(f.Path, f.Language, preSrc)
			}
		}

		var added, removed []string
		for name := range postNames {
			if !preNames[name] {
				added = append(added, name)
			}
		}
		for name := range preNames {
			if !postNames[name] {
				removed = append(removed, name)
			}
		}
		sort.Strings(added)
		sort.Strings(removed)
		if len(added) == 0 && len(removed) == 0 {
			continue
		}
		out[f.Path] = symbolDelta{
			Path:    f.Path,
			Added:   added,
			Removed: removed,
		}
	}
	return out
}

// oldPathOr returns OldPath for renames, else Path. Needed so `git show HEAD:`
// resolves to the file's pre-rename name.
func oldPathOr(f fileChange) string {
	if f.OldPath != "" {
		return f.OldPath
	}
	return f.Path
}

// symbolNameSet returns the exported + unexported top-level symbol names in a
// FileSymbols as a set. Returns empty set when fs is nil (new/unparsable file).
func symbolNameSet(fs *FileSymbols) map[string]bool {
	out := make(map[string]bool)
	if fs == nil {
		return out
	}
	for _, s := range fs.Symbols {
		out[s.Name] = true
	}
	return out
}

// parseSymbolsFromSource runs the same language-appropriate parser against an
// in-memory source string (HEAD content) to get the pre-change symbol set.
// Returns an empty set if parsing fails — the delta will just show all
// current symbols as "added", which is acceptable for new or renamed files.
func parseSymbolsFromSource(path, language, src string) map[string]bool {
	if parser, ok := langParsers[language]; ok {
		fs := &FileSymbols{Path: path, Language: language}
		parser(strings.Split(src, "\n"), fs)
		out := make(map[string]bool, len(fs.Symbols))
		for _, s := range fs.Symbols {
			out[s.Name] = true
		}
		return out
	}
	// Go uses AST; parse via an in-memory approach.
	if language == "go" {
		return parseGoSymbolsFromSource(path, src)
	}
	return map[string]bool{}
}

// parseGoSymbolsFromSource runs Go AST parser on an in-memory source buffer.
// We bypass ParseGoFile (which reads from disk) by shelling a tiny in-memory
// parse. Returns empty set on any parse error — caller treats missing as
// "all symbols are new".
func parseGoSymbolsFromSource(path, src string) map[string]bool {
	// Fall back: regex for "^func Name(" / "^type Name" — good enough for the
	// Removed-symbol delta use case without duplicating the AST parser.
	out := make(map[string]bool)
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "func "):
			// "func Foo(" or "func (r *T) Foo("
			name := extractGoFuncName(line)
			if name != "" {
				out[name] = true
			}
		case strings.HasPrefix(line, "type "):
			// "type Foo struct {" / "type Foo interface {"
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				out[parts[1]] = true
			}
		}
	}
	return out
}

// extractGoFuncName pulls the function/method name out of a "func ..." line.
// Handles "func Foo(", "func (r *T) Foo(", "func (T) Foo[".
func extractGoFuncName(line string) string {
	rest := strings.TrimPrefix(line, "func ")
	if strings.HasPrefix(rest, "(") {
		// Receiver — skip past closing paren.
		closing := strings.Index(rest, ")")
		if closing < 0 {
			return ""
		}
		rest = strings.TrimSpace(rest[closing+1:])
	}
	// Name is up to the first '(' or '[' or space.
	for i, r := range rest {
		if r == '(' || r == '[' || r == ' ' {
			return rest[:i]
		}
	}
	return rest
}

// suggestMessages fills in group.SuggestedMsg for each group using symbol
// deltas and dep-bump hints.
func suggestMessages(groups []CommitGroup, gs *gitState, deltas map[string]symbolDelta, bumps []DepBump) {
	for i := range groups {
		groups[i].SuggestedMsg = draftMessage(&groups[i], gs, deltas, bumps)
		groups[i].DiffOffsets = nil // filled in by analyze.go if offsets are tracked
	}
}

// draftMessage composes a conventional-commit subject for a group.
// Format: "<type>(<scope>): <subject>"
func draftMessage(g *CommitGroup, gs *gitState, deltas map[string]symbolDelta, bumps []DepBump) string {
	subject := summarizeGroupWork(g, gs, deltas, bumps)
	prefix := g.Type
	if g.Scope != "" {
		prefix = g.Type + "(" + g.Scope + ")"
	}
	// Cap subject at 72 chars including prefix.
	max := 72 - len(prefix) - 2
	if max > 0 && len(subject) > max {
		subject = subject[:max-1] + "…"
	}
	return prefix + ": " + subject
}

// summarizeGroupWork picks the most informative summary for a group based on
// its type + the symbol deltas of its files.
func summarizeGroupWork(g *CommitGroup, gs *gitState, deltas map[string]symbolDelta, bumps []DepBump) string {
	switch g.Type {
	case "deps":
		return depsSubject(g, bumps)
	case "docs":
		return docsSubject(g)
	case "test":
		return testSubject(g, deltas)
	case "chore", "artifact":
		return choreSubject(g)
	}
	// feat / fix / refactor: prefer symbol delta summary.
	if line := symbolDeltaSubject(g, deltas); line != "" {
		return line
	}
	return genericSubject(g)
}

func depsSubject(g *CommitGroup, bumps []DepBump) string {
	var names []string
	want := make(map[string]bool, len(g.Files))
	for _, p := range g.Files {
		want[p] = true
	}
	for _, b := range bumps {
		if !want[b.File] {
			continue
		}
		for _, c := range b.Changes {
			names = append(names, c)
		}
	}
	if len(names) == 0 {
		return fmt.Sprintf("bump dependencies (%d files)", len(g.Files))
	}
	if len(names) == 1 {
		return "bump " + names[0]
	}
	return fmt.Sprintf("bump %s and %d more", names[0], len(names)-1)
}

func docsSubject(g *CommitGroup) string {
	if len(g.Files) == 1 {
		return "update " + filepath.Base(g.Files[0])
	}
	if g.Scope != "" {
		return fmt.Sprintf("update %s docs (%d files)", g.Scope, len(g.Files))
	}
	return fmt.Sprintf("update docs (%d files)", len(g.Files))
}

func testSubject(g *CommitGroup, deltas map[string]symbolDelta) string {
	var added []string
	for _, p := range g.Files {
		d := deltas[p]
		added = append(added, d.Added...)
	}
	if len(added) == 0 {
		return fmt.Sprintf("update tests (%d files)", len(g.Files))
	}
	if len(added) <= 2 {
		return "add " + strings.Join(added, ", ")
	}
	return fmt.Sprintf("add %s and %d more tests", added[0], len(added)-1)
}

func choreSubject(g *CommitGroup) string {
	if len(g.Files) == 1 {
		return "update " + filepath.Base(g.Files[0])
	}
	return fmt.Sprintf("housekeeping (%d files)", len(g.Files))
}

func symbolDeltaSubject(g *CommitGroup, deltas map[string]symbolDelta) string {
	var added, removed []string
	for _, p := range g.Files {
		d := deltas[p]
		added = append(added, d.Added...)
		removed = append(removed, d.Removed...)
	}
	switch {
	case len(added) > 0 && len(removed) == 0:
		if len(added) == 1 {
			return "add " + added[0]
		}
		if len(added) <= 3 {
			return "add " + strings.Join(added, ", ")
		}
		return fmt.Sprintf("add %s and %d more", added[0], len(added)-1)
	case len(removed) > 0 && len(added) == 0:
		if len(removed) == 1 {
			return "remove " + removed[0]
		}
		return fmt.Sprintf("remove %s and %d more", removed[0], len(removed)-1)
	case len(added) > 0 && len(removed) > 0:
		return fmt.Sprintf("replace %s with %s", removed[0], added[0])
	}
	return ""
}

func genericSubject(g *CommitGroup) string {
	if len(g.Files) == 1 {
		return "update " + filepath.Base(g.Files[0])
	}
	if g.Scope != "" {
		return fmt.Sprintf("update %s (%d files)", g.Scope, len(g.Files))
	}
	return fmt.Sprintf("update %d files", len(g.Files))
}

// detectDepBumps parses go.mod/package.json/etc. diffs and returns structured
// bump descriptions. Pass gs so we can run `git diff HEAD -- <file>` for each
// dep file in the changeset.
func detectDepBumps(ctx context.Context, root string, files []fileChange) []DepBump {
	var bumps []DepBump
	for _, f := range files {
		if !f.IsDep {
			continue
		}
		diff, err := runGit(ctx, root, "diff", "HEAD", "--", f.Path)
		if err != nil || diff == "" {
			// Probably untracked or first-commit — skip.
			continue
		}
		changes := extractBumpChanges(f.Path, diff)
		if len(changes) == 0 {
			continue
		}
		bumps = append(bumps, DepBump{
			File:    f.Path,
			Manager: depManager(f.Path),
			Changes: changes,
		})
	}
	return bumps
}

// depManager identifies the package-manager family for a manifest path.
func depManager(path string) string {
	switch filepath.Base(path) {
	case "go.mod", "go.sum":
		return "go"
	case "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock":
		return "npm"
	case "Cargo.toml", "Cargo.lock":
		return "cargo"
	case "plugin.json", "marketplace.json":
		return "dc-plugin"
	case "requirements.txt", "Pipfile", "Pipfile.lock", "pyproject.toml":
		return "python"
	case "Gemfile", "Gemfile.lock":
		return "ruby"
	case "composer.json", "composer.lock":
		return "php"
	}
	return ""
}

// extractBumpChanges scans a unified diff for before/after version lines and
// emits "name vOLD -> vNEW" strings. Heuristic: match the same dependency name
// on a '-' line and a following '+' line within a ±5-line window.
func extractBumpChanges(path, diff string) []string {
	mgr := depManager(path)
	lines := strings.Split(diff, "\n")
	type bumpLine struct {
		name, version string
	}
	var minuses, pluses []bumpLine
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		body := line[1:]
		switch line[0] {
		case '-':
			if !strings.HasPrefix(line, "---") {
				if bl, ok := parseBumpLine(mgr, body); ok {
					minuses = append(minuses, bl)
				}
			}
		case '+':
			if !strings.HasPrefix(line, "+++") {
				if bl, ok := parseBumpLine(mgr, body); ok {
					pluses = append(pluses, bl)
				}
			}
		}
	}
	// Pair by name.
	minusByName := make(map[string]string, len(minuses))
	for _, m := range minuses {
		minusByName[m.name] = m.version
	}
	var out []string
	seen := make(map[string]bool)
	for _, p := range pluses {
		if old, ok := minusByName[p.name]; ok {
			if seen[p.name] {
				continue
			}
			seen[p.name] = true
			out = append(out, fmt.Sprintf("%s %s -> %s", p.name, old, p.version))
			delete(minusByName, p.name)
		}
	}
	for name, v := range minusByName {
		if seen[name] {
			continue
		}
		out = append(out, fmt.Sprintf("drop %s %s", name, v))
	}
	for _, p := range pluses {
		if !seen[p.name] && minusByName[p.name] == "" {
			// nothing paired, nothing dropped — pure add.
			out = append(out, fmt.Sprintf("add %s %s", p.name, p.version))
			seen[p.name] = true
		}
	}
	sort.Strings(out)
	return out
}

// parseBumpLine recognizes manifest-line formats and extracts (name, version).
// Returns (_, false) when the line isn't a dep pin.
func parseBumpLine(manager, line string) (bumpLine struct {
	name, version string
}, ok bool) {
	line = strings.TrimSpace(line)
	switch manager {
	case "go":
		// "require github.com/foo/bar v1.2.3" or "\tgithub.com/foo/bar v1.2.3"
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.HasPrefix(fields[len(fields)-1], "v") {
			bumpLine.name = fields[len(fields)-2]
			bumpLine.version = fields[len(fields)-1]
			return bumpLine, true
		}
	case "npm":
		// `"foo": "^1.2.3",`
		if strings.Contains(line, ":") && strings.Count(line, `"`) >= 4 {
			parts := strings.SplitN(line, ":", 2)
			name := strings.Trim(strings.TrimSpace(parts[0]), `"`)
			ver := strings.Trim(strings.TrimSpace(strings.TrimRight(parts[1], ",")), `"`)
			if name != "" && ver != "" && !strings.ContainsAny(ver, "{[") {
				bumpLine.name = name
				bumpLine.version = ver
				return bumpLine, true
			}
		}
	case "cargo":
		// `foo = "1.2.3"` or `foo = { version = "1.2.3" }`
		if eq := strings.Index(line, "="); eq > 0 {
			name := strings.TrimSpace(line[:eq])
			rest := strings.TrimSpace(line[eq+1:])
			if strings.HasPrefix(rest, `"`) {
				ver := strings.Trim(strings.TrimSuffix(rest, ","), `"`)
				if name != "" && ver != "" {
					bumpLine.name = name
					bumpLine.version = ver
					return bumpLine, true
				}
			}
		}
	case "dc-plugin":
		// `"version": "1.2.3"`
		if strings.Contains(line, `"version"`) {
			if idx := strings.Index(line, ":"); idx > 0 {
				ver := strings.Trim(strings.TrimRight(strings.TrimSpace(line[idx+1:]), ","), `"`)
				if ver != "" {
					bumpLine.name = "plugin"
					bumpLine.version = ver
					return bumpLine, true
				}
			}
		}
	}
	return bumpLine, false
}
