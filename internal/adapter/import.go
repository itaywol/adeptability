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

	"gopkg.in/yaml.v3"

	"github.com/itaywol/adeptability/pkg/adept"
)

// ErrImportUnsupported is the sentinel callers can match against to skip
// non-importable adapters during bulk import. Retained for backward
// compatibility, though synthetic adapters now implement Import.
var ErrImportUnsupported = errors.New("import not supported")

// importedReplaceWarning is appended to ImportedSkill.Warnings for every skill
// recovered from an adapter whose forward config includes body.replace rules.
// Regex substitutions are not generically reversible.
const importedReplaceWarning = "regex replace rules were not reversed; manual cleanup may be needed"

// adeptBeginRE / adeptEndRE match the section markers emitted by the
// aggregator render path so multi-skill aggregator output can be split back
// into individual skills on import.
var (
	adeptBeginRE = regexp.MustCompile(`<!--\s*adeptability:begin\s+id=([a-z0-9_][a-z0-9_-]{0,49})\s+hash=([a-f0-9]{8})\s*-->`)
	adeptEndRE   = regexp.MustCompile(`<!--\s*adeptability:end\s+id=[a-z0-9_][a-z0-9_-]{0,49}\s*-->`)
)

// Import reverse-renders a synthetic adapter's on-disk state into canonical
// skills. Behavior depends on the adapter Kind:
//
//   - per-skill          : walk the {id}-templated output pattern, recover
//     frontmatter + body, collect sidecars when
//     needs-directory is true.
//   - aggregator-single  : read the single output file, split by adept section
//     markers or fall back to one synthesized skill.
//   - aggregator-per-glob: enumerate files matching the {glob}-templated
//     output pattern, treat each bucket like the
//     single-aggregator case.
func (a *syntheticAdapter) Import(_ context.Context, projectRoot string) ([]adept.ImportedSkill, error) {
	switch a.spec.Kind {
	case adept.KindPerSkill:
		return a.importPerSkill(projectRoot)
	case adept.KindAggregatorSingle:
		return a.importAggregatorSingle(projectRoot)
	case adept.KindAggregatorPerGlob:
		return a.importAggregatorPerGlob(projectRoot)
	}
	return nil, fmt.Errorf("synthetic %s: %w: unknown kind %q", a.spec.ID, adept.ErrAdapterInvalid, a.spec.Kind)
}

// importPerSkill walks the output pattern, treating {id} as a wildcard.
func (a *syntheticAdapter) importPerSkill(projectRoot string) ([]adept.ImportedSkill, error) {
	matches, err := a.findOutputMatches(projectRoot, "{id}")
	if err != nil {
		return nil, err
	}
	out := make([]adept.ImportedSkill, 0, len(matches))
	for _, m := range matches {
		imp, err := a.importPerSkillFile(m.fullPath, m.token)
		if err != nil {
			return nil, err
		}
		out = append(out, imp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Skill.ID < out[j].Skill.ID })
	return out, nil
}

func (a *syntheticAdapter) importPerSkillFile(fullPath, id string) (adept.ImportedSkill, error) {
	raw, err := os.ReadFile(fullPath)
	if err != nil {
		return adept.ImportedSkill{}, fmt.Errorf("synthetic %s: read %s: %w", a.spec.ID, fullPath, err)
	}
	front, body, err := splitYAMLFrontmatter(raw)
	if err != nil {
		return adept.ImportedSkill{}, fmt.Errorf("synthetic %s import %s: %w", a.spec.ID, id, err)
	}
	skill := a.skillFromFrontmatter(front, id)
	skill.Body = a.stripBodyPrefixSuffix(body, id)

	imp := adept.ImportedSkill{
		Skill:      skill,
		SourcePath: fullPath,
		Warnings:   a.baseWarnings(),
	}
	if a.spec.NeedsDir {
		files, err := collectSyntheticSidecars(filepath.Dir(fullPath))
		if err != nil {
			return adept.ImportedSkill{}, fmt.Errorf("synthetic %s sidecars %s: %w", a.spec.ID, id, err)
		}
		imp.Files = files
	}
	return imp, nil
}

