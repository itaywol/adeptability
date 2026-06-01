package codex

import (
	"fmt"
	"sort"
	"strings"

	"github.com/itaywol/adeptability/internal/budget"
	"github.com/itaywol/adeptability/pkg/adept"
)

// aggregate concatenates per-skill fragments into one AGENTS.md document,
// packs them under budgetB, and prepends a truncation manifest when the
// packer dropped one or more parts.
//
// Inputs are merged deterministically. Output ordering inside the document
// is by (SkillID asc) regardless of pack order, so re-runs are byte-stable.
func aggregate(p budget.Packer, parts []adept.RenderOutput, budgetB int) (adept.RenderOutput, error) {
	if budgetB < 0 {
		return adept.RenderOutput{}, fmt.Errorf("codex aggregate: negative budget %d", budgetB)
	}

	bparts := make([]budget.Part, 0, len(parts))
	for _, pt := range parts {
		// Defensive: skip any non-AGENTS.md output that somehow slipped in.
		if pt.Path != OutputFile {
			continue
		}
		bparts = append(bparts, budget.Part{
			SkillID:  pt.SkillID,
			Bytes:    pt.Bytes,
			Priority: 0,
		})
	}

	res, err := p.Pack(bparts, budgetB, ManifestOverheadB)
	if err != nil {
		return adept.RenderOutput{}, fmt.Errorf("codex aggregate: pack: %w", err)
	}

	// Sort kept by SkillID for deterministic doc order independent of pack order.
	keptCopy := make([]budget.Part, len(res.Kept))
	copy(keptCopy, res.Kept)
	sort.SliceStable(keptCopy, func(i, j int) bool {
		return keptCopy[i].SkillID < keptCopy[j].SkillID
	})

	var body strings.Builder
	if len(res.Dropped) > 0 {
		body.WriteString(buildTruncationManifest(res.Dropped, budgetB))
		body.WriteByte('\n')
	}
	for i, kp := range keptCopy {
		if i > 0 {
			body.WriteByte('\n')
		}
		body.Write(kp.Bytes)
	}

	return adept.RenderOutput{
		Path:  OutputFile,
		Bytes: []byte(body.String()),
		Mode:  0o644,
	}, nil
}

// buildTruncationManifest emits the leading HTML comment that documents which
// skills were dropped due to the budget constraint.
func buildTruncationManifest(dropped []budget.Part, budgetB int) string {
	ids := make([]string, len(dropped))
	for i, d := range dropped {
		ids[i] = d.SkillID
	}
	sort.Strings(ids)
	return fmt.Sprintf(
		"<!-- adeptability: omitted %d skill(s) due to %dKiB budget. Run `adept apply --diet` to fit. Dropped: %s -->",
		len(dropped),
		budgetB/1024,
		strings.Join(ids, ","),
	)
}
