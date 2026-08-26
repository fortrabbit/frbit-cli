# List the available recipes.
default:
    @just --list

# Run the CLI without keeping a binary in the working directory.
# Example: just run apps list --json
run *args:
    go run ./cmd/frbit {{args}}

# Build a reusable local binary at ./frbit.
build:
    go build -o ./frbit ./cmd/frbit

# Symlink the built ./frbit binary into your path.
symlink: build
    sudo ln -sf $(pwd)/frbit /usr/local/bin/frbit

# Run the automated checks used by CI.
test:
    go test -race ./...

# Create the next release version; use --dry-run to preview.
release *args:
    bash .github/release.sh release {{args}}