// importAggregatorSingle reads the configured output path and splits on
// adept section markers if present, otherwise synthesizes a single skill.
func (a *syntheticAdapter) importAggregatorSingle(projectRoot string) ([]adept.ImportedSkill, error) {
	rel := a.spec.Output
	full := filepath.Join(projectRoot, rel)
	raw, err := os.ReadFile(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("synthetic %s: read %s: %w", a.spec.ID, full, err)
	}
	skills, err := a.splitAggregatorContent(raw, full, "")
	if err != nil {
		return nil, err
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Skill.ID < skills[j].Skill.ID })
	return skills, nil
}

// importAggregatorPerGlob enumerates every file matching the {glob}-templated
// output and parses each as an aggregator bucket.
func (a *syntheticAdapter) importAggregatorPerGlob(projectRoot string) ([]adept.ImportedSkill, error) {
	matches, err := a.findOutputMatches(projectRoot, "{glob}")
	if err != nil {
		return nil, err
	}
	var out []adept.ImportedSkill
	for _, m := range matches {
		raw, err := os.ReadFile(m.fullPath)
		if err != nil {
			return nil, fmt.Errorf("synthetic %s: read %s: %w", a.spec.ID, m.fullPath, err)
		}
		skills, err := a.splitAggregatorContent(raw, m.fullPath, m.token)
		if err != nil {
			return nil, err
		}
		out = append(out, skills...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Skill.ID < out[j].Skill.ID })
	return out, nil
}

// splitAggregatorContent splits raw bucket content on adept begin/end markers
// or, in their absence, synthesizes a single skill from the entire content.
// bucketToken is the value substituted for {glob} when applicable; it is used
// as the fallback skill id and to seed glob activation.
func (a *syntheticAdapter) splitAggregatorContent(raw []byte, sourcePath, bucketToken string) ([]adept.ImportedSkill, error) {
	// Detect adept-emitted frontmatter (currently used by aggregator-per-glob
	// for applyTo/globs metadata) before scanning markers.
	front, content, err := splitYAMLFrontmatter(raw)
	if err != nil {
		return nil, fmt.Errorf("synthetic %s aggregator: %w", a.spec.ID, err)
	}
	frontActivation, frontGlobs := activationFromFrontmatter(front)

	body := string(content)
	matches := adeptBeginRE.FindAllStringSubmatchIndex(body, -1)
	if len(matches) == 0 {
		// No markers — one synthesized skill.
		id := sanitizeSkillID(bucketToken)
		if id == "" {
			id = sanitizeSkillID(strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath)))
		}
		if id == "" {
			id = "imported"
		}
		skill := &adept.Skill{
			ID:          id,
			Description: fmt.Sprintf("Imported from %s %s", a.spec.ID, id),
			Activation:  adept.ActivationAgent,
			Body:        strings.TrimSpace(body),
		}
		applyAggregatorActivation(skill, frontActivation, frontGlobs)
		return []adept.ImportedSkill{{
			Skill:      skill,
			SourcePath: sourcePath,
			Warnings:   a.baseWarnings(),
		}}, nil
	}
	out := make([]adept.ImportedSkill, 0, len(matches))
	for _, m := range matches {
		bodyStart := m[1]
		id := body[m[2]:m[3]]
		endMatch := adeptEndRE.FindStringIndex(body[bodyStart:])
		if endMatch == nil {
			return nil, fmt.Errorf("synthetic %s aggregator: unterminated section for %q in %s", a.spec.ID, id, sourcePath)
		}
		section := strings.TrimSpace(body[bodyStart : bodyStart+endMatch[0]])
		desc := ""
		lines := strings.SplitN(section, "\n", 2)
		if len(lines) > 0 && strings.HasPrefix(lines[0], "## ") {
			desc = strings.TrimSpace(strings.TrimPrefix(lines[0], "## "))
			section = ""
			if len(lines) == 2 {
				section = strings.TrimSpace(lines[1])
			}
		}
		if desc == "" {
			desc = fmt.Sprintf("Imported from %s section %s", a.spec.ID, id)
		}
		skill := &adept.Skill{
			ID:          id,
			Description: desc,
			Activation:  adept.ActivationAgent,
			Body:        section,
		}
		applyAggregatorActivation(skill, frontActivation, frontGlobs)
		out = append(out, adept.ImportedSkill{
			Skill:      skill,
			SourcePath: sourcePath,
			Warnings:   a.baseWarnings(),
		})
	}
	return out, nil
}

