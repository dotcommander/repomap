package repomap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// PersistInventory writes the inventory to disk using atomic write (temp + rename).
func PersistInventory(inv *Inventory, cacheDir string) error {
	data, err := json.MarshalIndent(inv, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal inventory: %w", err)
	}
	path := filepath.Join(cacheDir, inventoryFilename)
	if err := atomicWriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write inventory: %w", err)
	}
	return nil
}

// LoadInventory reads a previously persisted inventory from disk.
// Returns nil if the file is missing or corrupt.
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
