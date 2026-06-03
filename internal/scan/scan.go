// Package scan is the static (regex/AST) layer of adept's skill safety
// checks. It runs without LLM access, returns structured findings, and
// gives the caller a clear severity-driven gate.
//
// Phase 2.1 only — a follow-up phase wraps these findings with an LLM
// intent-evaluation pass and supports remote provider configuration.
// The package contract (Scanner.Scan -> Report) is deliberately stable
// so the LLM layer can produce additional findings of the same shape.
//
// Inspiration: getsentry/skills/skill-scanner — its 7-category taxonomy
// and severity ladder map cleanly onto the structured Finding type
// below. We do NOT vendor or call that scanner; we implement our own.
package scan

import (
	"bufio"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Severity ranks findings from Clean (no issues) to Critical (block).
type Severity string

const (
	SeverityClean    Severity = "clean"
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// Confidence is the scanner's certainty about a single finding.
// Used to surface "review carefully" hints without false-positive noise.
type Confidence string

const (
	ConfidenceLow    Confidence = "low"
	ConfidenceMedium Confidence = "medium"
	ConfidenceHigh   Confidence = "high"
)

// Category mirrors the skill-scanner taxonomy so users moving between
// tools see the same nouns.
type Category string

const (
	CategoryPromptInjection Category = "prompt-injection"
	CategoryMaliciousCode   Category = "malicious-code"
	CategoryExcessivePerms  Category = "excessive-permissions"
	CategorySecretExposure  Category = "secret-exposure"
	CategorySupplyChain     Category = "supply-chain"
	CategoryURLAnalysis     Category = "url-analysis"
	CategoryFrontmatter     Category = "frontmatter"
)

// Finding is one structured issue. ID is a stable opaque identifier
// (SKILL-SEC-NNN) so users can suppress or reference individual rules.
type Finding struct {
	ID          string     `json:"id"`
	Category    Category   `json:"category"`
	Severity    Severity   `json:"severity"`
	Confidence  Confidence `json:"confidence"`
	Location    string     `json:"location"`
	Issue       string     `json:"issue"`
	Evidence    string     `json:"evidence,omitempty"`
	Risk        string     `json:"risk,omitempty"`
	Remediation string     `json:"remediation,omitempty"`
}

// Report aggregates the findings for one target and exposes the highest
// severity for gating decisions.
type Report struct {
	Target   string    `json:"target"`
	Findings []Finding `json:"findings"`
}

// Worst returns the highest severity present in r.Findings. Empty
// findings → SeverityClean.
func (r Report) Worst() Severity {
	order := map[Severity]int{
		SeverityClean: 0, SeverityLow: 1, SeverityMedium: 2,
		SeverityHigh: 3, SeverityCritical: 4,
	}
	worst := SeverityClean
	for _, f := range r.Findings {
		if order[f.Severity] > order[worst] {
			worst = f.Severity
		}
	}
	return worst
}

// Counts buckets findings by severity for the table renderer.
func (r Report) Counts() map[Severity]int {
	out := map[Severity]int{}
	for _, f := range r.Findings {
		out[f.Severity]++
	}
	return out
}

// Target describes WHAT was scanned. Body is the raw SKILL.md bytes;
// Sidecars maps relative paths to bytes for any scripts/references the
// scanner should pick up.
type Target struct {
	Name     string
	Body     []byte
	Sidecars map[string][]byte
}

// Scanner is the static analyzer. NewScanner returns one wired with the
// canonical rule list; tests pass custom Rules to exercise edge cases.
type Scanner struct {
	Rules []Rule
}

// Rule is one deterministic pattern. Findings produced by a Rule inherit
// its metadata; Match returns the matching slice (used as Evidence) and
// a boolean.
type Rule struct {
	ID          string
	Category    Category
	Severity    Severity
	Confidence  Confidence
	Issue       string
	Risk        string
	Remediation string
	Match       func(stripped string) (evidence string, ok bool)
}

// rmRootPattern matches a recursive+force rm targeting the filesystem
// root or a well-known system/home subtree. It is deliberately tolerant:
//
//   - either flag order (-rf or -fr) and combined short flags (-Rf,
//     -rfv, ...) — the flag group just has to contain both r and f.
//   - intervening flags such as `--no-preserve-root` between the rm
//     flags and the path.
//   - destruction of bare `/`, `/*`, or known critical roots
//     (/bin /etc /usr /var /home /root /boot /lib /sys ...), not just `/`.
//
// This closes the evasion gaps where `rm -fr /`, `rm -rf --no-preserve-root /`,
// and `rm -rf /etc` previously slipped past the narrower `-[rR]f?\s+/` form.
const rmRootPattern = `(?i)\brm\s+(?:--?[\w-]+\s+)*-[a-z]*(?:r[a-z]*f|f[a-z]*r)[a-z]*(?:\s+--?[\w-]+)*\s+/(?:bin|etc|usr|var|home|root|boot|lib|lib64|sys|proc|dev|opt|sbin)?(?:\s|$|\*|/)`

// NewScanner constructs a Scanner with the default rule set. Phase 2.2
// adds extension points for user-provided LLM-derived rules; for now the
// set is fixed.
func NewScanner() *Scanner {
	return &Scanner{Rules: DefaultRules()}
}

// Scan runs every rule against target.Body (after fenced-code stripping)
// and returns a Report. Sidecars are scanned with a smaller subset of
// "high-confidence script danger" rules — the body rules are tuned for
// prose markdown and would over-fire on bash.
func (s *Scanner) Scan(target Target) Report {
	raw := string(target.Body)
	stripped, balanced := stripFences(raw)
	// Scan-evasion guard: an unterminated/unbalanced fence makes
	// stripFences swallow everything after the lone opener, hiding any
	// dangerous prose or prompt injection placed below it. When fences
	// are unbalanced we cannot trust the stripped view, so we scan the
	// raw body instead — usage-example false positives are an acceptable
	// trade against silently dropping a payload.
	if !balanced {
		stripped = raw
	}
	out := Report{Target: target.Name}
	for _, rule := range s.Rules {
		if rule.Match == nil {
			continue
		}
		if ev, ok := rule.Match(stripped); ok {
			out.Findings = append(out.Findings, Finding{
				ID:          rule.ID,
				Category:    rule.Category,
				Severity:    rule.Severity,
				Confidence:  rule.Confidence,
				Location:    "SKILL.md",
				Issue:       rule.Issue,
				Evidence:    snippet(ev),
				Risk:        rule.Risk,
				Remediation: rule.Remediation,
			})
		}
	}
	// Frontmatter checks run separately so we can report `SKILL.md:1`
	// with confidence.
	if fmFindings := scanFrontmatter(target); len(fmFindings) > 0 {
		out.Findings = append(out.Findings, fmFindings...)
	}
	// Scripts/references receive a tighter rule subset (script-danger
	// only) — prose patterns over-fire on legit bash.
	for path, body := range target.Sidecars {
		if !looksLikeScript(path) {
			continue
		}
		for _, rule := range scriptRules {
			if ev, ok := rule.Match(string(body)); ok {
				f := Finding{
					ID:          rule.ID,
					Category:    rule.Category,
					Severity:    rule.Severity,
					Confidence:  rule.Confidence,
					Location:    path,
					Issue:       rule.Issue,
					Evidence:    snippet(ev),
					Risk:        rule.Risk,
					Remediation: rule.Remediation,
				}
				out.Findings = append(out.Findings, f)
			}
		}
	}
	// Stable ordering: severity desc, then ID asc.
	sort.Slice(out.Findings, func(i, j int) bool {
		oi := SeverityRank(out.Findings[i].Severity)
		oj := SeverityRank(out.Findings[j].Severity)
		if oi != oj {
			return oi > oj
		}
		return out.Findings[i].ID < out.Findings[j].ID
	})
	return out
}

// SeverityRank maps a severity to a monotonic integer (critical=4 .. clean=0)
// so callers can compare and order findings. An unknown, non-empty severity
// fails closed at the most-severe rank so a typo or attacker-supplied value
// can never silently lower a gate.
func SeverityRank(s Severity) int {
	switch s {
	case SeverityCritical:
		return 4
	case SeverityHigh:
		return 3
	case SeverityMedium:
		return 2
	case SeverityLow:
		return 1
	case SeverityClean, "":
		return 0
	}
	// Unknown but non-empty severity: fail closed. Treating it as the
	// lowest rank (0) would let a typo or attacker-supplied value
	// silently disable the gate, so we rank it as the most severe.
	return 4
}

// ParseSeverity normalizes an arbitrary (e.g. config- or LLM-supplied)
// severity string to one of the known Severity values. The match is
// case-insensitive and whitespace-trimmed. ok is false when the input
// does not name a known severity; callers should fail closed on !ok.
func ParseSeverity(s string) (Severity, bool) {
	switch Severity(strings.ToLower(strings.TrimSpace(s))) {
	case SeverityClean:
		return SeverityClean, true
	case SeverityLow:
		return SeverityLow, true
	case SeverityMedium:
		return SeverityMedium, true
	case SeverityHigh:
		return SeverityHigh, true
	case SeverityCritical:
		return SeverityCritical, true
	}
	return "", false
}

func looksLikeScript(path string) bool {
	low := strings.ToLower(path)
	for _, ext := range []string{".sh", ".bash", ".zsh", ".py", ".js", ".ts", ".rb", ".pl", ".ps1"} {
		if strings.HasSuffix(low, ext) {
			return true
		}
	}
	return strings.HasPrefix(low, "scripts/")
}

// snippet trims an evidence string to a single short line for table
// rendering. Keeps multi-line patterns readable in compact output.
func snippet(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i] + "…"
	}
	if len(s) > 120 {
		s = s[:117] + "…"
	}
	return s
}

