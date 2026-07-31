# Contributing

Repomap requires Go 1.26 or later. Start with a local build and test:

```bash
git clone https://github.com/dotcommander/repomap
cd repomap
go test ./...
```

Format changed Go files with `gofmt` before submitting a change:

```bash
gofmt -w path/to/changed.go
just fmt-check
```

Run the focused tests for the packages you changed, then run the repository
checks:

```bash
just check
```

`just check` builds Repomap, checks formatting, runs the full test suite, vets
and lints the module, and verifies downloaded modules. It requires
[`just`](https://just.systems/) and
[`golangci-lint`](https://golangci-lint.run/).

For changes that affect CLI behavior or the repository QA contract, also run:

```bash
just qa-e2e
```

The end-to-end workflow additionally requires `gopls` and records its local
evidence under `.work/qa/`.
