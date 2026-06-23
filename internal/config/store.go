// Package config reads and writes the project's .adeptability/config.json.
//
// The config holds ONLY project-level state — which harnesses are enabled,
// how to materialize them, where the org registry lives, and which user
// adapters are registered. Per-skill state (hashes) is NOT here: the
// filesystem itself is the source of truth (see internal/hash + status).
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/itaywol/adeptability/pkg/adeptschema"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// WriteFunc is the minimal write surface the store needs. It is injected so
// callers can plug in fsutil's tmp+rename atomic writer in production while
// tests can use a plain os.WriteFile.
type WriteFunc func(path string, data []byte, mode os.FileMode) error

// Store reads + writes the project config. Pure data layer; no policy.
type Store interface {
	// Read loads the config at path. Returns Empty() + nil when the file
	// does not exist (treat first-run as empty). Validates the on-disk JSON
	// against adeptschema.ConfigSchema. Returns an error wrapping
	// adept.ErrLockSchemaMismatch when the on-disk schema field is not 1.
	Read(path string) (*adept.Config, error)
	// Write atomically writes cfg to path, indent=2, trailing newline,
	// deterministic key order.
	Write(path string, cfg *adept.Config) error
	// Empty returns a zero-valued config with the current schema version.
	Empty() *adept.Config
	// SetMode sets the global materialization mode for every harness in this
	// project. Returns the same config pointer for fluent use.
	SetMode(cfg *adept.Config, m adept.HarnessMode) *adept.Config
	// GetMode returns the configured global mode, or adept.ModeSymlink when
	// unset.
	GetMode(cfg *adept.Config) adept.HarnessMode
}

// NewStore constructs a Store with an injected WriteFunc. If write is nil the
// store falls back to a plain os.WriteFile (intended for tests).
func NewStore(write WriteFunc) Store {
	if write == nil {
		write = defaultAtomicWrite
	}
	s, err := newStore(write)
	if err != nil {
		// The embedded schema is compiled at init time so this should be
		// unreachable in practice; panic mirrors the contract of NewValidator.
		panic(fmt.Sprintf("config: compile embedded schema: %v", err))
	}
	return s
}

type store struct {
	write  WriteFunc
	schema *jsonschema.Schema
}

func newStore(w WriteFunc) (*store, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(adeptschema.ConfigSchema))
	if err != nil {
		return nil, fmt.Errorf("load embedded config schema: %w", err)
	}
	c := jsonschema.NewCompiler()
	const url = "memory://config.schema.json"
	if err := c.AddResource(url, doc); err != nil {
		return nil, fmt.Errorf("add embedded config schema: %w", err)
	}
	sch, err := c.Compile(url)
	if err != nil {
		return nil, fmt.Errorf("compile embedded config schema: %w", err)
	}
	return &store{write: w, schema: sch}, nil
}

func (s *store) Empty() *adept.Config {
	return &adept.Config{
		Schema: adept.ConfigSchemaVersion,
	}
}

func (s *store) Read(path string) (*adept.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return s.Empty(), nil
		}
		return nil, fmt.Errorf("config read %s: %w", path, err)
	}

	// Schema check first — short-circuits on schema=2 so callers get a clean
	// sentinel before any structural validation runs.
	var probe struct {
		Schema int `json:"schema"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("config parse %s: %w", path, err)
	}
	if probe.Schema != adept.ConfigSchemaVersion {
		return nil, fmt.Errorf("config %s: %w: got %d, want %d",
			path, adept.ErrLockSchemaMismatch, probe.Schema, adept.ConfigSchemaVersion)
	}

	// JSON Schema validation rejects invalid harness modes, unknown fields,
	// etc. before we unmarshal into the typed struct.
	rawDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("config parse %s: %w", path, err)
	}
	if err := s.schema.Validate(rawDoc); err != nil {
		return nil, fmt.Errorf("config %s: invalid: %w", path, err)
	}

	cfg := &adept.Config{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("config parse %s: %w", path, err)
	}
	return cfg, nil
}

func (s *store) Write(path string, cfg *adept.Config) error {
	if cfg == nil {
		return fmt.Errorf("config write %s: nil config", path)
	}
	out := s.canonicalize(cfg)
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("config marshal: %w", err)
	}
	data = append(data, '\n')
	if err := s.write(path, data, 0o644); err != nil {
		return fmt.Errorf("config write %s: %w", path, err)
	}
	return nil
}

// canonicalForm mirrors adept.Config but pins the field order via struct
// declaration order. omitempty drops zero-valued collections so the on-disk
// JSON stays clean. We marshal this rather than the public Config so changes
// to the public field order do not bleed into the on-disk byte sequence.
type canonicalForm struct {
	Schema    int                `json:"schema"`
	Layout    string             `json:"layout,omitempty"`
	Harnesses []string           `json:"harnesses,omitempty"`
	Mode      adept.HarnessMode  `json:"mode,omitempty"`
	Libraries []adept.LibraryRef `json:"libraries,omitempty"`
	Scan      *adept.ScanConfig  `json:"scan,omitempty"`
	LLM       *adept.LLMConfig   `json:"llm,omitempty"`
}

func (s *store) canonicalize(cfg *adept.Config) canonicalForm {
	schema := cfg.Schema
	if schema == 0 {
		schema = adept.ConfigSchemaVersion
	}
	out := canonicalForm{Schema: schema, Layout: cfg.Layout, Mode: cfg.Mode}
	if len(cfg.Harnesses) > 0 {
		out.Harnesses = append(out.Harnesses, cfg.Harnesses...)
	}
	if len(cfg.Libraries) > 0 {
		out.Libraries = append(out.Libraries, cfg.Libraries...)
	}
	if cfg.Scan != nil {
		cp := *cfg.Scan
		out.Scan = &cp
	}
	if cfg.LLM != nil {
		cp := *cfg.LLM
		out.LLM = &cp
	}
	return out
}

func (s *store) SetMode(cfg *adept.Config, m adept.HarnessMode) *adept.Config {
	if cfg == nil {
		return nil
	}
	cfg.Mode = m
	return cfg
}

func (s *store) GetMode(cfg *adept.Config) adept.HarnessMode {
	if cfg == nil || cfg.Mode == "" {
		return adept.ModeSymlink
	}
	return cfg.Mode
}

func defaultAtomicWrite(path string, data []byte, mode os.FileMode) error {
	return os.WriteFile(path, data, mode)
}
