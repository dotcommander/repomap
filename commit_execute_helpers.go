package repomap

import (
	"fmt"
	"path/filepath"
)

func printDryRun(groups []CommitGroup, opts ExecuteOptions) {
	fmt.Printf("DRY RUN — no changes will be made\n\n")
	for i, g := range groups {
		fmt.Printf("Commit %d: %s\n", i+1, g.SuggestedMsg)
		for _, f := range g.Files {
			fmt.Printf("  + %s\n", f)
		}
	}
	if opts.Tag != "" {
		fmt.Printf("\nTag: %s\n", opts.Tag)
	}
	if opts.Push {
		fmt.Printf("Push: git push origin <branch> --follow-tags\n")
	}
	if opts.Push && opts.Tag != "" && !opts.NoRelease {
		fmt.Printf("Release: gh release create %s --generate-notes --latest\n", opts.Tag)
	}
}

// buildPartialResult constructs an exit-4 result when commits landed but push/release failed.
func buildPartialResult(branch string, landed []CommitRecord, opts ExecuteOptions, pushed bool, releaseURL *string, errMsg string) (*ExecuteResult, error) {
	result := &ExecuteResult{
		Branch:     branch,
		Commits:    landed,
		Tag:        tagPtr(opts.Tag),
		Pushed:     pushed,
		ReleaseURL: releaseURL,
		Postflight: PostflightCheck{Clean: true, Convent: true, TagLocal: opts.Tag != ""},
	}
	return result, execError{code: 4, msg: errMsg}
}

func resolveRoot(root string) (string, error) {
	if root == "" {
		root = "."
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	return abs, nil
}

func tagPtr(tag string) *string {
	if tag == "" {
		return nil
	}
	s := tag
	return &s
}
