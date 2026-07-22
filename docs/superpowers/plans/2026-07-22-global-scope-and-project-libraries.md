# Global Scope + Project-Scoped Libraries Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a global scope (skills render into home-level harness config like `~/.claude/skills/`) and flip library clones to project-local (`.adeptability/libs/`) by default, with machine-store fallback and `adept migrate`.

**Architecture:** Global scope is a second `project.Project` instance whose render root is the parent of the library root (default: `$HOME`) and whose base dir IS the library root (default: `~/.adeptability`). With that shape, config/skills/base/staging/libs paths and the claude render target all fall out of existing code. The only genuinely new machinery: `HarnessSpec.GlobalOutput` templates (codex/opencode global paths differ from project-relative ones), a `--global` flag + project-root walk-up in the CLI, scope-local library roots with machine-store fallback, and `.adeptability/.gitignore` management.

**Tech Stack:** Go, cobra, existing internal packages (`project`, `harness`, `library`, `cli`). No new dependencies.

**Spec:** `docs/superpowers/specs/2026-07-22-global-scope-and-project-libraries-design.md`

## Global Constraints

- No new third-party dependencies.
- Conventional commits (`feat:`, `fix:`, `test:`, `docs:`, `refactor:`).
- Follow existing test style in the package you touch (table-driven, `t.TempDir()`, stdlib `testing`; check a neighboring `_test.go` before writing).
- Never enumerate-and-delete in shared home dirs: global sync writes only paths it renders; foreign files in `~/.claude/skills/` etc. must survive untouched (spec §5 safety invariant).
- All tests must run with `ADEPT_LIBRARY` pointed at a temp dir — never touch the real `~/.adeptability` or `$HOME` in tests. For global-scope tests set `ADEPT_LIBRARY=$TMP/.adeptability` so the derived render root is `$TMP`.
- Run `go build ./...` and the package's tests before every commit.
- Skill ids / library names validation regexes already exist — reuse, don't re-declare.

---

### Task 1: `HarnessSpec` global fields + adapter values

**Files:**
- Modify: `pkg/adept/harness.go` (HarnessSpec struct, ~line 18)
- Modify: `internal/render/claude/renderer.go` or wherever `claude.Spec()` is defined (grep `func Spec()` in `internal/render/claude/`)
- Modify: same for `internal/render/codex/`, `internal/render/opencode/`, `internal/render/cursor/`, `internal/render/copilot/`
- Test: `pkg/adept/harness_test.go` (create if missing), plus each render package's existing spec test file

**Interfaces:**
- Produces: `HarnessSpec.GlobalOutput string`, `HarnessSpec.GlobalBaseDir string` — empty means "harness not global-capable". Later tasks (3, 5) branch on `spec.GlobalOutput == ""`.

- [ ] **Step 1: Write the failing test**

In each render package's spec test (e.g. `internal/render/claude/`), assert the global fields:

```go
func TestSpecGlobalOutput(t *testing.T) {
	s := Spec()
	if s.GlobalOutput != ".claude/skills/{id}/SKILL.md" {
		t.Fatalf("GlobalOutput = %q", s.GlobalOutput)
	}
	if s.GlobalBaseDir != ".claude" {
		t.Fatalf("GlobalBaseDir = %q", s.GlobalBaseDir)
	}
}
```

Expected values per harness (all relative to the global render root, i.e. `$HOME`):

| Harness  | GlobalOutput                          | GlobalBaseDir      |
| -------- | ------------------------------------- | ------------------ |
| claude   | `.claude/skills/{id}/SKILL.md`        | `.claude`          |
| codex    | `.codex/AGENTS.md`                    | `.codex`           |
| opencode | `.config/opencode/` + same tail as its project OutputPath | `.config/opencode` |
| cursor   | `""` (not global-capable)             | `""`               |
| copilot  | `""` (not global-capable)             | `""`               |

Before hardcoding the opencode path, verify OpenCode's global config dir against its docs (context7: `opencode`) — expected `~/.config/opencode/`; mirror the project-relative tail (e.g. project `.opencode/agents/{id}/SKILL.md` → global `.config/opencode/agents/{id}/SKILL.md`). If docs disagree, use the documented path and update this table in the plan file.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/render/... ./pkg/adept/...`
Expected: FAIL — `s.GlobalOutput undefined` (compile error) — that counts as the failing state.

- [ ] **Step 3: Implement**

`pkg/adept/harness.go` — extend the struct:

```go
type HarnessSpec struct {
	ID          string
	Name        string
	Kind        HarnessKind
	OutputPath  string // template; may include {id}
	SizeBudgetB int    // 0 = unlimited
	NeedsDir    bool
	BaseDir     string // detection root (e.g. ".claude" or ".cursor")

	// GlobalOutput is the OutputPath equivalent when rendering in global
	// scope, relative to the global render root (the parent of the library
	// root — $HOME by default). Empty means the harness has no real
	// home-level config location and cannot be enabled with --global.
	GlobalOutput string
	// GlobalBaseDir is the BaseDir equivalent for global scope detection.
	GlobalBaseDir string
}
```

