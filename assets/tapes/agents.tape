# adeptability — agents (subagents) demo
# Regenerate: build `adept` onto your PATH, then run from the repo root:
#   vhs assets/tapes/agents.tape
# Requires a monospace font (JetBrains Mono) and vhs >= 0.11.

Output "assets/agents.gif"

Set Shell "bash"
Set FontFamily "JetBrains Mono"
Set FontSize 24
Set Width 1500
Set Height 900
Set Padding 32
Set Margin 56
Set MarginFill "#0b111b"
Set BorderRadius 12
Set WindowBar Colorful
Set TypingSpeed 45ms
Set Theme { "background": "#111a29", "foreground": "#eaf0f8", "cursor": "#7c88ff", "selection": "#22314c", "black": "#0b111b", "red": "#ff6b6b", "green": "#7ed992", "yellow": "#f6c667", "blue": "#7c88ff", "magenta": "#b79cff", "cyan": "#4fe3cd", "white": "#c8d4e3", "brightBlack": "#64788f", "brightRed": "#ff8b64", "brightGreen": "#a5e8b5", "brightYellow": "#ffd98a", "brightBlue": "#a3acff", "brightMagenta": "#cdb8ff", "brightCyan": "#7ff0dd", "brightWhite": "#eaf0f8" }

# ---- setup (hidden) ----
Hide
Type "export ADEPT_LIBRARY=/tmp/adept-demo-agents/.lib" Enter
Type "export PS1=$'\033[38;2;124;136;255m❯\033[0m '" Enter
Type "rm -rf /tmp/adept-demo-agents && mkdir -p /tmp/adept-demo-agents && cd /tmp/adept-demo-agents && git init -q" Enter
Type "adept init --no-default-skills --mode copy >/dev/null" Enter
Type "for h in claude-code opencode codex; do adept harness add $h >/dev/null; done" Enter
Type "cat > reviewer.md <<'MD'" Enter
Type "---" Enter
Type "id: code-reviewer" Enter
Type "description: Reviews drafted code changes before commit. Use after any agent writes code." Enter
Type "tools: [Read, Grep, Bash]" Enter
Type "---" Enter
Type "You are an adversarial reviewer. Assume the work is broken until proven otherwise." Enter
Type "" Enter
Type "## Check, in order" Enter
Type "1. Does it run? Execute it — do not judge by reading." Enter
Type "2. Run the tests; paste the real output." Enter
Type "3. Hunt the edge cases the author skipped." Enter
Type "" Enter
Type "## Verdict" Enter
Type "PASS only if every check holds. Otherwise REJECT with each reason listed." Enter
Type "MD" Enter
Type "clear" Enter
Show

# ---- author a reviewer subagent from a file ----
Sleep 600ms
Type "adept agent add code-reviewer --from reviewer.md"
Sleep 300ms
Enter
Sleep 1600ms

Type "bat --style=plain .adeptability/agents/code-reviewer.md"
Sleep 300ms
Enter
Sleep 4000ms

Hide
Type "rm reviewer.md" Enter
Show

# ---- render it into every enabled harness ----
Type "adept sync"
Sleep 300ms
Enter
Sleep 2000ms

Type "tree -a --dirsfirst -I '.git|.adeptability' -C ."
Sleep 300ms
Enter
Sleep 2600ms

# ---- adept speaks 50+ harnesses out of the box ----
Type "adept harness list | head -18 | cut -c1-76"
Sleep 300ms
Enter
Sleep 3400ms
