package adept

// On-disk layout constants used across packages.
const (
	BaseDirName    = ".adeptability"
	SkillsDirName  = "skills"
	BaseSnapDir    = "base"
	StagingDir     = "staging"
	SkillFileName  = "SKILL.md"
	SkillYAMLName  = "skill.yaml"
	ConfigFileName = "config.json"
	SignatureName  = ".signature"
	IgnoreFileName = ".adeptignore"
	AdaptersDir    = "adapters"
	OrgFileName    = "org.yaml"

	// LibraryEnvVar overrides the default library location.
	LibraryEnvVar = "ADEPT_LIBRARY"
	// DefaultLibraryDir is the conventional library path under $HOME.
	DefaultLibraryDir = ".adeptability"

	// ExchangeServerEnvVar overrides the stored default billboard server URL.
	ExchangeServerEnvVar = "ADEPT_EXCHANGE_SERVER"
	// ExchangeTokenEnvVar overrides the on-disk billboard bearer token.
	ExchangeTokenEnvVar = "ADEPT_EXCHANGE_TOKEN"
	// ExchangeDirName is the subdir under the library root holding billboard
	// credentials (one file per server host, mode 0600).
	ExchangeDirName = "exchange"

	// LayoutLibrary marks a project initialized as a publishable library
	// (Config.Layout). In this layout canonical skills live at <root>/skills/
	// — the same place a consumer's `library add`/`init --from` reads them —
	// rather than the default consumer location <root>/.adeptability/skills/.
	// adept metadata (config.json, base snapshots) still lives under
	// .adeptability/. An empty/absent Layout means the default consumer layout.
	LayoutLibrary = "library"
)

// SkillIDPattern is the validation regex for skill ids. Kept as a string here so
// pkg/adept stays import-free; internal/canonical compiles it.
//
// The charset is the lowest common denominator across harnesses: lowercase
// ASCII letters, digits, and internal hyphens, no leading/trailing hyphen and
// no underscore. This matches the Agent Skills `name` rule (Claude Code,
// OpenCode) so a valid canonical id always renders a valid harness name and
// directory. Length is capped at 50 characters.
const SkillIDPattern = `^[a-z0-9](?:[a-z0-9-]{0,48}[a-z0-9])?$`
