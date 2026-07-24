# adeptability — hero demo
# Regenerate: build `adept` onto your PATH, then run from the repo root:
#   vhs assets/tapes/demo.tape
# Requires a monospace font (JetBrains Mono) and vhs >= 0.11.

Output "assets/demo.gif"

Set Shell "bash"
Set FontFamily "JetBrains Mono"
Set FontSize 26
Set Width 1520
Set Height 856
Set Padding 32
Set TypingSpeed 45ms
Set Theme { "background": "#111a29", "foreground": "#eaf0f8", "cursor": "#7c88ff", "selection": "#22314c", "black": "#0b111b", "red": "#ff6b6b", "green": "#7ed992", "yellow": "#f6c667", "blue": "#7c88ff", "magenta": "#b79cff", "cyan": "#4fe3cd", "white": "#c8d4e3", "brightBlack": "#64788f", "brightRed": "#ff8b64", "brightGreen": "#a5e8b5", "brightYellow": "#ffd98a", "brightBlue": "#a3acff", "brightMagenta": "#cdb8ff", "brightCyan": "#7ff0dd", "brightWhite": "#eaf0f8" }

# ---- setup (hidden) ----
Hide
Type "export ADEPT_LIBRARY=/tmp/adept-demo-hero/.lib" Enter
Type "export PS1=$'\033[38;2;124;136;255m❯\033[0m '" Enter
Type "rm -rf /tmp/adept-demo-hero && mkdir -p /tmp/adept-demo-hero && cd /tmp/adept-demo-hero && git init -q" Enter
Type "adept init --no-default-skills --mode copy >/dev/null" Enter
Type "for h in claude-code cursor codex copilot opencode; do adept harness add $h >/dev/null; done" Enter
Type "mkdir -p .adeptability/skills/code-style" Enter
Type "cat > .adeptability/skills/code-style/SKILL.md <<'MD'" Enter
Type "---" Enter
Type "id: code-style" Enter
Type "description: Apply when writing or reviewing code." Enter
Type "activation: globs" Enter
Type 'globs: ["**/*.go", "**/*.ts"]' Enter
Type "allowed-tools: [Read, Grep]" Enter
Type "---" Enter
Type "# House code style" Enter
Type "" Enter
Type "- Prefer early returns over nested conditionals." Enter
Type "- Name things for what they do, not how they do it." Enter
Type "- Wrap errors with context; never swallow them silently." Enter
Type "- No unused imports, no dead code." Enter
Type "MD" Enter
Type "clear" Enter
Show

# ---- one canonical skill ----
Sleep 600ms
Type "bat --style=plain .adeptability/skills/code-style/SKILL.md"
Sleep 400ms
Enter
Sleep 3s

# ---- render it into every enabled harness ----
Type "adept sync"
Sleep 400ms
Enter
Sleep 2500ms

# ---- one source, every harness's native format ----
Type "tree -a --dirsfirst -I '.git|.adeptability' -C ."
Sleep 400ms
Enter
Sleep 4500ms
