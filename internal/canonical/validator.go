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

// Validator checks a parsed Skill against the canonical JSON Schema.
type Validator interface {
	Validate(s *adept.Skill) error
}

type schemaValidator struct {
	schema    *jsonschema.Schema
	idPattern *regexp.Regexp
}

// idPatternRE is the compiled form of the compile-time constant
// adept.SkillIDPattern. MustCompile at package init is correct because the
// pattern is a constant and any compilation failure is a programming error.
var idPatternRE = regexp.MustCompile(adept.SkillIDPattern)

// NewValidator compiles the embedded skill.schema.json once. Returns an error
// if the embedded schema is malformed (should never happen at runtime).
func NewValidator() (Validator, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(adeptschema.SkillSchema))
	if err != nil {
		return nil, fmt.Errorf("validator: load embedded schema: %w", err)
	}
	c := jsonschema.NewCompiler()
	const schemaURL = "memory://skill.schema.json"
	if err := c.AddResource(schemaURL, doc); err != nil {
		return nil, fmt.Errorf("validator: add embedded schema: %w", err)
	}
	sch, err := c.Compile(schemaURL)
	if err != nil {
		return nil, fmt.Errorf("validator: compile embedded schema: %w", err)
	}
	return &schemaValidator{schema: sch, idPattern: idPatternRE}, nil
}

func (v *schemaValidator) Validate(s *adept.Skill) error {
	if s == nil {
		return fmt.Errorf("validate: %w: nil skill", adept.ErrSkillInvalid)
	}
	doc, err := skillToSchemaDoc(s)
	if err != nil {
		return fmt.Errorf("validate: %w: %w", adept.ErrSkillInvalid, err)
	}
	if err := v.schema.Validate(doc); err != nil {
		return fmt.Errorf("validate: %w: %w", adept.ErrSkillInvalid, err)
	}
	// Cross-check the id pattern explicitly: schemas may diverge from the
	// const exposed by pkg/adept.SkillIDPattern. This is defense in depth.
	if !v.idPattern.MatchString(s.ID) {
		return fmt.Errorf("validate: %w: id %q does not match %s", adept.ErrSkillInvalid, s.ID, adept.SkillIDPattern)
	}
	return nil
}

// skillToSchemaDoc serializes the Skill using the on-wire kebab-case keys the
// JSON Schema expects (e.g. "allowed-tools"). The schema is strict
// (additionalProperties: false) so this builder must not emit fields the
// schema does not declare.
func skillToSchemaDoc(s *adept.Skill) (any, error) {
	doc := map[string]any{
		"id":          s.ID,
		"description": s.Description,
	}
	if s.Activation != "" {
		doc["activation"] = string(s.Activation)
	}
	if len(s.Globs) > 0 {
		doc["globs"] = toAnySlice(s.Globs)
	}
	if len(s.AllowedTools) > 0 {
		doc["allowed-tools"] = toAnySlice(s.AllowedTools)
	}
	if len(s.Targets) > 0 {
		doc["targets"] = toAnySlice(s.Targets)
	}
	if len(s.Tags) > 0 {
		doc["tags"] = toAnySlice(s.Tags)
	}
	if len(s.Metadata) > 0 {
		md := map[string]any{}
		for k, v := range s.Metadata {
			md[k] = v
		}
		doc["metadata"] = md
	}
	if s.Model != "" {
		doc["model"] = s.Model
	}
	if len(s.Harness) > 0 {
		hd := map[string]any{}
		for harness, override := range s.Harness {
			ov := map[string]any{}
			for k, v := range override {
				ov[k] = v
			}
			hd[harness] = ov
		}
		doc["harness"] = hd
	}
	// Round-trip through JSON to convert numeric types into json.Number to
	// match jsonschema/v6 expectations.
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	return jsonschema.UnmarshalJSON(bytes.NewReader(raw))
}

func toAnySlice(xs []string) []any {
	out := make([]any, len(xs))
	for i, x := range xs {
		out[i] = x
	}
	return out
}
