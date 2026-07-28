//go:build unix

package repomap

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommitRewriteFlowsPreserveFilePermissions(t *testing.T) {
	flows := []struct {
		name  string
		apply func(t *testing.T, root, path string) error
	}{
		{
			name: "fix findings",
			apply: func(t *testing.T, root, path string) error {
				applied, _, err := ApplyFixFindings(context.Background(), root, []Finding{{
					Kind: "secret", File: path, Line: 1, Snippet: "sensitive", DefaultAction: ActionFix,
				}})
				require.Len(t, applied, 1)
				return err
			},
		},
		{
			name: "review decisions",
			apply: func(t *testing.T, root, path string) error {
				return ApplyReviewDecisions(context.Background(), root, []ReviewDecision{{
					ID: path + ":1", Verdict: VerdictUnsafe, Replacement: "REDACTED",
				}}, []Finding{{File: path, Line: 1, Snippet: "sensitive", DefaultAction: ActionReview}})
			},
		},
		{
			name: "candidates",
			apply: func(t *testing.T, root, path string) error {
				applied, _, err := ApplyCandidates(context.Background(), root, []Candidate{{
					File: path, Line: 1, Replacement: "REDACTED",
				}})
				require.Len(t, applied, 1)
				return err
			},
		},
	}

	for _, flow := range flows {
		t.Run(flow.name, func(t *testing.T) {
			for _, perm := range []fs.FileMode{0o600, 0o644, 0o755} {
				t.Run(perm.String(), func(t *testing.T) {
					root := t.TempDir()
					path := "target.txt"
					abs := filepath.Join(root, path)
					require.NoError(t, os.WriteFile(abs, []byte("sensitive\n"), perm))
					require.NoError(t, os.Chmod(abs, perm))

					require.NoError(t, flow.apply(t, root, path))
					info, err := os.Stat(abs)
					require.NoError(t, err)
					require.Equal(t, perm, info.Mode().Perm())
				})
			}
		})
	}
}

func TestCommitRewriteFlowsRejectEscapesAndSymlinks(t *testing.T) {
	flows := []struct {
		name  string
		apply func(t *testing.T, root, path string) error
	}{
		{
			name: "fix findings",
			apply: func(t *testing.T, root, path string) error {
				applied, _, err := ApplyFixFindings(context.Background(), root, []Finding{{
					Kind: "secret", File: path, Line: 1, Snippet: "sensitive", DefaultAction: ActionFix,
				}})
				require.Empty(t, applied)
				return err
			},
		},
		{
			name: "review decisions",
			apply: func(t *testing.T, root, path string) error {
				return ApplyReviewDecisions(context.Background(), root, []ReviewDecision{{
					ID: path + ":1", Verdict: VerdictUnsafe, Replacement: "REDACTED",
				}}, []Finding{{File: path, Line: 1, Snippet: "sensitive", DefaultAction: ActionReview}})
			},
		},
		{
			name: "candidates",
			apply: func(t *testing.T, root, path string) error {
				applied, _, err := ApplyCandidates(context.Background(), root, []Candidate{{
					File: path, Line: 1, Replacement: "REDACTED",
				}})
				require.Empty(t, applied)
				return err
			},
		},
	}
	targets := []struct {
		name string
		path func(t *testing.T, root, external string) string
	}{
		{name: "traversal", path: func(t *testing.T, root, external string) string { return "../outside.txt" }},
		{name: "absolute", path: func(t *testing.T, root, external string) string { return external }},
		{name: "symlink", path: func(t *testing.T, root, external string) string {
			require.NoError(t, os.Symlink(external, filepath.Join(root, "link.txt")))
			return "link.txt"
		}},
		{name: "symlinked parent", path: func(t *testing.T, root, external string) string {
			require.NoError(t, os.Symlink(filepath.Dir(external), filepath.Join(root, "linked-dir")))
			return "linked-dir/" + filepath.Base(external)
		}},
	}

	for _, flow := range flows {
		for _, target := range targets {
			t.Run(flow.name+"/"+target.name, func(t *testing.T) {
				workspace := t.TempDir()
				root := filepath.Join(workspace, "repo")
				require.NoError(t, os.Mkdir(root, 0o700))
				external := filepath.Join(workspace, "outside.txt")
				const original = "sensitive\n"
				require.NoError(t, os.WriteFile(external, []byte(original), 0o600))

				err := flow.apply(t, root, target.path(t, root, external))
				require.Error(t, err)
				data, readErr := os.ReadFile(external)
				require.NoError(t, readErr)
				require.Equal(t, original, string(data))
				info, statErr := os.Stat(external)
				require.NoError(t, statErr)
				require.Equal(t, fs.FileMode(0o600), info.Mode().Perm())
			})
		}
	}
}

func TestFilterScannableRejectsNonRegularFiles(t *testing.T) {
	workspace := t.TempDir()
	root := filepath.Join(workspace, "repo")
	require.NoError(t, os.Mkdir(root, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "regular.txt"), []byte("ok"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "outside.txt"), []byte("outside"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(root, "directory"), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(root, "regular.txt"), filepath.Join(root, "link.txt")))
	fifo := filepath.Join(root, "pipe")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))

	got := filterScannable(root, []fileChange{
		{Path: "regular.txt"},
		{Path: "directory"},
		{Path: "link.txt"},
		{Path: "pipe"},
		{Path: "../outside.txt"},
		{Path: filepath.Join(workspace, "outside.txt")},
	})
	require.Equal(t, []string{"regular.txt"}, got)
}
