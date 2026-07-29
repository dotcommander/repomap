package repomap

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskDefaultsAndJSONBudget(t *testing.T) {
	t.Parallel()
	root := taskFixture(t)
	report, err := New(root, DefaultConfig()).Task(t.Context(), "update handler contract", TaskOptions{})
	require.NoError(t, err)
	assert.Equal(t, defaultTaskTokens, report.Budget.MaxTokens)
	assert.NotEmpty(t, report.Targets)
	assert.LessOrEqual(t, taskOutputTokens(report), report.Budget.MaxTokens)
	data, err := json.Marshal(report)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &fields))
	assert.ElementsMatch(t, []string{"schema_version", "root", "goal", "budget", "selection", "rules", "related_changes", "targets", "read_next", "verify_commands", "follow_up_commands", "diagnostics", "truncations"}, mapKeys(fields))
}

func TestTaskRejectsInvalidGoalAndConsumedPath(t *testing.T) {
	t.Parallel()
	root := taskFixture(t)
	m := New(root, DefaultConfig())
	_, err := m.Task(context.Background(), " ", TaskOptions{})
	require.ErrorContains(t, err, "goal")
	_, err = m.Task(context.Background(), "handler", TaskOptions{MaxTokens: -1})
	require.ErrorContains(t, err, "negative")
	_, err = m.Task(context.Background(), "handler", TaskOptions{ConsumedPaths: []string{"../outside.go"}})
	require.ErrorContains(t, err, "outside")
	_, err = m.Task(context.Background(), "handler", TaskOptions{ConsumedPaths: []string{filepath.Join(t.TempDir(), "outside.go")}})
	require.ErrorContains(t, err, "outside")
}

func TestTaskSelectionDoesNotMixFallbacksAndNestsTests(t *testing.T) {
	t.Parallel()
	ranked := []RankedFile{
		{FileSymbols: &FileSymbols{Path: "handler.go", Symbols: []Symbol{{Name: "Handler"}}}, Score: 100},
		{FileSymbols: &FileSymbols{Path: "unrelated.go", Symbols: []Symbol{{Name: "Other"}}}, Score: 100},
		{FileSymbols: &FileSymbols{Path: "handler_test.go", Symbols: []Symbol{{Name: "TestHandler"}}}, Score: 50},
	}
	candidates := taskCandidates(ranked, "handler")
	require.Len(t, candidates, 2)
	assert.Equal(t, "handler.go", candidates[0].file.Path)
	assert.Equal(t, "handler_test.go", candidates[1].file.Path)
	assert.False(t, candidates[0].fallback)
	assert.False(t, candidates[1].fallback)
}

func TestTaskOutputBudgetAndTinyEnvelopeFailure(t *testing.T) {
	t.Parallel()
	root := taskFixture(t)
	report, err := New(root, DefaultConfig()).Task(t.Context(), "handler", TaskOptions{MaxTokens: 4096})
	require.NoError(t, err)
	jsonBytes, err := MarshalTaskJSON(report)
	require.NoError(t, err)
	assert.True(t, json.Valid(jsonBytes))
	assert.LessOrEqual(t, (max(len(jsonBytes), len(FormatTask(report)))+3)/4, report.Budget.MaxTokens)
	_, err = New(root, DefaultConfig()).Task(t.Context(), "handler", TaskOptions{MaxTokens: 1})
	require.Error(t, err)
}

func TestTaskBudgetAcceptanceIsMonotonic(t *testing.T) {
	t.Parallel()

	root := taskFixture(t)
	succeeded := false
	for budget := 128; budget <= 2304; budget += 64 {
		_, err := New(root, DefaultConfig()).Task(t.Context(), "handler", TaskOptions{MaxTokens: budget})
		if succeeded {
			require.NoError(t, err, "budget %d failed after a smaller budget succeeded", budget)
		}
		if err == nil {
			succeeded = true
		}
	}
	assert.True(t, succeeded)
}

