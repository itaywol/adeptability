package adept

// ConfigSchemaVersion is the current on-disk config schema version.
const ConfigSchemaVersion = 1

// HarnessMode controls how harness output is materialized on disk.
type HarnessMode string

const (
	ModeSymlink HarnessMode = "symlink"
	ModeCopy    HarnessMode = "copy"
)

// OrgRef points a project at a centralized org registry.
type OrgRef struct {
	Remote string `json:"remote"`
	Ref    string `json:"ref,omitempty"`
}

// Config is the on-disk project configuration (.adeptability/config.json).
//
// It carries ONLY project-level state — which harnesses are enabled, how to
// materialize them, where the org registry lives, and which user adapters
// are registered. Per-skill state (hashes, versions) does not live here:
// the filesystem itself is the source of truth.
//
// - Project canonical at <root>/.adeptability/skills/<id>/ is "ours"
// - Last-synced snapshot at <root>/.adeptability/base/<id>/ is the base
//   (common ancestor for the 3-way status machine and merge)
// - Library at $ADEPT_LIBRARY/skills/<id>/ is "theirs"
//
// Status, push, pull, and resolve all derive their state by hashing those
// three directories on demand.
type Config struct {
	Schema       int                    `json:"schema"`
	Harnesses    []string               `json:"harnesses,omitempty"`
	HarnessModes map[string]HarnessMode `json:"harnessModes,omitempty"`
	Org          *OrgRef                `json:"org,omitempty"`
	Adapters     []string               `json:"adapters,omitempty"`
}
