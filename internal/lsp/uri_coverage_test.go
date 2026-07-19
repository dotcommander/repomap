package lsp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestURIHelpersAndProtocol(t *testing.T) {
	t.Parallel()

	t.Run("URIToPath", func(t *testing.T) {
		uri := "file:///Users/test/project/main.go"
		path := URIToPath(uri)
		assert.Equal(t, "/Users/test/project/main.go", path)
	})

	t.Run("isSymbolInformationArray", func(t *testing.T) {
		symbolInfoJSON := json.RawMessage(`[{"name": "foo", "location": {"uri": "file:///foo.go"}}]`)
		docSymbolJSON := json.RawMessage(`[{"name": "foo", "selectionRange": {}}]`)
		emptyJSON := json.RawMessage(`[]`)

		assert.True(t, isSymbolInformationArray(symbolInfoJSON))
		assert.False(t, isSymbolInformationArray(docSymbolJSON))
		assert.False(t, isSymbolInformationArray(emptyJSON))
	})

	t.Run("NewQuerier", func(t *testing.T) {
		mgr := NewManager("/tmp/root")
		q := NewQuerier(mgr)
		assert.NotNil(t, q)
		assert.Equal(t, "/tmp/root", q.cwd)
	})
}
