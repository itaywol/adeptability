package adapter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/itaywol/adeptability/internal/render/common"
	"github.com/itaywol/adeptability/pkg/adept"
)

// NewSynthetic builds an adept.HarnessAdapter from a Spec. The returned
// adapter has a fully self-contained Renderer that applies frontmatter +
// body transforms; no external renderer packages are required.
func NewSynthetic(spec Spec) (adept.HarnessAdapter, error) {
	if spec.ID == "" {
		return nil, fmt.Errorf("synthetic: %w: empty id", adept.ErrAdapterInvalid)
	}
	if spec.Output == "" {
		return nil, fmt.Errorf("synthetic %q: %w: empty output", spec.ID, adept.ErrAdapterInvalid)
	}
	if spec.Kind == "" {
		return nil, fmt.Errorf("synthetic %q: %w: empty kind", spec.ID, adept.ErrAdapterInvalid)
	}
	switch spec.Kind {
	case adept.KindPerSkill, adept.KindAggregatorSingle, adept.KindAggregatorPerGlob:
	default:
		return nil, fmt.Errorf("synthetic %q: %w: invalid kind %q", spec.ID, adept.ErrAdapterInvalid, spec.Kind)
	}
	// Precompile body replace regexes for runtime efficiency.
	compiled := make([]bodyRule, 0, len(spec.Body.Replace))
	for i, r := range spec.Body.Replace {
		re, err := regexp.Compile(r.Regex)
		if err != nil {
			return nil, fmt.Errorf("synthetic %q: replace rule %d: %w: %w", spec.ID, i, adept.ErrAdapterInvalid, err)
		}
		compiled = append(compiled, bodyRule{re: re, with: r.With})
	}
	return &syntheticAdapter{
		spec:      spec,
		bodyRules: compiled,
		fmBuilder: common.NewFrontmatterBuilder(),
		baseSpec:  toHarnessSpec(spec),
	}, nil
}

type syntheticAdapter struct {
	spec      Spec
	bodyRules []bodyRule
	fmBuilder common.FrontmatterBuilder
	baseSpec  adept.HarnessSpec
}

type bodyRule struct {
	re   *regexp.Regexp
	with string
}

func toHarnessSpec(s Spec) adept.HarnessSpec {
	return adept.HarnessSpec{
		ID:          s.ID,
		Name:        s.Name,
		Kind:        s.Kind,
		OutputPath:  s.Output,
		SizeBudgetB: s.Budget,
		NeedsDir:    s.NeedsDir,
		BaseDir:     s.BaseDir,
	}
}

func (a *syntheticAdapter) Spec() adept.HarnessSpec { return a.baseSpec }

func (a *syntheticAdapter) Renderer() adept.Renderer {
	return &syntheticRenderer{a: a}
}

func (a *syntheticAdapter) Aggregate(_ context.Context, parts []adept.RenderOutput, budget int) ([]adept.RenderOutput, error) {
	switch a.spec.Kind {
	case adept.KindPerSkill:
		return parts, nil
	case adept.KindAggregatorSingle:
		return aggregateSingle(parts, a.spec.Output, budget)
	case adept.KindAggregatorPerGlob:
		return aggregatePerGlob(parts, a.spec.Output, budget)
	}
	return parts, nil
}

func (a *syntheticAdapter) Detect(projectRoot string) (bool, error) {
	for _, p := range a.spec.Detect {
		abs := filepath.Join(projectRoot, p)
		if _, err := os.Stat(abs); err == nil {
			return true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("detect %q: %w", abs, err)
		}
	}
	return false, nil
}

func (a *syntheticAdapter) Validate(projectRoot string, expected []adept.RenderOutput) (adept.DriftReport, error) {
	report := adept.DriftReport{}
	for _, out := range expected {
		abs := filepath.Join(projectRoot, out.Path)
		data, err := os.ReadFile(abs)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				report.Missing = append(report.Missing, out.Path)
				continue
			}
			report.Conflict = append(report.Conflict, out.Path)
			continue
		}
		if bytesEqual(data, out.Bytes) {
			report.Synced = append(report.Synced, out.Path)
		} else {
			report.Drifted = append(report.Drifted, out.Path)
		}
	}
	sort.Strings(report.Synced)
	sort.Strings(report.Drifted)
	sort.Strings(report.Missing)
	sort.Strings(report.Conflict)
	return report, nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// syntheticRenderer turns a single Skill into a RenderOutput by applying the
