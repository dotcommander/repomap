# Installation

## Requirements

- Go 1.23 or later
- `git` available on `$PATH` (used for file discovery)
- Optional: `ctags` for non-Go, non-tree-sitter languages

## Install the CLI

```bash
go install github.com/dotcommander/repomap/cmd/repomap@latest
```

The binary lands in `$GOBIN` (usually `~/go/bin`). Add that directory to your `$PATH` if you haven't already.

## Build from source

```bash
git clone https://github.com/dotcommander/repomap
cd repomap
go build -o repomap ./cmd/repomap
```

Move the binary anywhere on your path:

```bash
ln -sf "$(pwd)/repomap" ~/go/bin/repomap
```

## Import as a library

```bash
go get github.com/dotcommander/repomap
```

Then:

```go
import "github.com/dotcommander/repomap"
```

## Verify the install

```bash
repomap --help
```

You should see the flag list. Point it at any project with source files:

```bash
repomap ~/some/project
```

If the output says `no source files found`, the directory isn't a git repo and doesn't contain recognisable languages. Point at a real project root.

## Optional: ctags

Tree-sitter covers Go, Python, Rust, TypeScript, JavaScript, Java, C, and C++. For other languages (Ruby, PHP, Lua, etc.) repomap falls back to ctags when the binary is present, then regex when it isn't.

```bash
brew install universal-ctags   # macOS
apt install universal-ctags    # Debian/Ubuntu
```

You don't need ctags. The regex parser is never missing. ctags just produces cleaner symbol extraction for languages without tree-sitter support.

## Next

Read [Quick Start](02-quick-start.md) to see what repomap does in two minutes.
