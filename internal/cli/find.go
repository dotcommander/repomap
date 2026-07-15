package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dotcommander/repomap"
)

type findCommand struct {
	Kind      string `help:"Filter by symbol kind (struct, func, method, type, interface, var, const)"`
	File      string `help:"Filter to files matching this substring"`
	Limit     int    `default:"20" help:"Max results (0 = unlimited)"`
	Format    string `default:"text" help:"Output format: text or json"`
	Query     string `arg:"" help:"Symbol query"`
	Directory string `arg:"" optional:"" type:"path" default:"." help:"Directory to search"`
}

func (c *findCommand) Run(ctx context.Context, ioctx *commandIO) error {
	name, qKind, qFile := repomap.ParseFindQuery(c.Query)
	kind, file := c.Kind, c.File
	if kind == "" {
		kind = qKind
	}
	if file == "" {
		file = qFile
	}
	m := repomap.New(c.Directory, repomap.DefaultConfig())
	if err := m.Build(ctx); err != nil {
		return fmt.Errorf("build: %w", err)
	}
	matches := m.FindSymbol(name, kind, file)
	if c.Limit > 0 && len(matches) > c.Limit {
		matches = matches[:c.Limit]
	}
	if c.Format == "json" {
		enc := json.NewEncoder(ioctx.stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(matches)
	}
	for _, mt := range matches {
		sig := mt.Symbol.Signature
		if sig == "" {
			sig = mt.Symbol.Name
		}
		fmt.Fprintf(ioctx.stdout, "%-5.0f  %s:%d  %-10s  %s\n", mt.Score, mt.File, mt.Symbol.Line, mt.Symbol.Kind, sig)
	}
	return nil
}
