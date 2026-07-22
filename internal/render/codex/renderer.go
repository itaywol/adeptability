// Package codex implements the Codex (AGENTS.md) aggregator harness adapter.
//
// Codex consumes a single project-root AGENTS.md document. Renderer.Render
// produces a per-skill "fragment" — a section wrapped in begin/end markers.
// Adapter.Aggregate concatenates the fragments into one AGENTS.md, packs
// them under the 32 KiB project_doc_max_bytes budget, and prepends a
// truncation manifest when parts were dropped.
package codex

import (
	"context"
	"fmt"
	"strings"

	"github.com/itaywol/adeptability/internal/render/common"
	"github.com/itaywol/adeptability/pkg/adept"
)

// OutputFile is the on-disk file Codex reads from a project.
const OutputFile = "AGENTS.md"

// SizeBudgetB is Codex's project_doc_max_bytes default.
const SizeBudgetB = 32 * 1024

// ManifestOverheadB reserves bytes for the truncation manifest comment when
// the packer needs to drop skills. Keep generous enough for plausible ID lists.
const ManifestOverheadB = 512

// Renderer produces a single-skill fragment for an aggregated AGENTS.md.
//
// The fragment is wrapped in begin/end markers so the aggregator can
// concatenate parts deterministically and so future Detect logic can locate
// adept-owned sections.
type Renderer struct{}

// New returns a Codex renderer with default behavior.
func New() *Renderer { return &Renderer{} }

// Compile-time interface check.
var _ adept.Renderer = (*Renderer)(nil)

// Render emits the per-skill fragment. The Path is the aggregated file name,
// not a per-skill output — the Adapter merges fragments by Path.
func (r *Renderer) Render(_ context.Context, in adept.RenderInput) (adept.RenderOutput, error) {
	if in.Skill == nil {
		return adept.RenderOutput{}, fmt.Errorf("codex render: %w: nil skill", adept.ErrSkillInvalid)
	}
	s := in.Skill
	if s.ID == "" {
		return adept.RenderOutput{}, fmt.Errorf("codex render: %w: skill missing id", adept.ErrSkillInvalid)
	}

	hash := common.ShortSkillHash(s)
	frag := buildFragment(s, hash)
	// Honor the effective spec's output path (global scope swaps this to
	// ".codex/AGENTS.md"); fall back to the default when callers construct a
	// bare RenderInput without a Harness.
	outPath := in.Harness.OutputPath
	if outPath == "" {
		outPath = OutputFile
	}
	out := adept.RenderOutput{
		Path:      outPath,
		Bytes:     []byte(frag),
		Mode:      0o644,
		SkillID:   s.ID,
		SkillHash: hash,
	}
	for _, f := range s.Files {
		out.Warnings = append(out.Warnings,
			fmt.Sprintf("codex: dropped sidecar %q (codex aggregates skills into a single AGENTS.md)", f.RelPath))
	}
	return out, nil
}

// buildFragment emits the marker-wrapped section for a skill.
//
//	<!-- adeptability:begin id=<id> hash=<8-hex> -->
//	## <description as heading>
//	<body>
//	<!-- adeptability:end id=<id> -->
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
