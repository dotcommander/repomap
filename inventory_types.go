package repomap

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
	Truncated bool          `json:"truncated,omitzero"` // true when file cap was reached
}

const inventoryFilename = "inventory.json"
const inventoryFileCap = 500
