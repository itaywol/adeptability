# /home/itay/Work/adeptability/assets/demo.tape
#
# Reproduces the README hero demo: a canonical skill added once, then
# `adept sync` rendering it into Claude Code, Cursor, Codex, and OpenCode
# at the same time. Same story as the old assets/demo.sh (asciinema+agg
# pipeline), now recorded with VHS on the project's canonical recipe (see
# testless/assets/demo.tape for the recipe this mirrors).
#
# Prereqs (run once, from the repo root, before rendering):
#   go build -o /tmp/adept ./cmd/adept
#   export PATH=/tmp:$PATH
# Then render:
#   nix-shell -p vhs --run 'vhs assets/demo.tape'

Output assets/demo.gif
Set Shell "bash"
Set FontFamily "JetBrains Mono"
Set FontSize 26
Set Width 1400
Set Height 800
Set Theme "Catppuccin Mocha"
Set Padding 30
Set Margin 0
Set BorderRadius 0
Set WindowBar Colorful
Set TypingSpeed 35ms
Set Framerate 24

Hide
Type "export ADEPT_WORK=/tmp/adept-demo-work && export ADEPT_LIBRARY=/tmp/adept-demo-lib && rm -rf $ADEPT_WORK $ADEPT_LIBRARY && mkdir -p $ADEPT_WORK $ADEPT_LIBRARY && cp -r /home/itay/Work/adeptability/examples/skills/pr-review $ADEPT_WORK/pr-review && cd $ADEPT_WORK && git init -q && export PS1='\[\e[1;35m\]❯\[\e[0m\] ' && clear"
Enter
Sleep 300ms
Type "adept init --no-default-skills --mode copy"
Show

Sleep 700ms
Enter
Sleep 1500ms

Type "adept skill add pr-review --from ./pr-review"
Enter
Sleep 1500ms

Type "for h in claude-code cursor codex opencode; do adept harness add $h; done"
Enter
Sleep 2s

Type "adept sync"
Enter
Sleep 2500ms

Type "find .claude .cursor .opencode AGENTS.md -type f | sort"
Enter
Sleep 2500ms

Type "adept diff"
Enter
Sleep 2500ms
