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
	Scan      *ScanConfig  `json:"scan,omitempty"`
	LLM       *LLMConfig   `json:"llm,omitempty"`
}

// ScanConfig controls the safety scan behavior. Pointer fields mean
// "unset → use built-in default" so users can leave only the keys they
// actually want to override in the on-disk JSON.
type ScanConfig struct {
	// OnInstall toggles the scan gate. When nil the runtime default is
	// "on if an LLM provider is configured, off otherwise". Explicit
	// true/false overrides that heuristic.
	OnInstall *bool `json:"onInstall,omitempty"`
	// BlockSeverity is the lowest severity that aborts an install.
	// One of "critical" | "high" | "medium". Default "critical".
	BlockSeverity string `json:"blockSeverity,omitempty"`
}

// LLMConfig records WHICH provider/model adept uses for the optional
// intent-evaluation pass. API keys are intentionally NOT stored here —
// the provider implementations resolve them from environment variables
// at call time so secrets never end up in the project tree.
type LLMConfig struct {
	Provider string `json:"provider,omitempty"` // anthropic | ollama
	Model    string `json:"model,omitempty"`
	Endpoint string `json:"endpoint,omitempty"` // optional (ollama custom host)
}