// adapter spec's frontmatter + body transformations.
type syntheticRenderer struct {
	a *syntheticAdapter
}

func (r *syntheticRenderer) Render(_ context.Context, in adept.RenderInput) (adept.RenderOutput, error) {
	if in.Skill == nil {
		return adept.RenderOutput{}, fmt.Errorf("render: %w: nil skill", adept.ErrSkillInvalid)
	}
	fm, err := r.buildFrontmatter(in.Skill)
	if err != nil {
		return adept.RenderOutput{}, fmt.Errorf("render %q: %w", in.Skill.ID, err)
	}
	body := r.transformBody(in.Skill.Body)
	prefix := expandSkillTokens(r.a.spec.Body.Prefix, in.Skill)
	suffix := expandSkillTokens(r.a.spec.Body.Suffix, in.Skill)
	body = prefix + body + suffix
	path, err := resolveOutputPath(r.a.spec.Output, in.Skill)
	if err != nil {
		return adept.RenderOutput{}, fmt.Errorf("render %q: %w", in.Skill.ID, err)
	}
	bytes := []byte(fm + body)
	return adept.RenderOutput{
		Path:      path,
		Bytes:     bytes,
		Mode:      0o644,
		SkillID:   in.Skill.ID,
		SkillHash: common.ShortSkillHash(in.Skill),
	}, nil
}

func (r *syntheticRenderer) buildFrontmatter(s *adept.Skill) (string, error) {
	if len(r.a.spec.Frontmatter.Include) == 0 && len(r.a.spec.Frontmatter.Constants) == 0 {
		return "", nil
	}
	fields := make([]common.Field, 0)
	include := r.a.spec.Frontmatter.Include
	rename := r.a.spec.Frontmatter.Rename
	for _, key := range include {
		val, ok := skillFieldValue(s, key)
		if !ok {
			continue
		}
		outKey := key
		if r, ok := rename[key]; ok && r != "" {
			outKey = r
		}
		fields = append(fields, common.Field{Key: outKey, Value: val})
	}
	// Sort constants for deterministic output.
	keys := make([]string, 0, len(r.a.spec.Frontmatter.Constants))
	for k := range r.a.spec.Frontmatter.Constants {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fields = append(fields, common.Field{Key: k, Value: r.a.spec.Frontmatter.Constants[k]})
	}
	return r.a.fmBuilder.Build(fields)
}

func skillFieldValue(s *adept.Skill, key string) (any, bool) {
	switch key {
	case "id":
		return s.ID, true
	case "description":
		return s.Description, true
	case "activation":
		return string(s.Activation), s.Activation != ""
	case "globs":
		if len(s.Globs) == 0 {
			return nil, false
		}
		return s.Globs, true
	case "allowed-tools":
		if len(s.AllowedTools) == 0 {
			return nil, false
		}
		return s.AllowedTools, true
	case "targets":
		if len(s.Targets) == 0 {
			return nil, false
		}
		return s.Targets, true
	case "tags":
		if len(s.Tags) == 0 {
			return nil, false
		}
		return s.Tags, true
	}
	// Allow access to metadata keys via "metadata.<k>".
	if strings.HasPrefix(key, "metadata.") {
		mk := strings.TrimPrefix(key, "metadata.")
		if v, ok := s.Metadata[mk]; ok {
			return v, true
		}
	}
	return nil, false
}

func (r *syntheticRenderer) transformBody(body string) string {
	for _, rule := range r.a.bodyRules {
		body = rule.re.ReplaceAllString(body, rule.with)
	}
	return body
}

// expandSkillTokens substitutes the same set of tokens used in output paths
// into the prefix/suffix strings supplied by the adapter spec.
func expandSkillTokens(in string, s *adept.Skill) string {
	return strings.ReplaceAll(in, "{id}", s.ID)
}

func resolveOutputPath(tmpl string, s *adept.Skill) (string, error) {
	out := strings.ReplaceAll(tmpl, "{id}", s.ID)
	if strings.Contains(out, "{") {
		// Surface unresolved template tokens; the schema does not enumerate
		// allowed variables, so any "{...}" remaining is a configuration
		// error.
		open := strings.Index(out, "{")
		close := strings.Index(out, "}")
		if open >= 0 && close > open {
			return "", fmt.Errorf("output path %q: unknown template variable %q", tmpl, out[open:close+1])
		}
	}
	return out, nil
}

