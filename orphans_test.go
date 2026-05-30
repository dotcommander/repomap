package repomap

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRefs struct{ byName map[string][]Location }

func (f fakeRefs) Refs(_ context.Context, _ string, _ int, symbol string) ([]Location, error) {
	return f.byName[symbol], nil
}

type errRefs struct{}

func (errRefs) Refs(context.Context, string, int, string) ([]Location, error) {
	return nil, assert.AnError
}

func orphanNames(c []OrphanCandidate) []string {
	names := make([]string, 0, len(c))
	for _, cand := range c {
		names = append(names, cand.Name)
	}
	return names
}

func TestOrphanCandidatesBuckets(t *testing.T) {
	t.Parallel()

	m := New("/repo", DefaultConfig())
	m.ranked = []RankedFile{
		{
			FileSymbols: &FileSymbols{
				Path:     "service.go",
				Language: "go",
				Symbols: []Symbol{
					{Name: "ZeroRef", Kind: "function", Exported: true, Line: 10},
					{Name: "TestOnly", Kind: "function", Exported: true, Line: 20},
					{Name: "LiveRef", Kind: "function", Exported: true, Line: 30},
					{Name: "WithReceiver", Kind: "method", Receiver: "*Svc", Exported: true, Line: 40},
					{Name: "lowercase", Kind: "function", Exported: false, Line: 50},
				},
			},
		},
		{
			Tag: "entry",
			FileSymbols: &FileSymbols{
				Path:     "main.go",
				Language: "go",
				Symbols: []Symbol{
					{Name: "Run", Kind: "function", Exported: true, Line: 5},
				},
			},
		},
		{
			FileSymbols: &FileSymbols{
				Path:     "service_test.go",
				Language: "go",
				Symbols: []Symbol{
					{Name: "TestSomething", Kind: "function", Exported: true, Line: 5},
				},
			},
		},
	}

	q := fakeRefs{byName: map[string][]Location{
		// Only its own def line → ZeroRefs.
		"ZeroRef": {{File: "/repo/service.go", Line: 10}},
		// Def site + a *_test.go ref → TestOnlyRefs.
		"TestOnly": {
			{File: "/repo/service.go", Line: 20},
			{File: "/repo/service_test.go", Line: 7},
		},
		// Def site + a non-test ref → neither bucket.
		"LiveRef": {
			{File: "/repo/service.go", Line: 30},
			{File: "/repo/other.go", Line: 99},
		},
		// Zero inbound (only its own def line) → ZeroRefs, Receiver preserved.
		"WithReceiver": {{File: "/repo/service.go", Line: 40}},
		// Defined in a *_test.go file → excluded from both buckets.
		"TestSomething": {{File: "/repo/service_test.go", Line: 5}},
	}}

	report, err := m.OrphanCandidates(context.Background(), q)
	require.NoError(t, err)

	assert.Equal(t, orphanCaveat, report.Caveat)

	zeroNames := orphanNames(report.ZeroRefs)
	assert.Contains(t, zeroNames, "ZeroRef")
	assert.Contains(t, zeroNames, "WithReceiver")
	assert.NotContains(t, zeroNames, "LiveRef")
	assert.NotContains(t, zeroNames, "lowercase")
	assert.NotContains(t, zeroNames, "Run")
	// Defined in a *_test.go file → excluded from both buckets.
	assert.NotContains(t, zeroNames, "TestSomething")

	testOnlyNames := orphanNames(report.TestOnlyRefs)
	assert.Equal(t, []string{"TestOnly"}, testOnlyNames)
	assert.NotContains(t, testOnlyNames, "TestSomething")

	var withReceiver *OrphanCandidate
	for i := range report.ZeroRefs {
		if report.ZeroRefs[i].Name == "WithReceiver" {
			withReceiver = &report.ZeroRefs[i]
		}
	}
	require.NotNil(t, withReceiver)
	assert.Equal(t, "*Svc", withReceiver.Receiver)
}

func TestOrphanCandidatesSkipsRefsErrors(t *testing.T) {
	t.Parallel()

	m := New("/repo", DefaultConfig())
	m.ranked = []RankedFile{
		{
			FileSymbols: &FileSymbols{
				Path:     "service.go",
				Language: "go",
				Symbols: []Symbol{
					{Name: "Exported", Kind: "function", Exported: true, Line: 10},
				},
			},
		},
	}

	report, err := m.OrphanCandidates(context.Background(), errRefs{})
	require.NoError(t, err)
	assert.Empty(t, report.ZeroRefs)
	assert.Empty(t, report.TestOnlyRefs)
}
