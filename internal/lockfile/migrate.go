package lockfile

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/itaywol/adeptability/pkg/adept"
)

// skillbookSchema1 mirrors the relevant subset of the legacy skillbook
// lockfile (schema=1). Unknown fields are ignored.
type skillbookSchema1 struct {
	Schema       int                    `json:"schema"`
	Skills       map[string]legacyEntry `json:"skills"`
	Harnesses    []string               `json:"harnesses,omitempty"`
	HarnessModes map[string]string      `json:"harnessModes,omitempty"`
}

type legacyEntry struct {
	Version   int    `json:"version"`
	Hash      string `json:"hash"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// MigrateFromSkillbook reads a skillbook.lock.json (schema=1) and returns the
// equivalent adeptability lockfile (schema=2). Fields with no schema=1
// equivalent (Targets, Signature, Org, Adapters) are zero-valued.
func MigrateFromSkillbook(path string) (*adept.LockFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("lockfile: migrate read %s: %w", path, err)
	}
	var legacy skillbookSchema1
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, fmt.Errorf("lockfile: migrate parse %s: %w", path, err)
	}
	if legacy.Schema != 1 {
		return nil, fmt.Errorf("lockfile: migrate %s: %w: expected schema=1, got %d",
			path, adept.ErrLockSchemaMismatch, legacy.Schema)
	}
	out := &adept.LockFile{
		Schema: adept.LockSchemaVersion,
		Skills: map[string]adept.LockEntry{},
	}
	for id, e := range legacy.Skills {
		out.Skills[id] = adept.LockEntry{
			Version:   e.Version,
			Hash:      e.Hash,
			UpdatedAt: e.UpdatedAt,
		}
	}
	if len(legacy.Harnesses) > 0 {
		out.Harnesses = append(out.Harnesses, legacy.Harnesses...)
	}
	if len(legacy.HarnessModes) > 0 {
		out.HarnessModes = map[string]adept.HarnessMode{}
		for h, m := range legacy.HarnessModes {
			out.HarnessModes[h] = adept.HarnessMode(m)
		}
	}
	return out, nil
}
