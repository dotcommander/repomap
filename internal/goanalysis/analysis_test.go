package goanalysis

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyzeSemanticPrecisionAndDeterminism(t *testing.T) {
	t.Parallel()

	root := writeModule(t, map[string]string{
		"contract.go": `package fixture

type Worker interface { Work() }
type Agent struct{}
func (*Agent) Work() {}
`,
		"a.go": `package fixture

type A struct{}
func (A) Get() string { return "a" }
`,
		"b.go": `package fixture

type B struct{}
func (B) Get() string { return "b" }
`,
		"use.go": `package fixture

func Use() string { return A{}.Get() }
`,
	})

	first, err := Analyze(context.Background(), Options{Root: root, IncludeCalls: true})
	require.NoError(t, err)
	second, err := Analyze(context.Background(), Options{Root: root, IncludeCalls: true})
	require.NoError(t, err)

	assert.Equal(t, resultSnapshot(first), resultSnapshot(second), "repeated analysis must have stable ordering and paths")
	assert.Contains(t, first.Implementations, Implementation{
		TypeFile: "contract.go", TypeName: "Agent",
		InterfacePath: "example.com/fixture", InterfaceName: "Worker",
	}, "the pointer method set must satisfy Worker")

	var getCalls []CallEdge
	for _, call := range first.Calls {
		if call.CalleeSymbol == "Get" {
			getCalls = append(getCalls, call)
		}
	}
	require.Len(t, getCalls, 1, "A.Get must not be attributed to the same-named B.Get")
	assert.Equal(t, "a.go", getCalls[0].CalleeFile)
	assert.Equal(t, "use.go", getCalls[0].CallerFile)
}

func TestAnalyzeSkipsCallGraphUnlessRequested(t *testing.T) {
	t.Parallel()
	root := writeModule(t, map[string]string{
		"main.go": "package main\n\nfunc Target() {}\nfunc main() { Target() }\n",
	})

	result, err := Analyze(context.Background(), Options{Root: root})
	require.NoError(t, err)
	assert.Empty(t, result.Calls)
	for _, edge := range result.Edges {
		assert.NotEqual(t, EdgeCall, edge.Kind)
	}
}

func TestAnalyzeGenericCallsUseSourceOriginsAndReceivers(t *testing.T) {
	t.Parallel()

	root := writeModule(t, map[string]string{
		"generic.go": `package fixture

type Box[T any] struct{}

func (Box[T]) Value() {}
func (*Box[T]) Pointer() {}

type NamedA struct{}
func (NamedA) Get() {}

type NamedB struct{}
func (NamedB) Get() {}

func callValue[T interface{ Value() }](value T) { value.Value() }

func Use() {
	callValue(Box[int]{})
	callValue(Box[string]{})
	(&Box[int]{}).Pointer()
	NamedA{}.Get()
	NamedB{}.Get()
}
`,
	})

	result, err := Analyze(context.Background(), Options{Root: root, IncludeCalls: true})
	require.NoError(t, err)

	assert.Len(t, matchingCalls(result.Calls, "Value", "Box"), 1,
		"two generic instantiations at one source call must collapse to one caller coordinate")
	assert.Len(t, matchingCalls(result.Calls, "Pointer", "*Box"), 1,
		"pointer generic receivers retain their pointer marker")
	assert.Len(t, matchingCalls(result.Calls, "Get", "NamedA"), 1)
	assert.Len(t, matchingCalls(result.Calls, "Get", "NamedB"), 1)
	assert.NotEqual(t, matchingCalls(result.Calls, "Get", "NamedA"), matchingCalls(result.Calls, "Get", "NamedB"),
		"same-named methods remain receiver-disambiguated")
}

func TestAnalyzeReturnsDiagnosticsWithUsableResults(t *testing.T) {
	t.Parallel()

	root := writeModule(t, map[string]string{
		"valid/valid.go": `package valid

func Ready() bool { return true }
`,
		"broken/broken.go": `package broken

func Broken() { _ = missingIdentifier }
`,
	})

	result, err := Analyze(context.Background(), Options{Root: root})
	require.NoError(t, err, "package diagnostics must not discard usable analysis")
	assert.Contains(t, result.Files, "valid/valid.go")
	require.NotEmpty(t, result.Diagnostics)
	assert.Contains(t, result.Diagnostics[0].Message, "missingIdentifier")
}

func TestAnalyzeGoWorkspaceModules(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.work"), []byte("go 1.26\n\nuse (\n\t./one\n\t./two\n)\n"), 0o644))
	for _, module := range []string{"one", "two"} {
		dir := filepath.Join(root, module)
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/"+module+"\n\ngo 1.26\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, module+".go"), []byte("package "+module+"\n\nfunc Ready() bool { return true }\n"), 0o644))
	}

	result, err := Analyze(context.Background(), Options{Root: root})
	require.NoError(t, err)
	assert.Contains(t, result.Files, "one/one.go")
	assert.Contains(t, result.Files, "two/two.go")
}

type snapshot struct {
	Files           []string
	Diagnostics     []Diagnostic
	Edges           []Edge
	Calls           []CallEdge
	Implementations []Implementation
}

func resultSnapshot(result *Result) snapshot {
	files := make([]string, 0, len(result.Files))
	for path := range result.Files {
		files = append(files, path)
	}
	slices.Sort(files)
	return snapshot{
		Files: files, Diagnostics: result.Diagnostics, Edges: result.Edges,
		Calls: result.Calls, Implementations: result.Implementations,
	}
}

func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/fixture\n\ngo 1.26\n"), 0o644))
	for name, source := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(source), 0o644))
	}
	return root
}

func matchingCalls(calls []CallEdge, symbol, receiver string) []CallEdge {
	var out []CallEdge
	for _, call := range calls {
		if call.CalleeSymbol == symbol && call.CalleeReceiver == receiver {
			out = append(out, call)
		}
	}
	return out
}
