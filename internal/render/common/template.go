package common

import "strings"

// PathTemplater resolves harness output path templates by substituting
// placeholders ({id}) with the supplied skill id.
type PathTemplater interface {
	Resolve(template, skillID string) string
}

type pathTemplater struct{}

// NewPathTemplater returns the default templater.
func NewPathTemplater() PathTemplater { return &pathTemplater{} }

// Resolve replaces every occurrence of {id} in template with skillID.
// If template contains no placeholder it is returned unchanged.
func (p *pathTemplater) Resolve(template, skillID string) string {
	return strings.ReplaceAll(template, "{id}", skillID)
}
