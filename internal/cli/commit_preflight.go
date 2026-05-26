package cli

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// preflightProbeTimeout caps each git/gh probe so a stalled subprocess cannot
// hang the preflight indefinitely. Probes are individually small (status,
// branch, log --oneline); 10s is generous yet bounded. Declared as var so
// tests may shorten it; not part of the public API.
var preflightProbeTimeout = 10 * time.Second //nolint:gochecknoglobals // var not const: tests may shorten it

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
			ctx := cmd.Context()

			// Each probe is independent — a failure (or timeout) in one must not
			// abort the rest. runTrimmed/runLines/unpushedLines/ghAuthLine each
			// derive their own bounded sub-context from ctx.
			branch := runTrimmed(ctx, "git", "branch", "--show-current")
			working := runLines(ctx, "git", "status", "--short", "--", ".") // cap at 20 lines
			if len(working) > 20 {
				working = working[:20]
			}
			remote := runTrimmed(ctx, "git", "remote")
			if remote == "" {
				remote = "(none)"
			} else {
				// take first line only (mirrors `| head -1`)
				if idx := strings.IndexByte(remote, '\n'); idx >= 0 {
					remote = remote[:idx]
				}
			}
			unpushed := unpushedLines(ctx)
			latestTag := runTrimmed(ctx, "git", "describe", "--tags", "--abbrev=0")
			if latestTag == "" {
				latestTag = "(none)"
			}
			ghAuth := ghAuthLine(ctx)

			// Column widths match cpt.md exactly:
			//   Branch:      (6 spaces after colon)
			//   Working:     (5 spaces)
			//   Remote:      (6 spaces)
			//   Unpushed:    (4 spaces)
			//   Latest tag:  (2 spaces)
			//   GH auth:     (5 spaces)
			if _, err := fmt.Fprintf(out, "Branch:      %s\n", branch); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(out, "Working:     %s\n", strings.Join(working, "\n             ")); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(out, "Remote:      %s\n", remote); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(out, "Unpushed:    %s\n", unpushed); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(out, "Latest tag:  %s\n", latestTag); err != nil {
				return err
			}
			_, err := fmt.Fprintf(out, "GH auth:     %s\n", ghAuth)
			return err
		},
	}
}

// probeCmd builds an exec.Cmd whose context is bounded by
// preflightProbeTimeout AND any deadline already on parent. Caller must invoke
// the returned cancel func (defer) to release resources. WaitDelay ensures the
// process is force-detached if it ignores SIGKILL.
func probeCmd(parent context.Context, name string, args ...string) (*exec.Cmd, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(parent, preflightProbeTimeout)
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // callers pass only "git" or "gh"
	cmd.WaitDelay = 2 * time.Second
	return cmd, cancel
}

// runTrimmed runs a command and returns stdout trimmed of surrounding whitespace.
// Errors are swallowed — callers apply their own fallback.
func runTrimmed(ctx context.Context, name string, args ...string) string {
	cmd, cancel := probeCmd(ctx, name, args...)
	defer cancel()
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out))
}

// runLines runs a command and splits stdout into non-empty lines.
func runLines(ctx context.Context, name string, args ...string) []string {
	cmd, cancel := probeCmd(ctx, name, args...)
	defer cancel()
	out, _ := cmd.Output()
	var lines []string
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}

// unpushedLines returns up to 5 lines of `git log --oneline @{u}..HEAD`,
// or the literal "(no upstream)" when the command fails (no upstream set).
func unpushedLines(ctx context.Context) string {
	cmd, cancel := probeCmd(ctx, "git", "log", "--oneline", "@{u}..HEAD")
	defer cancel()
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
func ghAuthLine(ctx context.Context) string {
	cmd, cancel := probeCmd(ctx, "gh", "auth", "status")
	defer cancel()
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
