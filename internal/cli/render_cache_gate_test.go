package cli

import (
	"testing"

	"github.com/dotcommander/repomap"
	"github.com/stretchr/testify/assert"
)

// TestCallsRunDegraded covers the cache-write gate predicate directly: any
// nonzero LSP timeout or error count marks the run degraded so its result is
// NOT cached; a clean run is cacheable.
func TestCallsRunDegraded(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   repomap.CallsStats
		want bool
	}{
		{"clean run is cacheable", repomap.CallsStats{OK: 5}, false},
		{"zero work is cacheable", repomap.CallsStats{}, false},
		{"one timeout is degraded", repomap.CallsStats{OK: 4, Timeout: 1}, true},
		{"one error is degraded", repomap.CallsStats{OK: 4, Error: 1}, true},
		{"timeout and error degraded", repomap.CallsStats{OK: 2, Timeout: 1, Error: 1}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, callsRunDegraded(tc.in))
		})
	}
}
