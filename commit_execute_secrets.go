package repomap

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

// checkSecrets blocks execute when the plan contains any unresolved secret
// signal. Callers must adjudicate all findings (detected and ambiguous) before
// calling execute — execute is dumb-deterministic and never resolves findings.
//
// Blocking conditions:
//
//	FlagCount > 0           — deterministic FLAG hits (auto-fixable but not yet fixed)
//	AmbiguousCount > 0      — REVIEW/secret findings requiring LLM adjudication
//	!Clean && Gitleaks > 0  — gitleaks hits not captured by FlagCount (legacy plans)
func checkSecrets(s SecretsSummary, findingsPath string) error {
	hasDetected := s.FlagCount > 0 || (!s.Clean && s.GitleaksFindings > 0)
	hasAmbiguous := s.AmbiguousCount > 0
	if !hasDetected && !hasAmbiguous {
		return nil
	}

	// Collect per-category file lists from the findings file when available.
	detectedFiles, ambiguousFiles := loadSecretFileLists(findingsPath)

	var b strings.Builder
	fmt.Fprintf(&b, "plan has unresolved secrets — adjudicate before execute:")
	if hasDetected {
		fmt.Fprintf(&b, "\n  detected: %d file(s)", s.FlagCount)
		for _, f := range detectedFiles {
			fmt.Fprintf(&b, "\n    %s", f)
		}
	}
	if hasAmbiguous {
		fmt.Fprintf(&b, "\n  ambiguous: %d file(s)", s.AmbiguousCount)
		for _, f := range ambiguousFiles {
			fmt.Fprintf(&b, "\n    %s", f)
		}
	}
	return errors.New(b.String())
}

// loadSecretFileLists parses a findings JSON file and returns two deduplicated,
// sorted file lists: one for FLAG findings (detected) and one for REVIEW
// findings with default_action=review (ambiguous). Returns empty slices when
// the file is absent or unparseable — callers treat that as "no detail available".
func loadSecretFileLists(findingsPath string) (detected, ambiguous []string) {
	if findingsPath == "" {
		return nil, nil
	}
	data, err := os.ReadFile(findingsPath)
	if err != nil {
		return nil, nil
	}
	var findings []Finding
	if err := json.Unmarshal(data, &findings); err != nil {
		return nil, nil
	}
	detectedSet := make(map[string]bool)
	ambiguousSet := make(map[string]bool)
	for _, f := range findings {
		switch {
		case f.Class == "FLAG":
			detectedSet[f.File] = true
		case f.Class == "REVIEW" && f.DefaultAction == "review":
			ambiguousSet[f.File] = true
		}
	}
	return sortedKeys(detectedSet), sortedKeys(ambiguousSet)
}

// sortedKeys returns the keys of m in sorted order.
func sortedKeys(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
