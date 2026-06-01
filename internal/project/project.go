// Package project implements operations against a single project's
// <root>/.adeptability/ directory. Layout mirrors the library; the lockfile
// records which skills are installed and at what version/hash, so the status
// machine can compare the project against the library.
package project

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/itaywol/adeptability/internal/canonical"
	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/internal/hash"
	"github.com/itaywol/adeptability/internal/lockfile"
	"github.com/itaywol/adeptability/pkg/adept"
)

// Project is the contract for read/write operations against a project's
// .adeptability directory. The root passed to New is the project root, not
// the .adeptability subdir.
type Project interface {
	Root() string
	BaseDir() string
	SkillsDir() string
	// BaseSnapshotDir returns the root of the merge-base snapshot store:
	// <root>/.adeptability/base/. Per-skill snapshots live under that
	// directory, one subdirectory per skill id.
	BaseSnapshotDir() string
	// BaseDirForSkill returns the absolute path of the per-skill base
	// snapshot for id. The directory may not yet exist; callers should
	// use HasBaseSnapshot for a presence check.
	BaseDirForSkill(id string) string
	// HasBaseSnapshot reports whether a base snapshot exists for id.
	HasBaseSnapshot(id string) bool
	// SnapshotBase copies the project's current canonical state for id
	// into BaseDirForSkill(id), overwriting any prior snapshot. It is
	// idempotent and intended to be called immediately after a successful
	// pull / install / clean resolve so that future diverged resolves have
	// a real common ancestor to merge against.
	SnapshotBase(id string) error
	LockfilePath() string
	Lock() (*adept.LockFile, error)
	SaveLock(lf *adept.LockFile) error
	HasSkill(id string) bool
	GetSkill(id string) (*adept.Skill, error)
	ListSkills() ([]*adept.Skill, error)
	InstallSkill(s *adept.Skill, files []adept.SkillFile, libEntry adept.LockEntry) error
	UninstallSkill(id string) error
}

// New constructs a Project rooted at the given absolute project path. The
// .adeptability subdirectory does not need to exist; InstallSkill creates it.
func New(root string, parser canonical.Parser, hasher hash.Hasher, store lockfile.Store, w fsutil.Writer) Project {
	return &project{
		root:   root,
		parser: parser,
		hasher: hasher,
		store:  store,
		writer: w,
	}
}

type project struct {
	root   string
	parser canonical.Parser
	hasher hash.Hasher
	store  lockfile.Store
	writer fsutil.Writer
}

func (p *project) Root() string            { return p.root }
func (p *project) BaseDir() string         { return filepath.Join(p.root, adept.BaseDirName) }
func (p *project) SkillsDir() string       { return filepath.Join(p.BaseDir(), adept.SkillsDirName) }
func (p *project) BaseSnapshotDir() string { return filepath.Join(p.BaseDir(), adept.BaseSnapDir) }
func (p *project) BaseDirForSkill(id string) string {
	return filepath.Join(p.BaseSnapshotDir(), id)
}
func (p *project) LockfilePath() string { return filepath.Join(p.root, adept.LockFileName) }

func (p *project) HasBaseSnapshot(id string) bool {
	if id == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(p.BaseDirForSkill(id), adept.SkillFileName))
	return err == nil
}

// SnapshotBase mirrors <skillsDir>/<id>/ into <baseDir>/<id>/. The destination
// is wiped first so removed sidecars don't survive across snapshots. The copy
// uses the injected fsutil.Writer to keep IO behavior consistent (no globals).
func (p *project) SnapshotBase(id string) error {
	if id == "" {
		return fmt.Errorf("project snapshot base: %w: empty id", adept.ErrSkillInvalid)
	}
	src := filepath.Join(p.SkillsDir(), id)
	if _, err := os.Stat(filepath.Join(src, adept.SkillFileName)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("project snapshot base %q: %w", id, adept.ErrSkillNotFound)
		}
		return fmt.Errorf("project snapshot base %q: stat: %w", id, err)
	}
	dst := p.BaseDirForSkill(id)
	if err := p.writer.RemoveAll(dst); err != nil {
		return fmt.Errorf("project snapshot base %q: clear: %w", id, err)
	}
	if err := p.writer.EnsureDir(filepath.Dir(dst)); err != nil {
		return fmt.Errorf("project snapshot base %q: ensure: %w", id, err)
	}
	if err := p.writer.CopyDir(src, dst); err != nil {
		return fmt.Errorf("project snapshot base %q: copy: %w", id, err)
	}
	return nil
}

func (p *project) Lock() (*adept.LockFile, error) {
	lf, err := p.store.Read(p.LockfilePath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return p.store.Empty(), nil
		}
		return nil, fmt.Errorf("project lock load: %w", err)
	}
	if lf == nil {
		lf = p.store.Empty()
	}
	if lf.Schema == 0 {
		lf.Schema = adept.LockSchemaVersion
	}
	if lf.Skills == nil {
		lf.Skills = map[string]adept.LockEntry{}
	}
	return lf, nil
}

func (p *project) SaveLock(lf *adept.LockFile) error {
	if lf == nil {
		return fmt.Errorf("project save lock: nil lockfile")
	}
	if lf.Schema == 0 {
		lf.Schema = adept.LockSchemaVersion
	}
	if lf.Skills == nil {
		lf.Skills = map[string]adept.LockEntry{}
	}
	if err := p.store.Write(p.LockfilePath(), lf); err != nil {
		return fmt.Errorf("project save lock: %w", err)
	}
	return nil
}

func (p *project) HasSkill(id string) bool {
	_, err := os.Stat(filepath.Join(p.SkillsDir(), id, adept.SkillFileName))
	return err == nil
}

