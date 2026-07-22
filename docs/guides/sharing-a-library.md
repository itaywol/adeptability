# Sharing a library across a team

Author skills once, publish them as a library, and let every project and teammate consume them
by reference. Read [Libraries](../concepts/libraries.md) first for the package-manager model.

## 1. Create the library repo

Initialize a repo in **library layout** so its skills sit at `<root>/skills/`, directly
consumable:

```bash
mkdir team-skills && cd team-skills
git init
adept init --as-library
```

Add published skills:

```bash
adept skill add house-style --publish --edit
adept skill add pr-review   --publish --edit
```

Commit and push:

```bash
git add . && git commit -m "feat: initial team skills"
git remote add origin git@github.com:acme/skills.git
git push -u origin main
```

## 2. Consume it in a project

In any project that should use these skills:

```bash
cd ../webapp
adept library add team-skills --from git@github.com:acme/skills.git --ref main
adept harness add claude-code
adept sync
```

`adept` clones the library into the project-local libs dir
(`.adeptability/libs/team-skills/`, auto-gitignored) and records a **reference** — not the
bytes — in `.adeptability/config.json`. Commit that config:

```bash
git add .adeptability/config.json && git commit -m "chore: add team-skills library"
```

## 3. Teammates onboard automatically

A teammate cloning `webapp` gets the reference but not the skill content (it was never
committed). They resolve it into their own store and materialize:

```bash
git clone git@github.com:acme/webapp.git && cd webapp
adept status                                   # shows the library as "no (run `adept library add`…)"
adept library add team-skills --from git@github.com:acme/skills.git
adept sync                                      # materialize into their harnesses
```

See [Libraries → Teammate onboarding flow](../concepts/libraries.md#teammate-onboarding-flow)
for the full picture.

## 4. Ship updates

When you push new skills to the library repo, consumers pull them on demand:

```bash
adept status --fetch          # refresh remote tracking refs, show what's updatable
adept library update          # fetch + apply (prompts first)
adept library update --yes    # non-interactive
adept sync                     # re-materialize into harnesses
```

## Precedence rules

- **Project canonical shadows the library** — a project skill with the same id wins, so a
  project can override a shared skill locally.
- **First-wins across libraries** — stack multiple `library add` calls; on an id collision the
  first-added library wins.
- Confirm the resolved set anytime with `adept skill list`.
