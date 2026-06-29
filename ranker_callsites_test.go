//go:build !notreesitter

package repomap

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyCallSiteReferenceBonus(t *testing.T) {
	ranked := []RankedFile{
		{
			FileSymbols: &FileSymbols{
				Path:     "src/service.ts",
				Language: "typescript",
				Symbols:  []Symbol{{Name: "PaymentService", Kind: "class", Exported: true}},
			},
			ScoreComponents: map[string]int{},
		},
		{
			FileSymbols: &FileSymbols{
				Path:      "src/controller.ts",
				Language:  "typescript",
				CallSites: []CallSite{{Name: "PaymentService", Line: 3}, {Name: "PaymentService.boot", Line: 4}},
			},
			ScoreComponents: map[string]int{},
		},
		{
			FileSymbols: &FileSymbols{
				Path:      "src/other.ts",
				Language:  "typescript",
				CallSites: []CallSite{{Name: "PaymentService", Line: 2}},
			},
			ScoreComponents: map[string]int{},
		},
		{
			FileSymbols: &FileSymbols{
				Path:      "src/go_user.go",
				Language:  "go",
				CallSites: []CallSite{{Name: "PaymentService", Line: 2}},
			},
			ScoreComponents: map[string]int{},
		},
	}

	ApplyCallSiteReferenceBonus(ranked)

	byPath := rankedByPath(ranked)
	assert.Equal(t, 8, byPath["src/service.ts"].ScoreComponents[scoreComponentCallSites])
	assert.Equal(t, 8, byPath["src/service.ts"].Score)
	assert.Zero(t, byPath["src/controller.ts"].ScoreComponents[scoreComponentCallSites])
}

func TestBuildStructuredOutputIncludesCallSiteEvidence(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		path := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}

	write("src/service.ts", "export class PaymentService {}\n")
	write("src/controller.ts", "import { PaymentService } from './service';\nnew PaymentService();\n")

	m := New(root, Config{MaxTokens: 0})
	require.NoError(t, m.Build(context.Background()))

	out := m.StructuredOutput()
	var service StructuredFile
	for _, file := range out.Files {
		if file.Path == "src/service.ts" {
			service = file
			break
		}
	}
	require.Equal(t, "src/service.ts", service.Path)
	assert.Equal(t, callSiteBonusPerRef, service.ScoreComponents[scoreComponentCallSites])
	assert.Contains(t, relationEvidenceKinds(service.RelationEvidence), "call_site_reference")
}

func relationEvidenceKinds(evidence []StructuredEvidence) []string {
	kinds := make([]string, 0, len(evidence))
	for _, item := range evidence {
		kinds = append(kinds, item.Kind)
	}
	return kinds
}
