package cli

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type boundedOutput struct {
	Text       string
	Truncated  bool
	Lines      int
	Bytes      int
	OutputLine int
	OutputByte int
}

func formatBoundedText(raw string, maxLines, maxBytes int) boundedOutput {
	if raw == "" {
		return boundedOutput{}
	}
	totalBytes := len(raw)
	lines := strings.SplitAfter(raw, "\n")
	totalLines := len(lines)
	if strings.HasSuffix(raw, "\n") {
		totalLines--
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
	}

	var b strings.Builder
	outputLines := 0
	outputBytes := 0
	truncated := false
	for _, line := range lines {
		if maxLines > 0 && outputLines >= maxLines {
			truncated = true
			break
		}
		if maxBytes > 0 && outputBytes+len(line) > maxBytes {
			remaining := maxBytes - outputBytes
			if remaining > 0 {
				// Round cut down to a valid UTF-8 rune boundary so we never
				// split a multi-byte sequence mid-rune.
				cut := remaining
				for cut > 0 && !utf8.RuneStart(line[cut]) {
					cut--
				}
				b.WriteString(line[:cut])
				outputBytes += cut
				outputLines++
			}
			truncated = true
			break
		}
		b.WriteString(line)
		outputLines++
		outputBytes += len(line)
	}

	if truncated {
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "\n[Output truncated: showing %d of %d lines (%d of %d bytes). Narrow your query.]\n",
			outputLines, totalLines, outputBytes, totalBytes)
	}

	return boundedOutput{
		Text:       b.String(),
		Truncated:  truncated,
		Lines:      totalLines,
		Bytes:      totalBytes,
		OutputLine: outputLines,
		OutputByte: outputBytes,
	}
}
