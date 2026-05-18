//go:build !notreesitter

package repomap

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTreeSitterTypeScriptMethodReceivers(t *testing.T) {
	fs := parseWithTreeSitter([]byte(`
class Widget {
  render() {}
}

interface Runner {
  run(): void
}

function render() {}
`), "typescript", "widget.ts")
	require.NotNil(t, fs)

	method := findContextSymbol(fs.Symbols, "method", "render", "Widget")
	require.NotNil(t, method)
	assert.Equal(t, "Widget", method.Receiver)

	ifaceMethod := findContextSymbol(fs.Symbols, "method", "run", "Runner")
	require.NotNil(t, ifaceMethod)
	assert.Equal(t, "Runner", ifaceMethod.Receiver)

	topLevel := findContextSymbol(fs.Symbols, "function", "render", "")
	require.NotNil(t, topLevel)
	assert.Empty(t, topLevel.Receiver)
}

func TestTreeSitterPythonMethodReceivers(t *testing.T) {
	fs := parseWithTreeSitter([]byte(`
class Service:
    def handle(self):
        pass

def handle():
    pass
`), "python", "service.py")
	require.NotNil(t, fs)

	method := findContextSymbol(fs.Symbols, "method", "handle", "Service")
	require.NotNil(t, method)
	assert.Equal(t, "Service", method.Receiver)

	topLevel := findContextSymbol(fs.Symbols, "function", "handle", "")
	require.NotNil(t, topLevel)
	assert.Empty(t, topLevel.Receiver)
}

func findContextSymbol(symbols []Symbol, kind, name, receiver string) *Symbol {
	for i := range symbols {
		if symbols[i].Kind == kind && symbols[i].Name == name && symbols[i].Receiver == receiver {
			return &symbols[i]
		}
	}
	return nil
}
