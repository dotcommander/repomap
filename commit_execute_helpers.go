package repomap

import (
	"fmt"
	"io"
	"path/filepath"
)

func printDryRun(w io.Writer, groups []CommitGroup, opts ExecuteOptions) error {
	if _, err := fmt.Fprintf(w, "DRY RUN — no changes will be made\n\n"); err != nil {
		return err
	}
	for i, g := range groups {
		if _, err := fmt.Fprintf(w, "Commit %d: %s\n", i+1, g.SuggestedMsg); err != nil {
			return err
		}
		for _, f := range g.Files {
			if _, err := fmt.Fprintf(w, "  + %s\n", f); err != nil {
				return err
			}
		}
	}
	if opts.Tag != "" {
		if _, err := fmt.Fprintf(w, "\nTag: %s\n", opts.Tag); err != nil {
			return err
		}
	}
	if opts.Push {
		if _, err := fmt.Fprintf(w, "Push: git push origin <branch> --follow-tags\n"); err != nil {
			return err
		}
	}
	if opts.Push && opts.Tag != "" && !opts.NoRelease {
		if _, err := fmt.Fprintf(w, "Release: gh release create %s --generate-notes --latest\n", opts.Tag); err != nil {
			return err
		}
	}
	return nil
}

// buildPartialResult constructs a partial result when commits landed but a later step failed.
func buildPartialResult(branch string, landed []CommitRecord, opts ExecuteOptions, pushed bool, releaseURL *string, code int, errMsg string) (*ExecuteResult, error) {
	result := &ExecuteResult{
		Branch:     branch,
		Commits:    landed,
		Tag:        tagPtr(opts.Tag),
		Pushed:     pushed,
		ReleaseURL: releaseURL,
		Postflight: PostflightCheck{Clean: true, Convent: true, TagLocal: opts.Tag != ""},
	}
	return result, execError{code: code, msg: errMsg}
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
