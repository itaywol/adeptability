package adapter

import (
	"context"
	"fmt"

	"github.com/itaywol/adeptability/pkg/adept"
)

// Import on a config-driven (synthetic) adapter is not supported in v0.1.
// Generic reverse parsing requires the adapter to specify how to extract
// metadata from arbitrary harness file layouts — a v0.2 feature.
//
// We return an empty slice (not an error) so a bulk `harness import --id all`
// flow doesn't fail when synthetic adapters are registered. The CLI surfaces
// this via a per-harness warning in the import report.
func (a *syntheticAdapter) Import(_ context.Context, _ string) ([]adept.ImportedSkill, error) {
	return nil, fmt.Errorf("synthetic adapter %s: %w", a.spec.ID, errImportUnsupported)
}

var errImportUnsupported = fmt.Errorf("import not supported for config-driven adapters in v0.1")

// ErrImportUnsupported is the sentinel callers can match against to skip
// non-importable adapters during bulk import.
var ErrImportUnsupported = errImportUnsupported
