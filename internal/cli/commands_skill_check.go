package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/itaywol/adeptability/internal/library"
	gh "github.com/itaywol/adeptability/internal/registry/github"
	"github.com/itaywol/adeptability/internal/registry"
	"github.com/itaywol/adeptability/internal/scan"
)

// newSkillCheckCmd is `adept skill check <target>`. Target shapes:
//
//   <skill-id>                       — project canonical
//   library:<name>:<skill-id>        — library-resolved skill
//   <owner>/<repo>[#ref]/<skill>     — remote skills.sh / GitHub skill
//
// Phase 2.1 runs regex-only static rules. Phase 2.2 layers an LLM
// intent pass on top of the same Finding shape so output formats stay
// identical regardless of whether the LLM is configured.
func newSkillCheckCmd(d *Deps) *cobra.Command {
	var format string
	var useLLM, noLLM bool
	c := &cobra.Command{
		Use:   "check <target>",
		Short: "Scan a skill (project | library:<name>:<id> | <owner>/<repo>/<skill>) for safety issues",
		Args:  cobra.ExactArgs(1),
	}
	c.Flags().StringVar(&format, "format", "table", "output format: table|markdown|json")
	c.Flags().BoolVar(&useLLM, "llm", false, "force the LLM intent pass (errors out when no provider configured)")
	c.Flags().BoolVar(&noLLM, "no-llm", false, "skip the LLM intent pass even when a provider is configured")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		target, err := resolveCheckTarget(ctx, d, args[0])
		if err != nil {
			return err
		}
		report := scan.NewScanner().Scan(target)
		// LLM intent pass: default-on when a provider is configured.
		// --no-llm disables; --llm forces and errors when not configured.
		if !noLLM {
			prov := d.LLMProvider()
			if useLLM && prov == nil {
				return fmt.Errorf("--llm requested but no provider configured (run `adept config llm set <provider>`)")
			}
			if prov != nil {
				if availErr := prov.Available(ctx); availErr == nil {
					reviewer := &scan.LLMReviewer{Provider: prov}
					if merged, err := reviewer.Review(ctx, target, report); err == nil {
						report = merged
					} else {
						d.Log.Warn("llm review failed; static-only report", "err", err)
					}
				} else {
					d.Log.Warn("llm provider unavailable; static-only report", "err", availErr)
				}
			}
		}
		w := cmd.OutOrStdout()
		switch format {
		case "table":
			fmt.Fprint(w, scan.FormatTable(report))
		case "markdown":
			fmt.Fprint(w, scan.FormatMarkdown(report))
		case "json":
			enc := json.NewEncoder(w)
			enc.SetIndent("", "  ")
			if err := enc.Encode(report); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown --format %q (want table|markdown|json)", format)
		}
		switch report.Worst() {
		case scan.SeverityCritical:
			return ErrDirty // exit 2 — distinct from generic-error 1
		case scan.SeverityHigh:
			return fmt.Errorf("scan: high-severity findings present")
		}
		return nil
	}
	return c
}

// resolveCheckTarget loads the SKILL.md (and known sidecars) for one of
// the three target shapes. Remote targets fetch and discard a tarball
// — nothing is written to disk.
func resolveCheckTarget(ctx context.Context, d *Deps, raw string) (scan.Target, error) {
	switch {
	case strings.HasPrefix(raw, "library:"):
		return resolveLibraryTarget(d, strings.TrimPrefix(raw, "library:"))
	case strings.Count(raw, "/") >= 2:
		return resolveRemoteTarget(ctx, d, raw)
	default:
		return resolveProjectTarget(d, raw)
	}
}

func resolveProjectTarget(d *Deps, id string) (scan.Target, error) {
	p, err := d.Project()
	if err != nil {
		return scan.Target{}, err
	}
	dir := filepath.Join(p.SkillsDir(), id)
	return readTarget("project:"+id, dir)
}

func resolveLibraryTarget(d *Deps, ref string) (scan.Target, error) {
	parts := strings.SplitN(ref, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return scan.Target{}, fmt.Errorf("library target %q: want library:<name>:<skill-id>", ref)
	}
	libName, skillID := parts[0], parts[1]
	p, err := d.Project()
	if err != nil {
		return scan.Target{}, err
	}
	multi, err := openMultiLibrary(d, p)
	if err != nil {
		return scan.Target{}, err
	}
	if multi == nil {
		return scan.Target{}, fmt.Errorf("project has no libraries configured")
	}
	var lib library.Library
	for _, n := range multi.Libraries() {
		if n.Name == libName {
			lib = n.Library
			break
		}
	}
	if lib == nil {
		return scan.Target{}, fmt.Errorf("library %q: not configured", libName)
	}
	dir := filepath.Join(lib.SkillsDir(), skillID)
	return readTarget("library:"+libName+":"+skillID, dir)
}

// resolveRemoteTarget pulls just enough tarball to scan: extract the
// skill subtree into memory, hand the bytes to the scanner, throw the
// rest away.
func resolveRemoteTarget(ctx context.Context, d *Deps, raw string) (scan.Target, error) {
	slug, err := registry.ParseSlug(raw)
	if err != nil {
		return scan.Target{}, err
	}
	sha, err := d.GitHub.ResolveRef(ctx, slug.Owner, slug.Repo, slug.Ref)
	if err != nil {
		return scan.Target{}, err
	}
	body, err := d.GitHub.FetchTarball(ctx, slug.Owner, slug.Repo, sha)
	if err != nil {
		return scan.Target{}, err
	}
	defer body.Close()
	files, _, err := gh.ExtractSkillDir(body, slug.Skill, slug.CandidateLayouts())
	if err != nil {
		return scan.Target{}, err
	}
	return targetFromFiles("remote:"+slug.String(), files), nil
}

// readTarget reads SKILL.md plus every other file under dir into memory
// so the scanner can see scripts/references too.
func readTarget(name, dir string) (scan.Target, error) {
	mdPath := filepath.Join(dir, "SKILL.md")
	body, err := os.ReadFile(mdPath)
	if err != nil {
		return scan.Target{}, fmt.Errorf("read %s: %w", mdPath, err)
	}
	side := map[string][]byte{}
	err = filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		if path == mdPath {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		side[filepath.ToSlash(rel)] = b
		return nil
	})
	if err != nil {
		return scan.Target{}, err
	}
	return scan.Target{Name: name, Body: body, Sidecars: side}, nil
}

func targetFromFiles(name string, files map[string][]byte) scan.Target {
	body := files["SKILL.md"]
	side := map[string][]byte{}
	for k, v := range files {
		if k == "SKILL.md" {
			continue
		}
		side[k] = v
	}
	return scan.Target{Name: name, Body: body, Sidecars: side}
}

// Sanity: ensure io is referenced via the scan package's text outputs.
var _ io.Writer = (*os.File)(nil)
