package agentlint

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/itaywol/adeptability/internal/scan"
)

// FileRef is one file reference extracted from an agent body.
type FileRef struct {
	// Path as written (cleaned of decoration).
	Path string
	// Explicit is true for unambiguous references (./ or ../ prefixed, or
	// markdown link targets) — a miss on these is an error. Bare @word.ext
	// mentions are informational only.
	Explicit bool
}

var (
	// atRefRE: "@path/to/file" or "@notes.md" — the capture must contain a
	// path separator or a dot to be a plausible file, and the preceding
	// character must not be a word char (emails) or another @.
	atRefRE = regexp.MustCompile(`(?:^|[\s(])@([\w./-]+)`)
	// backtickRE captures `...` spans; filtering happens in code.
	backtickRE = regexp.MustCompile("`([^`\\s]+)`")
	// mdLinkRE captures markdown link targets that are relative paths.
	mdLinkRE = regexp.MustCompile(`\]\((\.{1,2}/[^)#?\s]+)\)`)
	// knownExtRE marks path-looking strings by extension.
	knownExtRE = regexp.MustCompile(`\.(md|sh|py|ts|js|go|json|yaml|yml|txt|toml|csv|sql)$`)
)

// ExtractFileRefs finds file references in an agent body, working on the
// fence-stripped text so code examples do not fire. False-positive
// containment: URLs, absolute paths, glob patterns, and <placeholders> are
// skipped.
func ExtractFileRefs(body string) []FileRef {
	stripped, balanced := scan.StripFences(body)
	if !balanced {
		stripped = body
	}
	seen := map[string]bool{}
	var out []FileRef
	add := func(path string, explicit bool) {
		path = strings.TrimRight(path, ".,;:")
		if path == "" || seen[path] || !plausibleFileRef(path) {
			return
		}
		seen[path] = true
		out = append(out, FileRef{Path: path, Explicit: explicit})
	}
	for _, m := range mdLinkRE.FindAllStringSubmatch(stripped, -1) {
		add(m[1], true)
	}
	for _, m := range backtickRE.FindAllStringSubmatch(stripped, -1) {
		s := m[1]
		if strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") {
			add(s, true)
			continue
		}
		if strings.Contains(s, "/") && knownExtRE.MatchString(s) {
			add(s, false)
		}
	}
	for _, m := range atRefRE.FindAllStringSubmatch(stripped, -1) {
		s := m[1]
		// Require a path separator or extension so @handles don't fire.
		if strings.Contains(s, "/") || knownExtRE.MatchString(s) {
			add(s, false)
		}
	}
	return out
}

// plausibleFileRef rejects candidates that cannot be project files. Rooted
// paths are checked with an explicit "/" prefix as well as filepath.IsAbs:
// agent bodies use forward slashes regardless of OS, and on Windows
// IsAbs("/abs/path.md") is false (no drive letter), which would leak
// absolute unix-style refs into the existence check.
func plausibleFileRef(s string) bool {
	if strings.Contains(s, "://") || strings.HasPrefix(s, "/") || filepath.IsAbs(s) {
		return false
	}
	if strings.ContainsAny(s, "*?[{<>$") {
		return false
	}
	// A bare extension-less single word is not a file reference.
	if !strings.Contains(s, "/") && !strings.Contains(s, ".") {
		return false
	}
	return true
}

// ruleFileReferences verifies extracted references resolve under the project
// root (then under the agents dir). Explicit misses are errors; bare mentions
// are info.
func ruleFileReferences(in Input) []scan.Finding {
	if in.ProjectRoot == "" {
		return nil
	}
	refs := ExtractFileRefs(in.Agent.Body)
	out := make([]scan.Finding, 0, len(refs))
	for _, ref := range refs {
		if refExists(in.ProjectRoot, ref.Path) {
			continue
		}
		sev, conf := scan.SeverityLow, scan.ConfidenceLow
		id := "AGENT-LINT-206"
		if ref.Explicit {
			sev, conf = scan.SeverityHigh, scan.ConfidenceMedium
			id = "AGENT-LINT-005"
		}
		out = append(out, finding(in.Agent, id, CategoryReference, sev, conf,
			fmt.Sprintf("referenced file %q does not exist in the project", ref.Path),
			ref.Path,
			"Fix the path or create the file — the agent will fail (or hallucinate) when it tries to read a missing reference."))
	}
	return out
}

func refExists(root, ref string) bool {
	candidates := []string{
		filepath.Join(root, filepath.FromSlash(ref)),
		filepath.Join(root, ".adeptability", "agents", filepath.FromSlash(ref)),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return true
		}
	}
	return false
}
