package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/itaywol/adeptability/internal/adapter"
	"github.com/itaywol/adeptability/internal/budget"
	"github.com/itaywol/adeptability/internal/canonical"
	"github.com/itaywol/adeptability/internal/config"
	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/internal/git"
	"github.com/itaywol/adeptability/internal/harness"
	"github.com/itaywol/adeptability/internal/hash"
	"github.com/itaywol/adeptability/internal/library"
	"github.com/itaywol/adeptability/internal/log"
	"github.com/itaywol/adeptability/internal/org"
	"github.com/itaywol/adeptability/internal/project"
	"github.com/itaywol/adeptability/internal/llm"
	"github.com/itaywol/adeptability/internal/llm/anthropic"
	"github.com/itaywol/adeptability/internal/llm/ollama"
	gh "github.com/itaywol/adeptability/internal/registry/github"
	"github.com/itaywol/adeptability/internal/registry/skillssh"
	"github.com/itaywol/adeptability/internal/render/claude"
	"github.com/itaywol/adeptability/internal/render/codex"
	"github.com/itaywol/adeptability/internal/render/copilot"
	"github.com/itaywol/adeptability/internal/render/cursor"
	"github.com/itaywol/adeptability/internal/render/opencode"
	"github.com/itaywol/adeptability/internal/status"
	"github.com/itaywol/adeptability/pkg/adept"
)

// Deps is the dependency container.
//
// Pure constructor injection — every collaborator is an interface, every
// field is read-only after construction. Commands receive a *Deps and
// resolve project/library roots per invocation so a single Deps instance
// services the whole CLI run.
type Deps struct {
	Flags *GlobalFlags
	Build BuildInfo

	// Core
	Parser    canonical.Parser
	Validator canonical.Validator
	Loader    canonical.Loader
	Hasher    hash.Hasher
	Config    config.Store
	Status    status.Resolver
	Writer    fsutil.Writer
	Linker    fsutil.Linker
	Git       git.Client
	Log       log.Logger

	// Orchestration
	Registry      harness.Registry
	Orchestrator  harness.Orchestrator
	AdapterLoader adapter.Loader

	// Library remote (used by init --from)
	OrgParser org.Parser

	// External skill registry clients (skill install/search/info)
	GitHub   gh.Client
	SkillsSh skillssh.Client

	// LLM provider resolver — used by skill check / install scan gates.
	// Empty when no provider is configured for this project.
	LLMRegistry llm.Registry
}

// LLMProvider returns the provider configured in the project, or nil
// when none is set. The caller is responsible for checking Available()
// before issuing an Evaluate.
func (d *Deps) LLMProvider() llm.Provider {
	if d.LLMRegistry == nil {
		return nil
	}
	p, err := d.Project()
	if err != nil {
		return nil
	}
	cfg, err := p.Config()
	if err != nil || cfg.LLM == nil || cfg.LLM.Provider == "" {
		return nil
	}
	prov, err := d.LLMRegistry.Get(cfg.LLM.Provider)
	if err != nil {
		return nil
	}
	// Per-config endpoint/model overrides are applied by re-constructing
	// the provider with the user values when they differ from defaults.
	switch cfg.LLM.Provider {
	case "anthropic":
		return anthropic.New(nil, cfg.LLM.Endpoint, cfg.LLM.Model)
	case "ollama":
		return ollama.New(nil, cfg.LLM.Endpoint, cfg.LLM.Model)
	}
	return prov
}

