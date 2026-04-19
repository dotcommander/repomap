//go:build !notreesitter

package repomap

import "strings"

// phpStripDocblock removes the /** / * framing from a PHPDoc comment block,
// returning plain text suitable for firstSentence extraction.
// Input: raw comment text including /** and */.
func phpStripDocblock(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "/**")
	raw = strings.TrimSuffix(raw, "*/")
	raw = strings.TrimSpace(raw)

	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "* ")
		line = strings.TrimPrefix(line, "*")
		line = strings.TrimSpace(line)
		// Drop @tag lines — they are signature-adjacent, not narrative.
		if strings.HasPrefix(line, "@") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, " ")
}

// phpFirstSentence extracts the first sentence from stripped PHPDoc text.
// Takes content up to and including the first '.', '!', or '?', then trims
// to 60 runes. Returns "" if the result is fewer than 5 characters (noise guard).
func phpFirstSentence(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.IndexAny(text, ".!?\n"); i >= 0 {
		if text[i] != '\n' {
			text = text[:i+1] // include the sentence terminator
		} else {
			text = text[:i]
		}
	}
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) > 60 {
		text = string(runes[:60])
	}
	if len([]rune(text)) < 5 {
		return ""
	}
	return text
}
