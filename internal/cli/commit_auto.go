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
	"os"
	"path/filepath"

	"github.com/dotcommander/repomap"
	"github.com/spf13/cobra"
)

func newCommitAutoCmd() *cobra.Command {
	var (
		noReview   bool
		allowLarge bool
		tag        string
		decisions  string
		forceMode  string
	)
	cmd := &cobra.Command{
		Use:   "auto [directory]",
		Short: "Atomic prep+finish: runs both when status=ready, returns prep JSON otherwise",
		Long: `Runs 'commit prep --json' and inspects the status field:

  ready           runs 'commit finish' with auto-detected --push/--tag and emits finish's JSON
  needs_judgment  emits prep's JSON unchanged; caller (LLM) must adjudicate, then call 'commit finish'
  abort           emits prep's JSON unchanged; caller surfaces abort_reason

Mode (FULL vs LOCAL) is auto-detected from preflight:
  FULL  = remote present AND gh auth logged in  -> push (and tag if version supplied)
  LOCAL = anything else                          -> no push, no tag`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := "."
			if len(args) > 0 {
				root = args[0]
			}
			abs, err := filepath.Abs(root)
			if err != nil {
				return fmt.Errorf("resolve root: %w", err)
			}
			return runCommitAuto(cmd.Context(), os.Stdout, abs, noReview, allowLarge, tag, decisions, forceMode)
		},
	}
	cmd.Flags().BoolVar(&noReview, "no-review", false, "Forwarded to prep: skip simplify scan")
	cmd.Flags().BoolVar(&allowLarge, "allow-large", false, "Forwarded to prep: suppress kitchen-sink guard")
	cmd.Flags().StringVar(&tag, "tag", "", "Annotated tag (vX.Y.Z); only honored in FULL mode")
	cmd.Flags().StringVar(&decisions, "decisions", "", "Forwarded to finish (reserved)")
	cmd.Flags().StringVar(&forceMode, "force-mode", "", "Test hook: FULL|LOCAL")
	_ = cmd.Flags().MarkHidden("force-mode")
	return cmd
}

// runCommitAuto orchestrates the prep → (emit | finish) routing.
// w receives the prep payload on the non-ready branches; tests pass a buffer.
func runCommitAuto(ctx context.Context, w io.Writer, repoRoot string, noReview, allowLarge bool, tag, decisions, forceMode string) error {
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
	return runCommitFinish(ctx, payload.PrepToken, decisions, push, effectiveTag, true)
}
