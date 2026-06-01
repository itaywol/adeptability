package copilot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/itaywol/adeptability/internal/budget"
	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/internal/render/common"
	"github.com/itaywol/adeptability/pkg/adept"
)

// FileReader exposes the minimal read surface needed by Detect/Validate.
// The Writer interface in internal/fsutil is write-only; reads use this
// dedicated seam so tests can inject in-memory fakes without depending on
// any disk activity.
type FileReader interface {
	ReadFile(path string) ([]byte, error)
	Exists(path string) (bool, error)
}

// Adapter implements adept.HarnessAdapter for GitHub Copilot.
type Adapter struct {
	r       *Renderer
	p       budget.Packer
	w       fsutil.Writer
	f       FileReader
	fmBuild common.FrontmatterBuilder
}

// NewAdapter builds a Copilot adapter. All collaborators are injected; no globals.
func NewAdapter(r *Renderer, p budget.Packer, w fsutil.Writer) *Adapter {
	return &Adapter{
		r:       r,
		p:       p,
		w:       w,
		f:       osReader{},
		fmBuild: common.NewFrontmatterBuilder(),
	}
}

// NewAdapterWithDeps is a test seam allowing custom frontmatter builder and reader.
func NewAdapterWithDeps(r *Renderer, p budget.Packer, w fsutil.Writer, fm common.FrontmatterBuilder, f FileReader) *Adapter {
	if fm == nil {
		fm = common.NewFrontmatterBuilder()
	}
	if f == nil {
		f = osReader{}
	}
	return &Adapter{r: r, p: p, w: w, f: f, fmBuild: fm}
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
		ID:          "copilot",
		Name:        "GitHub Copilot",
		Kind:        adept.KindAggregatorPerGlob,
		OutputPath:  BucketDir + "/{bucket}" + FileSuffix,
		SizeBudgetB: SizeBudgetB,
		NeedsDir:    false,
		BaseDir:     BucketDir,
	}
}

// Renderer returns the per-skill fragment renderer.
func (a *Adapter) Renderer() adept.Renderer { return a.r }

// Aggregate groups per-skill fragments by bucket and emits one RenderOutput
// per bucket. Pass budgetB=0 to use the spec default.
func (a *Adapter) Aggregate(_ context.Context, parts []adept.RenderOutput, budgetB int) ([]adept.RenderOutput, error) {
	if budgetB == 0 {
		budgetB = SizeBudgetB
	}
	return aggregate(a.p, a.fmBuild, parts, budgetB)
}

// Detect returns true if either .github/copilot-instructions.md exists or
// the .github/instructions/ directory is present.
func (a *Adapter) Detect(projectRoot string) (bool, error) {
	if projectRoot == "" {
		return false, fmt.Errorf("copilot detect: empty project root")
	}
	if a.f == nil {
		return false, fmt.Errorf("copilot detect: nil reader")
	}
	candidates := []string{
		filepath.Join(projectRoot, ".github", "copilot-instructions.md"),
		filepath.Join(projectRoot, ".github", "instructions"),
	}
	for _, c := range candidates {
		ok, err := a.f.Exists(c)
		if err != nil {
			return false, fmt.Errorf("copilot detect: stat %s: %w", c, err)
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

// Validate compares expected aggregated outputs to disk. Each bucket yields
// one entry in the DriftReport: Synced, Drifted (content mismatch), or
// Missing (file absent on disk).
func (a *Adapter) Validate(projectRoot string, expected []adept.RenderOutput) (adept.DriftReport, error) {
	rep := adept.DriftReport{}
	if projectRoot == "" {
		return rep, fmt.Errorf("copilot validate: empty project root")
	}
	if a.f == nil {
		return rep, fmt.Errorf("copilot validate: nil reader")
	}
	// Stable ordering so reports are byte-deterministic.
	sorted := make([]adept.RenderOutput, len(expected))
	copy(sorted, expected)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })

	for _, e := range sorted {
		key := bucketKeyFromPath(e.Path)
		abs := filepath.Join(projectRoot, e.Path)
		got, err := a.f.ReadFile(abs)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				rep.Missing = append(rep.Missing, string(key))
				continue
			}
			return rep, fmt.Errorf("copilot validate: read %s: %w", abs, err)
		}
		if bytes.Equal(got, e.Bytes) {
			rep.Synced = append(rep.Synced, string(key))
		} else {
			rep.Drifted = append(rep.Drifted, string(key))
		}
	}
	return rep, nil
}

// bucketKeyFromPath extracts the bucket key from a relative path like
// ".github/instructions/<key>.instructions.md".
func bucketKeyFromPath(path string) BucketKey {
	base := filepath.Base(path)
	key := base
	if i := len(base) - len(FileSuffix); i > 0 && base[i:] == FileSuffix {
		key = base[:i]
	}
	return BucketKey(key)
}
