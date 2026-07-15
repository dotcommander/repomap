package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/dotcommander/repomap"
)

type cacheCommand struct {
	Status cacheStatusCommand `cmd:"" help:"Show disk cache freshness and usability"`
}

type cacheStatusCommand struct {
	CacheDir  string `name:"cache-dir" help:"Cache directory (default: $HOME/.cache/repomap)"`
	JSON      bool   `help:"Emit machine-readable cache status JSON"`
	Directory string `arg:"" optional:"" type:"path" default:"." help:"Directory to inspect"`
}

func (c *cacheStatusCommand) Run(ctx context.Context, ioctx *commandIO) error {
	root, err := filepath.Abs(c.Directory)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	cacheDir := c.CacheDir
	if cacheDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve home dir: %w", err)
		}
		cacheDir = filepath.Join(home, ".cache", "repomap")
	}
	status := repomap.InspectCache(ctx, root, cacheDir)
	if c.JSON {
		enc := json.NewEncoder(ioctx.stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(status)
	}
	return printCacheStatus(ioctx.stdout, status)
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