func TestTaskPreservesReceiverFileSizeLimit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "small.go"), []byte("package task\nfunc Small() {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "large_owner.go"), []byte("package task\nfunc LargeOwner() {}\n"+strings.Repeat("// padding\n", 40)), 0o644))
	m := New(root, Config{MaxTokens: 2048, MaxFileSize: 64})
	report, err := m.Task(t.Context(), "large owner", TaskOptions{})
	require.NoError(t, err)
	assert.Equal(t, -1, taskTargetIndex(report.Targets, []string{"large_owner.go"}))
}

func TestTaskFollowUpShellQuotesEveryUserControlledArgument(t *testing.T) {
	t.Parallel()

	value := "space 'quote' $(touch injected) `touch injected2`;*"
	quoted := shellTaskArg(value)
	assert.Equal(t, "'space '\"'\"'quote'\"'\"' $(touch injected) `touch injected2`;*'", quoted)
	report := TaskReport{
		Goal: value, Root: "/tmp/root with space",
		Budget:      TaskBudget{MaxTokens: 100},
		Truncations: []TaskTruncation{{Field: "targets", Shown: 0, Total: 1, Reason: "budget"}},
	}
	command := taskFollowUpCommands(report, []string{"known file.go", "$(touch consumed)"})[0]
	assert.Contains(t, command, "--consumed 'known file.go,$(touch consumed)'")
	assert.Contains(t, command, "'/tmp/root with space'")
	assert.NotContains(t, command, "--consumed=known file.go")
}

func TestTaskDirtyOverlapIncludesSemanticCaller(t *testing.T) {
	t.Parallel()

	root := taskFixture(t)
	runTaskGit(t, root, "init", "-q")
	runTaskGit(t, root, "config", "user.email", "task@example.invalid")
	runTaskGit(t, root, "config", "user.name", "Task Test")
	runTaskGit(t, root, "add", ".")
	runTaskGit(t, root, "commit", "-qm", "initial")
	require.NoError(t, os.WriteFile(filepath.Join(root, "consumer.go"), []byte("package task\n\nfunc Use() error { return Handler() }\n// dirty caller\n"), 0o644))

	report, err := New(root, DefaultConfig()).Task(t.Context(), "handler", TaskOptions{})
	require.NoError(t, err)
	found := false
	for _, change := range report.RelatedChanges {
		found = found || change.Path == "consumer.go"
	}
	assert.True(t, found, "dirty exact caller omitted from related_changes")
}

func TestTaskSourceAndRelationshipCapsHaveAccounting(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	var body strings.Builder
	body.WriteString("package task\n\nfunc Long() {\n")
	for range 70 {
		body.WriteString("\t_ = 1\n")
	}
	body.WriteString("}\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "long.go"), []byte(body.String()), 0o644))
	report, err := New(root, DefaultConfig()).Task(t.Context(), "long", TaskOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, report.Targets)
	for _, source := range report.Targets[0].Source {
		assert.LessOrEqual(t, len(source.Lines), 60)
	}
	found := false
	for _, truncation := range report.Truncations {
		if strings.Contains(truncation.Field, ".source") {
			found = true
		}
		assert.GreaterOrEqual(t, truncation.Total, truncation.Shown)
	}
	assert.True(t, found)
}

func TestTaskSemanticDegradeAndDirtyOverlapHelpers(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/broken\n\ngo 1.26\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "broken.go"), []byte("package broken\nimport _ \"missing.invalid/pkg\"\nfunc Handler() {}\n"), 0o644))
	report, err := New(root, DefaultConfig()).Task(t.Context(), "handler", TaskOptions{})
	require.NoError(t, err)
	assert.Contains(t, strings.Join(report.Diagnostics, "\n"), "semantic analysis degraded")
	changes := filterTaskRelatedChanges([]TaskRelatedChange{{Path: "handler.go"}, {Path: "noise.go"}}, []TaskTarget{{Path: "handler.go"}})
	assert.Equal(t, []TaskRelatedChange{{Path: "handler.go"}}, changes)
}

