package cli

import (
	"fmt"

	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/internal/harness"
	"github.com/itaywol/adeptability/internal/render/perskill"
	"github.com/itaywol/adeptability/pkg/adept"
)

// vercelAgent is one row of the per-skill agent table. The table is
// derived from vercel-labs/skills' supported-agents matrix
// (https://github.com/vercel-labs/skills) so adeptability can target
// every harness that ecosystem speaks. Each agent gets its own
// registered ID; agents that share an on-disk path (e.g. several
// vendors that all write into `.agents/skills/`) are kept as distinct
// adapter IDs so users can opt-in by name.
//
// The five specialized adapters (claude-code, cursor, codex, copilot,
// opencode) keep their historical paths and richer renderers; they are
// intentionally NOT redefined here.
type vercelAgent struct {
	ID      string
	Name    string
	BaseDir string
	Output  string // path template, must contain "{id}"
}

// vercelAgentTable enumerates the per-skill harnesses to register.
// Source: vercel-labs/skills README supported-agents matrix.
var vercelAgentTable = []vercelAgent{
	{ID: "aider-desk", Name: "AiderDesk", BaseDir: ".aider-desk", Output: ".aider-desk/skills/{id}/SKILL.md"},
	{ID: "amp", Name: "Amp", BaseDir: ".agents", Output: ".agents/skills/{id}/SKILL.md"},
	{ID: "kimi-cli", Name: "Kimi Code CLI", BaseDir: ".agents", Output: ".agents/skills/{id}/SKILL.md"},
	{ID: "replit", Name: "Replit", BaseDir: ".agents", Output: ".agents/skills/{id}/SKILL.md"},
	{ID: "universal", Name: "Universal", BaseDir: ".agents", Output: ".agents/skills/{id}/SKILL.md"},
	{ID: "antigravity", Name: "Antigravity", BaseDir: ".agents", Output: ".agents/skills/{id}/SKILL.md"},
	{ID: "augment", Name: "Augment", BaseDir: ".augment", Output: ".augment/skills/{id}/SKILL.md"},
	{ID: "bob", Name: "IBM Bob", BaseDir: ".bob", Output: ".bob/skills/{id}/SKILL.md"},
	{ID: "openclaw", Name: "OpenClaw", BaseDir: "skills", Output: "skills/{id}/SKILL.md"},
	{ID: "cline", Name: "Cline", BaseDir: ".agents", Output: ".agents/skills/{id}/SKILL.md"},
	{ID: "dexto", Name: "Dexto", BaseDir: ".agents", Output: ".agents/skills/{id}/SKILL.md"},
	{ID: "warp", Name: "Warp", BaseDir: ".agents", Output: ".agents/skills/{id}/SKILL.md"},
	{ID: "codearts-agent", Name: "CodeArts Agent", BaseDir: ".codeartsdoer", Output: ".codeartsdoer/skills/{id}/SKILL.md"},
	{ID: "codebuddy", Name: "CodeBuddy", BaseDir: ".codebuddy", Output: ".codebuddy/skills/{id}/SKILL.md"},
	{ID: "codemaker", Name: "Codemaker", BaseDir: ".codemaker", Output: ".codemaker/skills/{id}/SKILL.md"},
	{ID: "codestudio", Name: "Code Studio", BaseDir: ".codestudio", Output: ".codestudio/skills/{id}/SKILL.md"},
	{ID: "command-code", Name: "Command Code", BaseDir: ".commandcode", Output: ".commandcode/skills/{id}/SKILL.md"},
	{ID: "continue", Name: "Continue", BaseDir: ".continue", Output: ".continue/skills/{id}/SKILL.md"},
	{ID: "cortex", Name: "Cortex Code", BaseDir: ".cortex", Output: ".cortex/skills/{id}/SKILL.md"},
	{ID: "crush", Name: "Crush", BaseDir: ".crush", Output: ".crush/skills/{id}/SKILL.md"},
	{ID: "deepagents", Name: "Deep Agents", BaseDir: ".agents", Output: ".agents/skills/{id}/SKILL.md"},
	{ID: "devin", Name: "Devin for Terminal", BaseDir: ".devin", Output: ".devin/skills/{id}/SKILL.md"},
	{ID: "droid", Name: "Droid", BaseDir: ".factory", Output: ".factory/skills/{id}/SKILL.md"},
	{ID: "firebender", Name: "Firebender", BaseDir: ".agents", Output: ".agents/skills/{id}/SKILL.md"},
	{ID: "forgecode", Name: "ForgeCode", BaseDir: ".forge", Output: ".forge/skills/{id}/SKILL.md"},
	{ID: "gemini-cli", Name: "Gemini CLI", BaseDir: ".agents", Output: ".agents/skills/{id}/SKILL.md"},
	{ID: "github-copilot", Name: "GitHub Copilot (agent skills)", BaseDir: ".agents", Output: ".agents/skills/{id}/SKILL.md"},
	{ID: "goose", Name: "Goose", BaseDir: ".goose", Output: ".goose/skills/{id}/SKILL.md"},
	{ID: "hermes-agent", Name: "Hermes Agent", BaseDir: ".hermes", Output: ".hermes/skills/{id}/SKILL.md"},
	{ID: "junie", Name: "Junie", BaseDir: ".junie", Output: ".junie/skills/{id}/SKILL.md"},
	{ID: "iflow-cli", Name: "iFlow CLI", BaseDir: ".iflow", Output: ".iflow/skills/{id}/SKILL.md"},
	{ID: "kilo", Name: "Kilo Code", BaseDir: ".kilocode", Output: ".kilocode/skills/{id}/SKILL.md"},
	{ID: "kiro-cli", Name: "Kiro CLI", BaseDir: ".kiro", Output: ".kiro/skills/{id}/SKILL.md"},
	{ID: "kode", Name: "Kode", BaseDir: ".kode", Output: ".kode/skills/{id}/SKILL.md"},
	{ID: "mcpjam", Name: "MCPJam", BaseDir: ".mcpjam", Output: ".mcpjam/skills/{id}/SKILL.md"},
	{ID: "mistral-vibe", Name: "Mistral Vibe", BaseDir: ".vibe", Output: ".vibe/skills/{id}/SKILL.md"},
	{ID: "mux", Name: "Mux", BaseDir: ".mux", Output: ".mux/skills/{id}/SKILL.md"},
	{ID: "openhands", Name: "OpenHands", BaseDir: ".openhands", Output: ".openhands/skills/{id}/SKILL.md"},
	{ID: "pi", Name: "Pi", BaseDir: ".pi", Output: ".pi/skills/{id}/SKILL.md"},
	{ID: "qoder", Name: "Qoder", BaseDir: ".qoder", Output: ".qoder/skills/{id}/SKILL.md"},
	{ID: "qwen-code", Name: "Qwen Code", BaseDir: ".qwen", Output: ".qwen/skills/{id}/SKILL.md"},
	{ID: "rovodev", Name: "Rovo Dev", BaseDir: ".rovodev", Output: ".rovodev/skills/{id}/SKILL.md"},
	{ID: "roo", Name: "Roo Code", BaseDir: ".roo", Output: ".roo/skills/{id}/SKILL.md"},
	{ID: "tabnine-cli", Name: "Tabnine CLI", BaseDir: ".tabnine", Output: ".tabnine/agent/skills/{id}/SKILL.md"},
	{ID: "trae", Name: "Trae", BaseDir: ".trae", Output: ".trae/skills/{id}/SKILL.md"},
	{ID: "trae-cn", Name: "Trae CN", BaseDir: ".trae", Output: ".trae/skills/{id}/SKILL.md"},
	{ID: "windsurf", Name: "Windsurf", BaseDir: ".windsurf", Output: ".windsurf/skills/{id}/SKILL.md"},
	{ID: "zencoder", Name: "Zencoder", BaseDir: ".zencoder", Output: ".zencoder/skills/{id}/SKILL.md"},
	{ID: "neovate", Name: "Neovate", BaseDir: ".neovate", Output: ".neovate/skills/{id}/SKILL.md"},
	{ID: "pochi", Name: "Pochi", BaseDir: ".pochi", Output: ".pochi/skills/{id}/SKILL.md"},
	{ID: "adal", Name: "AdaL", BaseDir: ".adal", Output: ".adal/skills/{id}/SKILL.md"},
}

// registerVercelAgents wires every entry from vercelAgentTable into reg
// using the generic perskill adapter. Skips any ID already present in
// the registry, so the five specialized adapters keep precedence over
// any table row that happens to share their ID.
func registerVercelAgents(reg harness.Registry, w fsutil.Writer, l fsutil.Linker) error {
	for _, a := range vercelAgentTable {
		if _, err := reg.Get(a.ID); err == nil {
			continue
		}
		spec := adept.HarnessSpec{
			ID:         a.ID,
			Name:       a.Name,
			Kind:       adept.KindPerSkill,
			OutputPath: a.Output,
			BaseDir:    a.BaseDir,
			NeedsDir:   true,
		}
		ad := perskill.NewAdapter(spec, w, l)
		if err := reg.Register(ad); err != nil {
			return fmt.Errorf("register vercel agent %s: %w", a.ID, err)
		}
	}
	return nil
}
