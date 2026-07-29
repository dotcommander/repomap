package cli

// commit_auto.go — `repomap commit auto` subcommand.
//
// Atomic prep+finish wrapper. Runs `commit prep` then dispatches:
//
//	ready           → runs `commit finish` with auto-detected --push/--tag
//	needs_judgment  → emits prep payload unchanged; caller adjudicates
//	abort           → emits prep payload unchanged; caller surfaces reason
//
// Mode (FULL vs LOCAL) is auto-detected from preflight signals: a remote AND
// gh auth means FULL (push, tag if version supplied); anything else is LOCAL.

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/dotcommander/repomap"
)

type commitAutoCommand struct {
	Directory  string `arg:"" optional:"" default:"." type:"path" help:"Repository directory"`
	NoReview   bool   `help:"Forwarded to prep: skip simplify scan"`
	AllowLarge bool   `help:"Forwarded to prep: suppress kitchen-sink guard"`
	Tag        string `help:"Annotated tag (vX.Y.Z); only honored in FULL mode"`
	Decisions  string `help:"Forwarded to finish (reserved)"`
	ForceMode  string `hidden:"" help:"Test hook: FULL|LOCAL"`
}

func (c *commitAutoCommand) Validate() error {
	if c.ForceMode != "" && c.ForceMode != "FULL" && c.ForceMode != "LOCAL" {
		return fmt.Errorf("--force-mode must be FULL or LOCAL")
	}
	return nil
}

func (c *commitAutoCommand) Run(ctx context.Context, ioctx *commandIO) error {
	abs, err := filepath.Abs(c.Directory)
	if err != nil {
		return fmt.Errorf("resolve root: %w", err)
	}
	return runCommitAuto(ctx, ioctx.stdout, ioctx.stderr, abs, c.NoReview, c.AllowLarge, c.Tag, c.Decisions, c.ForceMode)
}

// runCommitAuto orchestrates the prep → (emit | finish) routing.
// w receives the prep payload on the non-ready branches; tests pass a buffer.
func runCommitAuto(ctx context.Context, w, stderr io.Writer, repoRoot string, noReview, allowLarge bool, tag, decisions, forceMode string) error {
	// withTag=false: the release-gate is a separate concern from the auto-tag
	// path; auto only tags when status=ready AND mode=FULL AND --tag supplied.
	payload, err := buildPrepPayload(ctx, repoRoot, noReview, false, allowLarge)
	if err != nil {
		return err
	}
	switch forceMode {
	case "FULL", "LOCAL":
		payload.ModeHint = forceMode
	default:
		payload.ModeHint = repomap.ModeHint(payload.Preflight)
	}

	if payload.Status != repomap.PrepStatusReady {
		return emitPrep(w, true, payload)
	}

	push := payload.ModeHint == "FULL"
	effectiveTag := ""
	if push && tag != "" {
		effectiveTag = tag
	}
	return runCommitFinish(ctx, w, stderr, payload.PrepToken, decisions, push, effectiveTag, true)
}
