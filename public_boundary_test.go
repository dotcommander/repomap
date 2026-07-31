package repomap_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dotcommander/repomap"
)

// Compile representative public analysis API uses from an external package.
var (
	_ = repomap.AnalyzeCommit
	_ = repomap.EncodeJSON
	_ = repomap.RenderPlan
	_ = repomap.FormatTask
	_ = repomap.AuditHygiene
	_ = repomap.BuildStructuredOutput
	_ = (*repomap.Map).AuditRisks
	_ = (*repomap.Map).Context
	_ = (*repomap.Map).Impact
	_ = (*repomap.Map).Task
	_ = repomap.Config{}
	_ = repomap.AnalyzeOptions{}
	_ = repomap.CommitAnalysis{}
	_ = repomap.CommitGroup{}
	_ = repomap.Finding{}
	_ = repomap.SecretsSummary{}
)

func TestPublicPackageExcludesCommitMutationExports(t *testing.T) {
	t.Parallel()
	forbidden := map[string]bool{
		"ApplyCandidates": true, "ApplyFixFindings": true, "ApplyReviewDecisions": true,
		"BuildPrepStateBinding": true, "BuildReviewItems": true, "Candidate": true,
		"CommitRecord":      true,
		"ConsolidateGroups": true, "ContainsLowConf": true, "CrossRepoVerify": true,
		"DeletePrepState": true, "DetectJustfileRelease": true, "DetectSessionRepos": true,
		"EncodeExecuteResult": true, "ExecExitCode": true, "ExecuteCommit": true,
		"ExecuteFromGroups": true, "ExecuteOptions": true, "ExecuteResult": true,
		"GroupsToPlan": true, "IsKitchenSink": true, "LoadDiffSlice": true,
		"LoadFindings": true, "LoadPrepState": true, "ModeHint": true,
		"PersistPrepState": true, "PersistPrepStateAt": true, "Polish": true,
		"PolishGroup": true, "PostflightCheck": true, "PrepLowConf": true,
		"PrepPayload": true, "PrepPlanGroup": true, "PrepPreflight": true,
		"PrepReleaseGate": true, "PrepReviewItem": true, "PrepState": true,
		"PrepStatusAbort": true, "PrepStatusNeedsJudgment": true,
		"PrepStatusReady": true,
		"RepoStatus":      true, "ReviewDecision": true, "ReviewFindingCount": true,
		"RunReleaseGate": true, "RunReleaseGateContext": true,
		"RunSimplifyDetect": true, "SelfVerify": true, "StashArtifacts": true,
		"StashArtifactsContext": true, "ValidateConventionalMsg": true,
		"ValidateReviewDecisions": true, "ValidateTag": true,
		"VerifyPrepStateFresh": true, "VerifyResult": true,
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			for _, ident := range exportedDeclNames(decl) {
				if forbidden[ident] {
					t.Errorf("root package exports internal commit workflow symbol %s in %s", ident, name)
				}
			}
		}
	}
}

func exportedDeclNames(decl ast.Decl) []string {
	switch decl := decl.(type) {
	case *ast.FuncDecl:
		if decl.Recv == nil && decl.Name.IsExported() {
			return []string{decl.Name.Name}
		}
	case *ast.GenDecl:
		var names []string
		for _, spec := range decl.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if ok && typeSpec.Name.IsExported() {
					names = append(names, typeSpec.Name.Name)
				}
				continue
			}
			for _, ident := range value.Names {
				if ident.IsExported() {
					names = append(names, ident.Name)
				}
			}
		}
		return names
	}
	return nil
}
