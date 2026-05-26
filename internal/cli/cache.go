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
			return printCacheStatus(cmd.OutOrStdout(), status)
		},
	}
	cmd.Flags().StringVar(&cacheDir, "cache-dir", "", "Cache directory (default: $HOME/.cache/repomap)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable cache status JSON")
	return cmd
}

func printCacheStatus(w io.Writer, status repomap.CacheStatus) error {
	state := "missing"
	switch {
	case status.Usable && !status.Stale:
		state = "fresh"
	case status.Usable && status.Stale:
		state = "stale"
	case status.Exists:
		state = "unusable"
	}
	if _, err := fmt.Fprintf(w, "cache: %s\n", state); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  path: %s\n", status.CachePath); err != nil {
		return err
	}
	if status.Reason != "" {
		if _, err := fmt.Fprintf(w, "  reason: %s\n", status.Reason); err != nil {
			return err
		}
	}
	if status.BuiltAt != nil {
		if _, err := fmt.Fprintf(w, "  built: %s\n", status.BuiltAt.Format("2006-01-02 15:04:05 MST")); err != nil {
			return err
		}
	}
	if status.TrackedFiles > 0 {
		if _, err := fmt.Fprintf(w, "  tracked files: %d\n", status.TrackedFiles); err != nil {
			return err
		}
	}
	if status.SavedHead != "" || status.CurrentHead != "" {
		if _, err := fmt.Fprintf(w, "  saved HEAD: %s\n", status.SavedHead); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "  current HEAD: %s\n", status.CurrentHead); err != nil {
			return err
		}
	}
	return nil
}
