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
	"regexp"
	"sort"
	"strings"

	"github.com/itaywol/adeptability/internal/canonical"
	"github.com/itaywol/adeptability/internal/config"
	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/internal/hash"
	"github.com/itaywol/adeptability/pkg/adept"
)

// skillIDRE guards skill ids before they are joined into filesystem paths.
// Defense in depth: the canonical loader and CLI already validate ids, but a
// public store method must never join an unvalidated id (e.g. one containing
// `..` or a path separator) onto a directory it then creates or deletes.
var skillIDRE = regexp.MustCompile(adept.SkillIDPattern)

// Project is the contract for read/write operations against a project's
// .adeptability directory. The root passed to New is the project root, not
// the .adeptability subdir.
type Project interface {
	Root() string
	BaseDir() string          // <root>/.adeptability
	SkillsDir() string        // <root>/.adeptability/skills (consumer) or <root>/skills (library layout)
	BaseSnapshotsDir() string // <root>/.adeptability/base
	ConfigPath() string       // <root>/.adeptability/config.json
	BaseDirForSkill(id string) string

	// PrivateSkillsDir is the private dev-canonical directory, used only in
	// the library layout: <root>/.adeptability/skills/. Skills there render to
	// the maintainer's harnesses (via sync) but are NOT published to consumers
	// (who read <root>/skills/). Returns "" in the consumer layout, where that
	// path is already the single published canonical (SkillsDir).
	PrivateSkillsDir() string

	// Config loads the project config. Missing file = empty config.
	Config() (*adept.Config, error)
	// SaveConfig atomically persists cfg to ConfigPath().
	SaveConfig(cfg *adept.Config) error

	HasSkill(id string) bool
	GetSkill(id string) (*adept.Skill, error)
	ListSkills() ([]*adept.Skill, error)

	// Private dev-canonical accessors (library layout only). In the consumer
	// layout HasPrivateSkill is always false, GetPrivateSkill returns
	// ErrSkillNotFound, ListPrivateSkills returns nil, and the install/remove
	// variants error — there is no private canonical to write to.
	HasPrivateSkill(id string) bool
	GetPrivateSkill(id string) (*adept.Skill, error)
	ListPrivateSkills() ([]*adept.Skill, error)
	// InstallPrivateSkill writes canonical files into PrivateSkillsDir. Unlike
	// InstallSkill it takes NO base snapshot: private skills are dev-only and
	// never pulled/pushed against a remote, so they have no merge base.
	InstallPrivateSkill(s *adept.Skill, files []adept.SkillFile) error
	// RemovePrivateSkill deletes a skill from PrivateSkillsDir.
	RemovePrivateSkill(id string) error

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

// New constructs a Project rooted at the given absolute project path in the
// default consumer layout (canonical skills under <root>/.adeptability/skills/).
// The .adeptability subdirectory does not need to exist; InstallSkill creates it.
func New(root string, parser canonical.Parser, hasher hash.Hasher, store config.Store, w fsutil.Writer) Project {
	return &project{
		root:   root,
		parser: parser,
		hasher: hasher,
		store:  store,
		writer: w,
	}
}

// NewLibrary constructs a Project in the library layout: canonical skills live
// at <root>/skills/ (so the repo is directly consumable as an adept library)
// while adept metadata — config.json and base snapshots — stays under
// <root>/.adeptability/. Use this when Config.Layout == adept.LayoutLibrary.
func NewLibrary(root string, parser canonical.Parser, hasher hash.Hasher, store config.Store, w fsutil.Writer) Project {
	return &project{
		root:          root,
		parser:        parser,
		hasher:        hasher,
		store:         store,
		writer:        w,
		libraryLayout: true,
	}
}

type project struct {
	root   string
	parser canonical.Parser
	hasher hash.Hasher
	store  config.Store
	writer fsutil.Writer
	// libraryLayout places canonical skills at <root>/skills/ instead of
	// <root>/.adeptability/skills/. See NewLibrary.
	libraryLayout bool
}

func (p *project) Root() string    { return p.root }
func (p *project) BaseDir() string { return filepath.Join(p.root, adept.BaseDirName) }
func (p *project) SkillsDir() string {
	if p.libraryLayout {
		return filepath.Join(p.root, adept.SkillsDirName)
	}
	return filepath.Join(p.BaseDir(), adept.SkillsDirName)
}

// PrivateSkillsDir is the private dev-canonical dir, only meaningful in the
// library layout: <root>/.adeptability/skills/. Empty in consumer layout,
// where that path is already the single published canonical (SkillsDir).
func (p *project) PrivateSkillsDir() string {
	if !p.libraryLayout {
		return ""
	}
	return filepath.Join(p.BaseDir(), adept.SkillsDirName)
}
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

func (p *project) HasPrivateSkill(id string) bool {
	dir := p.PrivateSkillsDir()
	if dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, id, adept.SkillFileName))
	return err == nil
}