// matchedOutput represents one filesystem match of the output template, with
// the wildcard token extracted.
type matchedOutput struct {
	fullPath string
	token    string
}

// findOutputMatches enumerates filesystem paths under projectRoot that match
// the adapter's output template, with `wildcard` (e.g. "{id}" or "{glob}")
// treated as a wildcard segment captured per match. All other template tokens
// must be literal at import time.
func (a *syntheticAdapter) findOutputMatches(projectRoot, wildcard string) ([]matchedOutput, error) {
	tmpl := filepath.ToSlash(a.spec.Output)
	if !strings.Contains(tmpl, wildcard) {
		// Treat as a fixed file path.
		full := filepath.Join(projectRoot, filepath.FromSlash(tmpl))
		if _, err := os.Stat(full); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, nil
			}
			return nil, fmt.Errorf("synthetic %s: stat %s: %w", a.spec.ID, full, err)
		}
		return []matchedOutput{{fullPath: full, token: ""}}, nil
	}
	// Reject ambiguous templates with multiple wildcards — current scheme is
	// one path-segment wildcard.
	if strings.Count(tmpl, wildcard) > 1 {
		return nil, fmt.Errorf("synthetic %s: %w: multiple %s tokens in output %q", a.spec.ID, adept.ErrAdapterInvalid, wildcard, tmpl)
	}
	idx := strings.Index(tmpl, wildcard)
	prefix := tmpl[:idx]
	suffix := tmpl[idx+len(wildcard):]
	// We need a fixed directory to walk. Strip back to the last "/" before the
	// wildcard, since the wildcard must be a single path segment (no "/" in
	// the captured value).
	slash := strings.LastIndex(prefix, "/")
	var baseRel, prefixRem string
	if slash < 0 {
		baseRel = "."
		prefixRem = prefix
	} else {
		baseRel = prefix[:slash]
		prefixRem = prefix[slash+1:]
	}
	baseDir := filepath.Join(projectRoot, filepath.FromSlash(baseRel))
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("synthetic %s: read %s: %w", a.spec.ID, baseDir, err)
	}
	out := make([]matchedOutput, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		// Match either a file (when suffix has no "/") or a directory entry
		// matched against the first path segment of the remainder.
		token, child, ok := matchSegment(name, prefixRem, suffix)
		if !ok {
			continue
		}
		full := filepath.Join(baseDir, name)
		if child != "" {
			full = filepath.Join(full, filepath.FromSlash(child))
		}
		if _, err := os.Stat(full); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("synthetic %s: stat %s: %w", a.spec.ID, full, err)
		}
		out = append(out, matchedOutput{fullPath: full, token: token})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].fullPath < out[j].fullPath })
	return out, nil
}

// matchSegment returns (token, remainingChildPath, ok). It handles two shapes:
//
//	prefix=".junie/guidelines/", entryName="alpha.md", suffix=".md"   →
//	   token="alpha", child="", ok=true
//	prefix=".claude/skills/",   entryName="alpha",   suffix="/SKILL.md" →
//	   token="alpha", child="SKILL.md", ok=true (entry must be a dir)
func matchSegment(entryName, prefixRem, suffix string) (string, string, bool) {
	// Strip prefix remainder (chars between last slash before wildcard and
	// the wildcard itself).
	if !strings.HasPrefix(entryName, prefixRem) {
		return "", "", false
	}
	rest := entryName[len(prefixRem):]
	// If suffix starts with "/", the entry is a directory and the remainder
	// of the suffix is the relative file beneath it.
	if strings.HasPrefix(suffix, "/") {
		return rest, suffix[1:], true
	}
	if !strings.HasSuffix(rest, suffix) {
		return "", "", false
	}
	token := rest[:len(rest)-len(suffix)]
	if token == "" {
		return "", "", false
	}
	return token, "", true
}

// splitYAMLFrontmatter pulls the YAML between leading `---\n` and `\n---`.
// Returns (frontmatterBytes, bodyBytes, nil). When no frontmatter is present
// returns (nil, raw, nil).
func splitYAMLFrontmatter(raw []byte) ([]byte, []byte, error) {
	s := string(raw)
	if !strings.HasPrefix(s, "---\n") {
		return nil, raw, nil
	}
	rest := s[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, nil, fmt.Errorf("unterminated frontmatter")
	}
	front := rest[:end]
	bodyStr := strings.TrimPrefix(rest[end+4:], "\n")
	return []byte(front), []byte(bodyStr), nil
}