// stripFences removes fenced code blocks. Prose rules run on what
// remains so usage examples (“ ```bash curl X | sh ``` “) do not
// false-fire. The returned bool is false when the fences are
// unbalanced (an opener with no matching close): in that case the
// caller must NOT trust the stripped output, because an unterminated
// fence would otherwise hide all subsequent content from the rules.
func stripFences(body string) (string, bool) {
	var out strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)
	inFence := false
	for scanner.Scan() {
		line := scanner.Text()
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	// inFence still true at EOF ⇒ an opener was never closed.
	return out.String(), !inFence
}

// DefaultRules is the canonical static rule set. Patterns target prose
// instructions — the assumption is "the SKILL.md tells the agent what
// to do", so anything imperative + dangerous is a finding.
func DefaultRules() []Rule {
	mk := func(re string) func(string) (string, bool) {
		r := regexp.MustCompile(re)
		return func(s string) (string, bool) {
			loc := r.FindStringIndex(s)
			if loc == nil {
				return "", false
			}
			return s[loc[0]:loc[1]], true
		}
	}
	return []Rule{
		// ── Malicious code ───────────────────────────────────────────
		{
			ID: "SKILL-SEC-001", Category: CategoryMaliciousCode, Severity: SeverityCritical, Confidence: ConfidenceHigh,
			Issue:       "asks the agent to pipe a remote download into a shell",
			Risk:        "remote code execution from any controlled URL",
			Remediation: "remove the curl|sh pattern; reference signed binaries or vetted package managers",
			Match:       mk(`(?i)\b(curl|wget)\b[^|\n]{0,200}\|\s*(sh|bash|zsh)\b`),
		},
		{
			ID: "SKILL-SEC-002", Category: CategoryMaliciousCode, Severity: SeverityCritical, Confidence: ConfidenceHigh,
			Issue:       "asks the agent to recursively delete a root path",
			Risk:        "destruction of host filesystem",
			Remediation: "scope deletion to the project tree or use the agent's safer file APIs",
			Match:       mk(rmRootPattern),
		},
		{
			ID: "SKILL-SEC-003", Category: CategoryMaliciousCode, Severity: SeverityHigh, Confidence: ConfidenceHigh,
			Issue:       "asks the agent to escalate privileges via sudo",
			Risk:        "agent runs commands as root",
			Remediation: "drop sudo; document the requirement and let the user grant it",
			Match:       mk(`(?i)\bsudo\b`),
		},
		{
			ID: "SKILL-SEC-004", Category: CategoryMaliciousCode, Severity: SeverityHigh, Confidence: ConfidenceHigh,
			Issue:       "asks the agent to chmod 777 or mark files executable",
			Risk:        "broad permission grants",
			Remediation: "specify exact permission bits or use the host package manager",
			Match:       mk(`(?i)\bchmod\s+(?:777|\+x)\b`),
		},
		{
			ID: "SKILL-SEC-005", Category: CategoryMaliciousCode, Severity: SeverityHigh, Confidence: ConfidenceMedium,
			Issue:       "calls exec/eval/os.system at runtime",
			Risk:        "dynamic code execution surface",
			Remediation: "call the function directly without string evaluation",
			Match:       mk(`(?i)\bexec\(|\beval\(|\bos\.system\(`),
		},
		{
			ID: "SKILL-SEC-006", Category: CategoryMaliciousCode, Severity: SeverityHigh, Confidence: ConfidenceMedium,
			Issue:       "decodes binary payloads from base64/xxd (obfuscation pattern)",
			Risk:        "hides intent behind encoded payloads",
			Remediation: "inline plaintext content or document the source",
			Match:       mk(`(?i)\bbase64\s+-d\b|\bxxd\s+-r\b`),
		},
		{
			ID: "SKILL-SEC-007", Category: CategoryMaliciousCode, Severity: SeverityMedium, Confidence: ConfidenceHigh,
			Issue:       "opens a listening network socket",
			Risk:        "agent exposes a service",
			Remediation: "remove or restrict to local loopback with explicit user consent",
			Match:       mk(`(?i)\b(nc|netcat)\s+-l\b`),
		},
		{
			ID: "SKILL-SEC-008", Category: CategoryMaliciousCode, Severity: SeverityHigh, Confidence: ConfidenceHigh,
			Issue:       "invokes dd (raw disk writes)",
			Risk:        "data loss / drive corruption",
			Remediation: "use higher-level file APIs",
			Match:       mk(`(?i)\bdd\s+if=`),
		},

		// ── Secret exposure ──────────────────────────────────────────
		{
			ID: "SKILL-SEC-101", Category: CategorySecretExposure, Severity: SeverityHigh, Confidence: ConfidenceMedium,
			Issue:       "references credential files or known secret env vars",
			Risk:        "agent reads or exfiltrates secrets",
			Remediation: "instruct the agent to ask the user instead of reading files directly",
			Match:       mk(`(?i)~/\.ssh/|AWS_SECRET_ACCESS_KEY|GITHUB_TOKEN|OPENAI_API_KEY|ANTHROPIC_API_KEY|GOOGLE_API_KEY|SLACK_(?:BOT|USER)_TOKEN|\.env(?:\.local)?\b`),
		},
		{
			ID: "SKILL-SEC-102", Category: CategorySecretExposure, Severity: SeverityMedium, Confidence: ConfidenceMedium,
			Issue:       "captures environment variables into a file or output",
			Risk:        "agent writes secrets to disk or stdout",
			Remediation: "remove the env dump; rely on the harness's secret injection",
			Match:       mk(`(?i)\benv\b.{0,40}>(?:>?)\s*\S+|printenv\s+>`),
		},

		// ── Prompt injection ─────────────────────────────────────────
		{
			ID: "SKILL-SEC-201", Category: CategoryPromptInjection, Severity: SeverityHigh, Confidence: ConfidenceMedium,
			Issue:       "instructs the agent to ignore previous instructions",
			Risk:        "classic prompt-injection vector",
			Remediation: "remove the override phrasing",
			Match:       mk(`(?i)\bignore (?:all )?(?:previous|prior|earlier) (?:instructions|prompts|rules)\b`),
		},
		{
			ID: "SKILL-SEC-202", Category: CategoryPromptInjection, Severity: SeverityHigh, Confidence: ConfidenceMedium,
			Issue:       "instructs the agent to reveal its system prompt or hidden context",
			Risk:        "exfiltrates parent system instructions",
			Remediation: "remove the reveal/print-system-prompt directive",
			Match:       mk(`(?i)\b(?:reveal|print|leak|dump|exfiltrate)\b.{0,30}\b(?:system|hidden|developer)\b.{0,30}\b(?:prompt|instructions|message)`),
		},
		{
			ID: "SKILL-SEC-203", Category: CategoryPromptInjection, Severity: SeverityMedium, Confidence: ConfidenceLow,
			Issue:       "uses jailbreak phrasing (DAN, do anything now, role-play override)",
			Risk:        "intent to bypass safety alignment",
			Remediation: "rewrite to describe the legitimate task without persona switching",
			Match:       mk(`(?i)\b(do anything now|DAN mode|developer mode|jailbreak|act as if you have no rules)\b`),
		},

		// ── URL analysis ─────────────────────────────────────────────
		{
			ID: "SKILL-SEC-301", Category: CategoryURLAnalysis, Severity: SeverityMedium, Confidence: ConfidenceMedium,
			Issue:       "references a URL shortener",
			Risk:        "destination URL hidden behind a shortener",
			Remediation: "use the canonical destination URL so reviewers can vet it",
			Match:       mk(`(?i)\bhttps?://(?:bit\.ly|t\.co|tinyurl\.com|goo\.gl|ow\.ly|is\.gd|buff\.ly|adf\.ly|cutt\.ly)/\S+`),
		},
		{
			ID: "SKILL-SEC-302", Category: CategoryURLAnalysis, Severity: SeverityMedium, Confidence: ConfidenceMedium,
			Issue:       "fetches remote content with raw IP addresses",
			Risk:        "no DNS auditability; commonly used in exfiltration",
			Remediation: "use a named domain or remove the network step",
			Match:       mk(`\bhttps?://\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`),
		},

		// ── Supply chain ─────────────────────────────────────────────
		{
			ID: "SKILL-SEC-401", Category: CategorySupplyChain, Severity: SeverityMedium, Confidence: ConfidenceMedium,
			Issue:       "installs from arbitrary git URLs at runtime",
			Risk:        "pulls unvetted code",
			Remediation: "pin to a tag or SHA via package manager",
			Match:       mk(`(?i)\b(?:pip|uv)\s+install\s+git\+|npm\s+(?:i|install)\s+git\+|go\s+install\s+\S+@latest`),
		},
	}
}