type taskManifest struct {
	Goal          string   `json:"goal"`
	Language      string   `json:"language"`
	OwnerDoc      string   `json:"owner_doc"`
	DistractorDoc string   `json:"distractor_doc"`
	Owners        []string `json:"owners"`
	Symbols       []string `json:"symbols"`
	Consumers     []string `json:"consumers"`
	Tests         []string `json:"tests"`
	Evidence      []string `json:"evidence"`
	Distractors   []string `json:"distractors"`
}

func TestTaskManifestContractAndOptionalReplay(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("testdata", "task")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 12)
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		require.NoError(t, err)
		var manifest taskManifest
		require.NoError(t, json.Unmarshal(data, &manifest), entry.Name())
		assert.NotEmpty(t, manifest.Goal)
		assert.NotEmpty(t, manifest.OwnerDoc)
		assert.NotEmpty(t, manifest.DistractorDoc)
		assert.NotEmpty(t, manifest.Owners)
		assert.NotEmpty(t, manifest.Symbols)
		assert.NotEmpty(t, manifest.Consumers)
		assert.NotEmpty(t, manifest.Tests)
		assert.NotEmpty(t, manifest.Evidence)
		assert.NotEmpty(t, manifest.Distractors)
	}
	path := os.Getenv("REPOMAP_TASK_REPLAY_MANIFEST")
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var manifests []taskManifest
	require.NoError(t, json.Unmarshal(data, &manifests))
	for _, manifest := range manifests {
		root := t.TempDir()
		owner := manifest.Owners[0]
		require.NoError(t, os.WriteFile(filepath.Join(root, owner), []byte("package replay\nfunc Replay() {}\n"), 0o644))
		report, err := New(root, DefaultConfig()).Task(t.Context(), manifest.Goal, TaskOptions{})
		require.NoError(t, err)
		data, err := MarshalTaskJSON(report)
		require.NoError(t, err)
		assert.True(t, json.Valid(data))
	}
}

func TestTaskAcceptanceCorpus(t *testing.T) {
	t.Parallel()

	manifests := readTaskManifests(t)
	ownerFound := 0
	ownerTopThree := 0
	symbolFound := 0
	symbolTotal := 0
	relationshipFound := 0
	relationshipTotal := 0
	followUps := make([]int, 0, len(manifests))
	baselineFollowUps := make([]int, 0, len(manifests))

	for _, manifest := range manifests {
		root := writeTaskCorpusFixture(t, manifest)
		baseline := New(root, Config{
			MaxTokens: 8192, MaxTokensNoCtx: 8192, Intent: manifest.Goal,
			GoAnalysis: true, GoAnalysisCalls: true, GoAnalysisTests: true,
		})
		require.NoError(t, baseline.Build(t.Context()), manifest.Goal)
		baselineFollowUps = append(baselineFollowUps, measuredBaselineMissingEvidence(t, baseline, manifest))
		first, err := New(root, DefaultConfig()).Task(t.Context(), manifest.Goal, TaskOptions{MaxTokens: 8192})
		require.NoError(t, err, manifest.Goal)
		second, err := New(root, DefaultConfig()).Task(t.Context(), manifest.Goal, TaskOptions{MaxTokens: 8192})
		require.NoError(t, err, manifest.Goal)
		firstJSON, err := MarshalTaskJSON(first)
		require.NoError(t, err)
		secondJSON, err := MarshalTaskJSON(second)
		require.NoError(t, err)
		assert.Equal(t, firstJSON, secondJSON, manifest.Goal)
		assert.True(t, json.Valid(firstJSON), manifest.Goal)
		assert.LessOrEqual(t, (max(len(firstJSON), len(FormatTask(first)))+3)/4, first.Budget.MaxTokens, manifest.Goal)
		assertTaskTruncationsComplete(t, manifest.Goal, first.Truncations)

		ownerIndex := taskTargetIndex(first.Targets, manifest.Owners)
		if ownerIndex >= 0 {
			ownerFound++
		}
		if ownerIndex >= 0 && ownerIndex < 3 {
			ownerTopThree++
		}
		require.NotEqual(t, -1, ownerIndex, "task missed every owner: %s", manifest.Goal)
		for _, distractor := range manifest.Distractors {
			assert.NotEqual(t, distractor, first.Targets[0].Path, manifest.Goal)
		}
		owner := first.Targets[ownerIndex]
		assertTaskRequiredEvidence(t, manifest, owner)
		for _, symbol := range manifest.Symbols {
			symbolTotal++
			if taskTargetHasSymbol(owner, symbol) {
				symbolFound++
			}
		}
		for _, path := range append(append([]string(nil), manifest.Consumers...), manifest.Tests...) {
			relationshipTotal++
			if taskTargetHasRelationship(owner, path) {
				relationshipFound++
			}
		}
		followUps = append(followUps, len(first.FollowUpCommands))
	}

	assert.GreaterOrEqual(t, float64(ownerFound)/float64(len(manifests)), 0.90, "owner recall@6")
	assert.GreaterOrEqual(t, ownerTopThree, 10, "gold owner top-three tasks")
	assert.GreaterOrEqual(t, float64(symbolFound)/float64(symbolTotal), 0.80, "symbol recall")
	assert.GreaterOrEqual(t, float64(relationshipFound)/float64(relationshipTotal), 0.80, "relationship recall")
	slices.Sort(followUps)
	assert.LessOrEqual(t, followUps[len(followUps)/2], 1, "median task follow-ups")
	assert.LessOrEqual(t, sumInts(followUps)*2, sumInts(baselineFollowUps), "follow-ups versus measured --intent --json-structured baseline")
}

