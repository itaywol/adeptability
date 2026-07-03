package agentlint

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/itaywol/adeptability/internal/scan"
	"github.com/itaywol/adeptability/pkg/adept"
)

// Rule IDs: AGENT-LINT-0xx = error (SeverityHigh), 1xx = warning
// (SeverityMedium), 2xx = info (SeverityLow).
//
// Future rules (documented, not implemented): temperature/effort range checks
// for harness overrides, cross-harness tool alias validation (copilot alias
// table), description length budgets per harness.

var (
	agentIDRE = regexp.MustCompile(adept.AgentIDPattern)
	// triggerClauseRE detects "when to use" phrasing in a description —
	// the lever every harness uses for automatic delegation.
	triggerClauseRE = regexp.MustCompile(`(?i)\b(use|when|after|invoke|before|during|whenever)\b`)
	// proactiveHintRE matches the phrasing Claude Code and Cursor document
	// as encouraging proactive delegation.
	proactiveHintRE = regexp.MustCompile(`(?i)use\s+(this\s+agent\s+)?(proactively|immediately|always)|always\s+use`)
	// readonlyIntentRE detects prose promising the agent will not modify
	// anything.
	readonlyIntentRE = regexp.MustCompile(`(?i)\b(never|do not|don't|must not|without)\s+(modify|edit|write|change|touch)\w*\b`)
	// boundaryRE detects any negative constraint — the "Stop" section an
	// agent definition cannot infer on its own.
	boundaryRE = regexp.MustCompile(`(?i)\b(never|do not|don't|must not|only)\b`)
	// evaluatorNameRE marks agents whose job is judging others' work.
	evaluatorNameRE = regexp.MustCompile(`(?i)review|evaluat|check|verif|audit|critic`)
	// numberedStepRE detects a procedural "1." style step list.
	numberedStepRE = regexp.MustCompile(`(?m)^\s*1[.)]\s`)
)

// claudeTools is the Claude Code built-in tool vocabulary for
// unknown-tool-for-target. MCP tools (mcp__*) are always accepted.
var claudeTools = map[string]bool{
	"Task": true, "Bash": true, "Glob": true, "Grep": true, "Read": true,
	"Edit": true, "Write": true, "WebFetch": true, "WebSearch": true,
	"NotebookEdit": true, "TodoWrite": true, "SlashCommand": true,
	"Skill": true, "BashOutput": true, "KillShell": true,
	"ExitPlanMode": true, "AskUserQuestion": true, "SendMessage": true,
}

// writeCapableTools are tool names that let an agent mutate the workspace
// (used by the boundaries rule — Bash counts, since it can mutate).
var writeCapableTools = map[string]bool{
	"Edit": true, "Write": true, "NotebookEdit": true, "Bash": true,
	"edit": true, "execute": true, "shell": true,
}

// mutationTools is the narrower set for the readonly-intent rule: direct
// file-mutation tools only. Bash is excluded on purpose — a read-only-ish
// agent legitimately keeps Bash to run tests, and a "Do not modify" boundary
// line must not flag it.
var mutationTools = map[string]bool{
	"Edit": true, "Write": true, "NotebookEdit": true, "edit": true,
}

// executionTools let an agent act (run code/tests) rather than just read.
var executionTools = map[string]bool{
	"Bash": true, "execute": true, "shell": true, "Task": true,
}

func defaultRules() []rule {
	return []rule{
		ruleMissingDescription,
		ruleBadID,
		ruleEmptyBody,
		ruleModelGrammar,
		ruleFileReferences,
		ruleVagueDescription,
		ruleBodyTooShort,
		ruleReadonlyIntentMismatch,
		ruleUnknownClaudeTools,
		ruleOverlappingDescriptions,
		ruleNoProactiveHint,
		ruleMissingStructure,
		ruleOverlongPrompt,
		ruleMissingBoundaries,
		ruleEvaluatorCannotAct,
	}
}

// ---------- errors (SeverityHigh) ----------

func ruleMissingDescription(in Input) []scan.Finding {
	if strings.TrimSpace(in.Agent.Description) != "" {
		return nil
	}
	return []scan.Finding{finding(in.Agent, "AGENT-LINT-001", CategoryBestPractice, scan.SeverityHigh, scan.ConfidenceHigh,
		"missing description",
		"",
		"Every harness routes delegation through the description; without one the agent never auto-triggers. Describe what the agent does AND when to invoke it.")}
}

func ruleBadID(in Input) []scan.Finding {
	if agentIDRE.MatchString(in.Agent.ID) {
		return nil
	}
	return []scan.Finding{finding(in.Agent, "AGENT-LINT-002", CategoryBestPractice, scan.SeverityHigh, scan.ConfidenceHigh,
		"agent id does not match "+adept.AgentIDPattern,
		in.Agent.ID,
		"Use lowercase letters, digits, and internal hyphens — the common charset across Claude Code, OpenCode, Cursor, Copilot, and Codex agent names.")}
}

