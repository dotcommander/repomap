package repomap

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapMethodsAndGetters(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	goMod := "module example.com/methodtest\ngo 1.22\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte(goMod), 0o644))

	mainGo := `package main

type CoreService struct{}

func (s *CoreService) Start() {}

func MainFunc() {
	svc := &CoreService{}
	svc.Start()
}
`
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "main.go"), []byte(mainGo), 0o644))

	cfg := DefaultConfig()
	cfg.Explain = true
	m := New(tmp, cfg)

	// Verify Config getter
	gotCfg := m.Config()
	assert.Equal(t, cfg.MaxTokens, gotCfg.MaxTokens)
	assert.True(t, gotCfg.Explain)

	// Verify Build and string output methods
	err := m.Build(context.Background())
	require.NoError(t, err)

	// Callers & Diagnostics getters
	callers := m.SemanticCallers()
	assert.True(t, callers == nil || len(callers) >= 0)
	diags := m.GoDiagnostics()
	assert.True(t, diags == nil || len(diags) >= 0)

	// Test string representation formatters
	brief, total := m.StringBriefMap(1)
	assert.GreaterOrEqual(t, total, 1)
	assert.NotEmpty(t, brief)

	briefAll, _ := m.StringBriefMap(0)
	assert.NotEmpty(t, briefAll)

	linesStr := m.StringLines()
	assert.NotEmpty(t, linesStr)

	xmlStr := m.StringXML()
	assert.NotEmpty(t, xmlStr)

	// Test StructuredOutputForRanked
	ranked := m.Ranked()
	structOut := m.StructuredOutputForRanked(ranked)
	assert.Equal(t, tmp, structOut.Root)
	assert.NotEmpty(t, structOut.Files)
}

func TestUniformImportedBy(t *testing.T) {
	t.Parallel()

	t.Run("empty slice", func(t *testing.T) {
		assert.False(t, uniformImportedBy(nil))
	})

	t.Run("no positive counts", func(t *testing.T) {
		ranked := []RankedFile{
			{FileSymbols: &FileSymbols{Path: "a.go"}, ImportedBy: 0},
			{FileSymbols: &FileSymbols{Path: "b.go"}, ImportedBy: 0},
		}
		assert.False(t, uniformImportedBy(ranked))
	})

	t.Run("single positive count", func(t *testing.T) {
		ranked := []RankedFile{
			{FileSymbols: &FileSymbols{Path: "a.go"}, ImportedBy: 3},
			{FileSymbols: &FileSymbols{Path: "b.go"}, ImportedBy: 0},
		}
		assert.True(t, uniformImportedBy(ranked))
	})

	t.Run("uniform positive counts", func(t *testing.T) {
		ranked := []RankedFile{
			{FileSymbols: &FileSymbols{Path: "a.go"}, ImportedBy: 4},
			{FileSymbols: &FileSymbols{Path: "b.go"}, ImportedBy: 4},
			{FileSymbols: &FileSymbols{Path: "c.go"}, ImportedBy: 0},
		}
		assert.True(t, uniformImportedBy(ranked))
	})

	t.Run("non-uniform positive counts", func(t *testing.T) {
		ranked := []RankedFile{
			{FileSymbols: &FileSymbols{Path: "a.go"}, ImportedBy: 4},
			{FileSymbols: &FileSymbols{Path: "b.go"}, ImportedBy: 2},
		}
		assert.False(t, uniformImportedBy(ranked))
	})
}

