package adept

// LockSchemaVersion is the current lockfile schema version.
const LockSchemaVersion = 2

// HarnessMode controls how harness output is materialized on disk.
type HarnessMode string

const (
	ModeSymlink HarnessMode = "symlink"
	ModeCopy    HarnessMode = "copy"
)

// LockEntry is the per-skill record in a lockfile.
type LockEntry struct {
	Version   int      `json:"version"`
	Hash      string   `json:"hash"`
	Targets   []string `json:"targets,omitempty"`
	UpdatedAt string   `json:"updatedAt,omitempty"`
	Signature string   `json:"signature,omitempty"`
}

// OrgRef points a project lockfile at a centralized org registry.
type OrgRef struct {
	Remote string `json:"remote"`
	Ref    string `json:"ref,omitempty"`
}

// LockFile is the on-disk lockfile format (schema=2).
type LockFile struct {
	Schema       int                    `json:"schema"`
	Skills       map[string]LockEntry   `json:"skills"`
	Harnesses    []string               `json:"harnesses,omitempty"`
	HarnessModes map[string]HarnessMode `json:"harnessModes,omitempty"`
	Org          *OrgRef                `json:"org,omitempty"`
	Adapters     []string               `json:"adapters,omitempty"`
}
