package adept

// ConfigSchemaVersion is the current on-disk config schema version.
const ConfigSchemaVersion = 1

// HarnessMode controls how harness output is materialized on disk.
type HarnessMode string

const (
	ModeSymlink HarnessMode = "symlink"
	ModeCopy    HarnessMode = "copy"
)

// LibraryRef points a project at one named remote skill library. Multiple
// libraries are stacked in Config.Libraries; first-match wins on cross-
// library skill-id collisions.
type LibraryRef struct {
	Name   string `json:"name"`
	Remote string `json:"remote"`
	Ref    string `json:"ref,omitempty"`
}

// Config is the on-disk project configuration (.adeptability/config.json).
//
// It carries ONLY project-level state — which harnesses are enabled, how to
// materialize them, and which libraries the project pulls skills from.
// Per-skill state (hashes, versions) does not live here: the filesystem
// itself is the source of truth.
//
// - Project canonical at <root>/.adeptability/skills/<id>/ is "ours"
// - Last-synced snapshot at <root>/.adeptability/base/<id>/ is the base
//   (common ancestor for the 3-way status machine and merge)
// - Library at $ADEPT_LIBRARY/libs/<name>/skills/<id>/ is the upstream
//   source. Multiple libraries supported via Libraries[]; project canonical
//   shadows library skills sharing the same id.
type Config struct {
	Schema    int          `json:"schema"`
	Harnesses []string     `json:"harnesses,omitempty"`
	Mode      HarnessMode  `json:"mode,omitempty"`
	Libraries []LibraryRef `json:"libraries,omitempty"`
}
