package repomap

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

func taskAnalysisDiagnostics(m *Map) []string {
	diagnostics := m.GoDiagnostics()
	out := []string{}
	for _, diagnostic := range diagnostics {
		out = append(out, "semantic analysis degraded: "+diagnostic.Message)
	}
	return out
}
func taskReadNext(target TaskTarget) []ReadNextItem {
	out := []ReadNextItem{}
	for _, symbol := range target.Symbols {
		if symbol.Line > 0 {
			end := symbol.EndLine
			if end < symbol.Line {
				end = symbol.Line
			}
			out = append(out, readNextRange(target.Path, symbol.Line, end, "inspect selected target"))
			break
		}
	}
	for _, path := range target.Consumers {
		out = append(out, readNextAround(path, 1, "inspect consumer before editing target"))
		break
	}
	for _, path := range target.Tests {
		out = append(out, readNextAround(path, 1, "inspect related test coverage"))
		break
	}
	return out
}
func taskVerifyCommands(targets []TaskTarget) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, target := range targets {
		if target.Parse == "go_ast" {
			dir := filepath.ToSlash(filepath.Dir(target.Path))
			command := "go test ./..."
			if dir != "." {
				command = "go test ./" + dir
			}
			if !seen[command] {
				seen[command] = true
				out = append(out, command)
			}
			continue
		}
		if strings.HasSuffix(target.Path, ".php") {
			command := "php -l " + target.Path
			if !seen[command] {
				seen[command] = true
				out = append(out, command)
			}
		}
	}
	slices.Sort(out)
	return out
}
func taskFollowUpCommands(report TaskReport, consumed []string) []string {
	if len(report.Truncations) == 0 {
		return nil
	}
	nextTokens := report.Budget.MaxTokens * 2
	if nextTokens < report.Budget.MaxTokens {
		nextTokens = report.Budget.MaxTokens
	}
	args := []string{"repomap task", "--tokens=" + fmt.Sprint(nextTokens)}
	if len(consumed) > 0 {
		args = append(args, "--consumed", shellTaskArg(strings.Join(consumed, ",")))
	}
	args = append(args, shellTaskArg(report.Goal), shellTaskArg(report.Root))
	return []string{strings.Join(args, " ")}
}
func shellTaskArg(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

// FormatTask renders the human packet exclusively from the shared task report.
func FormatTask(r TaskReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Task: %s\n\nRoot: %s\nBudget: %d/%d tokens\n", r.Goal, r.Root, r.Budget.UsedTokens, r.Budget.MaxTokens)
	writeTaskStrings(&b, "Rules", taskRulePaths(r.Rules))
	writeTaskChanges(&b, r.RelatedChanges)
	if len(r.Targets) > 0 {
		b.WriteString("\n## Targets\n")
		for _, t := range r.Targets {
			fmt.Fprintf(&b, "- %s confidence=%s package=%s risk=%s parse=%s\n", t.Path, t.Confidence, t.Package, t.Risk, t.Parse)
			if len(t.AffectedPackages) > 0 {
				fmt.Fprintf(&b, "  affected_packages: %s\n", strings.Join(t.AffectedPackages, ", "))
			}
			for _, e := range t.Evidence {
				fmt.Fprintf(&b, "  evidence %s: %s\n", e.Field, e.Value)
			}
			for _, s := range t.Symbols {
				fmt.Fprintf(&b, "  symbol %s %s lines=%d-%d %s\n", s.Kind, s.Name, s.Line, s.EndLine, s.Signature)
				if s.Doc != "" {
					fmt.Fprintf(&b, "    doc: %s\n", s.Doc)
				}
			}
			for _, rel := range t.Relationships {
				fmt.Fprintf(&b, "  relationship %s %s (%s)\n", rel.Kind, rel.Path, rel.Provenance)
			}
			for _, effect := range t.Effects {
				fmt.Fprintf(&b, "  effect %s (%s)\n", taskEffectString(effect), effect.Provenance)
			}
			for _, src := range t.Source {
				fmt.Fprintf(&b, "  source %s:\n", src.Symbol)
				for _, line := range src.Lines {
					fmt.Fprintf(&b, "    %d: %s\n", line.Number, line.Text)
				}
			}
		}
	}
	writeTaskReadNext(&b, r.ReadNext)
	writeTaskStrings(&b, "Verify", r.VerifyCommands)
	writeTaskStrings(&b, "Follow up", r.FollowUpCommands)
	writeTaskStrings(&b, "Diagnostics", r.Diagnostics)
	if len(r.Truncations) > 0 {
		b.WriteString("\n## Truncations\n")
		for _, t := range r.Truncations {
			fmt.Fprintf(&b, "- %s: %d/%d (%s)\n", t.Field, t.Shown, t.Total, t.Reason)
		}
	}
	return b.String()
}
func taskRulePaths(rules []TaskRule) []string {
	out := make([]string, 0, len(rules))
	for _, rule := range rules {
		out = append(out, rule.Path)
	}
	return out
}
func writeTaskStrings(b *strings.Builder, title string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## %s\n", title)
	for _, value := range values {
		fmt.Fprintf(b, "- %s\n", value)
	}
}
func writeTaskChanges(b *strings.Builder, changes []TaskRelatedChange) {
	if len(changes) == 0 {
		return
	}
	b.WriteString("\n## Related changes\n")
	for _, change := range changes {
		fmt.Fprintf(b, "- %s %s\n", change.Status, change.Path)
	}
}
func writeTaskReadNext(b *strings.Builder, items []ReadNextItem) {
	if len(items) == 0 {
		return
	}
	b.WriteString("\n## Read next\n")
	for _, item := range items {
		fmt.Fprintf(b, "- %s:%d-%d %s\n", item.File, item.StartLine, item.EndLine, item.Reason)
	}
}
