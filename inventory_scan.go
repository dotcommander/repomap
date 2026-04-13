package repomap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// errFileCapReached is a sentinel returned by the walk callback when the
// inventory file cap is hit, so the caller can distinguish it from ctx
// cancellation and real filesystem errors.
var errFileCapReached = errors.New("inventory file cap reached")

func ScanInventory(ctx context.Context, root string) (*Inventory, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve root: %w", err)
	}
	var files []FileMetrics
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if d.IsDir() {
			name := d.Name()
			if skipDirs[name] || (len(name) > 0 && (name[0] == '.' || name[0] == '_')) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}
		files = append(files, FileMetrics{
			Path: rel, Lines: bytes.Count(data, []byte{'\n'}) + 1,
			Imports: countImports(data), LastMod: info.ModTime().Format("2006-01-02"),
		})
		if len(files) >= inventoryFileCap {
			return errFileCapReached
		}
		return nil
	})
	truncated := errors.Is(err, errFileCapReached)
	if err != nil && !truncated {
		return nil, fmt.Errorf("scan: %w", err)
	}
	slices.SortFunc(files, func(a, b FileMetrics) int {
		if a.Lines != b.Lines {
			return b.Lines - a.Lines
		}
		return strings.Compare(a.Path, b.Path)
	})
	return &Inventory{Files: files, Scanned: time.Now().Format(time.RFC3339), RootPath: root, Truncated: truncated}, nil
}

func countImports(data []byte) int {
	content := string(data)
	if idx := strings.Index(content, "import ("); idx != -1 {
		block := content[idx:]
		end := strings.Index(block, ")")
		if end < 0 {
			return 0
		}
		block = block[:end]
		n := 0
		for _, line := range strings.Split(block, "\n") {
			t := strings.TrimSpace(line)
			if t != "" && !strings.HasPrefix(t, "//") && t != "import (" {
				n++
			}
		}
		return n
	}
	if idx := strings.Index(content, "import "); idx != -1 {
		rest := content[idx+len("import "):]
		if strings.HasPrefix(strings.TrimSpace(rest), "\"") {
			return 1
		}
	}
	return 0
}
