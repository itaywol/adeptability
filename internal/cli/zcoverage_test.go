package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/internal/canonical"
	"github.com/itaywol/adeptability/internal/config"
	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/internal/hash"
	"github.com/itaywol/adeptability/internal/locks"
	"github.com/itaywol/adeptability/internal/log"
	"github.com/itaywol/adeptability/internal/project"
	"github.com/itaywol/adeptability/internal/scan"
	"github.com/itaywol/adeptability/pkg/adept"
)

// ---------- shared fixtures ----------

// skillMD returns a minimal-but-valid canonical SKILL.md with frontmatter so
// the parser and ListSkills do not reject it.
func skillMD(name, desc string) []byte {
	return []byte("---\nname: " + name + "\ndescription: " + desc + "\n---\nbody for " + name + "\n")
}

// testDeps builds a real *Deps wired against a temp project root and a temp
// library root. No network collaborators are exercised by the tests that use
// it; the discard logger keeps stderr clean while still driving log branches.
func testDeps(t *testing.T, projectRoot, libraryRoot string) *Deps {
	t.Helper()
	parser := canonical.NewParser()
	writer := fsutil.NewWriter()
	return &Deps{
		Flags:  &GlobalFlags{ProjectDir: projectRoot, LibraryDir: libraryRoot},
		Parser: parser,
		Hasher: hash.NewHasher(),
		Config: config.NewStore(writer.AtomicWrite),
		Writer: writer,
		Log:    log.NewLogger(log.Level("error"), false, io.Discard),
	}
}

// initProject lays down .adeptability/{skills,base}/ + config.json for root so
// collectStatus sees an initialized project and Project() round-trips config.
func initProject(t *testing.T, d *Deps, root string, cfg *adept.Config) project.Project {
	t.Helper()
	p, err := d.Project()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(p.SkillsDir(), 0o755))
	require.NoError(t, os.MkdirAll(p.BaseSnapshotsDir(), 0o755))
	require.NoError(t, p.SaveConfig(cfg))
	return p
}

// writeProjectSkill writes a parseable skill under the project canonical dir.
func writeProjectSkill(t *testing.T, p project.Project, id, desc string) {
	t.Helper()
	dir := filepath.Join(p.SkillsDir(), id)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, adept.SkillFileName), skillMD(id, desc), 0o644))
}

// writeLibrarySkill writes a parseable skill under libs/<lib>/skills/<id>/.
func writeLibrarySkill(t *testing.T, libraryRoot, lib, id, desc string) {
	t.Helper()
	dir := filepath.Join(libraryRoot, "libs", lib, adept.SkillsDirName, id)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, adept.SkillFileName), skillMD(id, desc), 0o644))
}

func ptrBool(b bool) *bool { return &b }

// ---------- commands_library.go: seedDefaultSkills ----------

func TestSeedDefaultSkills(t *testing.T) {
	root := t.TempDir()
	d := testDeps(t, root, t.TempDir())
	p := initProject(t, d, root, &adept.Config{})

	// First seed writes every bundled default.
	first, err := seedDefaultSkills(p)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"adept-self-improve", "authoring-adept-skills", "expertise-exchange", "using-adept"}, first)
	for _, id := range first {
		require.True(t, p.HasSkill(id))
		require.DirExists(t, filepath.Join(p.BaseSnapshotsDir(), id))
	}
	// Sidecar files are seeded alongside SKILL.md.
	require.FileExists(t, filepath.Join(p.SkillsDir(), "expertise-exchange", "references", "setup-and-usage.md"))

	// Re-seed is idempotent: nothing already-present is rewritten.
	second, err := seedDefaultSkills(p)
	require.NoError(t, err)
	require.Empty(t, second)

	// A pre-existing skill is left untouched (user content wins).
	writeProjectSkill(t, p, "using-adept", "user's own override")
	third, err := seedDefaultSkills(p)
	require.NoError(t, err)
	require.NotContains(t, third, "using-adept")
	body, err := os.ReadFile(filepath.Join(p.SkillsDir(), "using-adept", adept.SkillFileName))
	require.NoError(t, err)
	require.Contains(t, string(body), "user's own override")
}

// ---------- scan_gate.go: shouldScanOnInstall ----------