// skillFromFrontmatter constructs a Skill, honoring frontmatter.include and
// reversing frontmatter.rename. The fallback id is used when frontmatter
// doesn't carry one.
func (a *syntheticAdapter) skillFromFrontmatter(front []byte, fallbackID string) *adept.Skill {
	skill := &adept.Skill{
		ID:         fallbackID,
		Activation: adept.ActivationAgent,
	}
	if len(front) == 0 {
		if skill.Description == "" {
			skill.Description = fmt.Sprintf("Imported from %s %s", a.spec.ID, fallbackID)
		}
		return skill
	}
	parsed := map[string]any{}
	if err := yaml.Unmarshal(front, &parsed); err != nil {
		// Soft-fail on malformed frontmatter: keep defaults, leave a sane id.
		if skill.Description == "" {
			skill.Description = fmt.Sprintf("Imported from %s %s", a.spec.ID, fallbackID)
		}
		return skill
	}

	included := includeSet(a.spec.Frontmatter.Include)
	inverse := a.inverseRename()

	for harnessKey, v := range parsed {
		canonical := inverse[harnessKey]
		if canonical == "" {
			canonical = harnessKey
		}
		if len(included) > 0 {
			if _, ok := included[canonical]; !ok {
				continue
			}
		}
		assignSkillField(skill, canonical, v)
	}

	if skill.ID == "" {
		skill.ID = fallbackID
	}
	// Infer activation when canonical fields imply it but no explicit
	// `activation:` key was recovered. This handles forward configs that only
	// include `globs:` in the frontmatter and rely on the canonical default.
	if skill.Activation == "" || skill.Activation == adept.ActivationAgent {
		if len(skill.Globs) > 0 {
			skill.Activation = adept.ActivationGlobs
		}
	}
	if skill.Activation == "" {
		skill.Activation = adept.ActivationAgent
	}
	if skill.Description == "" {
		skill.Description = fmt.Sprintf("Imported from %s %s", a.spec.ID, skill.ID)
	}
	return skill
}

// inverseRename returns the harness-key → canonical-key map. Explicit
// spec.Import.Rename wins; otherwise we invert spec.Frontmatter.Rename.
func (a *syntheticAdapter) inverseRename() map[string]string {
	if len(a.spec.Import.Rename) > 0 {
		out := make(map[string]string, len(a.spec.Import.Rename))
		for k, v := range a.spec.Import.Rename {
			out[k] = v
		}
		return out
	}
	out := make(map[string]string, len(a.spec.Frontmatter.Rename))
	for canon, harness := range a.spec.Frontmatter.Rename {
		if harness == "" {
			continue
		}
		out[harness] = canon
	}
	return out
}

func includeSet(include []string) map[string]struct{} {
	if len(include) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(include))
	for _, k := range include {
		out[k] = struct{}{}
	}
	return out
}

// assignSkillField writes a parsed frontmatter value into the canonical Skill
// field identified by canonicalKey. Unknown keys are silently ignored.
func assignSkillField(s *adept.Skill, canonicalKey string, v any) {
	switch canonicalKey {
	case "id":
		if str, ok := v.(string); ok {
			s.ID = str
		}
	case "description":
		if str, ok := v.(string); ok {
			s.Description = str
		}
	case "activation":
		if str, ok := v.(string); ok {
			s.Activation = adept.ActivationMode(str)
		}
	case "globs":
		if g := toStringSlice(v); g != nil {
			s.Globs = g
		}
	case "allowed-tools":
		if g := toStringSlice(v); g != nil {
			s.AllowedTools = g
		}
	case "targets":
		if g := toStringSlice(v); g != nil {
			s.Targets = g
		}
	case "tags":
		if g := toStringSlice(v); g != nil {
			s.Tags = g
		}
	default:
		if strings.HasPrefix(canonicalKey, "metadata.") {
			mk := strings.TrimPrefix(canonicalKey, "metadata.")
			if s.Metadata == nil {
				s.Metadata = map[string]string{}
			}
			if str, ok := v.(string); ok {
				s.Metadata[mk] = str
			}
		}
	}
}

