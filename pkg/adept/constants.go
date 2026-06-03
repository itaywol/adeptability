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
