#!/usr/bin/env bash
# Generate shell completion scripts into completions/ by asking the CLI
# itself, so the shipped files are exactly what `bulle completion` prints.
# File names follow each shell's installation convention.
set -euo pipefail

cd "$(dirname "$0")/.."
mkdir -p completions

go run ./cmd/bulle completion bash > completions/bulle.bash
go run ./cmd/bulle completion zsh > completions/_bulle
go run ./cmd/bulle completion fish > completions/bulle.fish

echo "wrote completions/bulle.bash completions/_bulle completions/bulle.fish"
