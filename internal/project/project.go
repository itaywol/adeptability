// Package project implements operations against a single project's
// <root>/.adeptability/ directory.
//
// Layout:
//
//	<root>/.adeptability/
//	    config.json          — project state (harnesses, modes, org, adapters)
//	    skills/<id>/         — project canonical skill content ("ours")
//	    base/<id>/           — last-synced snapshot ("merge base")
//
// The library is "theirs". Status, push, pull, and resolve derive their state
// by hashing those three directories on demand — there is no per-skill
// lockfile.
package project

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/itaywol/adeptability/internal/canonical"
	"github.com/itaywol/adeptability/internal/config"
	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/internal/hash"
	"github.com/itaywol/adeptability/pkg/adept"
)

// Project is the contract for read/write operations against a project's
// .adeptability directory. The root passed to New is the project root, not
// the .adeptability subdir.
type Project interface {
	Root() string
	BaseDir() string                 // <root>/.adeptability
	SkillsDir() string               // <root>/.adeptability/skills
	BaseSnapshotsDir() string        // <root>/.adeptability/base
	ConfigPath() string              // <root>/.adeptability/config.json
	BaseDirForSkill(id string) string

	// Config loads the project config. Missing file = empty config.
	Config() (*adept.Config, error)
	// SaveConfig atomically persists cfg to ConfigPath().
	SaveConfig(cfg *adept.Config) error

	HasSkill(id string) bool
	GetSkill(id string) (*adept.Skill, error)
	ListSkills() ([]*adept.Skill, error)

	// HashSkill returns the content hash of the project canonical skill dir
	// (<skills>/<id>). Empty string + nil when not present.
	HashSkill(id string) (string, error)
	// HashBase returns the content hash of the base snapshot for id.
	// Empty string + nil when not present.
	HashBase(id string) (string, error)

	HasBaseSnapshot(id string) bool
	// SnapshotBase mirrors <skills>/<id>/ into <base>/<id>/, overwriting any
	// prior snapshot. Intended to be called after every successful install,
	// pull, push, or clean merge so future diverged resolves have an
	// accurate common ancestor.
	SnapshotBase(id string) error

	// InstallSkill writes canonical files for s into <skills>/<id>/ and
	// then snapshots the new state as the merge base.
	InstallSkill(s *adept.Skill, files []adept.SkillFile) error
	// UninstallSkill removes the project canonical directory and its base
	// snapshot. Returns adept.ErrSkillNotFound when id is not installed.
	UninstallSkill(id string) error
}

// New constructs a Project rooted at the given absolute project path. The
// .adeptability subdirectory does not need to exist; InstallSkill creates it.
func New(root string, parser canonical.Parser, hasher hash.Hasher, store config.Store, w fsutil.Writer) Project {
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
	store  config.Store
	writer fsutil.Writer
}

func (p *project) Root() string             { return p.root }
func (p *project) BaseDir() string          { return filepath.Join(p.root, adept.BaseDirName) }
func (p *project) SkillsDir() string        { return filepath.Join(p.BaseDir(), adept.SkillsDirName) }
func (p *project) BaseSnapshotsDir() string { return filepath.Join(p.BaseDir(), adept.BaseSnapDir) }
func (p *project) ConfigPath() string {
	return filepath.Join(p.BaseDir(), adept.ConfigFileName)
}
func (p *project) BaseDirForSkill(id string) string {
	return filepath.Join(p.BaseSnapshotsDir(), id)
}

func (p *project) HasBaseSnapshot(id string) bool {
	if id == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(p.BaseDirForSkill(id), adept.SkillFileName))
	return err == nil
}

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

func (p *project) Config() (*adept.Config, error) {
	cfg, err := p.store.Read(p.ConfigPath())
	if err != nil {
		return nil, fmt.Errorf("project config load: %w", err)
	}
	return cfg, nil
}

func (p *project) SaveConfig(cfg *adept.Config) error {
	if cfg == nil {
		return fmt.Errorf("project save config: nil config")
	}
	if err := p.writer.EnsureDir(p.BaseDir()); err != nil {
		return fmt.Errorf("project save config: ensure dir: %w", err)
	}
	if err := p.store.Write(p.ConfigPath(), cfg); err != nil {
		return fmt.Errorf("project save config: %w", err)
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

func (p *project) HashSkill(id string) (string, error) {
	if id == "" {
		return "", nil
	}
	dir := filepath.Join(p.SkillsDir(), id)
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("project hash %q: stat: %w", id, err)
	}
	return p.hasher.HashSkillDir(dir)
}

func (p *project) HashBase(id string) (string, error) {
	if id == "" {
		return "", nil
	}
	dir := p.BaseDirForSkill(id)
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("project hash base %q: stat: %w", id, err)
	}
	return p.hasher.HashSkillDir(dir)
}

func (p *project) InstallSkill(s *adept.Skill, files []adept.SkillFile) error {
	if s == nil {
		return fmt.Errorf("project install: %w: nil skill", adept.ErrSkillInvalid)
	}
	if s.ID == "" {
		return fmt.Errorf("project install: %w: empty id", adept.ErrSkillInvalid)
	}
	if err := p.writeSkillFiles(s, files); err != nil {
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
	if !p.HasSkill(id) {
		// FRICTION BUG 2: silent success on missing skill hides typos and
		// makes uninstall feel wrong. Surface a typed error so the CLI can
		// exit non-zero with a clear message.
		return fmt.Errorf("project uninstall %q: %w", id, adept.ErrSkillNotFound)
	}
	dir := filepath.Join(p.SkillsDir(), id)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("project uninstall %q: %w", id, err)
	}
	// Drop the merge-base snapshot so a future re-install starts clean.
	if err := os.RemoveAll(p.BaseDirForSkill(id)); err != nil {
		return fmt.Errorf("project uninstall %q: clear base: %w", id, err)
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