// NewDeps wires concrete implementations. This is the only place where
// concrete types are mentioned — every downstream package consumes
// interfaces.
func NewDeps(gf *GlobalFlags, b BuildInfo) (*Deps, error) {
	parser := canonical.NewParser()
	validator, err := canonical.NewValidator()
	if err != nil {
		return nil, fmt.Errorf("build canonical validator: %w", err)
	}

	writer := fsutil.NewWriter()
	linker := fsutil.NewLinker(writer)
	logger := log.NewLogger(log.Level(gf.LogLevel), gf.JSON, os.Stderr)

	reg := harness.NewRegistry()
	if err := registerBuiltinAdapters(reg, writer, linker); err != nil {
		return nil, fmt.Errorf("register built-in adapters: %w", err)
	}

	adapterValidator, err := adapter.NewSchemaValidator()
	if err != nil {
		return nil, fmt.Errorf("build adapter validator: %w", err)
	}
	adapterLoader := adapter.NewLoader(adapterValidator, writer, linker)

	orgParser, err := org.NewParser()
	if err != nil {
		return nil, fmt.Errorf("build org parser: %w", err)
	}

	return &Deps{
		Flags:         gf,
		Build:         b,
		Parser:        parser,
		Validator:     validator,
		Loader:        canonical.NewLoader(parser, validator),
		Hasher:        hash.NewHasher(),
		Config:        config.NewStore(writer.AtomicWrite),
		Status:        status.NewResolver(),
		Writer:        writer,
		Linker:        linker,
		Git:           git.NewClient(git.NewExecRunner("git")),
		Log:           logger,
		Registry:      reg,
		Orchestrator:  harness.NewOrchestrator(reg, parser, writer, linker, logger),
		AdapterLoader: adapterLoader,
		OrgParser:     orgParser,
		GitHub:        gh.New(nil),
		SkillsSh:      skillssh.New(nil, ""),
		LLMRegistry:   llm.NewRegistry(anthropic.New(nil, "", ""), ollama.New(nil, "", "")),
	}, nil
}

// registerBuiltinAdapters wires every built-in harness into the registry.
// External harnesses are loaded later from $ADEPT_LIBRARY/adapters/ via
// AdapterLoader, then registered the same way through Deps.LoadUserAdapters().
func registerBuiltinAdapters(reg harness.Registry, w fsutil.Writer, l fsutil.Linker) error {
	pack := budget.NewPacker()
	adapters := []adept.HarnessAdapter{
		claude.NewAdapter(claude.New(), w, l),
		cursor.NewAdapter(cursor.New(), w, l),
		opencode.NewAdapter(opencode.New(), w, l),
		codex.NewAdapter(codex.New(), pack, w),
		copilot.NewAdapter(copilot.New(), pack, w),
	}
	for _, a := range adapters {
		if err := reg.Register(a); err != nil {
			return fmt.Errorf("register %s: %w", a.Spec().ID, err)
		}
	}
	return nil
}

// LoadUserAdapters reads adapter YAMLs from <library>/adapters/ and registers them.
// Best-effort: missing directory is not an error.
func (d *Deps) LoadUserAdapters() error {
	libRoot, err := d.ResolveLibraryRoot()
	if err != nil {
		return err
	}
	dir := filepath.Join(libRoot, adept.AdaptersDir)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	}
	adapters, err := d.AdapterLoader.LoadDir(dir)
	if err != nil {
		return fmt.Errorf("load user adapters: %w", err)
	}
	for _, a := range adapters {
		if err := d.Registry.Register(a); err != nil {
			return fmt.Errorf("register user adapter %s: %w", a.Spec().ID, err)
		}
	}
	return nil
}

// Library returns a Library bound to the resolved root.
func (d *Deps) Library() (library.Library, error) {
	root, err := d.ResolveLibraryRoot()
	if err != nil {
		return nil, err
	}
	return library.New(root, d.Parser, d.Hasher, d.Writer), nil
}

// Project returns a Project bound to the resolved root.
func (d *Deps) Project() (project.Project, error) {
	root, err := d.ResolveProjectRoot()
	if err != nil {
		return nil, err
	}
	return project.New(root, d.Parser, d.Hasher, d.Config, d.Writer), nil
}

// ResolveLibraryRoot returns --library, then $ADEPT_LIBRARY, then $HOME/.adeptability.
func (d *Deps) ResolveLibraryRoot() (string, error) {
	if d.Flags != nil && d.Flags.LibraryDir != "" {
		return d.Flags.LibraryDir, nil
	}
	if env := os.Getenv(adept.LibraryEnvVar); env != "" {
		return env, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve $HOME: %w", err)
	}
	return filepath.Join(home, adept.DefaultLibraryDir), nil
}

// ResolveLibrariesRoot returns the directory under which named libraries
// live: <ResolveLibraryRoot>/libs/. Each library has its own subdirectory
// (e.g. libs/default/skills/<id>/, libs/org-shared/skills/<id>/).
func (d *Deps) ResolveLibrariesRoot() (string, error) {
	root, err := d.ResolveLibraryRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "libs"), nil
}

// ResolveProjectRoot returns --project or cwd.
func (d *Deps) ResolveProjectRoot() (string, error) {
	if d.Flags != nil && d.Flags.ProjectDir != "" {
		return d.Flags.ProjectDir, nil
	}
	return os.Getwd()
}