func ruleEmptyBody(in Input) []scan.Finding {
	if strings.TrimSpace(in.Agent.Body) != "" {
		return nil
	}
	return []scan.Finding{finding(in.Agent, "AGENT-LINT-003", CategoryBestPractice, scan.SeverityHigh, scan.ConfidenceHigh,
		"empty body (system prompt)",
		"",
		"The body is the agent's entire system prompt — subagents do not inherit the main system prompt. Codex additionally requires it (developer_instructions).")}
}

func ruleModelGrammar(in Input) []scan.Finding {
	a := in.Agent
	if a.Model == "" {
		return nil
	}
	targetsOnly := func(h string) bool {
		return len(a.Targets) > 0 && len(a.Targets) == 1 && a.Targets[0] == h
	}
	var out []scan.Finding
	// OpenCode model values are provider/model-id; a bare alias will not
	// resolve there.
	if targetsOnly("opencode") && !strings.Contains(a.Model, "/") {
		out = append(out, finding(a, "AGENT-LINT-004", CategoryBestPractice, scan.SeverityHigh, scan.ConfidenceHigh,
			fmt.Sprintf("model %q is not valid for opencode", a.Model),
			a.Model,
			"OpenCode expects provider/model-id (e.g. anthropic/claude-sonnet-4-20250514)."))
	}
	// One model string rendered to every harness cannot satisfy all their
	// grammars at once (Claude aliases vs OpenCode provider/model).
	if len(a.Targets) == 0 {
		overridden := false
		for _, ov := range a.Harness {
			if _, ok := ov["model"]; ok {
				overridden = true
				break
			}
		}
		if !overridden {
			out = append(out, finding(a, "AGENT-LINT-106", CategoryBestPractice, scan.SeverityMedium, scan.ConfidenceMedium,
				fmt.Sprintf("model %q passes through verbatim to every harness, whose value grammars differ", a.Model),
				a.Model,
				"Scope the agent with targets:, or set per-harness models via harness: overrides."))
		}
	}
	return out
}

// ---------- warnings (SeverityMedium) ----------

func ruleVagueDescription(in Input) []scan.Finding {
	desc := strings.TrimSpace(in.Agent.Description)
	if desc == "" {
		return nil // AGENT-LINT-001 already fired
	}
	if len(desc) >= 20 && triggerClauseRE.MatchString(desc) {
		return nil
	}
	return []scan.Finding{finding(in.Agent, "AGENT-LINT-101", CategoryBestPractice, scan.SeverityMedium, scan.ConfidenceMedium,
		"description does not say when to use the agent",
		desc,
		`State what the agent does AND when to invoke it — e.g. "Reviews Go changes for error-handling bugs. Use after editing internal/ packages." The description is the delegation trigger.`)}
}

func ruleBodyTooShort(in Input) []scan.Finding {
	body := strings.TrimSpace(in.Agent.Body)
	if body == "" || len(body) >= 50 {
		return nil
	}
	return []scan.Finding{finding(in.Agent, "AGENT-LINT-102", CategoryBestPractice, scan.SeverityMedium, scan.ConfidenceHigh,
		"body is shorter than 50 characters",
		body,
		"A one-line prompt rarely encodes a role, procedure, and output contract. Structure the body: role → when-invoked steps → output → boundaries.")}
}

func ruleReadonlyIntentMismatch(in Input) []scan.Finding {
	a := in.Agent
	stripped, balanced := scan.StripFences(a.Body)
	if !balanced {
		stripped = a.Body
	}
	if !readonlyIntentRE.MatchString(stripped) {
		return nil
	}
	for _, tool := range a.Tools {
		if mutationTools[tool] {
			return []scan.Finding{finding(a, "AGENT-LINT-103", CategoryBestPractice, scan.SeverityMedium, scan.ConfidenceMedium,
				fmt.Sprintf("body promises not to modify anything but tools grant %q", tool),
				readonlyIntentRE.FindString(stripped),
				"Drop write-capable tools from the allowlist (or use disallowed-tools) so the capability matches the promise — prompts alone do not enforce read-only.")}
		}
	}
	return nil
}

func ruleUnknownClaudeTools(in Input) []scan.Finding {
	a := in.Agent
	// Only meaningful when the agent renders to Claude Code.
	applies := len(a.Targets) == 0
	for _, t := range a.Targets {
		if t == "claude-code" {
			applies = true
		}
	}
	if !applies {
		return nil
	}
	tools := append(append([]string{}, a.Tools...), a.DisallowedTools...)
	out := make([]scan.Finding, 0, len(tools))
	for _, tool := range tools {
		if claudeTools[tool] || strings.HasPrefix(tool, "mcp__") {
			continue
		}
		// Copilot aliases are lowercase; don't flag them as unknown for
		// multi-target agents.
		if len(a.Targets) == 0 && tool == strings.ToLower(tool) {
			continue
		}
		out = append(out, finding(a, "AGENT-LINT-104", CategoryBestPractice, scan.SeverityMedium, scan.ConfidenceLow,
			fmt.Sprintf("tool %q is not a known Claude Code tool", tool),
			tool,
			"Unknown tool names are silently ignored by the harness, leaving the agent with more or less capability than intended."))
	}
	return out
}

