package cli

import (
	"bufio"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

// newCommitPreflightCmd builds the `repomap commit-preflight` subcommand.
// It runs six independent git/gh probes and emits a column-aligned context
// block identical to the cpt.md preflight header, so commit-agent can read
// it via `!repomap commit-preflight` instead of six separate shell expansions.
func newCommitPreflightCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "commit-preflight",
		Short: "Emit git/gh context block for commit preflight",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			// Each probe is independent — a failure in one must not abort the rest.
			branch := runTrimmed("git", "branch", "--show-current")
			working := runLines("git", "status", "--short", "--", ".") // cap at 20 lines
			if len(working) > 20 {
				working = working[:20]
			}
			remote := runTrimmed("git", "remote")
			if remote == "" {
				remote = "(none)"
			} else {
				// take first line only (mirrors `| head -1`)
				if idx := strings.IndexByte(remote, '\n'); idx >= 0 {
					remote = remote[:idx]
				}
			}
			unpushed := unpushedLines()
			latestTag := runTrimmed("git", "describe", "--tags", "--abbrev=0")
			if latestTag == "" {
				latestTag = "(none)"
			}
			ghAuth := ghAuthLine()

			// Column widths match cpt.md exactly:
			//   Branch:      (6 spaces after colon)
			//   Working:     (5 spaces)
			//   Remote:      (6 spaces)
			//   Unpushed:    (4 spaces)
			//   Latest tag:  (2 spaces)
			//   GH auth:     (5 spaces)
			fmt.Fprintf(out, "Branch:      %s\n", branch)
			fmt.Fprintf(out, "Working:     %s\n", strings.Join(working, "\n             "))
			fmt.Fprintf(out, "Remote:      %s\n", remote)
			fmt.Fprintf(out, "Unpushed:    %s\n", unpushed)
			fmt.Fprintf(out, "Latest tag:  %s\n", latestTag)
			fmt.Fprintf(out, "GH auth:     %s\n", ghAuth)
			return nil
		},
	}
}

// runTrimmed runs a command and returns stdout trimmed of surrounding whitespace.
// Errors are swallowed — callers apply their own fallback.
func runTrimmed(name string, args ...string) string {
	out, _ := exec.Command(name, args...).Output()
	return strings.TrimSpace(string(out))
}

// runLines runs a command and splits stdout into non-empty lines.
func runLines(name string, args ...string) []string {
	out, _ := exec.Command(name, args...).Output()
	var lines []string
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}

// unpushedLines returns up to 5 lines of `git log --oneline @{u}..HEAD`,
// or the literal "(no upstream)" when the command fails (no upstream set).
func unpushedLines() string {
	cmd := exec.Command("git", "log", "--oneline", "@{u}..HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "(no upstream)"
	}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	var lines []string
	for sc.Scan() && len(lines) < 5 {
		lines = append(lines, sc.Text())
	}
	return strings.Join(lines, "\n             ")
}

var ghAuthRe = regexp.MustCompile(`(?i)logged in|not logged`)

// ghAuthLine captures `gh auth status` combined output, finds the first line
// matching /(?i)logged in|not logged/, and returns it trimmed.
// Returns an empty string if gh is not installed or no matching line is found.
func ghAuthLine() string {
	cmd := exec.Command("gh", "auth", "status")
	out, _ := cmd.CombinedOutput()
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		line := sc.Text()
		if ghAuthRe.MatchString(line) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}