func (p *project) GetSkill(id string) (*adept.Skill, error) {
	if id == "" {
		return nil, fmt.Errorf("project get: %w: empty id", adept.ErrSkillInvalid)
	}
	skillPath := filepath.Join(p.SkillsDir(), id, adept.SkillFileName)
	data, err := os.ReadFile(skillPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("project get %q: %w", id, adept.ErrSkillNotFound)
		}
		return nil, fmt.Errorf("project get %q: read: %w", id, err)
	}
	skill, body, err := p.parser.ParseFrontmatter(data)
	if err != nil {
		return nil, fmt.Errorf("project get %q: %w", id, err)
	}
	skill.Body = body
	files, err := p.loadSidecars(id)
	if err != nil {
		return nil, fmt.Errorf("project get %q: %w", id, err)
	}
	skill.Files = files
	return skill, nil
}

func (p *project) ListSkills() ([]*adept.Skill, error) {
	ids, err := p.listSkillIDs()
	if err != nil {
		return nil, err
	}
	out := make([]*adept.Skill, 0, len(ids))
	for _, id := range ids {
		s, err := p.GetSkill(id)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func (p *project) listSkillIDs() ([]string, error) {
	entries, err := os.ReadDir(p.SkillsDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("project list: %w", err)
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		_, err := os.Stat(filepath.Join(p.SkillsDir(), e.Name(), adept.SkillFileName))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("project list: probe %q: %w", e.Name(), err)
		}
		ids = append(ids, e.Name())
	}
	sort.Strings(ids)
	return ids, nil
}

func (p *project) InstallSkill(s *adept.Skill, files []adept.SkillFile, libEntry adept.LockEntry) error {
	if s == nil {
		return fmt.Errorf("project install: %w: nil skill", adept.ErrSkillInvalid)
	}
	if s.ID == "" {
		return fmt.Errorf("project install: %w: empty id", adept.ErrSkillInvalid)
	}
	if err := p.writeSkillFiles(s, files); err != nil {
		return fmt.Errorf("project install %q: %w", s.ID, err)
	}
	digest := libEntry.Hash
	if digest == "" {
		// Inline the sidecars so HashSkill sees the full envelope.
		s.Files = files
		h, err := p.hasher.HashSkill(s)
		if err != nil {
			return fmt.Errorf("project install %q: hash: %w", s.ID, err)
		}
		digest = h
	}
	lf, err := p.Lock()
	if err != nil {
		return fmt.Errorf("project install %q: %w", s.ID, err)
	}
	ver := s.Version
	if libEntry.Version > 0 {
		ver = libEntry.Version
	}
	lf.Skills[s.ID] = adept.LockEntry{
		Version:   ver,
		Hash:      digest,
		Targets:   libEntry.Targets,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		Signature: libEntry.Signature,
	}
	if err := p.SaveLock(lf); err != nil {
		return fmt.Errorf("project install %q: %w", s.ID, err)
	}
	// Snapshot the just-installed canonical state as the new merge base.
	// Failure here is fatal: without a base the next diverged resolve has
	// no common ancestor to consult.
	if err := p.SnapshotBase(s.ID); err != nil {
		return fmt.Errorf("project install %q: %w", s.ID, err)
	}
	return nil
}

func (p *project) UninstallSkill(id string) error {
	if id == "" {
		return fmt.Errorf("project uninstall: %w: empty id", adept.ErrSkillInvalid)
	}
	dir := filepath.Join(p.SkillsDir(), id)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("project uninstall %q: %w", id, err)
	}
	// Drop the merge-base snapshot too so a future re-install starts clean.
	if err := os.RemoveAll(p.BaseDirForSkill(id)); err != nil {
		return fmt.Errorf("project uninstall %q: clear base: %w", id, err)
	}
	lf, err := p.Lock()
	if err != nil {
		return fmt.Errorf("project uninstall %q: %w", id, err)
	}
	if _, ok := lf.Skills[id]; !ok {
		return nil
	}
	delete(lf.Skills, id)
	if err := p.SaveLock(lf); err != nil {
		return fmt.Errorf("project uninstall %q: %w", id, err)
	}
	return nil
}

func (p *project) writeSkillFiles(s *adept.Skill, files []adept.SkillFile) error {
	body, err := renderCanonical(s)
	if err != nil {
		return err
	}
	skillPath := filepath.Join(p.SkillsDir(), s.ID, adept.SkillFileName)
	if err := p.writer.AtomicWrite(skillPath, body, 0o644); err != nil {
		return fmt.Errorf("write SKILL.md: %w", err)
	}
	for _, f := range files {
		if f.RelPath == "" {
			return fmt.Errorf("write sidecar: empty rel path")
		}
		if filepath.IsAbs(f.RelPath) {
			return fmt.Errorf("write sidecar %q: must be relative", f.RelPath)
		}
		clean := filepath.Clean(f.RelPath)
		if strings.HasPrefix(clean, "..") {
			return fmt.Errorf("write sidecar %q: escapes skill dir", f.RelPath)
		}
		mode := f.Mode
		if mode == 0 {
			mode = 0o644
		}
		dst := filepath.Join(p.SkillsDir(), s.ID, clean)
		if err := p.writer.AtomicWrite(dst, f.Bytes, mode); err != nil {
			return fmt.Errorf("write sidecar %q: %w", f.RelPath, err)
		}
	}
	return nil
}

func (p *project) loadSidecars(id string) ([]adept.SkillFile, error) {
	root := filepath.Join(p.SkillsDir(), id)
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
// library and project produce byte-identical output for the same Skill.
func renderCanonical(s *adept.Skill) ([]byte, error) {
	return canonical.RenderCanonical(s)
}
