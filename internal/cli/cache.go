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
	Warm   cacheWarmCommand   `cmd:"" help:"Build and save a fresh disk cache"`
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
	cacheDir, err := resolveCacheDir(c.CacheDir)
	if err != nil {
		return err
	}
	status := repomap.InspectCache(ctx, root, cacheDir)
	if c.JSON {
		enc := json.NewEncoder(ioctx.stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(status)
	}
	return printCacheStatus(ioctx.stdout, status)
}

type cacheWarmCommand struct {
	CacheDir  string `name:"cache-dir" help:"Cache directory (default: $HOME/.cache/repomap)"`
	Directory string `arg:"" optional:"" type:"path" default:"." help:"Directory to cache"`
}

func (c *cacheWarmCommand) Run(ctx context.Context, ioctx *commandIO) error {
	root, err := filepath.Abs(c.Directory)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	cacheDir, err := resolveCacheDir(c.CacheDir)
	if err != nil {
		return err
	}

	m := repomap.New(root, repomap.DefaultConfig())
	m.SetCacheDir(cacheDir)
	if err := m.Build(ctx); err != nil {
		return fmt.Errorf("build map: %w", err)
	}
	// Build writes its configured cache best-effort. Save again here so this
	// command can report disk failures instead of claiming a cache was warmed.
	if err := m.SaveCacheContext(ctx, cacheDir); err != nil {
		return fmt.Errorf("save cache: %w", err)
	}

	status := repomap.InspectCache(ctx, root, cacheDir)
	if !status.Usable || status.Stale {
		return fmt.Errorf("cache warm did not produce a fresh usable cache: %s", status.Reason)
	}
	return printCacheStatus(ioctx.stdout, status)
}

func resolveCacheDir(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".cache", "repomap"), nil
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