// nameProvider is the minimal interface shouldScanOnInstall accepts.
type nameProvider struct{ n string }

func (s nameProvider) Name() string { return s.n }

func TestShouldScanOnInstall(t *testing.T) {
	t.Parallel()
	prov := nameProvider{n: "anthropic"}
	tests := []struct {
		name string
		cfg  *adept.Config
		prov interface{ Name() string }
		want bool
	}{
		{"nil cfg never scans", nil, prov, false},
		{"explicit true + provider scans", &adept.Config{Scan: &adept.ScanConfig{OnInstall: ptrBool(true)}}, prov, true},
		{"explicit true + nil provider skips", &adept.Config{Scan: &adept.ScanConfig{OnInstall: ptrBool(true)}}, nil, false},
		{"explicit false never scans (provider present)", &adept.Config{Scan: &adept.ScanConfig{OnInstall: ptrBool(false)}}, prov, false},
		{"explicit false never scans (nil provider)", &adept.Config{Scan: &adept.ScanConfig{OnInstall: ptrBool(false)}}, nil, false},
		{"nil OnInstall + provider scans", &adept.Config{Scan: &adept.ScanConfig{}}, prov, true},
		{"nil OnInstall + nil provider skips", &adept.Config{Scan: &adept.ScanConfig{}}, nil, false},
		{"nil Scan + provider scans", &adept.Config{}, prov, true},
		{"nil Scan + nil provider skips", &adept.Config{}, nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// A typed-nil provider must be treated as nil; callers pass the
			// concrete interface so we pass nil directly when unset.
			require.Equal(t, tc.want, shouldScanOnInstall(tc.cfg, tc.prov))
		})
	}
}

// ---------- scan_gate.go: installBlocks ----------

func reportWorst(worst scan.Severity) scan.Report {
	if worst == scan.SeverityClean || worst == "" {
		return scan.Report{}
	}
	return scan.Report{Findings: []scan.Finding{{ID: "X", Severity: worst}}}
}

func TestInstallBlocks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		cfg   *adept.Config
		worst scan.Severity
		want  bool
	}{
		{"critical blocks at default threshold", &adept.Config{}, scan.SeverityCritical, true},
		{"high does not block at default (critical) threshold", &adept.Config{}, scan.SeverityHigh, false},
		{"high blocks when threshold=high", &adept.Config{Scan: &adept.ScanConfig{BlockSeverity: "high"}}, scan.SeverityHigh, true},
		{"medium does not block when threshold=high", &adept.Config{Scan: &adept.ScanConfig{BlockSeverity: "high"}}, scan.SeverityMedium, false},
		{"clean report never blocks", &adept.Config{}, scan.SeverityClean, false},
		{"invalid threshold fails closed to critical: high passes", &adept.Config{Scan: &adept.ScanConfig{BlockSeverity: "bogus"}}, scan.SeverityHigh, false},
		{"invalid threshold fails closed to critical: critical blocks", &adept.Config{Scan: &adept.ScanConfig{BlockSeverity: "bogus"}}, scan.SeverityCritical, true},
	}
	root := t.TempDir()
	d := testDeps(t, root, t.TempDir())
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := initProject(t, d, root, tc.cfg)
			require.Equal(t, tc.want, installBlocks(d, p, reportWorst(tc.worst)))
		})
	}
}

// TestSeverityRank_Ordering pins the monotonic ordering the gate relies on
// and asserts unknown/empty severities fail closed (unknown -> 4, empty -> 0).
func TestSeverityRank_Ordering(t *testing.T) {
	t.Parallel()
	require.Greater(t, scan.SeverityRank(scan.SeverityCritical), scan.SeverityRank(scan.SeverityHigh))
	require.Greater(t, scan.SeverityRank(scan.SeverityHigh), scan.SeverityRank(scan.SeverityMedium))
	require.Greater(t, scan.SeverityRank(scan.SeverityMedium), scan.SeverityRank(scan.SeverityLow))
	require.Greater(t, scan.SeverityRank(scan.SeverityLow), scan.SeverityRank(scan.SeverityClean))
	require.Equal(t, 0, scan.SeverityRank(scan.SeverityClean))
	require.Equal(t, 0, scan.SeverityRank(scan.Severity("")))
	// Unknown but non-empty fails closed to the most-severe rank.
	require.Equal(t, 4, scan.SeverityRank(scan.Severity("weird")))
}

