package repomap

import "strings"

// truncateAtWord shortens text to at most max runes, preferring to cut at a
// word boundary so a trailing word is never split mid-token. If text already
// fits within max runes it is returned unchanged (no ellipsis). Otherwise the
// result is cut at the last space within the window (only when that space sits
// at index >= max/2, to avoid over-trimming short results), trailing spaces are
// removed, and a single "…" (U+2026) is appended.
func truncateAtWord(text string, max int) string {
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	window := runes[:max]
	cut := max
	for i := max - 1; i >= 0; i-- {
		if window[i] == ' ' {
			if i >= max/2 {
				cut = i
			}
			break
		}
	}
	result := strings.TrimRight(string(runes[:cut]), " ")
	return result + "…"
}
