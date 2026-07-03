package canonical

import (
	"fmt"
	"sort"
	"strings"

	"github.com/itaywol/adeptability/pkg/adept"
)

// RenderCanonicalAgent produces the on-disk agent .md bytes (frontmatter +
// body) for an Agent. Single source of truth for canonical agent writes so a
// parser round-trips through whatever we write.
//
// Field order in the emitted frontmatter:
//
//	id
//	description
//	mode
//	tools
//	disallowed-tools
//	model
//	targets
//	tags
//	metadata
//	harness
func RenderCanonicalAgent(a *adept.Agent) ([]byte, error) {
	if a.ID == "" {
		return nil, fmt.Errorf("%w: empty id", adept.ErrAgentInvalid)
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("id: %s\n", a.ID))
	b.WriteString(fmt.Sprintf("description: %s\n", yamlQuote(a.Description)))
	if a.Mode != "" {
		b.WriteString(fmt.Sprintf("mode: %s\n", a.Mode))
	}
	writeStringList(&b, "tools", a.Tools)
	writeStringList(&b, "disallowed-tools", a.DisallowedTools)
	if a.Model != "" {
		b.WriteString(fmt.Sprintf("model: %s\n", yamlQuote(a.Model)))
	}
	writeStringList(&b, "targets", a.Targets)
	writeStringList(&b, "tags", a.Tags)
	if len(a.Metadata) > 0 {
		b.WriteString("metadata:\n")
		keys := make([]string, 0, len(a.Metadata))
		for k := range a.Metadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(fmt.Sprintf("  %s: %s\n", k, yamlQuote(a.Metadata[k])))
		}
	}
	if len(a.Harness) > 0 {
		block, err := marshalHarnessBlock(a.Harness)
		if err != nil {
			return nil, err
		}
		b.WriteString(block)
	}
	b.WriteString("---\n")
	b.WriteString(a.Body)
	return []byte(b.String()), nil
}