// ---------- verify_locks.go: hashFiles / hashSkillDir ----------

// TestHashFiles_MatchesHashSkillDir is the install-vs-verify regression guard:
// the in-memory hash (hashFiles) must equal the on-disk hash (hashSkillDir)
// for the same content, must be key-order independent, and must flip on any
// content/file-set change.
func TestHashFiles_MatchesHashSkillDir(t *testing.T) {
	t.Parallel()
	files := map[string][]byte{
		"SKILL.md":          skillMD("foo", "desc"),
		"scripts/run.sh":    []byte("#!/bin/sh\necho hi\n"),
		"references/api.md": []byte("# api\n"),
	}
	dir := filepath.Join(t.TempDir(), "skill")
	require.NoError(t, writeExternalSkillAt(dir, files))

	memHash := hashFiles(files)
	diskHash, err := hashSkillDir(dir)
	require.NoError(t, err)
	require.Equal(t, memHash, diskHash, "install (in-memory) and verify (on-disk) hashes must agree")

	// Key-order independence: a map literal built in another order yields the
	// same hash (Go map iteration order is randomized anyway, but assert it).
	reordered := map[string][]byte{}
	reordered["references/api.md"] = files["references/api.md"]
	reordered["SKILL.md"] = files["SKILL.md"]
	reordered["scripts/run.sh"] = files["scripts/run.sh"]
	require.Equal(t, memHash, hashFiles(reordered))

	// One byte change flips the hash.
	mutated := map[string][]byte{}
	for k, v := range files {
		mutated[k] = v
	}
	mutated["scripts/run.sh"] = []byte("#!/bin/sh\necho HI\n")
	require.NotEqual(t, memHash, hashFiles(mutated))

	// Adding a file flips the hash.
	added := map[string][]byte{}
	for k, v := range files {
		added[k] = v
	}
	added["extra.txt"] = []byte("x")
	require.NotEqual(t, memHash, hashFiles(added))
}

// TestHashSkillDir_MissingVsError distinguishes "missing" (empty,nil) from a
// real stat error, so verifyExternalLocks can tell missing from drifted.
func TestHashSkillDir_MissingDir(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	h, err := hashSkillDir(missing)
	require.NoError(t, err)
	require.Equal(t, "", h)

	// An empty (but existing) dir hashes deterministically to the
	// zero-file digest, not "".
	empty := filepath.Join(t.TempDir(), "empty")
	require.NoError(t, os.MkdirAll(empty, 0o755))
	h2, err := hashSkillDir(empty)
	require.NoError(t, err)
	require.NotEqual(t, "", h2)
}

// ---------- verify_locks.go: verifyExternalLocks ----------