// scriptRules apply to script sidecars where bash is expected, so we
// only flag high-confidence dangers (curl|sh, rm -rf /, raw secrets).
var scriptRules = []Rule{
	{
		ID: "SKILL-SEC-001", Category: CategoryMaliciousCode, Severity: SeverityCritical, Confidence: ConfidenceHigh,
		Issue:       "script pipes a remote download into a shell",
		Risk:        "remote code execution from controlled URL",
		Remediation: "fetch via vetted package manager",
		Match: func(s string) (string, bool) {
			r := regexp.MustCompile(`(?i)(curl|wget)\b[^|\n]{0,200}\|\s*(sh|bash|zsh)\b`)
			loc := r.FindStringIndex(s)
			if loc == nil {
				return "", false
			}
			return s[loc[0]:loc[1]], true
		},
	},
	{
		ID: "SKILL-SEC-002", Category: CategoryMaliciousCode, Severity: SeverityCritical, Confidence: ConfidenceHigh,
		Issue:       "script removes a root path",
		Risk:        "destruction of host filesystem",
		Remediation: "scope deletion to a known path",
		Match: func(s string) (string, bool) {
			r := regexp.MustCompile(rmRootPattern)
			loc := r.FindStringIndex(s)
			if loc == nil {
				return "", false
			}
			return s[loc[0]:loc[1]], true
		},
	},
	{
		ID: "SKILL-SEC-103", Category: CategorySecretExposure, Severity: SeverityHigh, Confidence: ConfidenceHigh,
		Issue:       "script reads ssh/credential files",
		Risk:        "credential theft surface",
		Remediation: "use the harness's secret API instead of reading from disk",
		Match: func(s string) (string, bool) {
			r := regexp.MustCompile(`(?i)~/\.ssh/|~/\.aws/credentials|/root/\.docker/config\.json|kubectl\s+config\s+view`)
			loc := r.FindStringIndex(s)
			if loc == nil {
				return "", false
			}
			return s[loc[0]:loc[1]], true
		},
	},
}