// aggregateSingle concatenates all parts into a single output keyed by path.
// Parts are sorted by SkillID for deterministic output.
//
// When a size budget forces parts to be omitted, the dropped skills are NOT
// silently discarded: a truncation manifest comment listing the omitted skill
// ids is prepended to the document body, mirroring the built-in codex
// aggregator (internal/render/codex/aggregator.go). This keeps budget overflow
// visible in the written file rather than vanishing without a trace.
func aggregateSingle(parts []adept.RenderOutput, outPath string, budget int) ([]adept.RenderOutput, error) {
	if len(parts) == 0 {
		return nil, nil
	}
	cp := make([]adept.RenderOutput, len(parts))
	copy(cp, parts)
	sort.Slice(cp, func(i, j int) bool {
		return cp[i].SkillID < cp[j].SkillID
	})

	var buf strings.Builder
	var dropped []string
	for _, p := range cp {
		// Skip (don't break on) any part that would overflow the budget so a
		// single large skill early in sort order can't drop every smaller part
		// that would otherwise still fit.
		if budget > 0 && buf.Len()+len(p.Bytes) > budget {
			if p.SkillID != "" {
				dropped = append(dropped, p.SkillID)
			} else {
				dropped = append(dropped, "<unknown>")
			}
			continue
		}
		buf.Write(p.Bytes)
		if !strings.HasSuffix(string(p.Bytes), "\n") {
			buf.WriteByte('\n')
		}
	}

	// Surface dropped skills via a leading manifest comment so budget overflow
	// leaves a trace in the file instead of vanishing. The manifest is appended
	// after the budget check so a pathologically tiny budget (smaller than the
	// manifest itself) still emits the trace rather than erroring on the
	// comment alone — the kept content is what the budget governs.
	if budget > 0 && buf.Len() > budget {
		return nil, fmt.Errorf("aggregate single: %w: %d > %d", adept.ErrBudgetOverflow, buf.Len(), budget)
	}
	body := buf.String()
	if len(dropped) > 0 {
		body = buildSyntheticTruncationManifest(dropped, budget) + "\n" + body
	}
	return []adept.RenderOutput{{
		Path:  outPath,
		Bytes: []byte(body),
		Mode:  0o644,
	}}, nil
}

// buildSyntheticTruncationManifest emits a leading HTML comment documenting
// which skills were omitted due to the size budget, matching the spirit of the
// codex aggregator's manifest so dropped skills leave a trace in the file.
func buildSyntheticTruncationManifest(dropped []string, budget int) string {
	ids := append([]string(nil), dropped...)
	sort.Strings(ids)
	return fmt.Sprintf(
		"<!-- adeptability: omitted %d skill(s) due to %d byte budget. Dropped: %s -->",
		len(ids),
		budget,
		strings.Join(ids, ","),
	)
}

// aggregatePerGlob buckets parts by the first glob declared by each skill
// emitted RenderOutput. The output path is interpreted as a template where
// {glob} is replaced with a sanitized glob token. Skills without a glob fall
// into a "_default" bucket.
func aggregatePerGlob(parts []adept.RenderOutput, outPath string, budget int) ([]adept.RenderOutput, error) {
	if len(parts) == 0 {
		return nil, nil
	}
	// Group by the first warning marker "glob:" or default. Synthetic adapters
	// don't have access to the source skill from here, so they instead encode
	// the glob bucket via the RenderOutput.Path (which the renderer set to
	// outPath with {glob} already substituted). For the synthetic case we
	// simply collapse identical paths.
	groups := map[string][]adept.RenderOutput{}
	order := []string{}
	for _, p := range parts {
		if _, ok := groups[p.Path]; !ok {
			order = append(order, p.Path)
		}
		groups[p.Path] = append(groups[p.Path], p)
	}
	sort.Strings(order)
	out := make([]adept.RenderOutput, 0, len(groups))
	for _, path := range order {
		merged, err := aggregateSingle(groups[path], path, budget)
		if err != nil {
			return nil, err
		}
		out = append(out, merged...)
	}
	return out, nil
}