func TestVerifyExternalLocks_Branches(t *testing.T) {
	t.Parallel()

	t.Run("empty external -> no output", func(t *testing.T) {
		root := t.TempDir()
		d := testDeps(t, root, t.TempDir())
		p := initProject(t, d, root, &adept.Config{})
		var stderr bytes.Buffer
		verifyExternalLocks(d, p, &stderr)
		require.Empty(t, stderr.String())
	})

	t.Run("matching hash -> no warning", func(t *testing.T) {
		root := t.TempDir()
		d := testDeps(t, root, t.TempDir())
		p := initProject(t, d, root, &adept.Config{})
		files := map[string][]byte{"SKILL.md": skillMD("ext", "d")}
		dir := filepath.Join(p.SkillsDir(), "ext")
		require.NoError(t, writeExternalSkillAt(dir, files))

		lock := locks.New()
		lock.Set("ext", locks.Entry{Source: locks.SourceSkillsSh, Slug: "o/r/ext", ContentHash: hashFiles(files), SHA: "deadbeefcafef00d"})
		require.NoError(t, locks.Save(lockPath(p), lock))

		var stderr bytes.Buffer
		verifyExternalLocks(d, p, &stderr)
		require.Empty(t, stderr.String(), "matching content must not warn")
	})

	t.Run("missing project copy -> warns", func(t *testing.T) {
		root := t.TempDir()
		d := testDeps(t, root, t.TempDir())
		p := initProject(t, d, root, &adept.Config{})
		// Note: deliberately do NOT write the skill dir.
		lock := locks.New()
		lock.Set("ext", locks.Entry{Source: locks.SourceSkillsSh, Slug: "o/r/ext", ContentHash: "sha256:abc", SHA: "deadbeefcafef00d"})
		require.NoError(t, locks.Save(lockPath(p), lock))

		var stderr bytes.Buffer
		verifyExternalLocks(d, p, &stderr)
		require.Contains(t, stderr.String(), "project copy missing")
	})

	t.Run("drifted content -> warns", func(t *testing.T) {
		root := t.TempDir()
		d := testDeps(t, root, t.TempDir())
		p := initProject(t, d, root, &adept.Config{})
		files := map[string][]byte{"SKILL.md": skillMD("ext", "d")}
		dir := filepath.Join(p.SkillsDir(), "ext")
		require.NoError(t, writeExternalSkillAt(dir, files))

		lock := locks.New()
		// Record a hash that will NOT match the on-disk content.
		lock.Set("ext", locks.Entry{Source: locks.SourceSkillsSh, Slug: "o/r/ext", ContentHash: "sha256:stale", SHA: "deadbeefcafef00d"})
		require.NoError(t, locks.Save(lockPath(p), lock))

		var stderr bytes.Buffer
		verifyExternalLocks(d, p, &stderr)
		require.Contains(t, stderr.String(), "drifted from upstream pin")
	})
}

// ---------- patterns.go + commands_skill.go: validateSkillID via skillIDPattern ----------

// validateSkillID is the documented intent of patterns.go skillIDPattern; the
// pattern is the single source of truth, so the test drives it directly.
func TestValidateSkillID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id string
		ok bool
	}{
		{"", false},
		{"good-id-1", true},
		{"a", true},
		{"under_score", false}, // underscore no longer allowed
		{"trail-", false},      // trailing dash
		{"Bad", false},         // uppercase
		{"-lead", false},       // leading dash
		{"a/b", false},         // slash
		{"a b", false},         // space
		{"dotted.id", false},   // dot
		{strings.Repeat("a", 50), true},
		{strings.Repeat("a", 51), false},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			require.Equal(t, tc.ok, skillIDPattern.MatchString(tc.id))
		})
	}
	// libraryNamePattern shares the same shape.
	require.True(t, libraryNamePattern.MatchString("org-shared"))
	require.False(t, libraryNamePattern.MatchString("Org"))
}

// ---------- commands_library.go: upsertLibraryRef ----------

func TestUpsertLibraryRef(t *testing.T) {
	t.Parallel()

	// Append to empty slice.
	out := upsertLibraryRef(nil, adept.LibraryRef{Name: "a", Remote: "r1", Ref: "main"})
	require.Len(t, out, 1)
	require.Equal(t, "r1", out[0].Remote)

	// Append a distinct name.
	out = upsertLibraryRef(out, adept.LibraryRef{Name: "b", Remote: "r2"})
	require.Len(t, out, 2)
	require.Equal(t, "a", out[0].Name)
	require.Equal(t, "b", out[1].Name)

	// Replace existing name in place: len unchanged, order preserved, fields updated.
	out = upsertLibraryRef(out, adept.LibraryRef{Name: "a", Remote: "r1-new", Ref: "v2"})
	require.Len(t, out, 2)
	require.Equal(t, "a", out[0].Name)
	require.Equal(t, "r1-new", out[0].Remote)
	require.Equal(t, "v2", out[0].Ref)
	require.Equal(t, "b", out[1].Name, "order must be preserved")

	// No duplicate names.
	seen := map[string]int{}
	for _, r := range out {
		seen[r.Name]++
	}
	for name, n := range seen {
		require.Equal(t, 1, n, "duplicate library name %q", name)
	}
}

// ---------- commands_skill_check.go: resolveCheckTarget dispatch + error paths ----------