// extractFrontmatter returns the YAML frontmatter block (the text
// between the opening and closing delimiters) when the body actually
// begins with a frontmatter block. It requires the opening delimiter to
// be a line that is EXACTLY "---" (trimmed) and a later line that is
// EXACTLY "---" to close it. This rejects markdown thematic breaks like
// "------" or "----" at the top of a prose document, which the previous
// HasPrefix("---") + Index("---") logic mistook for an empty frontmatter
// block and then false-flagged as "missing description".
func extractFrontmatter(body string) (string, bool) {
	lines := strings.Split(body, "\n")
	i := 0
	// Skip leading blank lines before the opening delimiter.
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= len(lines) || strings.TrimSpace(lines[i]) != "---" {
		return "", false
	}
	start := i + 1
	for j := start; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == "---" {
			return strings.Join(lines[start:j], "\n"), true
		}
	}
	return "", false
}

// scanFrontmatter parses the YAML frontmatter (best-effort) and reports
// validation issues. It does not validate against the canonical schema;
// the canonical loader does that on install. The intent here is to
// catch over-permissioned skills and missing description before the
// agent ever runs them.
func scanFrontmatter(t Target) []Finding {
	fm, ok := extractFrontmatter(string(t.Body))
	if !ok {
		return nil
	}
	var out []Finding
	if !strings.Contains(fm, "description:") {
		out = append(out, Finding{
			ID: "SKILL-FM-001", Category: CategoryFrontmatter, Severity: SeverityMedium, Confidence: ConfidenceHigh,
			Location: "SKILL.md:1", Issue: "missing description in frontmatter",
			Risk:        "harness cannot decide when to activate the skill",
			Remediation: "add a one-line description that mentions the activation triggers",
		})
	}
	// allowed-tools: * is the textbook over-broad permission. Catch the
	// inline scalar, the inline-flow-array, AND the YAML block-sequence
	// form:
	//   allowed-tools: '*'
	//   allowed-tools: ['*']
	//   allowed-tools:
	//     - '*'
	if regexp.MustCompile(`(?m)^allowed-tools:\s*['\"]?\*['\"]?\s*$`).MatchString(fm) ||
		regexp.MustCompile(`(?m)^allowed-tools:\s*\[\s*['\"]?\*['\"]?\s*\]\s*$`).MatchString(fm) ||
		allowedToolsBlockListStar(fm) {
		out = append(out, Finding{
			ID: "SKILL-PERM-001", Category: CategoryExcessivePerms, Severity: SeverityHigh, Confidence: ConfidenceHigh,
			Location: "SKILL.md:1", Issue: "allowed-tools is unrestricted (*)",
			Risk:        "skill can drive every tool the harness exposes",
			Remediation: "narrow allowed-tools to the minimum set the skill needs",
		})
	}
	// Bash without explicit justification is worth flagging.
	if regexp.MustCompile(`(?i)\ballowed-tools:.*\bBash\b`).MatchString(fm) {
		out = append(out, Finding{
			ID: "SKILL-PERM-002", Category: CategoryExcessivePerms, Severity: SeverityMedium, Confidence: ConfidenceLow,
			Location: "SKILL.md:1", Issue: "skill requests the Bash tool",
			Risk:        "bash exec breaks containment promises",
			Remediation: "confirm the SKILL.md body explains why bash is necessary",
		})
	}
	return out
}

