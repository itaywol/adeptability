// Package common provides shared helpers for harness renderers:
// YAML frontmatter assembly, path template resolution, and drift computation.
package common

import (
	"bytes"
	"fmt"
	"sort"
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
		// Arbitrary values (e.g. per-harness overrides decoded from YAML as
		// float64, []interface{}, or map[string]interface{}) are serialized via
		// yaml.v3. The typed cases above stay for Quote support and flow-style
		// slices.
		return encodeArbitrary(v)
	}
}

// encodeArbitrary yaml-encodes any value into a node. yaml.v3 panics on values
// it cannot represent (chan, func, …) instead of returning an error, so the
// panic is recovered and surfaced as an error — the renderer never crashes on
// a malformed override value.
func encodeArbitrary(v any) (node *yaml.Node, err error) {
	defer func() {
		if r := recover(); r != nil {
			node, err = nil, fmt.Errorf("unsupported value type %T: %v", v, r)
		}
	}()
	raw, err := yaml.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("unsupported value type %T: %w", v, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("unsupported value type %T: %w", v, err)
	}
	if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
		return doc.Content[0], nil
	}
	return &doc, nil
}

// MergeOverride applies a per-harness override map onto a renderer's derived
// fields: a key that already exists replaces that field's value (last-wins);
// a new key is appended. Override keys are applied in sorted order so output
// is deterministic regardless of map iteration order. A nil/empty override is
// a no-op. The identity fields (id/name/description) are guarded by the schema
// before render, so this function does not re-check them.
func MergeOverride(fields []Field, override map[string]any) ([]Field, error) {
	if len(override) == 0 {
		return fields, nil
	}
	keys := make([]string, 0, len(override))
	for k := range override {
		if k == "" {
			return nil, fmt.Errorf("frontmatter override: empty key")
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := override[k]
		replaced := false
		for i := range fields {
			if fields[i].Key == k {
				fields[i].Value = v
				fields[i].Quote = false
				replaced = true
				break
			}
		}
		if !replaced {
			fields = append(fields, Field{Key: k, Value: v})
		}
	}
	return fields, nil
}
