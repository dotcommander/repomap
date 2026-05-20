package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/dotcommander/repomap"
	"github.com/spf13/cobra"
)

func newCacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Inspect repomap disk cache state",
	}
	cmd.AddCommand(newCacheStatusCmd())
	return cmd
}

func newCacheStatusCmd() *cobra.Command {
	var (
		cacheDir string
		jsonOut  bool
	)
	cmd := &cobra.Command{
		Use:   "status [directory]",
		Short: "Show disk cache freshness and usability",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}
			root, err := filepath.Abs(dir)
			if err != nil {
				return fmt.Errorf("resolve path: %w", err)
			}
			if cacheDir == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("resolve home dir: %w", err)
				}
				cacheDir = filepath.Join(home, ".cache", "repomap")
			}
			status := repomap.InspectCache(cmd.Context(), root, cacheDir)
			if jsonOut {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(status)
			}
			printCacheStatus(cmd.OutOrStdout(), status)
			return nil
		},
	}
	cmd.Flags().StringVar(&cacheDir, "cache-dir", "", "Cache directory (default: $HOME/.cache/repomap)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable cache status JSON")
	return cmd
}

func printCacheStatus(w io.Writer, status repomap.CacheStatus) {
	state := "missing"
	switch {
	case status.Usable && !status.Stale:
		state = "fresh"
	case status.Usable && status.Stale:
		state = "stale"
	case status.Exists:
		state = "unusable"
	}
	fmt.Fprintf(w, "cache: %s\n", state)
	fmt.Fprintf(w, "  path: %s\n", status.CachePath)
	if status.Reason != "" {
		fmt.Fprintf(w, "  reason: %s\n", status.Reason)
	}
	if status.BuiltAt != nil {
		fmt.Fprintf(w, "  built: %s\n", status.BuiltAt.Format("2006-01-02 15:04:05 MST"))
	}
	if status.TrackedFiles > 0 {
		fmt.Fprintf(w, "  tracked files: %d\n", status.TrackedFiles)
	}
	if status.SavedHead != "" || status.CurrentHead != "" {
		fmt.Fprintf(w, "  saved HEAD: %s\n", status.SavedHead)
		fmt.Fprintf(w, "  current HEAD: %s\n", status.CurrentHead)
	}
}
