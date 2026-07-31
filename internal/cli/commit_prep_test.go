package cli

import (
	"testing"

	"github.com/dotcommander/repomap"
	commitflow "github.com/dotcommander/repomap/internal/commit"
)

func TestPrepStatusAbortsOnUncappedReviewCount(t *testing.T) {
	t.Parallel()

	status, reason := prepStatus(&repomap.CommitAnalysis{
		Secrets: repomap.SecretsSummary{AmbiguousCount: 6},
	}, 6, nil)

	if status != commitflow.PrepStatusAbort {
		t.Fatalf("status = %q, want %q", status, commitflow.PrepStatusAbort)
	}
	if reason == "" {
		t.Fatal("expected abort reason")
	}
}

func TestPrepStatusNeedsJudgmentWithCappedReviewCount(t *testing.T) {
	t.Parallel()

	status, reason := prepStatus(&repomap.CommitAnalysis{
		Secrets: repomap.SecretsSummary{AmbiguousCount: 5},
	}, 5, nil)

	if status != commitflow.PrepStatusNeedsJudgment {
		t.Fatalf("status = %q, want %q", status, commitflow.PrepStatusNeedsJudgment)
	}
	if reason != "" {
		t.Fatalf("reason = %q, want empty", reason)
	}
}