func TestTaskRerunBytesAndRelationshipProvenance(t *testing.T) {
	t.Parallel()
	root := taskFixture(t)
	first, err := New(root, DefaultConfig()).Task(t.Context(), "handler", TaskOptions{})
	require.NoError(t, err)
	second, err := New(root, DefaultConfig()).Task(t.Context(), "handler", TaskOptions{})
	require.NoError(t, err)
	firstJSON, err := MarshalTaskJSON(first)
	require.NoError(t, err)
	secondJSON, err := MarshalTaskJSON(second)
	require.NoError(t, err)
	assert.Equal(t, firstJSON, secondJSON)
	for _, target := range first.Targets {
		for _, relationship := range target.Relationships {
			assert.Contains(t, []string{"exact", "syntactic", "heuristic"}, relationship.Provenance)
		}
	}
}

func TestTaskConsumedOmitsSourceAndKeepsRelationships(t *testing.T) {
	t.Parallel()
	root := taskFixture(t)
	report, err := New(root, DefaultConfig()).Task(t.Context(), "handler", TaskOptions{ConsumedPaths: []string{"handler.go"}})
	require.NoError(t, err)
	var target *TaskTarget
	for i := range report.Targets {
		if report.Targets[i].Path == "handler.go" {
			target = &report.Targets[i]
			break
		}
	}
	require.NotNil(t, target)
	assert.True(t, target.Consumed)
	assert.Empty(t, target.Source)
	assert.NotEmpty(t, target.Relationships)
}

func TestTaskFallbackAndNonGitDiagnostic(t *testing.T) {
	t.Parallel()
	root := taskFixture(t)
	report, err := New(root, DefaultConfig()).Task(t.Context(), "unrelated terminology", TaskOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, report.Targets)
	assert.Equal(t, "fallback", report.Targets[0].Evidence[0].Field)
	assert.Contains(t, report.Diagnostics, "git status unavailable; related_changes omitted")
}

func taskFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/task\n\ngo 1.26\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "handler.go"), []byte("package task\n\n// Handler updates a contract.\nfunc Handler() error { return nil }\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "consumer.go"), []byte("package task\n\nfunc Use() error { return Handler() }\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "handler_test.go"), []byte("package task\n\nimport \"testing\"\n\nfunc TestHandler(t *testing.T) { _ = Handler() }\n"), 0o644))
	return root
}

