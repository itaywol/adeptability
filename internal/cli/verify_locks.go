package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/itaywol/adeptability/internal/locks"
	"github.com/itaywol/adeptability/internal/project"
)

// verifyExternalLocks walks the project's lockfile and re-hashes each
// locked skill's on-disk content. Drift surfaces as a warning on stderr
// — never a hard failure — so a sync still completes when a user edited
// an upstream skill locally (the next `skill update` reconciles it).
//
// The intent is to keep the user honest: external skills are pinned for
// reproducibility, so any local edit is worth knowing about. Phase 2
// will route this same signal through the LLM check to summarize what
// changed.
func verifyExternalLocks(d *Deps, p project.Project, stderr io.Writer) {
	lock, err := locks.Load(lockPath(p))
	if err != nil {
		d.Log.Warn("verify locks: load", "err", err)
		return
	}
	if len(lock.External) == 0 {
		return
	}
	for _, id := range lock.IDs() {
		entry, _ := lock.Get(id)
		dir := filepath.Join(p.SkillsDir(), id)
		hash, err := hashSkillDir(dir)
		if err != nil {
			fmt.Fprintf(stderr, "warn: lock %s: cannot hash %s: %v\n", id, dir, err)
			continue
		}
		if hash == "" {
			fmt.Fprintf(stderr, "warn: lock %s: project copy missing (run `adept skill install %s` to restore)\n", id, entry.Slug)
			continue
		}
		if hash != entry.ContentHash {
			fmt.Fprintf(stderr,
				"warn: lock %s: drifted from upstream pin (sha=%s)\n  recorded: %s\n  on-disk:  %s\n  run `adept skill update %s` to refresh or `adept skill remove %s` then re-install\n",
				id, shortSHA(entry.SHA), entry.ContentHash, hash, id, id)
		}
	}
}

// hashSkillDir mirrors hashFiles (commands_skill_external.go) but reads
// from disk. Returns "" + nil when dir does not exist so the caller can
// distinguish "missing" from "drifted".
func hashSkillDir(dir string) (string, error) {
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	type file struct {
		rel  string
		body []byte
	}
	var files []file
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, file{rel: filepath.ToSlash(rel), body: body})
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f.rel))
		h.Write([]byte{0})
		h.Write(f.body)
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
