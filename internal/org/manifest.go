// Package org reads and validates org manifests (org.yaml) that pin the set
// of skills a project must adopt. Manifests live either in a remote library
// (TODO: v0.2) or in a local file; in v0.1 only the file client is shipped.
package org

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"

	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/itaywol/adeptability/pkg/adeptschema"
)

// SkillRef pins a single skill at an optional minimum version.
type SkillRef struct {
	ID         string
	MinVersion int
}

// Manifest is the parsed, validated org.yaml document.
type Manifest struct {
	Version  int
	Name     string
	Required []SkillRef
	Optional []SkillRef
}

// Parser parses raw org.yaml bytes.
type Parser interface {
	Parse(data []byte) (*Manifest, error)
}

// NewParser returns a Parser that validates against the embedded
// org.schema.json before constructing the Manifest.
func NewParser() (Parser, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(adeptschema.OrgSchema))
	if err != nil {
		return nil, fmt.Errorf("org parser: load schema: %w", err)
	}
	c := jsonschema.NewCompiler()
	const schemaURL = "memory://org.schema.json"
	if err := c.AddResource(schemaURL, doc); err != nil {
		return nil, fmt.Errorf("org parser: add schema: %w", err)
	}
	sch, err := c.Compile(schemaURL)
	if err != nil {
		return nil, fmt.Errorf("org parser: compile: %w", err)
	}
	return &parser{schema: sch}, nil
}

type parser struct {
	schema *jsonschema.Schema
}

func (p *parser) Parse(data []byte) (*Manifest, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("org parse: empty document")
	}
	var generic any
	if err := yaml.Unmarshal(data, &generic); err != nil {
		return nil, fmt.Errorf("org parse: yaml: %w", err)
	}
	normalized, err := normalizeYAMLNode(generic)
	if err != nil {
		return nil, fmt.Errorf("org parse: normalize: %w", err)
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("org parse: marshal: %w", err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("org parse: parse json: %w", err)
	}
	if err := p.schema.Validate(doc); err != nil {
		return nil, fmt.Errorf("org parse: %w: %v", adept.ErrAdapterInvalid, err)
	}
	var raw2 rawManifest
	if err := yaml.Unmarshal(data, &raw2); err != nil {
		return nil, fmt.Errorf("org parse: decode: %w", err)
	}
	m := &Manifest{
		Version: raw2.Version,
		Name:    raw2.Name,
	}
	for _, r := range raw2.Skills.Required {
		m.Required = append(m.Required, SkillRef{ID: r.ID, MinVersion: r.MinVersion})
	}
	for _, r := range raw2.Skills.Optional {
		m.Optional = append(m.Optional, SkillRef{ID: r.ID, MinVersion: r.MinVersion})
	}
	return m, nil
}

type rawManifest struct {
	Version int       `yaml:"version"`
	Name    string    `yaml:"name"`
	Skills  rawSkills `yaml:"skills"`
}

type rawSkills struct {
	Required []rawSkillRef `yaml:"required"`
	Optional []rawSkillRef `yaml:"optional"`
}

type rawSkillRef struct {
	ID         string `yaml:"id"`
	MinVersion int    `yaml:"min-version"`
}

func normalizeYAMLNode(v any) (any, error) {
	switch t := v.(type) {
	case map[any]any:
		out := map[string]any{}
		for k, val := range t {
			ks, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("map key must be string, got %T", k)
			}
			nv, err := normalizeYAMLNode(val)
			if err != nil {
				return nil, err
			}
			out[ks] = nv
		}
		return out, nil
	case map[string]any:
		out := map[string]any{}
		for k, val := range t {
			nv, err := normalizeYAMLNode(val)
			if err != nil {
				return nil, err
			}
			out[k] = nv
		}
		return out, nil
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			nv, err := normalizeYAMLNode(item)
			if err != nil {
				return nil, err
			}
			out[i] = nv
		}
		return out, nil
	default:
		return t, nil
	}
}
