#!/bin/sh
# Generates shell completions for the release archives (goreleaser before hook).
set -e
rm -rf completions
mkdir completions
for sh in bash zsh fish; do
	go run ./cmd/adept completion "$sh" >"completions/adept.$sh"
done
