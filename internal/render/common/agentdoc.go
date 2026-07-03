package common

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseAgentMarkdown splits a harness agent .md document into its frontmatter
// (as a generic key map, so importers can detect foreign keys) and body.
// Shared by every markdown-based agent importer.
func ParseAgentMarkdown(raw []byte) (map[string]any, string, error) {
	s := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return nil, "", fmt.Errorf("agent doc: missing frontmatter")
	}
	rest := s[len("---\n"):]
	closer := "\n---\n"
	end := strings.Index(rest, closer)
	body := ""
	switch {
	case end >= 0:
		body = rest[end+len(closer):]
	case strings.HasSuffix(rest, "\n---"):
		end = len(rest) - len("\n---")
	default:
		return nil, "", fmt.Errorf("agent doc: missing frontmatter terminator")
	}
	fields := map[string]any{}
	if err := yaml.Unmarshal([]byte(rest[:end]), &fields); err != nil {
		return nil, "", fmt.Errorf("agent doc: parse frontmatter: %w", err)
	}
	// Renderers emit one blank separator line between frontmatter and body;
	// strip it so render → import round-trips the body byte-exactly.
	return fields, strings.TrimLeft(body, "\n"), nil
}

// StringField reads a scalar string field from a parsed frontmatter map.
func StringField(fields map[string]any, key string) string {
	if v, ok := fields[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// StringListField reads a field that may be a YAML sequence or a
// comma-separated string (both accepted by Claude Code and Copilot for
// `tools`) and returns it as a clean string slice.
func StringListField(fields map[string]any, key string) []string {
	var out []string
	switch v := fields[key].(type) {
	case string:
		for _, item := range strings.Split(v, ",") {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
	case []any:
		for _, raw := range v {
			if s, ok := raw.(string); ok {
				if s = strings.TrimSpace(s); s != "" {
					out = append(out, s)
				}
			}
		}
	}
	return out
}

// ForeignKeyWarnings returns one warning per frontmatter key not in known,
// in sorted order. harness and id label the message; the canonical harness
// override map is intentionally never reconstructed on import (same policy
// as skills), so dropped keys must be surfaced.
func ForeignKeyWarnings(harness, id string, fields map[string]any, known map[string]bool) []string {
	var foreign []string
	for k := range fields {
		if !known[k] {
			foreign = append(foreign, k)
		}
	}
	sort.Strings(foreign)
	warnings := make([]string, 0, len(foreign))
	for _, k := range foreign {
		warnings = append(warnings, fmt.Sprintf("%s import %s: dropped field %q (no canonical analog; use a harness override to keep it)", harness, id, k))
	}
	return warnings
}