func readTaskManifests(t *testing.T) []taskManifest {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join("testdata", "task"))
	require.NoError(t, err)
	manifests := make([]taskManifest, 0, len(entries))
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join("testdata", "task", entry.Name()))
		require.NoError(t, err)
		var manifest taskManifest
		require.NoError(t, json.Unmarshal(data, &manifest))
		manifests = append(manifests, manifest)
	}
	return manifests
}

func writeTaskCorpusFixture(t *testing.T, manifest taskManifest) string {
	t.Helper()
	root := t.TempDir()
	if manifest.Language == "go" {
		require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/taskcorpus\n\ngo 1.26\n"), 0o644))
	}
	owner, consumer, testFile := manifest.Owners[0], manifest.Consumers[0], manifest.Tests[0]
	symbol := manifest.Symbols[0]
	writeTaskCorpusFile(t, root, owner, taskCorpusSource(manifest.Language, "owner", symbol, filepath.Base(owner), manifest.Evidence, manifest.OwnerDoc))
	writeTaskCorpusFile(t, root, consumer, taskCorpusSource(manifest.Language, "consumer", symbol, filepath.Base(owner), manifest.Evidence))
	writeTaskCorpusFile(t, root, testFile, taskCorpusSource(manifest.Language, "test", symbol, filepath.Base(owner), manifest.Evidence))
	for _, distractor := range manifest.Distractors {
		writeTaskCorpusFile(t, root, distractor, taskCorpusSource(manifest.Language, "distractor", "Unrelated", filepath.Base(owner), nil, manifest.DistractorDoc))
	}
	if manifest.Language == "typescript" && slices.Contains(manifest.Evidence, "import") {
		writeTaskCorpusFile(t, root, "helper.ts", "export function helper(): void {}\n")
	}
	return root
}

func writeTaskCorpusFile(t *testing.T, root, path, source string) {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(path))
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.WriteFile(target, []byte(source), 0o644))
}

func taskCorpusSource(language, role, symbol, owner string, evidence []string, docs ...string) string {
	stem := strings.TrimSuffix(owner, filepath.Ext(owner))
	doc := "owns the selected contract behavior"
	if len(docs) > 0 && docs[0] != "" {
		doc = docs[0]
	}
	switch language {
	case "go":
		switch role {
		case "owner":
			if slices.Contains(evidence, "effect") {
				return fmt.Sprintf("package taskcorpus\n\nimport \"os\"\n\n// %s %s.\nfunc %s() { _ = os.WriteFile(\"task.out\", nil, 0o600) }\n", symbol, doc, symbol)
			}
			return fmt.Sprintf("package taskcorpus\n\n// %s %s.\nfunc %s() error { return nil }\n", symbol, doc, symbol)
		case "consumer":
			if slices.Contains(evidence, "effect") {
				return fmt.Sprintf("package taskcorpus\n\nfunc Use%s() { %s() }\n", symbol, symbol)
			}
			return fmt.Sprintf("package taskcorpus\n\nfunc Use%s() { _ = %s() }\n", symbol, symbol)
		case "test":
			if slices.Contains(evidence, "effect") {
				return fmt.Sprintf("package taskcorpus\n\nimport \"testing\"\n\nfunc Test%s(t *testing.T) { %s() }\n", symbol, symbol)
			}
			return fmt.Sprintf("package taskcorpus\n\nimport \"testing\"\n\nfunc Test%s(t *testing.T) { _ = %s() }\n", symbol, symbol)
		default:
			return fmt.Sprintf("package taskcorpus\n\n// Unrelated %s.\nfunc Unrelated() {}\n", doc)
		}
	case "typescript":
		switch role {
		case "owner":
			prefix := ""
			body := "return;"
			if slices.Contains(evidence, "import") {
				prefix = "import { helper } from \"./helper\";\n"
				body = "helper();"
			}
			if slices.Contains(evidence, "effect") {
				body = "void fetch(\"/task\");"
			}
			return fmt.Sprintf("%s/** %s %s. */\nexport function %s(): void { %s }\n", prefix, symbol, doc, symbol, body)
		case "consumer":
			return fmt.Sprintf("import { %s } from \"./%s\";\nexport function use%s(): void { %s(); }\n", symbol, stem, symbol, symbol)
		case "test":
			return fmt.Sprintf("import { %s } from \"./%s\";\ntest(\"%s\", () => %s());\n", symbol, stem, symbol, symbol)
		default:
			return fmt.Sprintf("/** Unrelated %s. */\nexport function Unrelated(): void {}\n", doc)
		}
	default:
		switch role {
		case "owner":
			method := ""
			if slices.Contains(evidence, "effect") {
				method = " public function send(): void { mail(\"task@example.invalid\", \"task\", \"body\"); }"
			}
			return fmt.Sprintf("<?php\n/** %s %s. */\nclass %s {%s}\n", symbol, doc, symbol, method)
		case "consumer":
			return fmt.Sprintf("<?php\nrequire_once __DIR__ . '/%s';\nfunction use%s(): void { new %s(); }\n", owner, symbol, symbol)
		case "test":
			return fmt.Sprintf("<?php\nrequire_once __DIR__ . '/%s';\nfunction test%s(): void { new %s(); }\n", owner, symbol, symbol)
		default:
			return fmt.Sprintf("<?php\n/** Unrelated %s. */\nclass Unrelated {}\n", doc)
		}
	}
}

