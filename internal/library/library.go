// Package library implements operations against a centralized skill library
// rooted at $ADEPT_LIBRARY (default: $HOME/.adeptability). All skills live
// under <root>/skills/<id>/. The library has NO lockfile — the on-disk tree
// is the single source of truth and content hashes drive all sync decisions.
package library

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/itaywol/adeptability/internal/canonical"
	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/internal/hash"
	"github.com/itaywol/adeptability/pkg/adept"
)

// Library is the contract for read/write operations on the centralized skill
// library. Implementations must be safe for concurrent reads; writes are
// serialized by the caller (the CLI is single-process).
type Library interface {
	Root() string
	SkillsDir() string
	HasSkill(id string) bool
	GetSkill(id string) (*adept.Skill, error)
	ListSkills() ([]*adept.Skill, error)
	AddSkill(s *adept.Skill, files []adept.SkillFile) error
	RemoveSkill(id string) error
	// HashSkill returns the content hash of the on-disk skill directory.
	// Empty string + nil error when the skill is not present.
	HashSkill(id string) (string, error)
}

// New constructs a Library rooted at the given absolute path. The directory
// does not need to exist; AddSkill will create it on demand.
func New(root string, parser canonical.Parser, hasher hash.Hasher, w fsutil.Writer) Library {
	return &library{
		root:   root,
		parser: parser,
		hasher: hasher,
		writer: w,
	}
}

// DefaultRoot returns $ADEPT_LIBRARY or $HOME/.adeptability. When neither is
// resolvable the function returns the literal ".adeptability" in the current
// working directory so the CLI can still operate (with a warning surfaced by
// the caller).
func DefaultRoot() string {
	if v := os.Getenv(adept.LibraryEnvVar); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return adept.DefaultLibraryDir
	}
	return filepath.Join(home, adept.DefaultLibraryDir)
}

type library struct {
	root   string
	parser canonical.Parser
	hasher hash.Hasher
	writer fsutil.Writer
}

func (l *library) Root() string      { return l.root }
func (l *library) SkillsDir() string { return filepath.Join(l.root, adept.SkillsDirName) }

func (l *library) HasSkill(id string) bool {
	_, err := os.Stat(filepath.Join(l.SkillsDir(), id, adept.SkillFileName))
	return err == nil
}

func (l *library) GetSkill(id string) (*adept.Skill, error) {
	if id == "" {
		return nil, fmt.Errorf("library get: %w: empty id", adept.ErrSkillInvalid)
	}
	skillPath := filepath.Join(l.SkillsDir(), id, adept.SkillFileName)
	data, err := os.ReadFile(skillPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("library get %q: %w", id, adept.ErrSkillNotFound)
		}
		return nil, fmt.Errorf("library get %q: read: %w", id, err)
	}
	skill, body, err := l.parser.ParseFrontmatter(data)
	if err != nil {
		return nil, fmt.Errorf("library get %q: %w", id, err)
	}
	skill.Body = body
	files, err := l.loadSidecars(id)
	if err != nil {
		return nil, fmt.Errorf("library get %q: %w", id, err)
	}
	skill.Files = files
	return skill, nil
}

func (l *library) ListSkills() ([]*adept.Skill, error) {
	dirs, err := l.listSkillIDs()
	if err != nil {
		return nil, err
	}
	out := make([]*adept.Skill, 0, len(dirs))
	for _, id := range dirs {
		s, err := l.GetSkill(id)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func (l *library) listSkillIDs() ([]string, error) {
	entries, err := os.ReadDir(l.SkillsDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("library list: %w", err)
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		_, err := os.Stat(filepath.Join(l.SkillsDir(), e.Name(), adept.SkillFileName))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("library list: probe %q: %w", e.Name(), err)
		}
		ids = append(ids, e.Name())
	}
	sort.Strings(ids)
	return ids, nil
}

// AddSkill writes canonical files (SKILL.md + sidecars) for s into the
// library. Idempotent: rewriting identical bytes is a no-op against the
// authoritative on-disk state (AtomicWrite handles the rename safely).
// If the directory already exists with different content, the new bytes
// overwrite cleanly.
func (l *library) AddSkill(s *adept.Skill, files []adept.SkillFile) error {
	if s == nil {
		return fmt.Errorf("library add: %w: nil skill", adept.ErrSkillInvalid)
	}
	if s.ID == "" {
		return fmt.Errorf("library add: %w: empty id", adept.ErrSkillInvalid)
	}
	s.Files = files
	if err := l.writeSkillFiles(s, files); err != nil {
		return fmt.Errorf("library add %q: %w", s.ID, err)
	}
	return nil
}

func (l *library) RemoveSkill(id string) error {
	if id == "" {
		return fmt.Errorf("library remove: %w: empty id", adept.ErrSkillInvalid)
	}
	dir := filepath.Join(l.SkillsDir(), id)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("library remove %q: %w", id, err)
	}
	return nil
}

func (l *library) HashSkill(id string) (string, error) {
	if id == "" {
		return "", nil
	}
	dir := filepath.Join(l.SkillsDir(), id)
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("library hash %q: stat: %w", id, err)
	}
	return l.hasher.HashSkillDir(dir)
}

func (l *library) writeSkillFiles(s *adept.Skill, files []adept.SkillFile) error {
	body, err := renderCanonical(s)
	if err != nil {
		return err
	}
	skillPath := filepath.Join(l.SkillsDir(), s.ID, adept.SkillFileName)
	if err := l.writer.AtomicWrite(skillPath, body, 0o644); err != nil {
		return fmt.Errorf("write SKILL.md: %w", err)
	}
	for _, f := range files {
		if f.RelPath == "" {
			return fmt.Errorf("write sidecar: empty rel path")
		}
		if filepath.IsAbs(f.RelPath) {
			return fmt.Errorf("write sidecar %q: must be relative", f.RelPath)
		}
		// Reject path traversal.
		clean := filepath.Clean(f.RelPath)
		if strings.HasPrefix(clean, "..") {
			return fmt.Errorf("write sidecar %q: escapes skill dir", f.RelPath)
		}
		mode := f.Mode
		if mode == 0 {
			mode = 0o644
		}
		dst := filepath.Join(l.SkillsDir(), s.ID, clean)
		if err := l.writer.AtomicWrite(dst, f.Bytes, mode); err != nil {
			return fmt.Errorf("write sidecar %q: %w", f.RelPath, err)
		}
	}
	return nil
}

func (l *library) loadSidecars(id string) ([]adept.SkillFile, error) {
	root := filepath.Join(l.SkillsDir(), id)
	var out []adept.SkillFile
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if rel == adept.SkillFileName {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		info, statErr := d.Info()
		if statErr != nil {
			return statErr
		}
		out = append(out, adept.SkillFile{
			RelPath: filepath.ToSlash(rel),
			Mode:    info.Mode(),
			Bytes:   data,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RelPath < out[j].RelPath })
	return out, nil
}

// renderCanonical delegates to the shared writer in internal/canonical so
// library- and project-side skills round-trip through one serializer.
func renderCanonical(s *adept.Skill) ([]byte, error) {
	return canonical.RenderCanonical(s)
}
