package canonical

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/itaywol/adeptability/pkg/adept"
)

// Loader reads a skill directory and returns a fully populated *adept.Skill,
// including its body, sidecars, and post-validation result.
type Loader interface {
	LoadSkillDir(dir string) (*adept.Skill, error)
}

type loader struct {
	parser    Parser
	validator Validator
}

// NewLoader wires a Loader from a Parser and Validator. Both must be non-nil.
func NewLoader(parser Parser, validator Validator) Loader {
	return &loader{parser: parser, validator: validator}
}

func (l *loader) LoadSkillDir(dir string) (*adept.Skill, error) {
	if l.parser == nil || l.validator == nil {
		return nil, fmt.Errorf("loader: %w: missing parser or validator", adept.ErrSkillInvalid)
	}
	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("loader: %w: %s", adept.ErrSkillNotFound, dir)
		}
		return nil, fmt.Errorf("loader: stat %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("loader: %w: %s is not a directory", adept.ErrSkillInvalid, dir)
	}

	yamlPath := filepath.Join(dir, adept.SkillYAMLName)
	mdPath := filepath.Join(dir, adept.SkillFileName)

	var (
		skill *adept.Skill
		body  string
	)

	yamlBytes, yamlErr := os.ReadFile(yamlPath)
	switch {
	case yamlErr == nil:
		s, err := l.parser.ParseSkillYAML(yamlBytes)
		if err != nil {
			return nil, err
		}
		mdBytes, mdErr := os.ReadFile(mdPath)
		if mdErr == nil {
			body = stripOptionalFrontmatter(mdBytes)
		} else if !errors.Is(mdErr, fs.ErrNotExist) {
			return nil, fmt.Errorf("loader: read %s: %w", mdPath, mdErr)
		}
		s.Body = body
		skill = s
	case errors.Is(yamlErr, fs.ErrNotExist):
		mdBytes, mdErr := os.ReadFile(mdPath)
		if mdErr != nil {
			if errors.Is(mdErr, fs.ErrNotExist) {
				return nil, fmt.Errorf("loader: %w: neither %s nor %s found in %s",
					adept.ErrSkillInvalid, adept.SkillYAMLName, adept.SkillFileName, dir)
			}
			return nil, fmt.Errorf("loader: read %s: %w", mdPath, mdErr)
		}
		s, b, err := l.parser.ParseFrontmatter(mdBytes)
		if err != nil {
			return nil, err
		}
		s.Body = b
		skill = s
	default:
		return nil, fmt.Errorf("loader: read %s: %w", yamlPath, yamlErr)
	}

	// Directory name is authoritative for skill identity.
	if skill.ID == "" {
		skill.ID = filepath.Base(dir)
	}

	if err := l.validator.Validate(skill); err != nil {
		return nil, err
	}

	files, err := collectSidecars(dir)
	if err != nil {
		return nil, err
	}
	skill.Files = files
	return skill, nil
}

// stripOptionalFrontmatter removes a leading YAML frontmatter block from
// SKILL.md when a sibling skill.yaml is the authoritative source.
func stripOptionalFrontmatter(md []byte) string {
	norm := strings.ReplaceAll(string(md), "\r\n", "\n")
	if !strings.HasPrefix(norm, "---\n") {
		return norm
	}
	rest := norm[len("---\n"):]
	closer := "\n---\n"
	idx := strings.Index(rest, closer)
	if idx < 0 {
		if strings.HasSuffix(rest, "\n---") {
			return ""
		}
		return norm
	}
	return rest[idx+len(closer):]
}

// collectSidecars walks dir collecting every file other than the skill source
// files, applying any .adeptignore patterns located at the skill root.
func collectSidecars(dir string) ([]adept.SkillFile, error) {
	patterns, err := readIgnore(filepath.Join(dir, adept.IgnoreFileName))
	if err != nil {
		return nil, err
	}
	var out []adept.SkillFile
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		base := filepath.Base(path)
		// Skip dot-files / dot-dirs except recognized skill files.
		if strings.HasPrefix(base, ".") && base != adept.SkillYAMLName && base != adept.SkillFileName {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		// Exclude skill source files themselves.
		if rel == adept.SkillYAMLName || rel == adept.SkillFileName || rel == adept.IgnoreFileName {
			return nil
		}
		if matchAny(patterns, rel) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("loader: read sidecar %s: %w", path, err)
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("loader: stat sidecar %s: %w", path, err)
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

// readIgnore loads a .adeptignore file as a slice of doublestar patterns.
// A missing file returns no patterns and no error.
func readIgnore(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("loader: read %s: %w", path, err)
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	patterns := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		patterns = append(patterns, trimmed)
	}
	return patterns, nil
}

func matchAny(patterns []string, rel string) bool {
	for _, p := range patterns {
		ok, err := doublestar.Match(p, rel)
		if err == nil && ok {
			return true
		}
	}
	return false
}