func TestResolveCheckTarget_Dispatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("library: with no name or id errors", func(t *testing.T) {
		root := t.TempDir()
		d := testDeps(t, root, t.TempDir())
		initProject(t, d, root, &adept.Config{})
		_, err := resolveCheckTarget(ctx, d, "library:")
		require.Error(t, err)
		require.Contains(t, err.Error(), "want library:<name>:<skill-id>")
	})

	t.Run("library:onlyname errors", func(t *testing.T) {
		root := t.TempDir()
		d := testDeps(t, root, t.TempDir())
		initProject(t, d, root, &adept.Config{})
		_, err := resolveCheckTarget(ctx, d, "library:onlyname")
		require.Error(t, err)
		require.Contains(t, err.Error(), "want library:<name>:<skill-id>")
	})

	t.Run("library ref but project has no libraries", func(t *testing.T) {
		root := t.TempDir()
		d := testDeps(t, root, t.TempDir())
		initProject(t, d, root, &adept.Config{})
		_, err := resolveCheckTarget(ctx, d, "library:foo:bar")
		require.Error(t, err)
		require.Contains(t, err.Error(), "project has no libraries configured")
	})

	t.Run("project target for missing skill -> read error", func(t *testing.T) {
		root := t.TempDir()
		d := testDeps(t, root, t.TempDir())
		initProject(t, d, root, &adept.Config{})
		_, err := resolveCheckTarget(ctx, d, "no-such-skill")
		require.Error(t, err)
		require.Contains(t, err.Error(), "read ")
	})

	t.Run("project target for present skill resolves", func(t *testing.T) {
		root := t.TempDir()
		d := testDeps(t, root, t.TempDir())
		p := initProject(t, d, root, &adept.Config{})
		writeProjectSkill(t, p, "present", "a skill")
		tgt, err := resolveCheckTarget(ctx, d, "present")
		require.NoError(t, err)
		require.Equal(t, "project:present", tgt.Name)
		require.Contains(t, string(tgt.Body), "name: present")
	})

	t.Run("library target resolves when library + skill present", func(t *testing.T) {
		root := t.TempDir()
		libRoot := t.TempDir()
		d := testDeps(t, root, libRoot)
		initProject(t, d, root, &adept.Config{Libraries: []adept.LibraryRef{{Name: "shared", Remote: "r"}}})
		writeLibrarySkill(t, libRoot, "shared", "libskill", "from library")
		tgt, err := resolveCheckTarget(ctx, d, "library:shared:libskill")
		require.NoError(t, err)
		require.Equal(t, "library:shared:libskill", tgt.Name)
		require.Contains(t, string(tgt.Body), "from library")
	})

	t.Run("configured library name not matching errors", func(t *testing.T) {
		root := t.TempDir()
		libRoot := t.TempDir()
		d := testDeps(t, root, libRoot)
		initProject(t, d, root, &adept.Config{Libraries: []adept.LibraryRef{{Name: "shared", Remote: "r"}}})
		writeLibrarySkill(t, libRoot, "shared", "libskill", "x")
		_, err := resolveCheckTarget(ctx, d, "library:other:libskill")
		require.Error(t, err)
		require.Contains(t, err.Error(), "not configured")
	})
}

// ---------- commands_skill_check.go: readTarget / targetFromFiles ----------

func TestReadTarget(t *testing.T) {
	t.Parallel()

	t.Run("reads SKILL.md + sidecars, excludes SKILL.md from sidecars", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "skill")
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "scripts"), 0o755))
		md := skillMD("x", "d")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), md, 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "scripts", "x.sh"), []byte("echo"), 0o644))

		tgt, err := readTarget("project:x", dir)
		require.NoError(t, err)
		require.Equal(t, md, tgt.Body)
		require.Len(t, tgt.Sidecars, 1)
		_, hasMD := tgt.Sidecars["SKILL.md"]
		require.False(t, hasMD, "SKILL.md must be excluded from sidecars")
		require.Equal(t, []byte("echo"), tgt.Sidecars["scripts/x.sh"], "sidecar key must use forward slashes")
	})

	t.Run("missing SKILL.md -> wrapped read error", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "empty")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		_, err := readTarget("project:x", dir)
		require.Error(t, err)
		require.Contains(t, err.Error(), "read ")
	})

	t.Run("targetFromFiles splits the same way", func(t *testing.T) {
		files := map[string][]byte{
			"SKILL.md":       []byte("body"),
			"scripts/run.sh": []byte("run"),
		}
		tgt := targetFromFiles("remote:o/r/x", files)
		require.Equal(t, []byte("body"), tgt.Body)
		require.Len(t, tgt.Sidecars, 1)
		require.Equal(t, []byte("run"), tgt.Sidecars["scripts/run.sh"])
	})
}