func ruleOverlappingDescriptions(in Input) []scan.Finding {
	a := in.Agent
	mine := tokenSet(a.Description)
	if len(mine) == 0 {
		return nil
	}
	var out []scan.Finding
	for _, other := range in.AllAgents {
		if other == nil || other.ID == a.ID {
			continue
		}
		if j := jaccard(mine, tokenSet(other.Description)); j > 0.8 {
			out = append(out, finding(a, "AGENT-LINT-105", CategoryBestPractice, scan.SeverityMedium, scan.ConfidenceMedium,
				fmt.Sprintf("description overlaps heavily with agent %q (similarity %.2f)", other.ID, j),
				a.Description,
				"Near-duplicate descriptions make automatic delegation unreliable — merge the agents or sharpen each description's trigger."))
		}
	}
	return out
}

// ---------- info (SeverityLow) ----------

func ruleNoProactiveHint(in Input) []scan.Finding {
	if in.Agent.Mode == adept.AgentModePrimary {
		return nil // primary agents are user-driven, not delegated to
	}
	if proactiveHintRE.MatchString(in.Agent.Description) {
		return nil
	}
	return []scan.Finding{finding(in.Agent, "AGENT-LINT-201", CategoryBestPractice, scan.SeverityLow, scan.ConfidenceLow,
		"description has no proactive trigger hint",
		"",
		`If the agent should auto-fire, include phrasing like "use proactively" or "use immediately after …" — both Claude Code and Cursor document this as the delegation lever.`)}
}

func ruleMissingStructure(in Input) []scan.Finding {
	body := in.Agent.Body
	if strings.TrimSpace(body) == "" {
		return nil
	}
	if strings.Contains(body, "\n## ") || strings.HasPrefix(body, "## ") || numberedStepRE.MatchString(body) {
		return nil
	}
	return []scan.Finding{finding(in.Agent, "AGENT-LINT-202", CategoryBestPractice, scan.SeverityLow, scan.ConfidenceLow,
		"body has no procedural structure (no sections or numbered steps)",
		"",
		"Effective agents state a role, then \"when invoked: 1..2..3\" steps, then an output contract. Unstructured prose underperforms.")}
}

func ruleOverlongPrompt(in Input) []scan.Finding {
	lines := strings.Count(in.Agent.Body, "\n")
	if lines <= 500 {
		return nil
	}
	return []scan.Finding{finding(in.Agent, "AGENT-LINT-203", CategoryBestPractice, scan.SeverityLow, scan.ConfidenceMedium,
		fmt.Sprintf("body is %d lines long", lines),
		"",
		"Long, rambling prompts dilute focus. Move reference material into skills the agent can load on demand.")}
}

func ruleMissingBoundaries(in Input) []scan.Finding {
	a := in.Agent
	if strings.TrimSpace(a.Body) == "" {
		return nil
	}
	// Applies to write-capable agents: an explicit tools allowlist without
	// write tools is exempt; no tools at all inherits everything.
	writeCapable := len(a.Tools) == 0
	for _, tool := range a.Tools {
		if writeCapableTools[tool] {
			writeCapable = true
		}
	}
	if !writeCapable {
		return nil
	}
	stripped, balanced := scan.StripFences(a.Body)
	if !balanced {
		stripped = a.Body
	}
	if boundaryRE.MatchString(stripped) {
		return nil
	}
	return []scan.Finding{finding(a, "AGENT-LINT-204", CategoryBestPractice, scan.SeverityLow, scan.ConfidenceMedium,
		"write-capable agent has no boundaries (no negative constraints in body)",
		"",
		`Add a Boundaries section with explicit "do not" lines (files never to touch, actions never to take). The boundary is the one part of an agent the harness cannot infer.`)}
}

func ruleEvaluatorCannotAct(in Input) []scan.Finding {
	a := in.Agent
	if !evaluatorNameRE.MatchString(a.ID) && !evaluatorNameRE.MatchString(a.Description) {
		return nil
	}
	if len(a.Tools) == 0 {
		return nil // inherits everything, including execution
	}
	for _, tool := range a.Tools {
		if executionTools[tool] {
			return nil
		}
	}
	return []scan.Finding{finding(a, "AGENT-LINT-205", CategoryBestPractice, scan.SeverityLow, scan.ConfidenceLow,
		"evaluator-shaped agent cannot act (no execution tool granted)",
		strings.Join(a.Tools, ", "),
		"An evaluator that only reads judges \"does this look right\", not \"does it run right\". Grant a way to execute (run tests, run the code) so verdicts come from behavior.")}
}

// ---------- helpers ----------

func tokenSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(strings.ToLower(s)) {
		w = strings.Trim(w, ".,;:!?()[]{}\"'`")
		if len(w) > 2 {
			out[w] = true
		}
	}
	return out
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for w := range a {
		if b[w] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}
