package cli

import (
	"testing"

	"github.com/dotcommander/repomap"
)

func TestPrepStatusAbortsOnUncappedReviewCount(t *testing.T) {
	t.Parallel()

	status, reason := prepStatus(&repomap.CommitAnalysis{
		Secrets: repomap.SecretsSummary{AmbiguousCount: 6},
	}, 6, nil)

	if status != repomap.PrepStatusAbort {
		t.Fatalf("status = %q, want %q", status, repomap.PrepStatusAbort)
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

	if status != repomap.PrepStatusNeedsJudgment {
		t.Fatalf("status = %q, want %q", status, repomap.PrepStatusNeedsJudgment)
	}
	if reason != "" {
		t.Fatalf("reason = %q, want empty", reason)
	}
}
