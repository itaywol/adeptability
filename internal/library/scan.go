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

// ErrScanRootMissing is returned when a scan root does not exist on disk.
// The CLI surfaces this as exit 1 instead of silently printing an empty
// table (FRICTION BUG 6).
var ErrScanRootMissing = errors.New("scan root missing")

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
				return nil, fmt.Errorf("%w: %s", ErrScanRootMissing, root)
			}
			return nil, fmt.Errorf("scan: stat %q: %w", root, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%w: %s is not a directory", ErrScanRootMissing, root)
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
				// An unreadable/unparseable copy cannot be confirmed equal to
				// the library, so report it as diverged and keep scanning
				// rather than aborting the whole walk.
				res = ScanResult{SourcePath: abs, Status: adept.StatusDiverged}
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
		// The library copy exists (HasSkill was true) but cannot be
		// read/parsed. We cannot confirm it is equal to the on-disk source,
		// so mark it diverged rather than emitting an empty Status that would
		// render as a blank, misleading status row. This matches the
		// parse-failure branch in Scan.
		res.Status = adept.StatusDiverged
		return res, nil //nolint:nilerr // unreadable library copy is reported as diverged, not a hard error
	}
	libHash, err := s.hasher.HashSkill(libSkill)
	if err != nil {
		// The library copy parsed but could not be hashed; equality is
		// indeterminable. Treat as diverged for the same reason.
		res.Status = adept.StatusDiverged
		return res, nil //nolint:nilerr // unhashable library copy is reported as diverged, not a hard error
	}
	res.LibraryHash = libHash
	if digest == libHash {
		res.Status = adept.StatusSynced
	} else {
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
			return nil //nolint:nilerr // unreadable sidecar entry is skipped, not a hard error
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return nil //nolint:nilerr // sidecar with no relative path is skipped, not a hard error
		}
		if rel == adept.SkillFileName {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil //nolint:nilerr // unreadable sidecar file is skipped, not a hard error
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil //nolint:nilerr // sidecar with no stat info is skipped, not a hard error
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
	if strings.HasPrefix(name, ".") && name != adept.BaseDirName {
		return true
	}
	return false
}
