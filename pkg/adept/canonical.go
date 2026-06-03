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

// Skill is the canonical representation of a skill in the library or project.
// Body is the markdown that follows skill.yaml or SKILL.md frontmatter. The
// identity of a skill is (id, hash); version numbers are intentionally absent
// — content hash is the source of truth for "did this change".
type Skill struct {
	ID           string            `yaml:"id"            json:"id"`
	Description  string            `yaml:"description"   json:"description"`
	Activation   ActivationMode    `yaml:"activation"    json:"activation"`
	Globs        []string          `yaml:"globs,omitempty"          json:"globs,omitempty"`
	AllowedTools []string          `yaml:"allowed-tools,omitempty"  json:"allowedTools,omitempty"`
	Targets      []string          `yaml:"targets,omitempty"        json:"targets,omitempty"`
	Tags         []string          `yaml:"tags,omitempty"           json:"tags,omitempty"`
	Metadata     map[string]string `yaml:"metadata,omitempty"       json:"metadata,omitempty"`

	// Model is a promoted, optional model hint. Only harnesses that understand
	// a per-skill model (currently Claude Code) emit it; others ignore it. It
	// is promoted to a top-level field because it is the single most commonly
	// wanted per-skill knob with no analog elsewhere.
	Model string `yaml:"model,omitempty" json:"model,omitempty"`

	// Harness holds per-harness frontmatter/option overrides keyed by harness
	// id (e.g. "claude-code", "cursor"). Each renderer merges its own entry
	// over the fields it derives from the canonical skill, last-wins. Keys a
	// harness has no concept of are ignored, so the map degrades safely. The
	// canonical identity fields (id/name/description) cannot be overridden;
	// the schema rejects that. The block round-trips verbatim through the
	// canonical writer but is intentionally NOT reconstructed on import.
	Harness map[string]map[string]any `yaml:"harness,omitempty" json:"harness,omitempty"`

	Body  string      `yaml:"-" json:"-"`
	Files []SkillFile `yaml:"-" json:"-"`
}

// ProjectInfo carries the minimal project context renderers may need.
type ProjectInfo struct {
	Name string
	Root string
}
