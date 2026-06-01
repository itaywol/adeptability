package copilot

import (
	"fmt"
	"sort"
	"strings"

	"github.com/itaywol/adeptability/internal/budget"
	"github.com/itaywol/adeptability/internal/render/common"
	"github.com/itaywol/adeptability/pkg/adept"
)

// aggregate groups per-skill fragments by bucket path and emits one
// RenderOutput per bucket. Each output gets a YAML frontmatter block with
// `applyTo:` derived from the bucket meta sidecar emitted by Renderer.
// Output ordering across buckets is deterministic (alphabetical by Path),
// as is skill ordering within a bucket (alphabetical by SkillID).
func aggregate(
	p budget.Packer,
	fmBuilder common.FrontmatterBuilder,
	parts []adept.RenderOutput,
	budgetB int,
) ([]adept.RenderOutput, error) {
	if budgetB < 0 {
		return nil, fmt.Errorf("copilot aggregate: negative budget %d", budgetB)
	}

	type bucketAcc struct {
		applyTo string
		parts   []budget.Part
	}
	groups := make(map[string]*bucketAcc)

	for _, pt := range parts {
		if pt.Path == "" {
			// Non-eligible skill: renderer emitted a zero output.
			continue
		}
		acc, present := groups[pt.Path]
		if !present {
			applyTo := applyToFor(pt)
			if applyTo == "" {
				return nil, fmt.Errorf(
					"copilot aggregate: %w: missing applyTo for path %q",
					adept.ErrAdapterInvalid, pt.Path,
				)
			}
			acc = &bucketAcc{applyTo: applyTo}
			groups[pt.Path] = acc
		}
		// Defensive: if subsequent parts in the same bucket disagree on
		// applyTo we surface the conflict immediately.
		if got := applyToFor(pt); got != "" && got != acc.applyTo {
			return nil, fmt.Errorf(
				"copilot aggregate: %w: conflicting applyTo for path %q: %q vs %q",
				adept.ErrAdapterInvalid, pt.Path, acc.applyTo, got,
			)
		}
		acc.parts = append(acc.parts, budget.Part{
			SkillID:      pt.SkillID,
			SkillVersion: pt.SkillVersion,
			Bytes:        pt.Bytes,
			Priority:     0,
		})
	}

	if len(groups) == 0 {
		return nil, nil
	}

	// Deterministic file ordering by Path.
	paths := make([]string, 0, len(groups))
	for k := range groups {
		paths = append(paths, k)
	}
	sort.Strings(paths)

	outs := make([]adept.RenderOutput, 0, len(paths))
	for _, path := range paths {
		acc := groups[path]
		res, err := p.Pack(acc.parts, budgetB, 0)
		if err != nil {
			return nil, fmt.Errorf("copilot aggregate: pack %s: %w", path, err)
		}

		// Within a bucket, sort kept parts by SkillID for stable output.
		kept := make([]budget.Part, len(res.Kept))
		copy(kept, res.Kept)
		sort.SliceStable(kept, func(i, j int) bool { return kept[i].SkillID < kept[j].SkillID })

		fm, err := fmBuilder.Build([]common.Field{{
			Key:   "applyTo",
			Value: acc.applyTo,
			Quote: true,
		}})
		if err != nil {
			return nil, fmt.Errorf("copilot aggregate: frontmatter for %s: %w", path, err)
		}

		var body strings.Builder
		body.WriteString(fm)
		body.WriteByte('\n')
		for i, kp := range kept {
			if i > 0 {
				body.WriteByte('\n')
			}
			body.Write(kp.Bytes)
		}

		outs = append(outs, adept.RenderOutput{
			Path:  path,
			Bytes: []byte(body.String()),
			Mode:  0o644,
		})
	}
	return outs, nil
}

// applyToFor extracts the bucket meta applyTo string from a RenderOutput's
// in-memory sidecar. Returns "" if the sidecar is absent.
func applyToFor(o adept.RenderOutput) string {
	for _, sc := range o.Sidecars {
		if sc.RelPath == metaSidecarName {
			return string(sc.Bytes)
		}
	}
	return ""
}