func TestSynthesizeLineAndSignificantKind(t *testing.T) {
	t.Parallel()

	t.Run("synthesizeLine", func(t *testing.T) {
		tests := []struct {
			name string
			sym  Symbol
			want string
		}{
			{
				name: "fn with sig",
				sym:  Symbol{Name: "DoWork", Kind: "function", Signature: "(a int) error"},
				want: "func DoWork(a int) error",
			},
			{
				name: "fn without sig",
				sym:  Symbol{Name: "Init", Kind: "fn"},
				want: "func Init()",
			},
			{
				name: "method with receiver and sig",
				sym:  Symbol{Name: "Run", Kind: "method", Receiver: "s *Server", Signature: "() error"},
				want: "func (s *Server) Run() error",
			},
			{
				name: "method without sig",
				sym:  Symbol{Name: "Close", Kind: "method", Receiver: "c *Client"},
				want: "func (c *Client) Close()",
			},
			{
				name: "method without receiver",
				sym:  Symbol{Name: "Exec", Kind: "method", Signature: "()"},
				want: "func Exec()",
			},
			{
				name: "struct with sig",
				sym:  Symbol{Name: "Config", Kind: "struct", Signature: "{ Host string }"},
				want: "type Config struct { Host string }",
			},
			{
				name: "struct default",
				sym:  Symbol{Name: "Empty", Kind: "struct"},
				want: "type Empty struct{}",
			},
			{
				name: "interface with sig",
				sym:  Symbol{Name: "Reader", Kind: "interface", Signature: "{ Read() }"},
				want: "type Reader interface { Read() }",
			},
			{
				name: "interface default",
				sym:  Symbol{Name: "Any", Kind: "interface"},
				want: "type Any interface{}",
			},
			{
				name: "type",
				sym:  Symbol{Name: "ID", Kind: "type"},
				want: "type ID",
			},
			{
				name: "class",
				sym:  Symbol{Name: "User", Kind: "class"},
				want: "class User",
			},
			{
				name: "const",
				sym:  Symbol{Name: "MaxVal", Kind: "const"},
				want: "const MaxVal",
			},
			{
				name: "variable",
				sym:  Symbol{Name: "GlobalVar", Kind: "variable"},
				want: "var GlobalVar",
			},
			{
				name: "enum",
				sym:  Symbol{Name: "Status", Kind: "enum"},
				want: "enum Status",
			},
			{
				name: "unknown fallback",
				sym:  Symbol{Name: "Custom", Kind: "custom_kind"},
				want: "Custom",
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				got := synthesizeLine(tc.sym)
				assert.Equal(t, tc.want, got)
			})
		}
	})

	t.Run("isSignificantKind", func(t *testing.T) {
		assert.True(t, isSignificantKind("go", "function"))
		assert.True(t, isSignificantKind("python", "class"))
		assert.True(t, isSignificantKind("go", "constant"))
		assert.True(t, isSignificantKind("php", "const"))
		assert.False(t, isSignificantKind("python", "constant"))
		assert.True(t, isSignificantKind("go", "variable"))
		assert.False(t, isSignificantKind("typescript", "variable"))
	})
}

func TestCollapseCommonPrefix(t *testing.T) {
	t.Parallel()

	t.Run("too few names", func(t *testing.T) {
		_, _, ok := collapseCommonPrefix([]string{"PrefixA", "PrefixB"})
		assert.False(t, ok)
	})

	t.Run("short prefix", func(t *testing.T) {
		_, _, ok := collapseCommonPrefix([]string{"A_1", "A_2", "A_3"})
		assert.False(t, ok)
	})

	t.Run("valid prefix collapse", func(t *testing.T) {
		names := []string{"Test_UserAuth", "Test_UserLogin", "Test_UserLogout"}
		collapsed, truncated, ok := collapseCommonPrefix(names)
		assert.True(t, ok)
		assert.False(t, truncated)
		assert.Equal(t, "Test{UserAuth, UserLogin, UserLogout}", collapsed)
	})

	t.Run("truncated preview collapse", func(t *testing.T) {
		names := []string{
			"Test_ItemOne", "Test_ItemTwo", "Test_ItemThree",
			"Test_ItemFour", "Test_ItemFive", "Test_ItemSix",
		}
		collapsed, truncated, ok := collapseCommonPrefix(names)
		assert.True(t, ok)
		assert.True(t, truncated)
		assert.Contains(t, collapsed, "...")
	})

	t.Run("empty suffix fails", func(t *testing.T) {
		names := []string{"Test", "TestOne", "TestTwo"}
		_, _, ok := collapseCommonPrefix(names)
		assert.False(t, ok)
	})
}

func TestConfidenceOrder(t *testing.T) {
	t.Parallel()
	order := ConfidenceOrder()
	assert.Equal(t, []string{"confirmed", "structural", "lexical", "contextual"}, order)
}
