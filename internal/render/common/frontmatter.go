// Package common provides shared helpers for harness renderers:
// YAML frontmatter assembly, path template resolution, and drift computation.
package common

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Field is a single key/value pair in a YAML frontmatter block.
// Value is intentionally typed any because the YAML serializer must accept
// strings, bools, ints, and string slices. This is the only place in the
// renderer surface where any is permitted.
type Field struct {
	Key   string
	Value any
	// Quote forces scalar string values to be emitted with double quotes.
	// Used for fields where downstream parsers are picky (e.g. Cursor
	// description containing a colon).
	Quote bool
}

// FrontmatterBuilder assembles a YAML frontmatter block.
// Implementations preserve the input field order.
type FrontmatterBuilder interface {
	Build(fields []Field) (string, error)
}

type frontmatterBuilder struct{}

// NewFrontmatterBuilder returns the default YAML frontmatter builder.
func NewFrontmatterBuilder() FrontmatterBuilder { return &frontmatterBuilder{} }

// Build serializes the fields into a YAML block delimited by --- markers.
// The block is terminated with a trailing newline so callers can append a
// body directly.
func (b *frontmatterBuilder) Build(fields []Field) (string, error) {
	if len(fields) == 0 {
		return "", nil
	}
	root := &yaml.Node{Kind: yaml.MappingNode}
	for _, f := range fields {
		if f.Key == "" {
			return "", fmt.Errorf("frontmatter: empty key")
		}
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: f.Key, Tag: "!!str"}
		valueNode, err := toNode(f.Value, f.Quote)
		if err != nil {
			return "", fmt.Errorf("frontmatter: key %q: %w", f.Key, err)
		}
		root.Content = append(root.Content, keyNode, valueNode)
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return "", fmt.Errorf("frontmatter: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("frontmatter: close: %w", err)
	}
	body := strings.TrimRight(buf.String(), "\n")
	return "---\n" + body + "\n---\n", nil
}

func toNode(v any, quote bool) (*yaml.Node, error) {
	switch val := v.(type) {
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	case string:
		node := &yaml.Node{Kind: yaml.ScalarNode, Value: val, Tag: "!!str"}
		if quote {
			node.Style = yaml.DoubleQuotedStyle
		}
		return node, nil
	case bool:
		node := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool"}
		if val {
			node.Value = "true"
		} else {
			node.Value = "false"
		}
		return node, nil
	case int:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprintf("%d", val)}, nil
	case int64:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprintf("%d", val)}, nil
	case []string:
		seq := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
		for _, item := range val {
			seq.Content = append(seq.Content, &yaml.Node{
				Kind:  yaml.ScalarNode,
				Tag:   "!!str",
				Value: item,
			})
		}
		return seq, nil
	default:
		return nil, fmt.Errorf("unsupported value type %T", v)
	}
}
