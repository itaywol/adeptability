package cli

import (
	"bufio"
	"regexp"
	"strings"
)

// sniffSkillBody is a cheap regex-based sanity check for SKILL.md
// content. We strip fenced code blocks so usage examples don't trip the
// scanner, then look for instructions that ask the agent to perform
// dangerous actions out-of-the-box.
//
// This is intentionally conservative — false positives are recoverable
// (`--allow-unsafe`); silent installs of malicious content are not.
// Phase 2 layers an LLM-based deeper check on top of this floor.
func sniffSkillBody(body string) []string {
	stripped := stripFences(body)
	var hits []string
	for _, rule := range sniffRules {
		if rule.re.MatchString(stripped) {
			hits = append(hits, rule.msg)
		}
	}
	return hits
}

type sniffRule struct {
	re  *regexp.Regexp
	msg string
}

// sniffRules is the canonical heuristic list. Each rule pairs a regex
// against a one-line message the install preview shows to the user.
// Patterns target the prose body — agent instructions like "run X" or
// "execute Y" — not legit code blocks.
var sniffRules = []sniffRule{
	{re: regexp.MustCompile(`(?i)\b(curl|wget)\b[^|\n]*\|\s*(sh|bash|zsh)\b`), msg: "asks the agent to pipe a remote download into a shell"},
	{re: regexp.MustCompile(`(?i)\brm\s+-[rR]f?\s+/(?:\s|$|\*)`), msg: "asks the agent to rm -rf a root path"},
	{re: regexp.MustCompile(`(?i)\bsudo\b`), msg: "asks the agent to escalate privileges via sudo"},
	{re: regexp.MustCompile(`(?i)\bchmod\s+(?:777|\+x)\b`), msg: "asks the agent to chmod 777 or mark files executable"},
	{re: regexp.MustCompile(`(?i)\bdd\s+if=`), msg: "asks the agent to invoke dd (raw disk writes)"},
	{re: regexp.MustCompile(`(?i)\bexec\(|\beval\(|\bos\.system\(`), msg: "asks the agent to call exec/eval/os.system"},
	{re: regexp.MustCompile(`(?i)~/\.ssh/|AWS_SECRET|GITHUB_TOKEN|OPENAI_API_KEY|ANTHROPIC_API_KEY`), msg: "references secrets / credential files"},
	{re: regexp.MustCompile(`(?i)\b(nc|netcat)\s+-l\b`), msg: "asks the agent to open a listening socket"},
	{re: regexp.MustCompile(`(?i)\bbase64\s+-d\b|\bxxd\s+-r\b`), msg: "decodes binary payloads from base64/xxd (obfuscation pattern)"},
}

// stripFences removes ``` … ``` and indented code blocks from md so the
// rules above only fire on prose instructions. Inline `code` spans stay
// (the rules' word boundaries make those safe).
func stripFences(body string) string {
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
	return out.String()
}
