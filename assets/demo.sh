#!/usr/bin/env bash
# Driver for the README demo GIF. Records a real `adept` session showing one
# canonical skill rendered into several harnesses at once.
#
# Regenerate the GIF (needs asciinema + agg + a monospace font):
#   go build -o /tmp/adept ./cmd/adept
#   asciinema rec -c "assets/demo.sh /tmp/adept" --overwrite assets/demo.cast
#   agg --font-family "JetBrains Mono" --theme dracula \
#       --cols 82 --rows 26 assets/demo.cast assets/demo.gif
set -e

ADEPT="${1:-adept}"
PROMPT="\033[38;5;212m$\033[0m"

# Type a command out like a human, then run it.
run() {
	printf "%b " "$PROMPT"
	printf '%s' "$1" | while IFS= read -r -n1 c; do printf '%s' "$c"; sleep 0.02; done
	printf '\n'
	sleep 0.4
	eval "$1"
	sleep 1.0
}
say() { printf "\033[38;5;245m# %s\033[0m\n" "$1"; sleep 0.8; }

REPO="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"
export ADEPT_LIBRARY="$(mktemp -d)"
cp -r "$REPO/examples/skills/pr-review" "$WORK/pr-review"
cd "$WORK"
git init -q
clear

say "one skill. every AI coding agent. no copy-paste."
sleep 0.6
run "$ADEPT init --no-default-skills --mode copy"
run "$ADEPT skill add pr-review --from ./pr-review"
say "pick the agents your team actually uses"
run "for h in claude-code cursor codex opencode; do $ADEPT harness add \$h; done"
say "render the canonical skill into every one, native format each"
run "$ADEPT sync"
say "one source you wrote -> every harness's format, none by hand"
run "find .claude .cursor .opencode AGENTS.md -type f | sort"
say "and it stays honest — drift detection is built in"
run "$ADEPT diff"
sleep 1.2
