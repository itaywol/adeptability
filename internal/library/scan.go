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
	"github.com/itaywol/adeptability/internal/hash"
	"github.com/itaywol/adeptability/pkg/adept"
)

// Scanner walks candidate directories looking for SKILL.md files. It is used
// by the CLI's `scan` command to surface skills that exist on disk but have
// not yet been registered with the library.
type Scanner interface {
	Scan(roots []string) ([]ScanResult, error)
}

// ScanResult describes a single SKILL.md found on disk and its sync state
// relative to the library.
type ScanResult struct {
	SkillID     string
	SourcePath  string
	Hash        string
	LibraryHash string // empty if the skill is not in the library
	Status      adept.Status
}

// NewScanner returns a Scanner backed by the given library, parser and hasher.
// The scanner reads files using os; it does NOT mutate the library.
func NewScanner(lib Library, parser canonical.Parser, hasher hash.Hasher) Scanner {
	return &scanner{lib: lib, parser: parser, hasher: hasher}
}

type scanner struct {
	lib    Library
	parser canonical.Parser
	hasher hash.Hasher
}

func (s *scanner) Scan(roots []string) ([]ScanResult, error) {
	if len(roots) == 0 {
		return nil, nil
	}
	var results []ScanResult
	seen := map[string]struct{}{}
	for _, root := range roots {
		root = filepath.Clean(root)
		info, err := os.Stat(root)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("scan: stat %q: %w", root, err)
		}
		if !info.IsDir() {
			continue
		}
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				// Skip dirs we can't read but keep walking.
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				// Skip common noise.
				if shouldSkipDir(d.Name()) {
					return fs.SkipDir
				}
				return nil
			}
			if d.Name() != adept.SkillFileName {
				return nil
			}
			abs, absErr := filepath.Abs(path)
			if absErr != nil {
				abs = path
			}
			if _, dup := seen[abs]; dup {
				return nil
			}
			seen[abs] = struct{}{}
			res, parseErr := s.classify(abs)
			if parseErr != nil {
				// A malformed SKILL.md should not abort an entire scan; record
				// it with empty id and diverged status so the caller can show
				// the file to the user.
				results = append(results, ScanResult{SourcePath: abs, Status: adept.StatusDiverged})
				return nil
			}
			results = append(results, res)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("scan: walk %q: %w", root, err)
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].SkillID != results[j].SkillID {
			return results[i].SkillID < results[j].SkillID
		}
		return results[i].SourcePath < results[j].SourcePath
	})
	return results, nil
}

func (s *scanner) classify(path string) (ScanResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ScanResult{}, fmt.Errorf("read %q: %w", path, err)
	}
	skill, body, err := s.parser.ParseFrontmatter(data)
	if err != nil {
		return ScanResult{}, err
	}
	skill.Body = body
	skill.Files = sidecarsFor(path)
	digest, err := s.hasher.HashSkill(skill)
	if err != nil {
		return ScanResult{}, fmt.Errorf("hash %q: %w", path, err)
	}
	res := ScanResult{
		SkillID:    skill.ID,
		SourcePath: path,
		Hash:       digest,
	}
	if !s.lib.HasSkill(skill.ID) {
		res.Status = adept.StatusLocalOnly
		return res, nil
	}
	libSkill, err := s.lib.GetSkill(skill.ID)
	if err != nil {
		return res, nil
	}
	libHash, err := s.hasher.HashSkill(libSkill)
	if err != nil {
		return res, nil
	}
	res.LibraryHash = libHash
	switch {
	case digest == libHash:
		res.Status = adept.StatusSynced
	case skill.Version > libSkill.Version:
		res.Status = adept.StatusAhead
	case skill.Version < libSkill.Version:
		res.Status = adept.StatusBehind
	default:
		res.Status = adept.StatusDiverged
	}
	return res, nil
}

// sidecarsFor enumerates files alongside the SKILL.md, used to compute the
// content hash for status comparison. We deliberately read all files in the
// skill's parent directory.
func sidecarsFor(skillPath string) []adept.SkillFile {
	dir := filepath.Dir(skillPath)
	var out []adept.SkillFile
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return nil
		}
		if rel == adept.SkillFileName {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		out = append(out, adept.SkillFile{
			RelPath: filepath.ToSlash(rel),
			Mode:    info.Mode(),
			Bytes:   data,
		})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].RelPath < out[j].RelPath })
	return out
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "dist", "build", ".venv", "__pycache__", ".cache":
		return true
	}
	// Skip hidden dirs except the adeptability dir itself, which can hold
	// canonical skills the user may want to surface.
	if strings.HasPrefix(name, ".") && name != adept.BaseDirName {
		return true
	}
	return false
}
