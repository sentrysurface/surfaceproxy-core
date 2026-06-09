#!/usr/bin/env bash
# generate-gosum.sh — Run this script once to generate go.sum using Docker.
# Prerequisites: Docker Desktop must be running.
#
# Usage:
#   bash generate-gosum.sh
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
echo "[INFO] Generating go.sum using Docker Go 1.23 container..."

docker run --rm \
  -v "${SCRIPT_DIR}:/workspace" \
  -w /workspace \
  golang:1.23-alpine \
  sh -c "apk add --no-cache git && go mod tidy && go build ./... && go test ./..."

echo "[SUCCESS] go.sum generated and build verified."