Then set the two fields in each built-in `Spec()` function per the table (cursor/copilot: leave zero-valued, add a comment `// No real home-level config location — not global-capable.`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/render/... ./pkg/adept/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/adept/harness.go internal/render/
git commit -m "feat(harness): declare global output targets on HarnessSpec"
```

---

### Task 2: Global-capable `Project` + staging path via `BaseDir()`

**Files:**
- Modify: `internal/project/project.go` (add `baseDirOverride` field + `NewGlobal`)
- Modify: `internal/harness/orchestrator.go:696-698` (`stagingPathFor`)
- Test: `internal/project/project_test.go` (or the package's existing test file), `internal/harness/orchestrator_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `project.NewGlobal(root, baseDir string, parser canonical.Parser, hasher hash.Hasher, store config.Store, w fsutil.Writer) project.Project` — like `New` but `BaseDir()` returns `baseDir` verbatim instead of `<root>/.adeptability`. Task 4 calls it as `project.NewGlobal(filepath.Dir(libRoot), libRoot, …)`.
- Produces: `stagingPathFor(p project.Project, out adept.RenderOutput) string` = `filepath.Join(p.BaseDir(), adept.StagingDir, out.Path)` — same result as before for normal projects.

- [ ] **Step 1: Write the failing tests**

```go
func TestNewGlobalBaseDirOverride(t *testing.T) {
	tmp := t.TempDir()
	base := filepath.Join(tmp, "custom-lib-root")
	p := project.NewGlobal(tmp, base, canonical.NewParser(), hash.NewHasher(),
		config.NewStore(fsutil.NewWriter().AtomicWrite), fsutil.NewWriter())
	if p.Root() != tmp {
		t.Fatalf("Root() = %q, want %q", p.Root(), tmp)
	}
	if p.BaseDir() != base {
		t.Fatalf("BaseDir() = %q, want %q", p.BaseDir(), base)
	}
	if got, want := p.SkillsDir(), filepath.Join(base, "skills"); got != want {
		t.Fatalf("SkillsDir() = %q, want %q", got, want)
	}
	if got, want := p.ConfigPath(), filepath.Join(base, "config.json"); got != want {
		t.Fatalf("ConfigPath() = %q, want %q", got, want)
	}
}
```

(Adapt constructor args to the package's existing test helpers if it has them.) For the orchestrator: find the existing test that syncs into a temp project and add a variant asserting staging lands under `p.BaseDir()/staging/` when BaseDir is overridden.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/project/... ./internal/harness/...`
Expected: FAIL — `undefined: project.NewGlobal`

- [ ] **Step 3: Implement**

`internal/project/project.go`:

```go
// NewGlobal constructs a Project for the global scope: root is the render
// root (parent of the library root — $HOME by default) and baseDir is the
// adept metadata dir itself (the library root, ~/.adeptability by default).
// Consumer layout only.
func NewGlobal(root, baseDir string, parser canonical.Parser, hasher hash.Hasher, store config.Store, w fsutil.Writer) Project {
	return &project{
		root:            root,
		baseDirOverride: baseDir,
		parser:          parser,
		hasher:          hasher,
		store:           store,
		writer:          w,
	}
}
```

Add the field and change `BaseDir()`:

```go
type project struct {
	root            string
	baseDirOverride string // when set, BaseDir() returns this verbatim (global scope)
	// … existing fields unchanged
}

func (p *project) BaseDir() string {
	if p.baseDirOverride != "" {
		return p.baseDirOverride
	}
	return filepath.Join(p.root, adept.BaseDirName)
}
```

`internal/harness/orchestrator.go` — change `stagingPathFor` and both call sites (`o.write(...)` currently receives `p.Root()`; pass `p` through or compute staging in the caller):

```go
func stagingPathFor(p project.Project, out adept.RenderOutput) string {
	return filepath.Join(p.BaseDir(), adept.StagingDir, out.Path)
}
```

Thread `p` into `o.write` (replace the `projectRoot string` first parameter with `p project.Project`, use `stagingPathFor(p, out)` inside; call sites at orchestrator.go:242 and :275 pass `p`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/project/... ./internal/harness/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/project/project.go internal/harness/orchestrator.go internal/project/*_test.go internal/harness/*_test.go
git commit -m "feat(project): global-scope Project with base-dir override; staging path via BaseDir"
```

---

### Task 3: Orchestrator global mode (spec swap + capability guard)

**Files:**
- Modify: `internal/harness/orchestrator.go` (SyncOptions, StatusOptions, syncHarness, Status, renderAll)
- Test: `internal/harness/orchestrator_test.go`

**Interfaces:**
- Consumes: `HarnessSpec.GlobalOutput/GlobalBaseDir` (Task 1), `stagingPathFor(p, out)` (Task 2).
- Produces: `SyncOptions.Global bool`, `StatusOptions.Global bool`; sentinel error `ErrNotGlobalCapable` in `pkg/adept/errors.go` (follow the existing sentinel pattern there, e.g. `ErrHarnessUnknown`): `var ErrNotGlobalCapable = errors.New("harness has no global config location")`. Task 5 matches it with `errors.Is`.

- [ ] **Step 1: Write the failing tests**

Two behaviors:

```go
func TestSyncGlobalSwapsSpecPaths(t *testing.T) {
	// Arrange a temp global project: root=$TMP, baseDir=$TMP/.adeptability,
	// one skill in $TMP/.adeptability/skills/demo/SKILL.md, config enabling
	// the claude harness. Reuse the package's existing sync test scaffolding.
	// Act: orch.Sync(ctx, p, SyncOptions{Global: true, Skills: skills})
	// Assert: $TMP/.claude/skills/demo/SKILL.md exists (claude's GlobalOutput
	// equals its OutputPath, so this proves the plumbing end-to-end), and
	// staging landed at $TMP/.adeptability/staging/.claude/skills/demo/SKILL.md.
}

func TestSyncGlobalRejectsNonCapableHarness(t *testing.T) {
	// Same arrangement but config enables "cursor".
	// Act: orch.Sync(ctx, p, SyncOptions{Global: true, Skills: skills})
	// Assert: errors.Is(err, adept.ErrNotGlobalCapable)
}
```

Write them concretely against the scaffolding you find in `orchestrator_test.go` (it exists — the sync flow is tested). Also add a codex variant asserting output at `$TMP/.codex/AGENTS.md` (proves the swap actually changes a path).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/harness/...`
Expected: FAIL — `unknown field Global` (compile error)

- [ ] **Step 3: Implement**

Add to both option structs:

```go
// Global renders into the harness's home-level config location
// (Spec().GlobalOutput) instead of the project-relative OutputPath.
// Harnesses without a GlobalOutput fail with adept.ErrNotGlobalCapable.
Global bool
```

Add a helper in orchestrator.go:

```go
// effectiveSpec returns the spec with global paths substituted when global
// scope is requested. Harnesses without a global target are rejected.
func effectiveSpec(spec adept.HarnessSpec, global bool) (adept.HarnessSpec, error) {
	if !global {
		return spec, nil
	}
	if spec.GlobalOutput == "" {
		return spec, fmt.Errorf("%s: %w", spec.ID, adept.ErrNotGlobalCapable)
	}
	spec.OutputPath = spec.GlobalOutput
	if spec.GlobalBaseDir != "" {
		spec.BaseDir = spec.GlobalBaseDir
	}
	return spec, nil
}
```

Threading (the mechanical part):
- `syncHarness`: after `spec := adapter.Spec()` insert `spec, err = effectiveSpec(spec, opts.Global)` (return the error). Every later use of the spec in this function already reads the local `spec` variable.
- `renderAll` currently calls `adapter.Spec()` internally (orchestrator.go:314) — change its signature to `renderAll(ctx, adapter, spec, skills, p)` and pass the effective spec from both callers (`syncHarness`, `Status`).
- `Status`: same substitution after `spec := adapter.Spec()` (line ~468), plus it must be applied *before* the earlier `renderAll` call — reorder so the effective spec is computed first, and pass `opts.Global` from `StatusOptions`.
- Aggregate call sites pass `spec.SizeBudgetB` from the effective spec (unchanged value — budgets don't differ by scope).
- Agent sync (`syncHarnessAgents`): v1 renders **skills only** in global scope. At the top of the agents path in `Sync`, skip when `opts.Global` (one `if opts.Global { … skip … }` with a comment: `// v1: agents are project-scope only; global agent targets are not yet defined.`).

Do NOT add any directory enumeration or deletion — write paths only (safety invariant).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/harness/... ./pkg/adept/...`
Expected: PASS (including the pre-existing suite — the default `Global: false` path must be byte-identical to before)

- [ ] **Step 5: Commit**

```bash
git add internal/harness/ pkg/adept/errors.go
git commit -m "feat(orchestrator): global-scope rendering via GlobalOutput spec swap"
```

---

### Task 4: CLI scope resolution (`--global`, walk-up, `ScopedProject`)

**Files:**
- Modify: `internal/cli/root.go` (GlobalFlags, flag registration, PersistentPreRunE)
- Modify: `internal/cli/deps.go` (ScopedProject, GlobalProject, findProjectRoot)
- Test: `internal/cli/root_test.go` or new `internal/cli/scope_test.go`

**Interfaces:**
- Consumes: `project.NewGlobal` (Task 2).
- Produces:
  - `GlobalFlags.Global bool` and `GlobalFlags.projectDirExplicit bool` (unexported, set in PersistentPreRunE before defaulting).
  - `(d *Deps) GlobalProject() (project.Project, error)` — `libRoot := d.ResolveLibraryRoot()`; returns `project.NewGlobal(filepath.Dir(libRoot), libRoot, …)`.
  - `(d *Deps) ScopedProject() (project.Project, bool, error)` — the bool is "is global scope". Resolution: `--global` → global; explicit `--project` → that dir as-is (current behavior); else walk up from cwd for a dir containing `.adeptability/config.json` → project there; none found → global, after printing one line to stderr: `no project found (no .adeptability/config.json up the tree) — operating on global scope; pass --project to target a project`.

- [ ] **Step 1: Write the failing tests**

```go
func TestScopedProjectResolution(t *testing.T) {
	// Table:
	// 1. Global flag set → returns global project rooted at Dir(libRoot),
	//    BaseDir()==libRoot.
	// 2. Explicit --project=<tmp> (projectDirExplicit=true) → project at tmp
	//    even when tmp has no config.
	// 3. cwd = <tmp>/nested/deep, config at <tmp>/.adeptability/config.json →
	//    project root <tmp> (walk-up).
	// 4. cwd = <tmp2> with no config anywhere up to a stop boundary →
	//    global scope, second return true.
	// Build Deps directly: NewDeps(&GlobalFlags{...}, BuildInfo{}) and set
	// ADEPT_LIBRARY via t.Setenv to a temp .adeptability dir.
}
```

Walk-up stop condition: stop at filesystem root; also stop at `$HOME` boundary is NOT needed — plain root walk is fine (config.json probe is cheap). Note case 4 must not walk into the real `$HOME`: run with `--project` unset and cwd inside `t.TempDir()`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run TestScopedProjectResolution`
Expected: FAIL — `undefined: (*Deps).ScopedProject`

- [ ] **Step 3: Implement**

`root.go`:

```go
type GlobalFlags struct {
	JSON       bool
	LogLevel   string
	ProjectDir string
	LibraryDir string
	Global     bool

	// projectDirExplicit records whether --project was user-supplied (before
	// PersistentPreRunE defaults it to cwd). Explicit dirs skip the walk-up.
	projectDirExplicit bool
}
```

Register: `root.PersistentFlags().BoolVar(&gf.Global, "global", false, "operate on the global scope (home-level harness config) instead of a project")`.

In `PersistentPreRunE`, before the existing default: `gf.projectDirExplicit = gf.ProjectDir != ""`.

`deps.go`:

```go
// GlobalProject returns the global-scope Project: metadata at the library
// root (~/.adeptability), rendering into its parent (default $HOME).
func (d *Deps) GlobalProject() (project.Project, error) {
	libRoot, err := d.ResolveLibraryRoot()
	if err != nil {
		return nil, err
	}
	return project.NewGlobal(filepath.Dir(libRoot), libRoot, d.Parser, d.Hasher, d.Config, d.Writer), nil
}

// findProjectRoot walks up from start looking for .adeptability/config.json.
func findProjectRoot(start string) (string, bool) {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, adept.BaseDirName, adept.ConfigFileName)); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// ScopedProject resolves the project the current invocation operates on.
// --global forces global scope; an explicit --project pins that directory;
// otherwise the nearest ancestor with .adeptability/config.json wins, and
// with no project anywhere up the tree the global scope is used (noticed
// on stderr once).
func (d *Deps) ScopedProject() (project.Project, bool, error) {
	if d.Flags != nil && d.Flags.Global {
		p, err := d.GlobalProject()
		return p, true, err
	}
	if d.Flags != nil && d.Flags.projectDirExplicit {
		p, err := d.Project()
		return p, false, err
	}
	start, err := d.ResolveProjectRoot()
	if err != nil {
		return nil, false, err
	}
	if root, ok := findProjectRoot(start); ok {
		return d.projectAt(root, d.detectLibraryLayout(root)), false, nil
	}
	fmt.Fprintln(os.Stderr, "no project found (no .adeptability/config.json up the tree) — operating on global scope; pass --project to target a project")
	p, err := d.GlobalProject()
	return p, true, err
}
```

(If the codebase routes user-facing stderr through `d.Log` or command writers, follow that pattern instead of `os.Stderr` — check how existing warnings print in `commands_sync.go`.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go internal/cli/deps.go internal/cli/scope_test.go
git commit -m "feat(cli): --global flag and scope resolution with project walk-up"
```

---

### Task 5: Wire scope into commands

**Files:**
- Modify: `internal/cli/commands_sync.go`, `commands_status.go`, `commands_diff.go`, `commands_sync_from.go`, `commands_harness.go`, `commands_config.go`, `commands_skill.go`, `commands_hook.go`
- Test: `internal/cli/commands_libraryref_test.go` style — extend the relevant command test files

**Interfaces:**
- Consumes: `d.ScopedProject()` (Task 4), `SyncOptions.Global` / `StatusOptions.Global` (Task 3), `adept.ErrNotGlobalCapable` (Task 3).
- Produces: every listed command operates on the scoped project; `sync`/`status`/`diff`/`sync-from` pass `Global: isGlobal` into orchestrator options.

- [ ] **Step 1: Write the failing tests**

Pick the two highest-value command tests (follow the existing command-test scaffolding in `internal/cli/*_test.go` — they build a root command and execute with args):

```go
func TestSyncGlobalRendersToHomeTargets(t *testing.T) {
	// t.Setenv("ADEPT_LIBRARY", filepath.Join(tmp, ".adeptability"))
	// Seed: tmp/.adeptability/config.json {"schema":1,"harnesses":["claude-code"]}
	//        (use the actual claude harness id — check claude.Spec().ID)
	// Seed: tmp/.adeptability/skills/demo/SKILL.md (valid frontmatter)
	// Execute: adept --global sync
	// Assert: tmp/.claude/skills/demo/SKILL.md exists.
	// Assert foreign-file safety: pre-create tmp/.claude/skills/foreign/SKILL.md
	// before running and assert it still exists, byte-identical, after sync.
}

func TestHarnessAddGlobalRejectsNonCapable(t *testing.T) {
	// Execute: adept --global harness add cursor
	// Assert: error wrapping adept.ErrNotGlobalCapable; global config's
	// Harnesses list stays empty.
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestSyncGlobalRendersToHomeTargets|TestHarnessAddGlobalRejectsNonCapable'`
Expected: FAIL — sync renders nothing to `tmp/.claude` (commands still use `d.Project()` on cwd), harness add succeeds where it should error

- [ ] **Step 3: Implement**

Mechanical substitution in each listed command's RunE:

```go
p, isGlobal, err := d.ScopedProject()
if err != nil {
	return err
}
```

and for orchestrator calls add the flag:

```go
results, err := d.Orchestrator.Sync(cmd.Context(), p, harness.SyncOptions{
	HarnessIDs: harnessIDs,
	Force:      force,
	DryRun:     dryRun,
	Skills:     skills,
	Global:     isGlobal,
})
```

`commands_harness.go` add-command: before persisting, guard:

```go
if isGlobal {
	if a, err := d.Registry.Get(id); err == nil && a.Spec().GlobalOutput == "" {
		return fmt.Errorf("harness %q: %w (project scope only)", id, adept.ErrNotGlobalCapable)
	}
}
```

Notes:
- `commands_library.go` (`init`) keeps `d.Project()`/`ProjectWithLayout` — init always creates a project at cwd/--project; do not wire scope there.
- `verifyExternalLocks`, `resolveSkills` calls: pass the scoped `p` — they only consume the `Project` interface (Task 6 makes `resolveSkills` scope-aware).
- `sync-from --global`: after computing `isGlobal`, reject harnesses whose `GlobalOutput != OutputPath` with a clear message (`sync-from --global currently supports harnesses whose global layout matches the project layout (claude-code)`) — the import path reads project-relative locations, which for claude coincide with global ones. One `if` guard, comment `// ponytail: import is spec-path-unaware; global import beyond claude needs spec threading through Import()`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/...`
Expected: PASS (full package — the substitution must not regress project-scope behavior; the walk-up change makes commands in nested dirs resolve to the repo root, which existing tests using `--project` won't hit)

- [ ] **Step 5: Commit**

```bash
git add internal/cli/
git commit -m "feat(cli): scope-aware commands — sync/status/diff/harness/skill/config honor --global"
```

---

### Task 6: Scope-local library roots + gitignore management

**Files:**
- Modify: `internal/cli/deps.go` (add `LibsRootFor`)
- Modify: `internal/cli/commands_libraryref.go` (add/remove/update/list use scoped root)
- Create: `internal/cli/gitignore.go`
- Test: `internal/cli/commands_libraryref_test.go`, `internal/cli/gitignore_test.go`

**Interfaces:**
- Consumes: `d.ScopedProject()` (Task 4).
- Produces:
  - `(d *Deps) LibsRootFor(p project.Project) string` = `filepath.Join(p.BaseDir(), library.LibrariesDirName)`. For the global project this equals today's machine store (`~/.adeptability/libs`) — global behavior is unchanged by construction.
  - `ensureScopeGitignore(w fsutil.Writer, baseDir string) error` — idempotently ensures `<baseDir>/.gitignore` contains lines `libs/` and `staging/`.

- [ ] **Step 1: Write the failing tests**

```go
func TestLibraryAddClonesIntoProjectScope(t *testing.T) {
	// Seed a git "remote": a bare-cloneable dir with skills/demo/SKILL.md
	// (see existing library add tests for the local-path remote pattern —
	// commands_libraryref_test.go already builds these).
	// Execute in a temp project (has .adeptability/config.json):
	//   adept library add team --from <remote>
	// Assert: <project>/.adeptability/libs/team/skills/demo/SKILL.md exists.
	// Assert: machine store (ADEPT_LIBRARY temp) does NOT gain libs/team.
	// Assert: <project>/.adeptability/.gitignore contains "libs/" and "staging/".
}

func TestLibraryAddGlobalClonesIntoMachineStore(t *testing.T) {
	// Execute: adept --global library add team --from <remote>
	// Assert: $ADEPT_LIBRARY/libs/team/... exists; global config.json gained the ref.
}

func TestEnsureScopeGitignoreIdempotent(t *testing.T) {
	// Call twice; file contains each line exactly once; pre-existing custom
	// lines are preserved.
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestLibraryAdd|TestEnsureScopeGitignore'`
Expected: FAIL — clone lands in machine store, no .gitignore written, `ensureScopeGitignore` undefined

- [ ] **Step 3: Implement**

`deps.go`:

```go
// LibsRootFor returns the scope-local library clone root: <scope base>/libs.
// Project scope: <project>/.adeptability/libs. Global scope: the machine
// store itself (~/.adeptability/libs) — identical to the legacy location.
func (d *Deps) LibsRootFor(p project.Project) string {
	return filepath.Join(p.BaseDir(), library.LibrariesDirName)
}
```

`gitignore.go`:

```go
package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/itaywol/adeptability/internal/fsutil"
)

// scopeIgnoreLines are the .adeptability subpaths that are machine-local by
// default: library clones (re-hydratable from config.json) and render staging.
var scopeIgnoreLines = []string{"libs/", "staging/"}

// ensureScopeGitignore idempotently appends the scope-local ignore lines to
// <baseDir>/.gitignore, preserving any user content already there.
func ensureScopeGitignore(w fsutil.Writer, baseDir string) error {
	path := filepath.Join(baseDir, ".gitignore")
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	have := map[string]bool{}
	for _, line := range strings.Split(string(existing), "\n") {
		have[strings.TrimSpace(line)] = true
	}
	out := strings.TrimRight(string(existing), "\n")
	added := false
	for _, line := range scopeIgnoreLines {
		if !have[line] {
			if out != "" {
				out += "\n"
			}
			out += line
			added = true
		}
	}
	if !added {
		return nil
	}
	return w.AtomicWrite(path, []byte(out+"\n"), 0o644)
}
```

`commands_libraryref.go` — in `add`, `remove --purge`, `update`, `list`: replace

```go
p, err := d.Project()          →  p, isGlobal, err := d.ScopedProject()
libsRoot, err := d.ResolveLibrariesRoot()  →  libsRoot := d.LibsRootFor(p)
```

In `add` only, after a successful clone+SaveConfig, when `!isGlobal`:

```go
if err := ensureScopeGitignore(d.Writer, p.BaseDir()); err != nil {
	d.Log.Warn("write .adeptability/.gitignore", "err", err)
}
```

Update the `add` success message to print the actual dest (it already does). Update the command Short text: global scope wording (`"Clone a remote library into the current scope"`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/...`
Expected: PASS. Pre-existing library tests that asserted machine-store paths will fail — update them to the project-scope expectation (that behavior change is the point of this feature; do not "fix" it by reverting the code).

- [ ] **Step 5: Commit**

```bash
git add internal/cli/
git commit -m "feat(library): clone into scope-local libs/ by default; manage .adeptability/.gitignore"
```

---

### Task 7: Resolution fallback to machine store

**Files:**
- Modify: `internal/cli/resolve.go` (`openMultiLibrary`)
- Test: `internal/cli/zcoverage_test.go` (the resolveSkills section, line ~555) or a new focused test file

**Interfaces:**
- Consumes: `d.LibsRootFor(p)` (Task 6), `library.NamedLibrary` / `library.New` (existing exports).
- Produces: `openMultiLibrary(d, p)` resolves each configured ref from, in order: scope-local libs root, then machine store (`d.ResolveLibrariesRoot()`), warning `"library resolved from machine store — run `adept migrate` to localize"` on fallback. Missing in both → existing "skipped" warning.

- [ ] **Step 1: Write the failing test**

```go
func TestOpenMultiLibraryFallsBackToMachineStore(t *testing.T) {
	// Project config declares library "team". No clone under
	// <project>/.adeptability/libs/team. A valid clone with skills/demo/SKILL.md
	// exists under $ADEPT_LIBRARY/libs/team.
	// Act: resolveSkills(d, p)
	// Assert: "demo" is in the returned set (fallback worked).
	// Then create <project>/.adeptability/libs/team with a DIFFERENT skill body
	// for "demo" and assert the project-local copy wins (no fallback when local
	// exists).
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestOpenMultiLibraryFallsBack`
Expected: FAIL — with Task 6 in place, `openMultiLibrary` still reads only `d.ResolveLibrariesRoot()`... actually after Task 6 the *add* path moved but resolve still points at the machine store, so the "project-local wins" half fails. Either half failing is the red state.

- [ ] **Step 3: Implement**

Rewrite `openMultiLibrary` to build `[]library.NamedLibrary` per-ref:

```go
func openMultiLibrary(d *Deps, p project.Project) (library.Multi, error) {
	cfg, err := p.Config()
	if err != nil {
		return nil, err
	}
	if len(cfg.Libraries) == 0 {
		return nil, nil
	}
	localRoot := d.LibsRootFor(p)
	storeRoot, err := d.ResolveLibrariesRoot()
	if err != nil {
		return nil, err
	}
	named := make([]library.NamedLibrary, 0, len(cfg.Libraries))
	for _, ref := range cfg.Libraries {
		local := filepath.Join(localRoot, ref.Name)
		fallback := filepath.Join(storeRoot, ref.Name)
		var root string
		switch {
		case dirExists(local):
			root = local
		case local != fallback && dirExists(fallback):
			root = fallback
			d.Log.Warn("library resolved from machine store — run `adept migrate` to localize",
				"name", ref.Name, "path", fallback)
		default:
			d.Log.Warn("configured library missing on disk — skipped", "name", ref.Name, "remote", ref.Remote)
			continue
		}
		named = append(named, library.NamedLibrary{
			Name:    ref.Name,
			Library: library.New(root, d.Parser, d.Hasher, d.Writer),
		})
	}
	if len(named) == 0 {
		return nil, nil
	}
	return library.NewMulti(named), nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
```

(For the global project `local == fallback`, so no double-probe and no spurious warning.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/resolve.go internal/cli/
git commit -m "feat(resolve): per-library machine-store fallback with migrate hint"
```

---

### Task 8: `adept migrate`

**Files:**
- Create: `internal/cli/commands_migrate.go`
- Modify: `internal/cli/root.go` (AddCommand)
- Test: `internal/cli/commands_migrate_test.go`

**Interfaces:**
- Consumes: `d.ScopedProject()`, `d.LibsRootFor(p)`, `d.Git.CloneOrPull(ctx, remote, ref, dest)` (signature seen in commands_libraryref.go:124), `ensureScopeGitignore`.
- Produces: `adept migrate` — for each configured library missing from the scope-local libs root, clone it there; print a row per library (`localized` / `already local` / error). Rejects `--global` (global scope IS the machine store — nothing to migrate): `return fmt.Errorf("migrate applies to project scope only")`.

- [ ] **Step 1: Write the failing test**

```go
func TestMigrateLocalizesLibraries(t *testing.T) {
	// Project declares "team" (remote = local-path git repo fixture, same
	// pattern as library add tests). Clone exists ONLY in machine store.
	// Execute: adept migrate
	// Assert: <project>/.adeptability/libs/team/skills/demo/SKILL.md exists.
	// Assert: machine store copy still present (never deleted).
	// Assert: .adeptability/.gitignore written.
	// Execute again: output says already local, exit 0 (idempotent).
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestMigrateLocalizes`
Expected: FAIL — `unknown command "migrate"`

- [ ] **Step 3: Implement**

```go
package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
)

// newMigrateCmd registers `adept migrate`: re-clone the project's declared
// libraries into the project-local libs root so resolution stops falling
// back to the machine store. The machine store is never touched — other
// projects and the global scope may still use it.
func newMigrateCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:   "migrate",
		Short: "Localize configured libraries into this project's .adeptability/libs/",
		Args:  cobra.NoArgs,
	}
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		p, isGlobal, err := d.ScopedProject()
		if err != nil {
			return err
		}
		if isGlobal {
			return fmt.Errorf("migrate applies to project scope only (global libraries already live in the machine store)")
		}
		cfg, err := p.Config()
		if err != nil {
			return err
		}
		if len(cfg.Libraries) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "no libraries configured")
			return nil
		}
		libsRoot := d.LibsRootFor(p)
		w := cmd.OutOrStdout()
		for _, ref := range cfg.Libraries {
			dest := filepath.Join(libsRoot, ref.Name)
			if dirExists(dest) {
				fmt.Fprintf(w, "%s: already local\n", ref.Name)
				continue
			}
			gitRef := ref.Ref
			if gitRef == "" {
				gitRef = "main"
			}
			if err := d.Git.CloneOrPull(cmd.Context(), ref.Remote, gitRef, dest); err != nil {
				return fmt.Errorf("%s: %w", ref.Name, err)
			}
			fmt.Fprintf(w, "%s: localized (%s)\n", ref.Name, dest)
		}
		if err := ensureScopeGitignore(d.Writer, p.BaseDir()); err != nil {
			d.Log.Warn("write .adeptability/.gitignore", "err", err)
		}
		return nil
	}
	return c
}
```

Add `newMigrateCmd(deps)` to the `root.AddCommand(...)` list in root.go.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/commands_migrate.go internal/cli/commands_migrate_test.go internal/cli/root.go
git commit -m "feat(cli): adept migrate localizes machine-store library clones"
```

---

### Task 9: YAML adapter `global-output` support

**Files:**
- Modify: `internal/adapter/loader.go` (+ the embedded JSON schema file next to it — grep `embed` in that package)
- Test: `internal/adapter/loader_test.go`

**Interfaces:**
- Consumes: `HarnessSpec.GlobalOutput/GlobalBaseDir` (Task 1).
- Produces: adapter YAML keys `global-output` (string, optional) and `global-base-dir` (string, optional) mapped onto the spec.

- [ ] **Step 1: Write the failing test**

Extend the loader's existing round-trip test with a YAML fixture containing:

```yaml
id: my-agent
name: "My Agent"
kind: per-skill
output: .myagent/rules/{id}.rule
base-dir: .myagent
global-output: .config/myagent/rules/{id}.rule
global-base-dir: .config/myagent
```

Assert `a.Spec().GlobalOutput == ".config/myagent/rules/{id}.rule"` and `GlobalBaseDir == ".config/myagent"`. Also assert a YAML without the keys still loads (zero values).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/adapter/...`
Expected: FAIL — schema rejects unknown key or field is dropped (empty GlobalOutput)

- [ ] **Step 3: Implement**

- Add `GlobalOutput string \`yaml:"global-output"\`` and `GlobalBaseDir string \`yaml:"global-base-dir"\`` to the loader's YAML struct.
- Add both properties (type string) to the embedded JSON schema.
- Map them onto the constructed `HarnessSpec`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/adapter/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/
git commit -m "feat(adapter): global-output/global-base-dir keys for user-defined harnesses"
```

---

### Task 10: End-to-end integration test

**Files:**
- Create: `internal/cli/global_scope_e2e_test.go`

**Interfaces:**
- Consumes: everything above via the CLI surface only.

- [ ] **Step 1: Write the test (this task IS the test)**

One test function, sequential scenario, all inside `t.TempDir()` with `t.Setenv("ADEPT_LIBRARY", tmp+"/.adeptability")`:

1. `adept --global harness add <claude-id>` → global config created at `tmp/.adeptability/config.json` with the harness.
2. `adept --global skill add` (or seed `tmp/.adeptability/skills/hello/SKILL.md` directly + `adept --global sync`) → `tmp/.claude/skills/hello/SKILL.md` exists.
3. Foreign-file safety: pre-created `tmp/.claude/skills/foreign/SKILL.md` unchanged after two sync cycles (second with `--force`).
4. `adept --global library add team --from <local git fixture>` → clone at `tmp/.adeptability/libs/team/`; `adept --global sync` renders the library's skill to `tmp/.claude/skills/<id>/`.
5. `adept --global status` exits cleanly and reports synced.
6. Project scope untouched: create a project dir with config, run `adept --project <dir> sync`, assert nothing new under `tmp/.claude` beyond step 2–4 outputs.

- [ ] **Step 2: Run it**

Run: `go test ./internal/cli/ -run TestGlobalScopeEndToEnd -v`
Expected: PASS. Any failure here is a real integration bug — fix the product code (tasks above), not the test expectations, unless the expectation contradicts the spec.

- [ ] **Step 3: Full suite + build**

Run: `go build ./... && go test ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/cli/global_scope_e2e_test.go
git commit -m "test(cli): global scope end-to-end coverage incl. foreign-file safety"
```

---

### Task 11: Docs

**Files:**
- Create: `docs/concepts/scopes.md`
- Modify: `docs/concepts/libraries.md`, `docs/guides/vendoring.md`, `docs/concepts/harnesses.md`, `README.md`
- Modify: `mkdocs.yml` or docs nav config if one lists pages (check repo root)

**Interfaces:** none — prose.

- [ ] **Step 1: Write `docs/concepts/scopes.md`**

Cover, matching the shipped behavior exactly (verify claims against the code you just wrote — every command and path must be real):
- The two scopes table from the spec (§1).
- Scope resolution rules (`--global` > explicit `--project` > walk-up > global fallback with notice).
- Which harnesses are global-capable and why cursor/copilot are not.
- Quickstart: `adept --global harness add claude-code && adept --global skill add … && adept --global sync`.
- Safety note: global sync never touches unmanaged files in home harness dirs.

- [ ] **Step 2: Update the other docs**

- `libraries.md`: clones now land in `<project>/.adeptability/libs/` (gitignored); machine store is the global scope's libs root and the fallback location; `adept migrate` localizes.
- `vendoring.md`: shrink to "delete `libs/` from `.adeptability/.gitignore` and commit" — the `ADEPT_LIBRARY` redirect section is superseded (keep a short note for CI-hermetic use of `ADEPT_LIBRARY`).
- `harnesses.md`: add a Global column to the adapter table + document `global-output`/`global-base-dir` YAML keys.
- `README.md`: add a "Global skills" subsection to the quickstart (3–6 lines).

Per repo convention (memory: release-channels-honesty): document only what ships — no speculative flags.

- [ ] **Step 3: Verify docs build (if mkdocs config exists)**

Run: `nix-shell -p mkdocs --run 'mkdocs build --strict'` (skip if the repo has no mkdocs config; if CI builds docs another way, mirror that).

- [ ] **Step 4: Commit**

```bash
git add docs/ README.md mkdocs.yml
git commit -m "docs: scopes concept, project-local libraries, global harness targets"
```

---

## Self-review notes

- Spec coverage: §1 scope model → Tasks 2+4; §2 library flip → Task 6; §3 fallback+migrate → Tasks 7+8; §4 adapter targets → Tasks 1+9; §5 orchestrator/safety → Tasks 2+3+10; §6 CLI surface → Tasks 4+5+8; §7 testing → per-task tests + Task 10; §8 docs → Task 11.
- `sync-from --global` is guarded to claude-only in v1 (Task 5 note) — a conscious narrowing of spec §6, recorded here; full global import needs spec-threading through `Import()` and is deferred.
- Global agent rendering deferred (Task 3) — spec never defined global agent targets.
