package copilot

import (
	"context"
	"fmt"
	"strings"

	"github.com/itaywol/adeptability/internal/render/common"
	"github.com/itaywol/adeptability/pkg/adept"
)

// SizeBudgetB is the per-bucket size limit (64 KiB). The aggregator enforces
// it via budget.Packer: parts that do not fit a bucket are dropped (Pack
// discards res.Dropped) and the orchestrator reports them as DroppedSkillIDs.
// Unlike Codex, no in-file truncation manifest is emitted for Copilot.
const SizeBudgetB = 64 * 1024

// metaSidecarName is the in-memory-only sidecar used to convey the bucket
// applyTo value from Renderer to Aggregate. It is never materialized on disk;
// the aggregator strips it after reading.
const metaSidecarName = ".adept-bucket-meta"

// Renderer emits a per-skill fragment plus a metadata sidecar carrying the
// bucket's applyTo value so the aggregator can write correct frontmatter.
//
// Skills not eligible for Copilot (activation=agent, activation=manual) yield
// an empty RenderOutput (zero Path, zero Bytes) so the aggregator can skip
// them without erroring.
type Renderer struct {
	b Bucketer
}

// New constructs a Copilot renderer with the default Bucketer.
func New() *Renderer { return &Renderer{b: NewBucketer()} }

// NewWithBucketer injects a Bucketer for tests or alternate strategies.
func NewWithBucketer(b Bucketer) *Renderer { return &Renderer{b: b} }

// Compile-time interface check.
var _ adept.Renderer = (*Renderer)(nil)

// Render returns a fragment whose Path is the bucket file path. The Adapter
// groups by Path to materialize per-bucket files.
//
// For non-eligible skills, Render returns an empty RenderOutput (no error).
// Callers MUST check out.Path == "" before forwarding to the aggregator.
func (r *Renderer) Render(_ context.Context, in adept.RenderInput) (adept.RenderOutput, error) {
	if in.Skill == nil {
		return adept.RenderOutput{}, fmt.Errorf("copilot render: %w: nil skill", adept.ErrSkillInvalid)
	}
	s := in.Skill
	if s.ID == "" {
		return adept.RenderOutput{}, fmt.Errorf("copilot render: %w: skill missing id", adept.ErrSkillInvalid)
	}
	spec, ok := r.b.KeyFor(s)
	if !ok {
		// Non-eligible skill: zero-value output signals "skip".
		return adept.RenderOutput{}, nil
	}

	hash := common.ShortSkillHash(s)
	frag := buildFragment(s, hash)
	return adept.RenderOutput{
		Path:      spec.Path,
		Bytes:     []byte(frag),
		Mode:      0o644,
		SkillID:   s.ID,
		SkillHash: hash,
		Sidecars: []adept.SideFile{{
			RelPath: metaSidecarName,
			Bytes:   []byte(spec.ApplyTo),
			Mode:    0o600,
		}},
	}, nil
}

// buildFragment emits the marker-wrapped section. Bucket frontmatter is
// prepended later by the aggregator (one frontmatter block per file).
func buildFragment(s *adept.Skill, hash string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("<!-- adeptability:begin id=%s hash=%s -->\n", s.ID, hash))
	heading := strings.TrimSpace(s.Description)
	if heading == "" {
		heading = s.ID
	}
	b.WriteString("## ")
	b.WriteString(heading)
	b.WriteByte('\n')
	body := strings.TrimRight(s.Body, "\n")
	if body != "" {
		b.WriteByte('\n')
		b.WriteString(body)
		b.WriteByte('\n')
	}
	b.WriteString(fmt.Sprintf("<!-- adeptability:end id=%s -->\n", s.ID))
	return b.String()
}
