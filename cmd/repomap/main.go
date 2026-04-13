package main

import (
	"os"

	"github.com/dotcommander/repomap/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
