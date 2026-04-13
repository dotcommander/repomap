package repomap

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type FileMetrics struct {
	Path    string `json:"path"`
	Lines   int    `json:"lines"`
	Imports int    `json:"imports"`
	LastMod string `json:"last_modified"`
}

type Inventory struct {
	Files     []FileMetrics `json:"files"`
	Scanned   string        `json:"scanned"`
	RootPath  string        `json:"root_path"`
	Truncated bool          `json:"truncated,omitempty"` // true when file cap was reached
}

const inventoryFilename = "inventory.json"
const inventoryFileCap = 500

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
			Path: rel, Lines: strings.Count(string(data), "\n") + 1,
			Imports: countImports(data), LastMod: info.ModTime().Format("2006-01-02"),
		})
		if len(files) >= inventoryFileCap {
			return fmt.Errorf("file cap reached (%d)", inventoryFileCap)
		}
		return nil
	})
	truncated := err != nil && len(files) > 0 // file cap was hit
	if err != nil && len(files) == 0 {
		return nil, fmt.Errorf("scan: %w", err)
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Lines != files[j].Lines {
			return files[i].Lines > files[j].Lines
		}
		return files[i].Path < files[j].Path
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

func QueryInventory(cacheDir string, filter string) (string, error) {
	inv := LoadInventory(cacheDir)
	if inv == nil {
		return "No inventory found. Run 'scan' first to build the inventory.", nil
	}
	matched := inv.Files
	if filter != "" {
		op, key, value, err := parseFilter(filter)
		if err != nil {
			return "", fmt.Errorf("invalid filter %q: %w", filter, err)
		}
		var filtered []FileMetrics
		for _, f := range matched {
			if matchesFilter(f, op, key, value) {
				filtered = append(filtered, f)
			}
		}
		matched = filtered
	}
	if len(matched) == 0 {
		return "No files match the filter.", nil
	}
	return formatInventoryTable(matched, ""), nil
}

func parseFilter(filter string) (op, key, value string, err error) {
	for _, c := range []string{">=", "<=", "!=", ">", "<", "="} {
		if idx := strings.Index(filter, c); idx >= 0 {
			return c, strings.TrimSpace(filter[:idx]), strings.TrimSpace(filter[idx+len(c):]), nil
		}
	}
	return "", "", filter, fmt.Errorf("no operator found (expected one of = > < >= <= !=)")
}

func matchesFilter(f FileMetrics, op, key, value string) bool {
	var val int
	switch key {
	case "lines":
		val = f.Lines
	case "imports":
		val = f.Imports
	case "path":
		if op == "=" {
			return strings.Contains(f.Path, value)
		}
		return op == "!=" && !strings.Contains(f.Path, value)
	default:
		return false
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return false
	}
	switch op {
	case ">":
		return val > n
	case ">=":
		return val >= n
	case "<":
		return val < n
	case "<=":
		return val <= n
	case "=":
		return val == n
	default:
		return val != n
	}
}

// PersistInventory writes the inventory to disk using atomic write (temp + rename).
func PersistInventory(inv *Inventory, cacheDir string) error {
	data, err := json.MarshalIndent(inv, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal inventory: %w", err)
	}
	path := filepath.Join(cacheDir, inventoryFilename)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

func LoadInventory(cacheDir string) *Inventory {
	if cacheDir == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(cacheDir, inventoryFilename))
	if err != nil {
		return nil
	}
	var inv Inventory
	if err := json.Unmarshal(data, &inv); err != nil {
		return nil
	}
	return &inv
}

func formatInventoryTable(files []FileMetrics, header string) string {
	var b strings.Builder
	if header != "" {
		b.WriteString(header)
	}
	b.WriteString("| file | lines | imports | modified |\n")
	b.WriteString("|------|-------|---------|----------|\n")
	for _, f := range files {
		fmt.Fprintf(&b, "| %s | %d | %d | %s |\n", f.Path, f.Lines, f.Imports, f.LastMod)
	}
	return b.String()
}
