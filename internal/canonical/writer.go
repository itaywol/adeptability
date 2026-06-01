package canonical

import (
	"fmt"
	"sort"
	"strings"

	"github.com/itaywol/adeptability/pkg/adept"
)

// RenderCanonical produces the on-disk SKILL.md bytes (frontmatter + body) for
// a Skill. Single source of truth used by both the library and the project
// stores so a parser round-trips through whatever we write.
//
// Field order in the emitted frontmatter:
//
//	id
//	description
//	activation
//	globs
//	allowed-tools
//	targets
//	tags
//	metadata
//
// String values are always double-quoted to avoid YAML parser ambiguity
// around leading `*` (alias), `&` (anchor), `:` and `#`.
func RenderCanonical(s *adept.Skill) ([]byte, error) {
	if s.ID == "" {
		return nil, fmt.Errorf("%w: empty id", adept.ErrSkillInvalid)
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("id: %s\n", s.ID))
	b.WriteString(fmt.Sprintf("description: %s\n", yamlQuote(s.Description)))
	if s.Activation != "" {
		b.WriteString(fmt.Sprintf("activation: %s\n", s.Activation))
	}
	writeStringList(&b, "globs", s.Globs)
	writeStringList(&b, "allowed-tools", s.AllowedTools)
	writeStringList(&b, "targets", s.Targets)
	writeStringList(&b, "tags", s.Tags)
	if len(s.Metadata) > 0 {
		b.WriteString("metadata:\n")
		keys := make([]string, 0, len(s.Metadata))
		for k := range s.Metadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(fmt.Sprintf("  %s: %s\n", k, yamlQuote(s.Metadata[k])))
		}
	}
	b.WriteString("---\n")
	b.WriteString(s.Body)
	return []byte(b.String()), nil
}

func writeStringList(b *strings.Builder, key string, xs []string) {
	if len(xs) == 0 {
		return
	}
	b.WriteString(key)
	b.WriteString(":\n")
	for _, x := range xs {
		b.WriteString("  - ")
		b.WriteString(yamlQuote(x))
		b.WriteString("\n")
	}
}

func yamlQuote(s string) string {
	escaped := strings.ReplaceAll(s, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}