func (p *project) GetPrivateSkill(id string) (*adept.Skill, error) {
	dir := p.PrivateSkillsDir()
	if dir == "" {
		return nil, fmt.Errorf("project get private %q: %w", id, adept.ErrSkillNotFound)
	}
	return p.getSkillFromDir(dir, id)
}

func (p *project) InstallPrivateSkill(s *adept.Skill, files []adept.SkillFile) error {
	if s == nil {
		return fmt.Errorf("project install private: %w: nil skill", adept.ErrSkillInvalid)
	}
	if !skillIDRE.MatchString(s.ID) {
		return fmt.Errorf("project install private: %w: id %q does not match %s", adept.ErrSkillInvalid, s.ID, adept.SkillIDPattern)
	}
	dir := p.PrivateSkillsDir()
	if dir == "" {
		return fmt.Errorf("project install private %q: no private canonical (not a library project)", s.ID)
	}
	if err := p.writeSkillFilesToDir(dir, s, files); err != nil {
		return fmt.Errorf("project install private %q: %w", s.ID, err)
	}
	return nil
}

func (p *project) RemovePrivateSkill(id string) error {
	if !skillIDRE.MatchString(id) {
		return fmt.Errorf("project remove private: %w: id %q does not match %s", adept.ErrSkillInvalid, id, adept.SkillIDPattern)
	}
	dir := p.PrivateSkillsDir()
	if dir == "" || !p.HasPrivateSkill(id) {
		return fmt.Errorf("project remove private %q: %w", id, adept.ErrSkillNotFound)
	}
	if err := os.RemoveAll(filepath.Join(dir, id)); err != nil {
		return fmt.Errorf("project remove private %q: %w", id, err)
	}
	return nil
}

func (p *project) GetSkill(id string) (*adept.Skill, error) {
	return p.getSkillFromDir(p.SkillsDir(), id)
}

func (p *project) getSkillFromDir(dir, id string) (*adept.Skill, error) {
	if id == "" {
		return nil, fmt.Errorf("project get: %w: empty id", adept.ErrSkillInvalid)
	}
	skillPath := filepath.Join(dir, id, adept.SkillFileName)
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
	// Directory name is authoritative — overrides any in-file id/name
	// that drifted away from the on-disk layout (renames, installs from
	// external slugs, etc.).
	skill.ID = id
	skill.Body = body
	files, err := p.loadSidecarsFromDir(dir, id)
	if err != nil {
		return nil, fmt.Errorf("project get %q: %w", id, err)
	}
	skill.Files = files
	return skill, nil
}

func (p *project) ListSkills() ([]*adept.Skill, error) {
	return p.listSkillsInDir(p.SkillsDir())
}

// ListPrivateSkills lists skills in PrivateSkillsDir. Returns nil in the
// consumer layout (no private canonical there).
func (p *project) ListPrivateSkills() ([]*adept.Skill, error) {
	dir := p.PrivateSkillsDir()
	if dir == "" {
		return nil, nil
	}
	return p.listSkillsInDir(dir)
}

func (p *project) listSkillsInDir(dir string) ([]*adept.Skill, error) {
	ids, err := p.listSkillIDsIn(dir)
	if err != nil {
		return nil, err
	}
	out := make([]*adept.Skill, 0, len(ids))
	for _, id := range ids {
		s, err := p.getSkillFromDir(dir, id)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func (p *project) listSkillIDsIn(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
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
		_, err := os.Stat(filepath.Join(dir, e.Name(), adept.SkillFileName))
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
	if !skillIDRE.MatchString(s.ID) {
		return fmt.Errorf("project install: %w: id %q does not match %s", adept.ErrSkillInvalid, s.ID, adept.SkillIDPattern)
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
	if !skillIDRE.MatchString(id) {
		return fmt.Errorf("project uninstall: %w: id %q does not match %s", adept.ErrSkillInvalid, id, adept.SkillIDPattern)
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
	return p.writeSkillFilesToDir(p.SkillsDir(), s, files)
}

func (p *project) writeSkillFilesToDir(dir string, s *adept.Skill, files []adept.SkillFile) error {
	body, err := renderCanonical(s)
	if err != nil {
		return err
	}
	skillPath := filepath.Join(dir, s.ID, adept.SkillFileName)
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
		dst := filepath.Join(dir, s.ID, clean)
		if err := p.writer.AtomicWrite(dst, f.Bytes, mode); err != nil {
			return fmt.Errorf("write sidecar %q: %w", f.RelPath, err)
		}
	}
	return nil
}

func (p *project) loadSidecarsFromDir(dir, id string) ([]adept.SkillFile, error) {
	root := filepath.Join(dir, id)
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
