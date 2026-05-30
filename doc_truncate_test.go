package repomap

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTruncateAtWord(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		text string
		max  int
		want string
	}{
		{
			name: "space in back half cuts at word boundary",
			// 56 chars, then a space, then more — boundary space sits in the back half of a 60-window.
			text: strings.Repeat("a", 56) + " trailingword here",
			max:  60,
			want: strings.Repeat("a", 56) + "…",
		},
		{
			name: "no space in back half hard-cuts at max",
			text: strings.Repeat("x", 80),
			max:  60,
			want: strings.Repeat("x", 60) + "…",
		},
		{
			name: "under max returned unchanged",
			text: "short doc string",
			max:  60,
			want: "short doc string",
		},
		{
			name: "exactly max returned unchanged",
			text: strings.Repeat("y", 60),
			max:  60,
			want: strings.Repeat("y", 60),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := truncateAtWord(tc.text, tc.max)
			assert.Equal(t, tc.want, got)
			if tc.text == tc.want {
				assert.NotContains(t, got, "…", "unchanged input must not gain an ellipsis")
			}
		})
	}
}