// ---------- commands_status.go: collectStatus ----------

func TestCollectStatus_NotInitialized(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	d := testDeps(t, root, t.TempDir())
	// No .adeptability/ created.
	rep, err := collectStatus(context.Background(), d)
	require.NoError(t, err)
	require.False(t, rep.Initialized)
	require.Equal(t, root, rep.ProjectRoot)
	require.NotEmpty(t, rep.LibraryRoot)
}

func TestCollectStatus_Initialized(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	libRoot := t.TempDir()
	d := testDeps(t, root, libRoot)
	p := initProject(t, d, root, &adept.Config{
		Libraries: []adept.LibraryRef{
			{Name: "present", Remote: "rp"},
			{Name: "absent", Remote: "ra"},
		},
	})
	writeProjectSkill(t, p, "projskill", "p")
	// "present" exists on disk and carries a skill; "absent" does not.
	writeLibrarySkill(t, libRoot, "present", "libonly", "from lib")

	rep, err := collectStatus(context.Background(), d)
	require.NoError(t, err)
	require.True(t, rep.Initialized)
	require.Equal(t, 1, rep.SkillsCanonical)
	require.Equal(t, 1, rep.SkillsFromLibs, "library-only skill must be counted")
	require.Equal(t, 1, rep.MissingLibraries, "the absent library must be counted as missing")
	require.Len(t, rep.Libraries, 2)
	// No harnesses configured -> orchestrator not consulted -> empty slice.
	require.Empty(t, rep.Harnesses)
}

// ---------- resolve.go: openMultiLibrary / resolveSkills ----------

func TestOpenMultiLibrary_DropsMissing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	libRoot := t.TempDir()
	d := testDeps(t, root, libRoot)
	p := initProject(t, d, root, &adept.Config{
		Libraries: []adept.LibraryRef{
			{Name: "present", Remote: "rp"},
			{Name: "absent", Remote: "ra"},
		},
	})
	writeLibrarySkill(t, libRoot, "present", "a", "x")

	multi, err := openMultiLibrary(d, p)
	require.NoError(t, err)
	require.NotNil(t, multi)
	require.Len(t, multi.Libraries(), 1, "only the on-disk library survives")
	require.Equal(t, "present", multi.Libraries()[0].Name)
}

func TestOpenMultiLibrary_NoLibraries(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	d := testDeps(t, root, t.TempDir())
	p := initProject(t, d, root, &adept.Config{})
	multi, err := openMultiLibrary(d, p)
	require.NoError(t, err)
	require.Nil(t, multi, "zero configured libraries -> nil Multi (project-only mode)")
}

func TestResolveSkills_ProjectShadowsLibrary(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	libRoot := t.TempDir()
	d := testDeps(t, root, libRoot)
	p := initProject(t, d, root, &adept.Config{
		Libraries: []adept.LibraryRef{{Name: "shared", Remote: "r"}},
	})
	// Project canonical "dup" shadows the library "dup"; library "uniq" is added.
	writeProjectSkill(t, p, "dup", "project copy")
	writeLibrarySkill(t, libRoot, "shared", "dup", "library copy")
	writeLibrarySkill(t, libRoot, "shared", "uniq", "library only")

	skills, err := resolveSkills(d, p)
	require.NoError(t, err)

	byID := map[string]*adept.Skill{}
	for _, s := range skills {
		byID[s.ID] = s
	}
	require.Len(t, skills, 2, "dup (shadowed) + uniq")
	require.Contains(t, byID, "dup")
	require.Contains(t, byID, "uniq")
	// The project copy wins for "dup".
	require.Contains(t, byID["dup"].Description, "project copy")
}

func TestResolveSkills_NilMultiReturnsProjectSkills(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	d := testDeps(t, root, t.TempDir())
	p := initProject(t, d, root, &adept.Config{})
	writeProjectSkill(t, p, "only", "p")
	skills, err := resolveSkills(d, p)
	require.NoError(t, err)
	require.Len(t, skills, 1)
	require.Equal(t, "only", skills[0].ID)
}

