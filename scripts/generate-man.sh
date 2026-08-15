#!/usr/bin/env bash
# Generate the man page into man/ by asking the CLI itself, so the shipped
# file is exactly what `bulle __man` prints for this version.
set -euo pipefail

cd "$(dirname "$0")/.."
mkdir -p man

go run ./cmd/bulle __man > man/bulle.1

echo "wrote man/bulle.1"
