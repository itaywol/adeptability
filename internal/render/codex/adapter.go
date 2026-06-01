package codex

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/itaywol/adeptability/internal/budget"
	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/pkg/adept"
)

// FileReader exposes the minimal read surface needed by Detect/Validate.
// The Writer interface in internal/fsutil is write-only; reads use this
// dedicated seam so tests can inject in-memory fakes without depending on
// any disk activity.
type FileReader interface {
	// ReadFile returns the bytes at path or fs.ErrNotExist if absent.
	ReadFile(path string) ([]byte, error)
	// Exists reports whether path exists on disk.
	Exists(path string) (bool, error)
}

// Adapter implements adept.HarnessAdapter for Codex.
//
// Adapter is the Aggregate orchestrator; it does NOT itself write to disk.
// The outer apply/sync command takes the []RenderOutput we return and hands
// it to the fsutil.Writer at materialization time. The Writer is wired here
// so future variants of the adapter that perform their own writes can use it;
// reads (Detect/Validate) go through FileReader.
type Adapter struct {
	r *Renderer
	p budget.Packer
	w fsutil.Writer
	f FileReader
}

// NewAdapter builds a Codex adapter. All collaborators are injected; no globals.
// A default os-backed FileReader is used.
func NewAdapter(r *Renderer, p budget.Packer, w fsutil.Writer) *Adapter {
	return &Adapter{r: r, p: p, w: w, f: osReader{}}
}

// NewAdapterWithReader is a test seam allowing a custom FileReader.
func NewAdapterWithReader(r *Renderer, p budget.Packer, w fsutil.Writer, f FileReader) *Adapter {
	if f == nil {
		f = osReader{}
	}
	return &Adapter{r: r, p: p, w: w, f: f}
}

// osReader is the default FileReader, backed by the os package.
type osReader struct{}

func (osReader) ReadFile(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (osReader) Exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// Compile-time interface check.
var _ adept.HarnessAdapter = (*Adapter)(nil)

// Spec returns the static description used by the harness registry.
func (a *Adapter) Spec() adept.HarnessSpec {
	return adept.HarnessSpec{
		ID:          "codex",
		Name:        "Codex",
		Kind:        adept.KindAggregatorSingle,
		OutputPath:  OutputFile,
		SizeBudgetB: SizeBudgetB,
		NeedsDir:    false,
		BaseDir:     "",
	}
}

// Renderer returns the per-skill fragment renderer.
func (a *Adapter) Renderer() adept.Renderer { return a.r }

// Aggregate concatenates per-skill fragments into a single AGENTS.md output.
// The budgetB parameter overrides the spec default when non-zero; pass 0 to
// use the default (32 KiB).
func (a *Adapter) Aggregate(_ context.Context, parts []adept.RenderOutput, budgetB int) ([]adept.RenderOutput, error) {
	if budgetB == 0 {
		budgetB = SizeBudgetB
	}
	out, err := aggregate(a.p, parts, budgetB)
	if err != nil {
		return nil, err
	}
	return []adept.RenderOutput{out}, nil
}

// Detect returns true if either a project AGENTS.md exists or a .codex/
// directory is present at the project root.
func (a *Adapter) Detect(projectRoot string) (bool, error) {
	if projectRoot == "" {
		return false, fmt.Errorf("codex detect: empty project root")
	}
	if a.f == nil {
		return false, fmt.Errorf("codex detect: nil reader")
	}
	agents := filepath.Join(projectRoot, OutputFile)
	if ok, err := a.f.Exists(agents); err != nil {
		return false, fmt.Errorf("codex detect: stat %s: %w", agents, err)
	} else if ok {
		return true, nil
	}
	codexDir := filepath.Join(projectRoot, ".codex")
	if ok, err := a.f.Exists(codexDir); err != nil {
		return false, fmt.Errorf("codex detect: stat %s: %w", codexDir, err)
	} else if ok {
		return true, nil
	}
	return false, nil
}

// Validate compares expected aggregated output to disk and reports drift.
// The DriftReport carries a single entry keyed by "codex" — the whole harness
// is either Synced, Drifted, or Missing.
func (a *Adapter) Validate(projectRoot string, expected []adept.RenderOutput) (adept.DriftReport, error) {
	rep := adept.DriftReport{}
	if projectRoot == "" {
		return rep, fmt.Errorf("codex validate: empty project root")
	}
	if a.f == nil {
		return rep, fmt.Errorf("codex validate: nil reader")
	}
	if len(expected) == 0 {
		// Nothing expected — Codex is "synced" vacuously.
		rep.Synced = append(rep.Synced, "codex")
		return rep, nil
	}
	// Codex emits exactly one RenderOutput per aggregate call.
	target := expected[0]
	abs := filepath.Join(projectRoot, target.Path)

	got, err := a.f.ReadFile(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			rep.Missing = append(rep.Missing, "codex")
			return rep, nil
		}
		return rep, fmt.Errorf("codex validate: read %s: %w", abs, err)
	}
	if bytes.Equal(got, target.Bytes) {
		rep.Synced = append(rep.Synced, "codex")
	} else {
		rep.Drifted = append(rep.Drifted, "codex")
	}
	return rep, nil
}