// ---------- commands_skill_external.go: shortSHA / orPlaceholder / confirm ----------

func TestShortSHA_OrPlaceholder_Confirm(t *testing.T) {
	t.Parallel()

	require.Equal(t, "deadbeef", shortSHA("deadbeefcafebabe"))
	require.Equal(t, "short", shortSHA("short"))
	require.Equal(t, "12345678", shortSHA("12345678"), "exactly 8 passes through")

	require.Equal(t, "(none)", orPlaceholder("   ", "(none)"))
	require.Equal(t, "(none)", orPlaceholder("", "(none)"))
	require.Equal(t, "MIT", orPlaceholder("MIT", "(none)"))

	confirmCases := []struct {
		in   string
		want bool
	}{
		{"y\n", true},
		{"yes\n", true},
		{"Y\n", true},
		{"YES\n", true},
		{"  yes  \n", true},
		{"n\n", false},
		{"no\n", false},
		{"\n", false},
		{"", false}, // EOF
		{"maybe\n", false},
	}
	for _, tc := range confirmCases {
		t.Run("confirm:"+strings.TrimSpace(tc.in), func(t *testing.T) {
			var buf bytes.Buffer
			got := confirm(strings.NewReader(tc.in), &buf, "proceed?")
			require.Equal(t, tc.want, got)
			require.Contains(t, buf.String(), "proceed? [y/N]")
		})
	}
}

// ---------- json.go: Deps.Print / Deps.PrintError ----------

// fakeRenderable is a tiny Renderable for exercising Print/PrintError without
// pulling in a real command's output type.
type fakeRenderable struct {
	payload  any
	plain    string
	plainErr error
}

func (f fakeRenderable) JSON() any { return f.payload }
func (f fakeRenderable) Plain(w io.Writer) error {
	if f.plainErr != nil {
		return f.plainErr
	}
	_, err := io.WriteString(w, f.plain)
	return err
}

func TestPrintAndPrintError_JSONvsPlain(t *testing.T) {
	t.Parallel()

	t.Run("Print JSON mode encodes JSON()", func(t *testing.T) {
		d := &Deps{Flags: &GlobalFlags{JSON: true}}
		var buf bytes.Buffer
		r := fakeRenderable{payload: map[string]any{"k": "v"}, plain: "PLAIN"}
		require.NoError(t, d.Print(&buf, r))
		var out map[string]any
		require.NoError(t, json.Unmarshal(buf.Bytes(), &out))
		require.Equal(t, "v", out["k"])
		require.NotContains(t, buf.String(), "PLAIN")
		require.Contains(t, buf.String(), "\n  ", "JSON must be indented")
	})

	t.Run("Print plain mode calls Plain", func(t *testing.T) {
		d := &Deps{Flags: &GlobalFlags{}}
		var buf bytes.Buffer
		r := fakeRenderable{payload: map[string]any{"k": "v"}, plain: "PLAIN-OUT"}
		require.NoError(t, d.Print(&buf, r))
		require.Equal(t, "PLAIN-OUT", buf.String())
	})

	t.Run("Print plain mode surfaces Plain error", func(t *testing.T) {
		d := &Deps{Flags: &GlobalFlags{}}
		r := fakeRenderable{plainErr: errors.New("render boom")}
		err := d.Print(io.Discard, r)
		require.Error(t, err)
		require.Contains(t, err.Error(), "render boom")
	})

	t.Run("PrintError JSON mode emits {error:...}", func(t *testing.T) {
		d := &Deps{Flags: &GlobalFlags{JSON: true}}
		var buf bytes.Buffer
		d.PrintError(&buf, errors.New("kaboom"))
		var out map[string]string
		require.NoError(t, json.Unmarshal(buf.Bytes(), &out))
		require.Equal(t, "kaboom", out["error"])
	})

	t.Run("PrintError plain mode prints bare line", func(t *testing.T) {
		d := &Deps{Flags: &GlobalFlags{}}
		var buf bytes.Buffer
		d.PrintError(&buf, errors.New("kaboom"))
		require.Equal(t, "kaboom\n", buf.String())
		require.NotContains(t, buf.String(), "{")
	})

	t.Run("PrintError with nil Flags treats as plain", func(t *testing.T) {
		d := &Deps{}
		var buf bytes.Buffer
		d.PrintError(&buf, errors.New("x"))
		require.Equal(t, "x\n", buf.String())
	})
}

