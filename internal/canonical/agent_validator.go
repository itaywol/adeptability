package canonical

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/itaywol/adeptability/pkg/adeptschema"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// AgentValidator checks a parsed Agent against the canonical JSON Schema.
type AgentValidator interface {
	ValidateAgent(a *adept.Agent) error
}

type agentSchemaValidator struct {
	schema    *jsonschema.Schema
	idPattern *regexp.Regexp
}

// agentIDPatternRE is the compiled form of adept.AgentIDPattern (a constant;
// compilation failure would be a programming error).
var agentIDPatternRE = regexp.MustCompile(adept.AgentIDPattern)

// NewAgentValidator compiles the embedded agent.schema.json once. Returns an
// error only if the embedded schema is malformed (a build bug).
func NewAgentValidator() (AgentValidator, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(adeptschema.AgentSchema))
	if err != nil {
		return nil, fmt.Errorf("agent validator: load embedded schema: %w", err)
	}
	c := jsonschema.NewCompiler()
	const schemaURL = "memory://agent.schema.json"
	if err := c.AddResource(schemaURL, doc); err != nil {
		return nil, fmt.Errorf("agent validator: add embedded schema: %w", err)
	}
	sch, err := c.Compile(schemaURL)
	if err != nil {
		return nil, fmt.Errorf("agent validator: compile embedded schema: %w", err)
	}
	return &agentSchemaValidator{schema: sch, idPattern: agentIDPatternRE}, nil
}

func (v *agentSchemaValidator) ValidateAgent(a *adept.Agent) error {
	if a == nil {
		return fmt.Errorf("validate: %w: nil agent", adept.ErrAgentInvalid)
	}
	doc, err := agentToSchemaDoc(a)
	if err != nil {
		return fmt.Errorf("validate: %w: %w", adept.ErrAgentInvalid, err)
	}
	if err := v.schema.Validate(doc); err != nil {
		return fmt.Errorf("validate: %w: %w", adept.ErrAgentInvalid, err)
	}
	// Defense in depth: the schema pattern may diverge from the constant.
	if !v.idPattern.MatchString(a.ID) {
		return fmt.Errorf("validate: %w: id %q does not match %s", adept.ErrAgentInvalid, a.ID, adept.AgentIDPattern)
	}
	return nil
}

// agentToSchemaDoc serializes the Agent using the on-wire kebab-case keys the
// JSON Schema expects. The schema is strict (additionalProperties: false) so
// this builder must not emit fields the schema does not declare.
func agentToSchemaDoc(a *adept.Agent) (any, error) {
	doc := map[string]any{
		"id":          a.ID,
		"description": a.Description,
	}
	if a.Mode != "" {
		doc["mode"] = string(a.Mode)
	}
	if len(a.Tools) > 0 {
		doc["tools"] = toAnySlice(a.Tools)
	}
	if len(a.DisallowedTools) > 0 {
		doc["disallowed-tools"] = toAnySlice(a.DisallowedTools)
	}
	if a.Model != "" {
		doc["model"] = a.Model
	}
	if len(a.Targets) > 0 {
		doc["targets"] = toAnySlice(a.Targets)
	}
	if len(a.Tags) > 0 {
		doc["tags"] = toAnySlice(a.Tags)
	}
	if len(a.Metadata) > 0 {
		md := map[string]any{}
		for k, v := range a.Metadata {
			md[k] = v
		}
		doc["metadata"] = md
	}
	if len(a.Harness) > 0 {
		hd := map[string]any{}
		for harness, override := range a.Harness {
			ov := map[string]any{}
			for k, v := range override {
				ov[k] = v
			}
			hd[harness] = ov
		}
		doc["harness"] = hd
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	return jsonschema.UnmarshalJSON(bytes.NewReader(raw))
}