// allowedToolsBlockListStar reports whether `allowed-tools:` is followed
// by a YAML block-sequence whose first item is the `*` wildcard, e.g.
//
//	allowed-tools:
//	  - '*'
//
// The regex requires the wildcard item to immediately follow the key
// (optionally after blank lines) so a `- '*'` belonging to some other
// key does not trigger a false positive.
var allowedToolsBlockStarRE = regexp.MustCompile(`(?m)^allowed-tools:\s*$(?:\s*\n)*\s*-\s*['"]?\*['"]?\s*$`)

func allowedToolsBlockListStar(fm string) bool {
	return allowedToolsBlockStarRE.MatchString(fm)
}

// FormatTable renders r as a compact tab-separated table.
func FormatTable(r Report) string {
	var b strings.Builder
	counts := r.Counts()
	fmt.Fprintf(&b, "target: %s\nworst:  %s\n", r.Target, r.Worst())
	fmt.Fprintf(&b, "counts: critical=%d high=%d medium=%d low=%d\n\n",
		counts[SeverityCritical], counts[SeverityHigh],
		counts[SeverityMedium], counts[SeverityLow])
	if len(r.Findings) == 0 {
		b.WriteString("no findings\n")
		return b.String()
	}
	fmt.Fprintln(&b, "ID\tSEVERITY\tCATEGORY\tLOCATION\tISSUE")
	for _, f := range r.Findings {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\n",
			f.ID, f.Severity, f.Category, f.Location, f.Issue)
	}
	return b.String()
}

