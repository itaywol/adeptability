package adept

import "context"

// AgentRenderInput is what an AgentRenderer receives to produce
// harness-ready bytes for one agent.
type AgentRenderInput struct {
	Agent   *Agent
	Harness HarnessSpec
	Project ProjectInfo
}

// AgentRenderer translates a canonical Agent into harness-specific bytes.
// The returned RenderOutput reuses the skill shape: SkillID carries the agent
// id, SkillHash the canonical content hash, and Sidecars stays empty (agents
// are single files everywhere).
type AgentRenderer interface {
	RenderAgent(ctx context.Context, in AgentRenderInput) (RenderOutput, error)
}

// ImportedAgent is one canonical agent recovered from a harness's on-disk
// representation. Warnings carries non-fatal recovery notes (e.g. harness
// fields with no canonical analog that were dropped).
type ImportedAgent struct {
	Agent      *Agent
	SourcePath string
	Warnings   []string
}

// AgentSupport is the optional capability a HarnessAdapter implements when
// its harness understands agent definition files. The orchestrator
// type-asserts for it; adapters without it have applicable agents skipped
// with a warning, mirroring how single-file harnesses drop skill sidecars.
type AgentSupport interface {
	// AgentRenderer returns the renderer for this harness's agent format.
	AgentRenderer() AgentRenderer
	// ValidateAgents compares expected rendered agents against disk and
	// reports drift, exactly like HarnessAdapter.Validate does for skills.
	ValidateAgents(projectRoot string, expected []RenderOutput) (DriftReport, error)
	// ImportAgents reverse-renders the harness's on-disk agent files into
	// canonical agents.
	ImportAgents(ctx context.Context, projectRoot string) ([]ImportedAgent, error)
}
