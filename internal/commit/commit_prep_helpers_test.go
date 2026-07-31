package commit

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBuildReviewItems_TruncatesOnRuneBoundary(t *testing.T) {
	t.Parallel()
	// "世" is a 3-byte rune; 70 repeats = 210 bytes, so a naive snippet[:200]
	// cut lands mid-rune (200 is not a multiple of 3).
	findings := []Finding{
		{File: "multi.go", Line: 1, DefaultAction: ActionReview, Snippet: strings.Repeat("世", 70)},
	}
	out := BuildReviewItems(findings, 10)
	if len(out) != 1 {
		t.Fatalf("len(out) = %d, want 1", len(out))
	}
	if !utf8.ValidString(out[0].Snippet) {
		t.Fatalf("truncated snippet is not valid UTF-8: %q", out[0].Snippet)
	}
	if len(out[0].Snippet) > 200 {
		t.Fatalf("truncated snippet length = %d, want <= 200", len(out[0].Snippet))
	}
}
