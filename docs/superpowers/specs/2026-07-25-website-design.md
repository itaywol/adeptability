# adeptability website — design spec (2026-07-25)

Approved by Itay via interactive Q&A.

## Goal

Marketing + docs site at **https://adeptability.itaywol.tools**, replacing the
mkdocs GitHub Pages site. Modeled on testless (`site/` dir, Astro, Cloudflare
Workers static assets, `custom_domain` route) but with docs included.

## Architecture

- `site/` — one Astro 5 project.
  - Custom creative landing page at `/` (UI-Designer-driven, adeptability brand from `assets/logo.png`).
  - Starlight mounted at `/docs` with all mkdocs content migrated, nav preserved
    (Getting Started, Concepts, Guides, Command Reference, Harness Comparison, Exchange).
  - `wrangler.jsonc`: static assets from `./dist`, route `adeptability.itaywol.tools` with `custom_domain: true` (same shape as testless-site).
- mkdocs.yml removed. `.github/workflows/docs.yml` replaced by a Pages deploy of a
  meta-refresh redirect so `itaywol.github.io/adeptability/*` keeps resolving.

## Landing page content

Hero (what adept does: author a skill once → render into every coding agent),
demo gif section, install cards (Go / Homebrew / curl / Nix / Docker), how it
works (author → sync → 50+ harnesses), GitHub CTA. Footer:
GitHub · Buy Me a Coffee (buymeacoffee.com/woitayf) · itaywol.com · LinkedIn
(linkedin.com/in/itaywolfish).

## Demo gifs

Reproducible via vhs tapes committed at `assets/tapes/`:

1. `sync.tape` — hero: author skill, `adept sync`, fan-out into Claude Code/Cursor/Codex/Copilot/OpenCode.
2. `authoring.tape` — `adept skill add`, edit, drift detection (`adept status`).
3. `agents.tape` — `adept agent add` / exchange flow.

Consistent theme (same palette as site, generous padding, window chrome).
`assets/demo.gif` regenerated from the same pipeline for the README.

## Out of scope

CI deploy workflow for the site (manual `wrangler deploy`, like testless),
search, analytics, blog.