func toStringSlice(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		cp := make([]string, len(t))
		copy(cp, t)
		return cp
	case string:
		// Comma-separated fallback (applyTo-style).
		parts := strings.Split(t, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	return nil
}

// stripBodyPrefixSuffix removes the (token-expanded) body prefix/suffix the
// forward renderer would have prepended/appended. Empty values are no-ops.
func (a *syntheticAdapter) stripBodyPrefixSuffix(body []byte, id string) string {
	s := string(body)
	tokens := map[string]string{"{id}": id}
	prefix := expandTokens(a.spec.Body.Prefix, tokens)
	suffix := expandTokens(a.spec.Body.Suffix, tokens)
	if prefix != "" && strings.HasPrefix(s, prefix) {
		s = s[len(prefix):]
	}
	if suffix != "" && strings.HasSuffix(s, suffix) {
		s = s[:len(s)-len(suffix)]
	}
	return s
}

func expandTokens(in string, tokens map[string]string) string {
	out := in
	for k, v := range tokens {
		out = strings.ReplaceAll(out, k, v)
	}
	return out
}

// baseWarnings returns the warnings every imported skill from this adapter
// receives. Currently only the lossy-replace warning.
func (a *syntheticAdapter) baseWarnings() []string {
	if len(a.spec.Body.Replace) == 0 {
		return nil
	}
	return []string{importedReplaceWarning}
}

// activationFromFrontmatter inspects a parsed frontmatter map for applyTo /
// globs / alwaysApply-style fields. Returns (mode, globs); mode is empty when
// no activation info was found.
func activationFromFrontmatter(front []byte) (adept.ActivationMode, []string) {
	if len(front) == 0 {
		return "", nil
	}
	parsed := map[string]any{}
	if err := yaml.Unmarshal(front, &parsed); err != nil {
		return "", nil
	}
	if v, ok := parsed["alwaysApply"]; ok {
		if b, ok := v.(bool); ok && b {
			return adept.ActivationAlways, nil
		}
	}
	if v, ok := parsed["applyTo"]; ok {
		if s, ok := v.(string); ok {
			if s == "**" {
				return adept.ActivationAlways, nil
			}
			globs := toStringSlice(s)
			if len(globs) == 1 && globs[0] == "**" {
				return adept.ActivationAlways, nil
			}
			if len(globs) > 0 {
				return adept.ActivationGlobs, globs
			}
		}
	}
	if v, ok := parsed["globs"]; ok {
		if globs := toStringSlice(v); len(globs) > 0 {
			return adept.ActivationGlobs, globs
		}
	}
	return "", nil
}

// applyAggregatorActivation overlays activation info recovered from bucket
// frontmatter onto a per-section skill. Only fills empty fields, so explicit
// per-section data (when later supported) takes precedence.
func applyAggregatorActivation(s *adept.Skill, mode adept.ActivationMode, globs []string) {
	if mode == "" {
		return
	}
	if s.Activation == "" || s.Activation == adept.ActivationAgent {
		s.Activation = mode
	}
	if mode == adept.ActivationGlobs && len(s.Globs) == 0 {
		s.Globs = append([]string(nil), globs...)
	}
}

var skillIDSanitizeRE = regexp.MustCompile(`[^a-z0-9_-]+`)

// sanitizeSkillID coerces an arbitrary token into a legal skill id. Returns
// empty when the result would not satisfy the canonical pattern.
func sanitizeSkillID(s string) string {
	s = strings.ToLower(s)
	s = skillIDSanitizeRE.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return ""
	}
	if len(s) > 50 {
		s = s[:50]
	}
	return s
}

// collectSyntheticSidecars walks dir and returns every file other than
// SKILL.md.
func collectSyntheticSidecars(dir string) ([]adept.SkillFile, error) {
	var out []adept.SkillFile
	walkErr := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == dir {
			return nil
		}
		base := filepath.Base(path)
		if strings.HasPrefix(base, ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == adept.SkillFileName {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read sidecar %s: %w", path, err)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, adept.SkillFile{
			RelPath: rel,
			Mode:    info.Mode().Perm(),
			Bytes:   data,
		})
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RelPath < out[j].RelPath })
	return out, nil
}
