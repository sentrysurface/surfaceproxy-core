#!/usr/bin/env pwsh
# generate-gosum.ps1 — Run this script once to generate go.sum using Docker.
# Prerequisites: Docker Desktop must be running.
#
# Usage (PowerShell):
#   .\scripts\generate-gosum.ps1
#
$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectRoot = Split-Path -Parent $ScriptDir

Write-Host "[INFO] Generating go.sum using Docker Go 1.23 container..."

docker run --rm `
  -v "${ProjectRoot}:/workspace" `
  -w /workspace `
  golang:1.23-alpine `
  sh -c "apk add --no-cache git && go mod tidy && go build ./... && go test ./..."

Write-Host "[SUCCESS] go.sum generated and build verified."
