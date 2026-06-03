package cli

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/internal/project"
	"github.com/itaywol/adeptability/internal/scan"
	"github.com/itaywol/adeptability/pkg/adept"
)

// scanGateRegressStubProject is a minimal project.Project whose only
// meaningful behavior is returning a fixed Config. All other methods
// return zero values; installBlocks only consults Config().
type scanGateRegressStubProject struct {
	cfg *adept.Config
}

func (s scanGateRegressStubProject) Root() string                   { return "" }
func (s scanGateRegressStubProject) BaseDir() string                { return "" }
func (s scanGateRegressStubProject) SkillsDir() string              { return "" }
func (s scanGateRegressStubProject) BaseSnapshotsDir() string       { return "" }
func (s scanGateRegressStubProject) ConfigPath() string             { return "" }
func (s scanGateRegressStubProject) BaseDirForSkill(string) string  { return "" }
func (s scanGateRegressStubProject) Config() (*adept.Config, error) { return s.cfg, nil }
func (s scanGateRegressStubProject) SaveConfig(*adept.Config) error { return nil }
func (s scanGateRegressStubProject) HasSkill(string) bool           { return false }
func (s scanGateRegressStubProject) GetSkill(string) (*adept.Skill, error) {
	return nil, nil
}
func (s scanGateRegressStubProject) ListSkills() ([]*adept.Skill, error) { return nil, nil }
func (s scanGateRegressStubProject) HashSkill(string) (string, error)    { return "", nil }
func (s scanGateRegressStubProject) HashBase(string) (string, error)     { return "", nil }
func (s scanGateRegressStubProject) HasBaseSnapshot(string) bool         { return false }
func (s scanGateRegressStubProject) SnapshotBase(string) error           { return nil }
func (s scanGateRegressStubProject) InstallSkill(*adept.Skill, []adept.SkillFile) error {
	return nil
}
func (s scanGateRegressStubProject) UninstallSkill(string) error { return nil }

var _ project.Project = scanGateRegressStubProject{}

func scanGateRegressReport(worst scan.Severity) scan.Report {
	return scan.Report{Findings: []scan.Finding{{ID: "X", Severity: worst}}}
}

// Regression: an invalid / mis-cased blockSeverity must NOT silently
// disable the gate. It must fail closed to critical.
func TestInstallBlocksRegress_InvalidBlockSeverityFailsClosed(t *testing.T) {
	cfg := &adept.Config{Scan: &adept.ScanConfig{BlockSeverity: "HIGH-ish-typo"}}
	p := scanGateRegressStubProject{cfg: cfg}
	d := &Deps{}

	// A high finding must NOT block under the fail-closed (critical) default.
	require.False(t, installBlocks(d, p, scanGateRegressReport(scan.SeverityHigh)),
		"invalid blockSeverity must default to critical, so high does not block")
	// A critical finding still blocks.
	require.True(t, installBlocks(d, p, scanGateRegressReport(scan.SeverityCritical)))
}

// Mis-cased but otherwise valid severity must be normalized, not
// rejected.
func TestInstallBlocksRegress_MiscasedBlockSeverityNormalized(t *testing.T) {
	cfg := &adept.Config{Scan: &adept.ScanConfig{BlockSeverity: "HIGH"}}
	p := scanGateRegressStubProject{cfg: cfg}
	d := &Deps{}
	require.True(t, installBlocks(d, p, scanGateRegressReport(scan.SeverityHigh)),
		"mis-cased HIGH must normalize and block at high")
}

// Valid lowercase threshold behaves as documented.
func TestInstallBlocksRegress_ValidThreshold(t *testing.T) {
	cfg := &adept.Config{Scan: &adept.ScanConfig{BlockSeverity: "medium"}}
	p := scanGateRegressStubProject{cfg: cfg}
	d := &Deps{}
	require.True(t, installBlocks(d, p, scanGateRegressReport(scan.SeverityHigh)))
	require.True(t, installBlocks(d, p, scanGateRegressReport(scan.SeverityMedium)))
	require.False(t, installBlocks(d, p, scanGateRegressReport(scan.SeverityLow)))
}

// severityRank in this package must also fail closed for unknown values.
func TestSeverityRankRegress_FailsClosed(t *testing.T) {
	require.Equal(t, 4, severityRank(scan.Severity("nonsense")))
	require.Equal(t, 0, severityRank(scan.SeverityClean))
}

// Regression: ErrScanFindings must map to exit code 2 (via ErrDirty),
// distinct from generic errors which map to 1.
func TestSkillCheckRegress_ScanFindingsExitCode(t *testing.T) {
	require.Equal(t, 2, ExitFromError(ErrScanFindings))
	wrapped := errors.New("scan: high-severity findings present")
	require.Equal(t, 1, ExitFromError(wrapped), "a plain error stays exit 1")
	// errors.Is must see through the wrap chain to ErrDirty.
	require.True(t, errors.Is(ErrScanFindings, ErrDirty))
}
