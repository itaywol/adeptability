package org

import (
	"context"
	"fmt"
	"os"
)

// Client fetches an org Manifest from some location (local file, library
// remote, etc.). Concrete clients are constructed by the CLI based on what
// the project lockfile points at.
type Client interface {
	Fetch(ctx context.Context) (*Manifest, error)
}

// NewFileClient returns a Client that reads org.yaml from path. The path is
// resolved at Fetch time so callers can swap it without re-wiring.
func NewFileClient(path string, p Parser) Client {
	return &fileClient{path: path, parser: p}
}

type fileClient struct {
	path   string
	parser Parser
}

func (c *fileClient) Fetch(_ context.Context) (*Manifest, error) {
	if c.path == "" {
		return nil, fmt.Errorf("org file client: empty path")
	}
	data, err := os.ReadFile(c.path)
	if err != nil {
		return nil, fmt.Errorf("org file client: read %q: %w", c.path, err)
	}
	m, err := c.parser.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("org file client: parse %q: %w", c.path, err)
	}
	return m, nil
}
