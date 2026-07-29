package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/dotcommander/repomap"
)

type taskCommand struct {
	Goal      string   `arg:"" help:"Implementation goal"`
	Directory string   `arg:"" optional:"" type:"path" default:"." help:"Directory to inspect"`
	Tokens    int      `short:"t" default:"4096" help:"Maximum output token budget"`
	JSON      bool     `help:"Emit the schema-versioned task report as JSON"`
	Consumed  []string `sep:"," help:"Comma-separated file paths already read"`
}

func (c *taskCommand) Validate() error {
	if strings.TrimSpace(c.Goal) == "" {
		return fmt.Errorf("task goal must not be blank")
	}
	if c.Tokens <= 0 {
		return fmt.Errorf("--tokens must be greater than zero")
	}
	return nil
}

func (c *taskCommand) Run(ctx context.Context, ioctx *commandIO) error {
	root, err := filepath.Abs(c.Directory)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	report, err := repomap.New(root, repomap.DefaultConfig()).Task(ctx, c.Goal, repomap.TaskOptions{
		MaxTokens: c.Tokens, ConsumedPaths: c.Consumed,
	})
	if err != nil {
		return err
	}
	if c.JSON {
		return repomap.WriteTaskJSON(ioctx.stdout, report)
	}
	_, err = fmt.Fprint(ioctx.stdout, repomap.FormatTask(report))
	return err
}
