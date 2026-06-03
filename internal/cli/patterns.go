package cli

import (
	"regexp"

	"github.com/itaywol/adeptability/pkg/adept"
)

// skillIDPattern is the single source of truth for skill-id validation at the
// command layer, compiled from adept.SkillIDPattern so input validation can
// never drift from the canonical schema.
var skillIDPattern = regexp.MustCompile(adept.SkillIDPattern)

// libraryNamePattern matches a short, filesystem-safe library name. Library
// names are a local namespace (a subdirectory under $ADEPT_LIBRARY/libs/<name>/),
// not a harness-rendered identifier, so they are not bound by the stricter
// harness id charset and may contain underscores.
var libraryNamePattern = regexp.MustCompile(`^[a-z0-9_][a-z0-9_-]{0,49}$`)
