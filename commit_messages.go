package repomap

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// suggestMessages fills in group.SuggestedMsg and group.Breaking for each
// group using symbol deltas and dep-bump hints.
func suggestMessages(groups []CommitGroup, gs *gitState, deltas map[string]symbolDelta, bumps []DepBump) {
	for i := range groups {
		// Set Breaking: true if any constituent file has a breaking delta.
		for _, p := range groups[i].Files {
			if d, ok := deltas[p]; ok && d.Breaking {
				groups[i].Breaking = true
				break
			}
		}
		groups[i].SuggestedMsg = draftMessage(&groups[i], gs, deltas, bumps)
		groups[i].DiffOffsets = nil // filled in by analyze.go if offsets are tracked
	}
}

// draftMessage composes a conventional-commit subject for a group.
// Format: "<type>(<scope>): <subject>"
// When subject contains newlines (bullet-list form), the first line is capped
// at 72 chars and the bullet body is appended verbatim.
func draftMessage(g *CommitGroup, gs *gitState, deltas map[string]symbolDelta, bumps []DepBump) string {
	subject := summarizeGroupWork(g, gs, deltas, bumps)
	prefix := g.Type
	if g.Breaking {
		// Promote feat/fix to breaking-change type prefix.
		switch g.Type {
		case "feat":
			prefix = "feat!"
		case "fix":
			prefix = "fix!"
		}
	}
	if g.Scope != "" {
		prefix = prefix + "(" + g.Scope + ")"
	}

	// Multi-line subjects (bullet lists): cap only the first line.
	if idx := strings.Index(subject, "\n"); idx >= 0 {
		firstLine := subject[:idx]
		rest := subject[idx:]
		maxFirst := 72 - len(prefix) - 2
		if maxFirst > 0 && len(firstLine) > maxFirst {
			firstLine = firstLine[:maxFirst-1] + "…"
		}
		return prefix + ": " + firstLine + rest
	}

	// Single-line: cap at 72 chars including prefix.
	maxLen := 72 - len(prefix) - 2
	if maxLen > 0 && len(subject) > maxLen {
		subject = subject[:maxLen-1] + "…"
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
	v := g.Verb
	if v == "" {
		v = "update"
	}
	if len(g.Files) == 1 {
		return v + " " + filepath.Base(g.Files[0])
	}
	if g.Scope != "" {
		return fmt.Sprintf("%s %s docs (%d files)", v, g.Scope, len(g.Files))
	}
	return fmt.Sprintf("%s docs (%d files)", v, len(g.Files))
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

// symbolDeltaSubject composes a subject line (or multi-line subject with
// bullets when delta count > 3) from the symbol adds/removes/modifies in the
// group. Returns "" when there are no symbol changes.
func symbolDeltaSubject(g *CommitGroup, deltas map[string]symbolDelta) string {
	var added, removed, modified []string
	for _, p := range g.Files {
		d := deltas[p]
		added = append(added, d.Added...)
		removed = append(removed, d.Removed...)
		modified = append(modified, d.Modified...)
	}
	total := len(added) + len(removed) + len(modified)
	if total == 0 {
		return ""
	}

	// Build individual action tokens — used both for the short form and bullets.
	var tokens []string
	for _, n := range added {
		tokens = append(tokens, "add "+n)
	}
	for _, n := range modified {
		tokens = append(tokens, "modify "+n)
	}
	for _, n := range removed {
		tokens = append(tokens, "remove "+n)
	}

	// Bullet-list when more than 3 distinct symbol changes.
	if total > 3 {
		return symbolBullets(tokens)
	}

	// Short form — at most 3 tokens.
	switch {
	case len(modified) > 0 && len(added) == 0 && len(removed) == 0:
		if len(modified) == 1 {
			return "modify " + modified[0]
		}
		return fmt.Sprintf("modify %s and %d more", modified[0], len(modified)-1)
	case len(added) > 0 && len(removed) == 0 && len(modified) == 0:
		if len(added) == 1 {
			return "add " + added[0]
		}
		return "add " + strings.Join(added, ", ")
	case len(removed) > 0 && len(added) == 0 && len(modified) == 0:
		if len(removed) == 1 {
			return "remove " + removed[0]
		}
		return fmt.Sprintf("remove %s and %d more", removed[0], len(removed)-1)
	case len(added) > 0 && len(removed) > 0 && len(modified) == 0:
		return fmt.Sprintf("replace %s with %s", removed[0], added[0])
	default:
		// Mixed: join up to 3 tokens.
		if len(tokens) == 1 {
			return tokens[0]
		}
		return strings.Join(tokens[:min(len(tokens), 3)], ", ")
	}
}

// symbolBullets formats a token list as "first-token\n\n- t1\n- t2\n...".
// The first line is kept under 60 chars (the rest of the 72-char limit is
// claimed by the type prefix). Callers must not cap this output — the bullet
// section is intentional multi-line.
func symbolBullets(tokens []string) string {
	if len(tokens) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(tokens[0])
	sb.WriteString("\n")
	for _, t := range tokens {
		sb.WriteString("\n- ")
		sb.WriteString(t)
	}
	return sb.String()
}

func genericSubject(g *CommitGroup) string {
	v := g.Verb
	if v == "" {
		v = "update"
	}
	if len(g.Files) == 1 {
		return v + " " + filepath.Base(g.Files[0])
	}
	if g.Scope != "" {
		return fmt.Sprintf("%s %s (%d files)", v, g.Scope, len(g.Files))
	}
	return fmt.Sprintf("%s %d files", v, len(g.Files))
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
	slices.Sort(out)
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
