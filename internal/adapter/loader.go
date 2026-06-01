// Package adapter loads YAML-defined harness adapters at startup and wraps
// them into adept.HarnessAdapter implementations. Built-in harnesses are
// declared in code elsewhere; loaded adapters are registered alongside them.
package adapter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"

	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/itaywol/adeptability/pkg/adeptschema"
)

// Spec mirrors the adapter.schema.json document. Field names match the YAML
// keys (kebab-case is mapped via tags).
type Spec struct {
	ID          string            `yaml:"id"`
	Name        string            `yaml:"name"`
	Kind        adept.HarnessKind `yaml:"kind"`
	Output      string            `yaml:"output"`
	BaseDir     string            `yaml:"base-dir"`
	NeedsDir    bool              `yaml:"needs-directory"`
	Budget      int               `yaml:"size-budget-bytes"`
	Frontmatter Frontmatter       `yaml:"frontmatter"`
	Body        Body              `yaml:"body"`
	Detect      []string          `yaml:"detect"`
	Import      Import            `yaml:"import"`
}

// Import carries optional reverse-rendering hints. Auto-derived from forward
// config when fields are empty.
type Import struct {
	// Rename inverts Frontmatter.Rename. Keys are harness-side frontmatter
	// keys; values are canonical skill field names ("description", "globs",
	// ...). When non-empty, takes precedence over the auto-derived inverse.
	Rename map[string]string `yaml:"rename"`
}

// Frontmatter rules: include/rename/constants drive how a Skill's metadata is
// projected into the harness-specific frontmatter block.
type Frontmatter struct {
	Include   []string          `yaml:"include"`
	Rename    map[string]string `yaml:"rename"`
	Constants map[string]string `yaml:"constants"`
}

// Body rules: prefix/suffix wrap the skill body; replace runs regex
// substitutions in declaration order.
type Body struct {
	Prefix  string    `yaml:"prefix"`
	Suffix  string    `yaml:"suffix"`
	Replace []Replace `yaml:"replace"`
}

// Replace is a single regex substitution.
type Replace struct {
	Regex string `yaml:"regex"`
	With  string `yaml:"with"`
}

// AdapterValidator validates raw bytes against the adapter JSON Schema.
type AdapterValidator interface {
	Validate(data []byte) error
}

// Loader loads adapters from disk.
type Loader interface {
	LoadDir(dir string) ([]adept.HarnessAdapter, error)
	LoadFile(path string) (adept.HarnessAdapter, error)
}

// NewLoader returns a Loader that validates each file against the embedded
// adapter schema and constructs synthetic adapters from the parsed Spec.
func NewLoader(validator AdapterValidator, w fsutil.Writer, l fsutil.Linker) Loader {
	return &loader{validator: validator, writer: w, linker: l}
}

// NewSchemaValidator compiles the embedded adapter schema and returns an
// AdapterValidator. Most callers should use this rather than implementing
// their own.
func NewSchemaValidator() (AdapterValidator, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(adeptschema.AdapterSchema))
	if err != nil {
		return nil, fmt.Errorf("adapter validator: load schema: %w", err)
	}
	c := jsonschema.NewCompiler()
	const schemaURL = "memory://adapter.schema.json"
	if err := c.AddResource(schemaURL, doc); err != nil {
		return nil, fmt.Errorf("adapter validator: add schema: %w", err)
	}
	sch, err := c.Compile(schemaURL)
	if err != nil {
		return nil, fmt.Errorf("adapter validator: compile: %w", err)
	}
	return &schemaValidator{schema: sch}, nil
}

type schemaValidator struct {
	schema *jsonschema.Schema
}

func (v *schemaValidator) Validate(data []byte) error {
	// YAML -> generic -> JSON -> jsonschema document.
	var generic any
	if err := yaml.Unmarshal(data, &generic); err != nil {
		return fmt.Errorf("adapter validate: yaml: %w", err)
	}
	normalized, err := normalizeYAMLNode(generic)
	if err != nil {
		return fmt.Errorf("adapter validate: normalize: %w", err)
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return fmt.Errorf("adapter validate: marshal: %w", err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("adapter validate: parse json: %w", err)
	}
	if err := v.schema.Validate(doc); err != nil {
		return fmt.Errorf("adapter validate: %w: %v", adept.ErrAdapterInvalid, err)
	}
	return nil
}

// normalizeYAMLNode converts map[interface{}]interface{} into
// map[string]interface{} so encoding/json can marshal cleanly.
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

type loader struct {
	validator AdapterValidator
	writer    fsutil.Writer
	linker    fsutil.Linker
}

func (l *loader) LoadDir(dir string) ([]adept.HarnessAdapter, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("adapter load dir %q: %w", dir, err)
	}
	out := make([]adept.HarnessAdapter, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !isYAMLName(name) {
			continue
		}
		a, err := l.LoadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Spec().ID < out[j].Spec().ID })
	return out, nil
}

func (l *loader) LoadFile(path string) (adept.HarnessAdapter, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("adapter load %q: %w", path, err)
	}
	if l.validator != nil {
		if err := l.validator.Validate(data); err != nil {
			return nil, fmt.Errorf("adapter load %q: %w", path, err)
		}
	}
	var spec Spec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("adapter load %q: yaml: %w", path, err)
	}
	a, err := NewSynthetic(spec)
	if err != nil {
		return nil, fmt.Errorf("adapter load %q: %w", path, err)
	}
	return a, nil
}

func isYAMLName(n string) bool {
	switch filepath.Ext(n) {
	case ".yaml", ".yml":
		return true
	}
	return false
}
