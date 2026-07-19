package goanalysis

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAnalysisComparators(t *testing.T) {
	t.Parallel()

	t.Run("compareEdge", func(t *testing.T) {
		e1 := Edge{Kind: "call", File: "a.go", Line: 10, From: "FuncA", To: "FuncB"}
		e2 := Edge{Kind: "call", File: "a.go", Line: 10, From: "FuncA", To: "FuncB"}
		e3 := Edge{Kind: "call", File: "b.go", Line: 5, From: "FuncA", To: "FuncB"}

		assert.Equal(t, 0, compareEdge(e1, e2))
		assert.True(t, compareEdge(e1, e3) < 0)
	})

	t.Run("compareCall", func(t *testing.T) {
		c1 := CallEdge{CalleeFile: "a.go", CalleeSymbol: "SymA", CallerFile: "b.go", CallerLine: 20}
		c2 := CallEdge{CalleeFile: "a.go", CalleeSymbol: "SymA", CallerFile: "b.go", CallerLine: 20}
		c3 := CallEdge{CalleeFile: "a.go", CalleeSymbol: "SymB", CallerFile: "b.go", CallerLine: 20}

		assert.Equal(t, 0, compareCall(c1, c2))
		assert.True(t, compareCall(c1, c3) < 0)
	})

	t.Run("compareImplementation", func(t *testing.T) {
		i1 := Implementation{TypeFile: "struct.go", TypeName: "MyStruct", InterfacePath: "io", InterfaceName: "Reader"}
		i2 := Implementation{TypeFile: "struct.go", TypeName: "MyStruct", InterfacePath: "io", InterfaceName: "Reader"}
		i3 := Implementation{TypeFile: "struct.go", TypeName: "MyStruct", InterfacePath: "io", InterfaceName: "Writer"}

		assert.Equal(t, 0, compareImplementation(i1, i2))
		assert.True(t, compareImplementation(i1, i3) < 0)
	})
}
