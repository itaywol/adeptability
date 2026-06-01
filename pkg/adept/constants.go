package adept

// On-disk layout constants used across packages.
const (
	BaseDirName     = ".adeptability"
	SkillsDirName   = "skills"
	BaseSnapDir     = "base"
	StagingDir      = "staging"
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
const SkillIDPattern = `^[a-z0-9_][a-z0-9_-]{0,49}$`
