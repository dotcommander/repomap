package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// hookMarker identifies a post-commit hook that `repomap init` installed.
// Presence allows safe overwrite on re-run; absence means a user or other
// tool wrote the hook and we must not clobber it without --force.
const hookMarker = "# repomap post-commit hook"

const hookScript = `#!/bin/sh
# repomap post-commit hook — refreshes cache in background.
# Installed by ` + "`repomap init`" + `. Remove with ` + "`rm .git/hooks/post-commit`" + `.
command -v repomap >/dev/null 2>&1 && (repomap cache warm . >/dev/null 2>&1 &) || true
`

const configTemplate = `# .repomap.yaml — repomap configuration
# See https://github.com/dotcommander/repomap for full options

# Patterns to exclude from symbol extraction.
# Glob: Test*, *Mock
# Regex: /^pb_/, /^mock_/
method_blocklist: []
`

type initCommand struct {
	Force     bool   `help:"Overwrite existing files"`
	NoHook    bool   `name:"no-hook" help:"Skip git hook installation"`
	NoConfig  bool   `name:"no-config" help:"Skip .repomap.yaml scaffold"`
	Directory string `arg:"" optional:"" type:"path" default:"." help:"Directory to initialize"`
}

func (c *initCommand) Run(ioctx *commandIO) error {
	return runInit(ioctx.stdout, c.Directory, c.Force, c.NoHook, c.NoConfig)
}

// runInit is the testable core.
func runInit(out io.Writer, dir string, force, noHook, noConfig bool) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", dir, err)
	}

	if !noConfig {
		if err := writeConfig(out, abs, force); err != nil {
			return err
		}
	}
	if !noHook {
		if err := writeHook(out, abs, force); err != nil {
			return err
		}
	}
	return nil
}

func writeConfig(out io.Writer, root string, force bool) error {
	p := filepath.Join(root, ".repomap.yaml")
	if _, err := os.Stat(p); err == nil && !force {
		fmt.Fprintf(out, "skip  %s (exists)\n", ".repomap.yaml")
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", p, err)
	}
	if err := os.WriteFile(p, []byte(configTemplate), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	fmt.Fprintf(out, "write %s\n", ".repomap.yaml")
	return nil
}

func writeHook(out io.Writer, root string, force bool) error {
	gitDir := filepath.Join(root, ".git")
	info, err := os.Stat(gitDir)
	if os.IsNotExist(err) || (err == nil && !info.IsDir()) {
		fmt.Fprintf(out, "skip  .git/hooks/post-commit (not a git repo)\n")
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat %s: %w", gitDir, err)
	}
	hooksDir := filepath.Join(gitDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", hooksDir, err)
	}
	p := filepath.Join(hooksDir, "post-commit")
	existing, err := os.ReadFile(p)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// no existing hook — proceed
	case err != nil:
		return fmt.Errorf("read %s: %w", p, err)
	default:
		if bytes.Equal(existing, []byte(hookScript)) && !force {
			fmt.Fprintf(out, "skip  .git/hooks/post-commit (exists)\n")
			return nil
		}
		// Older marker-owned hooks are safe to upgrade without --force.
		if !bytes.Contains(existing, []byte(hookMarker)) && !force {
			return fmt.Errorf("%s exists and was not written by repomap; merge manually or re-run with --force", p)
		}
	}
	if err := os.WriteFile(p, []byte(hookScript), 0o755); err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	fmt.Fprintf(out, "write .git/hooks/post-commit (chmod 0755)\n")
	return nil
}
