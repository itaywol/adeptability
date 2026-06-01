// Package adept exposes the stable public types for the adeptability system.
// In-process consumers (tests, future LSP, plugins) depend on this package.
// All behavior lives in internal/.
package adept

import "io/fs"

// ActivationMode controls when a harness should surface a skill.
type ActivationMode string

const (
	ActivationAlways ActivationMode = "always"
	ActivationGlobs  ActivationMode = "globs"
	ActivationAgent  ActivationMode = "agent"
	ActivationManual ActivationMode = "manual"
)

// SkillFile is a sidecar resource that ships alongside SKILL.md
// (scripts/, references/, assets/).
type SkillFile struct {
	RelPath string
	Mode    fs.FileMode
	Bytes   []byte
}

// Skill is the canonical representation of a skill in the library.
// Body is the markdown that follows skill.yaml or SKILL.md frontmatter.
type Skill struct {
	ID           string            `yaml:"id"            json:"id"`
	Version      int               `yaml:"version"       json:"version"`
	Description  string            `yaml:"description"   json:"description"`
	Activation   ActivationMode    `yaml:"activation"    json:"activation"`
	Globs        []string          `yaml:"globs,omitempty"          json:"globs,omitempty"`
	AllowedTools []string          `yaml:"allowed-tools,omitempty"  json:"allowedTools,omitempty"`
	Targets      []string          `yaml:"targets,omitempty"        json:"targets,omitempty"`
	Tags         []string          `yaml:"tags,omitempty"           json:"tags,omitempty"`
	SizeHintKiB  int               `yaml:"size-hint-kib,omitempty"  json:"sizeHintKiB,omitempty"`
	Metadata     map[string]string `yaml:"metadata,omitempty"       json:"metadata,omitempty"`

	Body  string      `yaml:"-" json:"-"`
	Files []SkillFile `yaml:"-" json:"-"`
}

// ProjectInfo carries the minimal project context renderers may need.
type ProjectInfo struct {
	Name string
	Root string
}