// ---------- commands_config.go: scalarKeys / findKey ----------

func TestConfigScalarKeys_SetGetUnset(t *testing.T) {
	t.Parallel()

	t.Run("mode set/get/unset", func(t *testing.T) {
		k, ok := findKey("mode")
		require.True(t, ok)
		cfg := &adept.Config{}
		require.Equal(t, "symlink", k.get(cfg), "default mode is symlink")
		require.NoError(t, k.set(cfg, "copy"))
		require.Equal(t, "copy", k.get(cfg))
		require.Equal(t, adept.ModeCopy, cfg.Mode)
		err := k.set(cfg, "bogus")
		require.Error(t, err)
		require.Contains(t, err.Error(), "want symlink|copy")
		k.unset(cfg)
		require.Equal(t, "symlink", k.get(cfg))
	})

	t.Run("scan.onInstall set/get/unset collapses empty Scan", func(t *testing.T) {
		k, ok := findKey("scan.onInstall")
		require.True(t, ok)
		cfg := &adept.Config{}
		require.Contains(t, k.get(cfg), "unset")
		require.NoError(t, k.set(cfg, "true"))
		require.Equal(t, "true", k.get(cfg))
		require.NotNil(t, cfg.Scan)
		require.NotNil(t, cfg.Scan.OnInstall)
		require.True(t, *cfg.Scan.OnInstall)
		err := k.set(cfg, "notabool")
		require.Error(t, err)
		require.Contains(t, err.Error(), "want true|false")
		k.unset(cfg)
		require.Nil(t, cfg.Scan, "unsetting the only Scan field collapses Scan to nil")
	})

	t.Run("scan.blockSeverity set/get/unset", func(t *testing.T) {
		k, ok := findKey("scan.blockSeverity")
		require.True(t, ok)
		cfg := &adept.Config{}
		require.Contains(t, k.get(cfg), "critical")
		require.NoError(t, k.set(cfg, "HIGH"), "case-insensitive")
		require.Equal(t, "high", k.get(cfg), "value normalized to lowercase")
		err := k.set(cfg, "low")
		require.Error(t, err)
		require.Contains(t, err.Error(), "want critical|high|medium")
		k.unset(cfg)
		require.Nil(t, cfg.Scan)
	})

	t.Run("blockSeverity unset preserves onInstall", func(t *testing.T) {
		k, _ := findKey("scan.blockSeverity")
		cfg := &adept.Config{Scan: &adept.ScanConfig{OnInstall: ptrBool(true), BlockSeverity: "high"}}
		k.unset(cfg)
		require.NotNil(t, cfg.Scan, "Scan must survive because onInstall is still set")
		require.Equal(t, "", cfg.Scan.BlockSeverity)
		require.NotNil(t, cfg.Scan.OnInstall)
	})

	t.Run("findKey unknown -> (zero,false)", func(t *testing.T) {
		k, ok := findKey("nope")
		require.False(t, ok)
		require.Equal(t, configKey{}.name, k.name)
	})

	t.Run("keyNames is sorted and complete", func(t *testing.T) {
		names := keyNames()
		require.Equal(t, []string{"mode", "scan.blockSeverity", "scan.onInstall"}, names)
	})
}

// ---------- root.go: ExitFromError ----------

func TestExitFromError_Mapping(t *testing.T) {
	t.Parallel()
	require.Equal(t, 0, ExitFromError(nil))
	require.Equal(t, 1, ExitFromError(errors.New("plain")))
	require.Equal(t, 2, ExitFromError(ErrDirty))
	require.Equal(t, 2, ExitFromError(adept.ErrMergeConflict))
	// Wrapped sentinels still map through errors.Is.
	require.Equal(t, 2, ExitFromError(errors.Join(errors.New("ctx"), ErrDirty)))
	require.Equal(t, 2, ExitFromError(ErrScanFindings), "scan findings wrap ErrDirty -> exit 2")
}
