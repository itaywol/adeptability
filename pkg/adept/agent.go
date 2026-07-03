package adept

// AgentMode controls how a harness surfaces an agent. It maps to OpenCode's
// `mode` field; harnesses without the concept ignore it.
type AgentMode string

// Agent surface modes.
const (
	// AgentModeSubagent is the default: the agent is delegated to by a
	// primary agent and never drives a session itself.
	AgentModeSubagent AgentMode = "subagent"
	// AgentModePrimary marks an agent that drives a session directly.
	AgentModePrimary AgentMode = "primary"
	// AgentModeAll allows both primary and subagent use.
	AgentModeAll AgentMode = "all"
)

// Agent is the canonical representation of a subagent definition. Body is the
// markdown system prompt that follows the frontmatter. Like skills, identity
// is (id, hash) — no version numbers. Unlike skills, agents are single files
// (.adeptability/agents/<id>.md) with no sidecars: no supported harness can
// attach files to an agent definition.
//
// A field is canonical only when at least two harnesses can express it;
// single-harness knobs (permissionMode, temperature, readonly, sandbox_mode,
// …) belong in the Harness override map.
type Agent struct {
	ID          string    `yaml:"id"          json:"id"`
	Description string    `yaml:"description" json:"description"`
	Mode        AgentMode `yaml:"mode,omitempty" json:"mode,omitempty"`

	// Tools is an allowlist of tool names the agent may use. An empty list
	// means "inherit everything" on every harness that has the concept.
	Tools []string `yaml:"tools,omitempty" json:"tools,omitempty"`
	// DisallowedTools is the matching denylist (Claude Code only today, but
	// symmetric with Tools and cheap to carry).
	DisallowedTools []string `yaml:"disallowed-tools,omitempty" json:"disallowedTools,omitempty"`

	// Model is passed through verbatim. Value grammars differ per harness
	// (aliases, provider/model, inherit); the linter checks per-target
	// validity, renderers do not.
	Model string `yaml:"model,omitempty" json:"model,omitempty"`

	Targets  []string          `yaml:"targets,omitempty"  json:"targets,omitempty"`
	Tags     []string          `yaml:"tags,omitempty"     json:"tags,omitempty"`
	Metadata map[string]string `yaml:"metadata,omitempty" json:"metadata,omitempty"`

	// Harness holds per-harness frontmatter overrides keyed by harness id,
	// merged last-wins by each renderer — the same escape hatch as
	// Skill.Harness, with the same import policy: never reconstructed.
	Harness map[string]map[string]any `yaml:"harness,omitempty" json:"harness,omitempty"`

	Body string `yaml:"-" json:"-"`
}