func taskTargetIndex(targets []TaskTarget, owners []string) int {
	for i, target := range targets {
		if slices.Contains(owners, target.Path) {
			return i
		}
	}
	return -1
}

func taskTargetHasSymbol(target TaskTarget, name string) bool {
	for _, symbol := range target.Symbols {
		if symbol.Name == name {
			return true
		}
	}
	return false
}

func taskTargetHasRelationship(target TaskTarget, path string) bool {
	for _, relationship := range target.Relationships {
		if relationship.Path == path {
			return true
		}
	}
	return false
}

func assertTaskRequiredEvidence(t *testing.T, manifest taskManifest, target TaskTarget) {
	t.Helper()
	for _, category := range manifest.Evidence {
		switch category {
		case "path", "symbol", "doc", "signature":
			found := false
			for _, evidence := range target.Evidence {
				found = found || evidence.Field == category
			}
			if category == "doc" || category == "signature" {
				for _, symbol := range target.Symbols {
					found = found || category == "doc" && symbol.Doc != "" || category == "signature" && symbol.Signature != ""
				}
			}
			assert.True(t, found, "%s missing %s evidence", manifest.Goal, category)
		case "import":
			assert.NotEmpty(t, target.Imports, manifest.Goal)
		case "effect":
			assert.NotEmpty(t, target.Effects, manifest.Goal)
		}
	}
}

func assertTaskTruncationsComplete(t *testing.T, goal string, truncations []TaskTruncation) {
	t.Helper()
	for _, truncation := range truncations {
		assert.NotEmpty(t, truncation.Field, goal)
		assert.NotEmpty(t, truncation.Reason, goal)
		assert.Greater(t, truncation.Total, truncation.Shown, goal)
	}
}

func measuredBaselineMissingEvidence(t *testing.T, baseline *Map, manifest taskManifest) int {
	t.Helper()
	data, err := json.Marshal(baseline.StructuredOutput())
	require.NoError(t, err)
	var packet map[string]any
	require.NoError(t, json.Unmarshal(data, &packet))
	files, _ := packet["files"].([]any)
	ownerInTopSix := false
	for index, value := range files {
		if index >= 6 {
			break
		}
		file, _ := value.(map[string]any)
		path, _ := file["path"].(string)
		ownerInTopSix = ownerInTopSix || slices.Contains(manifest.Owners, path)
	}
	missing := 0
	if !ownerInTopSix {
		missing++
	}
	for _, field := range []string{"source", "consumers", "tests", "effects", "verify_commands"} {
		if _, present := packet[field]; !present {
			missing++
		}
	}
	return missing
}

func sumInts(values []int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}

func runTaskGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", append([]string{"-C", root}, args...)...)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
}

func mapKeys(values map[string]json.RawMessage) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	return out
}
