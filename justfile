# Repomap build recipes

binary := "repomap"
cmd    := "./cmd/" + binary

# Default: build
default: build

# Build the binary
build:
    go build -o {{ binary }} {{ cmd }}

# Build with race detector
build-race:
    go build -race -o {{ binary }} {{ cmd }}

# Install to ~/go/bin
install:
    go build -o ~/go/bin/{{ binary }} {{ cmd }}

# Symlink install (for active development)
install-symlink: build
    ln -sf "$(pwd)/{{ binary }}" ~/go/bin/{{ binary }}

# Run all tests
test:
    go test ./...

# Run tests with race detector and verbose output
test-race:
    go test -race -v ./...

# Run short tests only (skip integration)
test-short:
    go test -short ./...

# Run benchmarks
bench:
    go test -bench=. -benchmem ./...

# Run linter
lint:
    golangci-lint run ./...

# Run go vet
vet:
    go vet ./...

# Verify modules
verify:
    go mod verify

# Tidy modules
tidy:
    go mod tidy

# Full verification pipeline
check: build test vet lint verify

# Clean build artifacts
clean:
    rm -f {{ binary }}

# Rebuild from clean
rebuild: clean build

# Show help
list:
    @just --list
