package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatBoundedText_UnderLimit(t *testing.T) {
	t.Parallel()

	got := formatBoundedText("a\nb\n", 10, 100)

	assert.Equal(t, "a\nb\n", got.Text)
	assert.False(t, got.Truncated)
	assert.Equal(t, 2, got.Lines)
}

func TestFormatBoundedText_TruncatesByLines(t *testing.T) {
	t.Parallel()

	got := formatBoundedText("a\nb\nc\n", 2, 100)

	assert.True(t, got.Truncated)
	assert.Contains(t, got.Text, "showing 2 of 3 lines")
	assert.NotContains(t, got.Text, "c\n")
}

func TestFormatBoundedText_TruncatesByBytes(t *testing.T) {
	t.Parallel()

	got := formatBoundedText(strings.Repeat("x", 20), 10, 5)

	assert.True(t, got.Truncated)
	assert.Contains(t, got.Text, "5 of 20 bytes")
}