// FormatMarkdown renders r as a multi-section markdown report — one
// section per finding with evidence and remediation called out. Suitable
// for code review comments or piping into a PR body.
func FormatMarkdown(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Skill safety report — %s\n\n", r.Target)
	fmt.Fprintf(&b, "**Worst severity:** %s\n\n", r.Worst())
	counts := r.Counts()
	fmt.Fprintf(&b, "Counts: critical=%d high=%d medium=%d low=%d\n\n",
		counts[SeverityCritical], counts[SeverityHigh],
		counts[SeverityMedium], counts[SeverityLow])
	if len(r.Findings) == 0 {
		b.WriteString("No findings.\n")
		return b.String()
	}
	for _, f := range r.Findings {
		fmt.Fprintf(&b, "## [%s] %s (%s)\n", f.ID, f.Issue, f.Severity)
		fmt.Fprintf(&b, "- **Category:** %s\n", f.Category)
		fmt.Fprintf(&b, "- **Confidence:** %s\n", f.Confidence)
		fmt.Fprintf(&b, "- **Location:** `%s`\n", f.Location)
		if f.Evidence != "" {
			fmt.Fprintf(&b, "- **Evidence:** `%s`\n", f.Evidence)
		}
		if f.Risk != "" {
			fmt.Fprintf(&b, "- **Risk:** %s\n", f.Risk)
		}
		if f.Remediation != "" {
			fmt.Fprintf(&b, "- **Remediation:** %s\n", f.Remediation)
		}
		b.WriteString("\n")
	}
	return b.String()
}
