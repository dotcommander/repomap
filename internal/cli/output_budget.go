package cli

import (
	"fmt"
	"io"

	"github.com/dotcommander/repomap"
)

type rankedEncoder func([]repomap.RankedFile) ([]byte, error)

// writeRankedWithinBudget encodes before writing so a budget or encoding
// failure cannot leave partial stdout or a partially replaced artifact.
func writeRankedWithinBudget(w io.Writer, maxTokens int, ranked []repomap.RankedFile, encode rankedEncoder) error {
	data, err := largestRankedEncoding(maxTokens, ranked, encode)
	if err != nil {
		return err
	}
	n, err := w.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return nil
}

func largestRankedEncoding(maxTokens int, ranked []repomap.RankedFile, encode rankedEncoder) ([]byte, error) {
	maxBytes := maxTokens * 4
	if maxTokens <= 0 || maxBytes/maxTokens != 4 {
		return nil, fmt.Errorf("output token budget must be positive")
	}

	full, err := encode(ranked)
	if err != nil {
		return nil, err
	}
	if len(full) <= maxBytes {
		return full, nil
	}

	minimum, err := encode(nil)
	if err != nil {
		return nil, err
	}
	if len(minimum) > maxBytes {
		return nil, fmt.Errorf(
			"output budget %d tokens cannot fit the minimum valid envelope (%d tokens)",
			maxTokens,
			encodedTokens(minimum),
		)
	}

	low, high := 0, len(ranked)
	best := minimum
	for low <= high {
		mid := low + (high-low)/2
		candidate, encodeErr := encode(ranked[:mid])
		if encodeErr != nil {
			return nil, encodeErr
		}
		if len(candidate) <= maxBytes {
			best = candidate
			low = mid + 1
			continue
		}
		high = mid - 1
	}
	return best, nil
}

func encodedTokens(data []byte) int {
	return (len(data) + 3) / 4
}
