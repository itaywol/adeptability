# adeptability — authoring & drift demo
# Regenerate: build `adept` onto your PATH, then run from the repo root:
#   vhs assets/tapes/authoring.tape
# Requires a monospace font (JetBrains Mono) and vhs >= 0.11.

Output "assets/authoring.gif"

Set Shell "bash"
Set FontFamily "JetBrains Mono"
Set FontSize 26
Set Width 1320
Set Height 820
Set Padding 32
Set Margin 56
Set MarginFill "#0b111b"
Set BorderRadius 12
Set WindowBar Colorful
Set TypingSpeed 45ms
Set Theme { "background": "#111a29", "foreground": "#eaf0f8", "cursor": "#7c88ff", "selection": "#22314c", "black": "#0b111b", "red": "#ff6b6b", "green": "#7ed992", "yellow": "#f6c667", "blue": "#7c88ff", "magenta": "#b79cff", "cyan": "#4fe3cd", "white": "#c8d4e3", "brightBlack": "#64788f", "brightRed": "#ff8b64", "brightGreen": "#a5e8b5", "brightYellow": "#ffd98a", "brightBlue": "#a3acff", "brightMagenta": "#cdb8ff", "brightCyan": "#7ff0dd", "brightWhite": "#eaf0f8" }

# ---- setup (hidden) ----
Hide
Type "export ADEPT_LIBRARY=/tmp/adept-demo-auth/.lib" Enter
Type "export PS1=$'\033[38;2;124;136;255m❯\033[0m '" Enter
Type "rm -rf /tmp/adept-demo-auth && mkdir -p /tmp/adept-demo-auth && cd /tmp/adept-demo-auth && git init -q" Enter
Type "adept init --no-default-skills --mode copy >/dev/null" Enter
Type "for h in claude-code cursor codex opencode; do adept harness add $h >/dev/null; done" Enter
Type "clear" Enter
Show

# ---- author a new skill ----
Sleep 600ms
Type "adept skill add commit-style"
Sleep 300ms
Enter
Sleep 1800ms

# ---- publish it to every harness ----
Type "adept sync"
Sleep 300ms
Enter
Sleep 2200ms

# ---- refine the canonical skill ----
Type "echo '- Wrap the commit body at 72 columns.' >> .adeptability/skills/commit-style/SKILL.md"
Sleep 300ms
Enter
Sleep 800ms

# ---- adept sees the drift ----
Type "adept status"
Sleep 300ms
Enter
Sleep 3200ms

# ---- one sync brings every harness back in line ----
Type "adept sync"
Sleep 300ms
Enter
Sleep 2200ms

Type "adept status"
Sleep 300ms
Enter
Sleep 3000ms
