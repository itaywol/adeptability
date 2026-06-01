package common_test

import (
	"testing"

	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/internal/render/common"
)

// TestLinkerSatisfiedByFsutil locks in that the canonical fsutil.Linker
// satisfies the common.Linker interface. If fsutil drifts (e.g. PathType
// return type changes), this test breaks at compile time.
func TestLinkerSatisfiedByFsutil(t *testing.T) {
	t.Parallel()
	var _ common.Linker = fsutil.NewLinker(nil)
}

// TestWriterSatisfiedByFsutil locks in that fsutil.Writer satisfies our
// common.Writer surface.
func TestWriterSatisfiedByFsutil(t *testing.T) {
	t.Parallel()
	var _ common.Writer = fsutil.NewWriter()
}
