package repomap

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// gitOutput runs git with -C root and returns stdout. Stderr is captured
// separately and surfaced verbatim in errors — no paraphrase.
func gitOutput(ctx context.Context, root string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// gitExec runs git with -C root, discards stdout. Stderr is captured and
// folded into the returned error verbatim — callers never see a bare "exit
// status 1" without the underlying git message.
func gitExec(ctx context.Context, root string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// Surface the subcmd name so errors read "git commit: <msg>: exit status 1".
		subcmd := ""
		if len(args) > 0 {
			subcmd = args[0]
		}
		return fmt.Errorf("git %s: %s: %w", subcmd, strings.TrimSpace(stderr.String()), err)
	}
	return nil
}

// currentBranch returns the active branch name.
func currentBranch(ctx context.Context, root string) (string, error) {
	out, err := gitOutput(ctx, root, "branch", "--show-current")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// execCommit stages files then commits them. The message is written to a temp
// file and passed via git commit -F to avoid any shell interpolation.
func execCommit(ctx context.Context, root string, files []string, msg string) (string, error) {
	addArgs := append([]string{"add", "--"}, files...)
	if err := gitExec(ctx, root, addArgs...); err != nil {
		return "", err
	}

	tmp, err := os.CreateTemp("", "repomap-msg-*.txt")
	if err != nil {
		return "", fmt.Errorf("create msg tmpfile: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(msg); err != nil {
		tmp.Close()
		return "", fmt.Errorf("write msg: %w", err)
	}
	tmp.Close()

	// Pass explicit pathspec after -- so only this group's files land in this
	// commit, even when other paths are staged from prior state.
	commitArgs := append([]string{"commit", "-F", tmp.Name(), "--"}, files...)
	if err := gitExec(ctx, root, commitArgs...); err != nil {
		return "", err
	}

	sha, err := gitOutput(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(sha), nil
}

// execTag creates an annotated tag at HEAD.
func execTag(ctx context.Context, root, tag string) error {
	return gitExec(ctx, root, "tag", "-a", tag, "-m", "Release "+tag)
}

// execPush pushes branch to origin with --follow-tags.
func execPush(ctx context.Context, root, branch string) error {
	return gitExec(ctx, root, "push", "origin", branch, "--follow-tags")
}

// execRelease creates a GitHub release via `gh release create`.
func execRelease(ctx context.Context, root, tag, notesFrom string) (string, error) {
	args := []string{"release", "create", tag, "--generate-notes", "--latest"}
	if notesFrom != "" {
		args = append(args, "--notes-start-tag", notesFrom)
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w\n%s", err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}
