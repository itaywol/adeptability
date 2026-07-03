#!/bin/sh
# Generates shell completions for the release archives (goreleaser before hook).
set -e
cd "$(dirname "$0")/.."
rm -rf completions
mkdir completions
go run ./cmd/adept completion bash >completions/adept.bash
go run ./cmd/adept completion zsh >completions/adept.zsh
go run ./cmd/adept completion fish >completions/adept.fish
go run ./cmd/adept completion powershell >completions/adept.ps1
