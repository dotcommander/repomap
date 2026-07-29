package cli

import (
	"context"
	"fmt"

	"github.com/dotcommander/repomap"
)

type commitCommand struct {
	Analyze commitAnalyzeCommand `cmd:"" help:"Analyze changeset and emit a structured commit plan as JSON"`
	Execute commitExecuteCommand `cmd:"" help:"Execute a commit plan produced by commit analyze"`
	Prep    commitPrepCommand    `cmd:"" help:"Prepare a commit plan (analyze + fix + consolidate) in one call"`
	Finish  commitFinishCommand  `cmd:"" help:"Execute a prepared commit plan (output of commit prep)"`
	Auto    commitAutoCommand    `cmd:"" help:"Atomic prep+finish: runs both when status=ready, returns prep JSON otherwise"`
}

type commitAnalyzeCommand struct {
	Directory  string  `arg:"" optional:"" default:"." type:"path" help:"Directory to analyze"`
	Tag        bool    `help:"Activate release gate (go.mod tidy before commit)"`
	Pretty     bool    `help:"Pretty-print JSON (default: compact)"`
	Tmpdir     string  `help:"Override temp directory (for tests)"`
	Confidence float64 `default:"0.75" help:"Clustering confidence cutoff (0.0–1.0)"`
}

func (c *commitAnalyzeCommand) Validate() error {
	if c.Confidence < 0 || c.Confidence > 1 {
		return fmt.Errorf("--confidence must be between 0 and 1")
	}
	return nil
}

func (c *commitAnalyzeCommand) Run(ctx context.Context, ioctx *commandIO) error {
	analysis, err := repomap.AnalyzeCommit(ctx, repomap.AnalyzeOptions{Root: c.Directory, Tag: c.Tag, ConfidenceCutoff: c.Confidence, Tmpdir: c.Tmpdir})
	if err != nil {
		return fmt.Errorf("analyze: %w", err)
	}
	data, err := repomap.EncodeJSON(analysis, c.Pretty)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	if _, err := ioctx.stdout.Write(data); err != nil {
		return err
	}
	_, err = fmt.Fprintln(ioctx.stdout)
	return err
}

type commitExecuteCommand struct {
	PlanFile         string `required:"" type:"path" help:"Path to CommitAnalysis JSON (required)"`
	Push             bool   `help:"git push origin <branch> --follow-tags after commits"`
	Tag              string `help:"Create annotated tag at HEAD (vX.Y.Z format)"`
	NoRelease        bool   `help:"Skip gh release create even with --push --tag"`
	ReleaseNotesFrom string `help:"Pass --notes-start-tag to gh release create"`
	DryRun           bool   `help:"Print intended actions, mutate nothing"`
	JSON             bool   `help:"Emit machine-readable result on stdout"`
	SkipFix          bool   `help:"Bypass cap-3/fold-riders/merge-smallest consolidation"`
}

func (c *commitExecuteCommand) Run(ctx context.Context, ioctx *commandIO) error {
	result, err := repomap.ExecuteCommit(ctx, repomap.ExecuteOptions{PlanFile: c.PlanFile, Push: c.Push, Tag: c.Tag, NoRelease: c.NoRelease, ReleaseNotesFrom: c.ReleaseNotesFrom, DryRun: c.DryRun, JSON: c.JSON, SkipFix: c.SkipFix, Output: ioctx.stdout})
	if err != nil {
		if result != nil && c.JSON {
			data, encErr := repomap.EncodeExecuteResult(result, false)
			if encErr != nil {
				return fmt.Errorf("encode partial result: %w", encErr)
			}
			if _, writeErr := ioctx.stdout.Write(data); writeErr != nil {
				return writeErr
			}
			if _, writeErr := fmt.Fprintln(ioctx.stdout); writeErr != nil {
				return writeErr
			}
		}
		return commandExitError{code: repomap.ExecExitCode(err), err: err}
	}
	if !c.JSON {
		return nil
	}
	data, err := repomap.EncodeExecuteResult(result, false)
	if err != nil {
		return fmt.Errorf("encode result: %w", err)
	}
	if _, err := ioctx.stdout.Write(data); err != nil {
		return err
	}
	_, err = fmt.Fprintln(ioctx.stdout)
	return err
}
