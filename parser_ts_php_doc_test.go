//go:build !notreesitter

package repomap

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPhpFirstSentence(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		text string
		want string
	}{
		{
			name: "first sentence with terminator",
			text: "Returns the widget count for the account.",
			want: "Returns the widget count for the account.",
		},
		{
			name: "too short is noise-guarded to empty",
			text: "Hi.",
			want: "",
		},
		{
			name: "long text truncates at word boundary with ellipsis",
			text: strings.Repeat("a", 56) + " trailingword here",
			want: strings.Repeat("a", 56) + "…",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := phpFirstSentence(tc.text)
			assert.Equal(t, tc.want, got)
		})
	}
}
