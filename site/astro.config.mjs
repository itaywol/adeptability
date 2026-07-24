// @ts-check
import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";

// https://astro.build/config
export default defineConfig({
  site: "https://adeptability.itaywol.tools",
  integrations: [
    starlight({
      title: "adeptability",
      description:
        "Write an AI coding skill once, run it in every AI coding assistant.",
      logo: { src: "./src/assets/logo-mark.svg", alt: "adeptability" },
      favicon: "/favicon.svg",
      // Gold accent retheme, matching the landing page + neon-gold logo.
      customCss: ["./src/styles/starlight-gold.css"],
      // Social preview image on every docs page. Starlight already emits
      // og:title/description/url, og:site_name, and twitter:card, but not an
      // image, so we add og:image + twitter:image here.
      head: [
        {
          tag: "meta",
          attrs: {
            property: "og:image",
            content: "https://adeptability.itaywol.tools/social-card.png",
          },
        },
        {
          tag: "meta",
          attrs: {
            property: "og:image:alt",
            content:
              "adeptability: write an AI skill once, run it in every coding agent",
          },
        },
        {
          tag: "meta",
          attrs: {
            name: "twitter:image",
            content: "https://adeptability.itaywol.tools/social-card.png",
          },
        },
      ],
      social: [
        {
          icon: "github",
          label: "GitHub",
          href: "https://github.com/itaywol/adeptability",
        },
      ],
      // Docs live under /docs/ - content is nested in src/content/docs/docs/.
      sidebar: [
        { label: "Home", slug: "docs" },
        {
          label: "Getting Started",
          items: [
            { label: "Install", slug: "docs/getting-started/install" },
            { label: "Quickstart", slug: "docs/getting-started/quickstart" },
            { label: "Your first skill", slug: "docs/getting-started/first-skill" },
          ],
        },
        {
          label: "Concepts",
          items: [
            { label: "Canonical skills", slug: "docs/concepts/canonical-skills" },
            { label: "Canonical agents", slug: "docs/concepts/agents" },
            { label: "Harnesses & adapters", slug: "docs/concepts/harnesses" },
            { label: "Libraries", slug: "docs/concepts/libraries" },
            { label: "Scopes", slug: "docs/concepts/scopes" },
            { label: "Layouts", slug: "docs/concepts/layouts" },
            { label: "Drift & the 3-way model", slug: "docs/concepts/drift" },
          ],
        },
        {
          label: "Guides",
          items: [
            { label: "Authoring a skill", slug: "docs/guides/authoring" },
            { label: "Installing skills from skills.sh and GitHub", slug: "docs/guides/installing-skills" },
            { label: "Sharing a library with a team", slug: "docs/guides/sharing-a-library" },
            { label: "Importing existing rules", slug: "docs/guides/importing" },
            { label: "Vendoring a library in-repo", slug: "docs/guides/vendoring" },
            { label: "Git drift hook", slug: "docs/guides/git-hook" },
            { label: "Composing loops", slug: "docs/guides/loops" },
            { label: "Safety scans & LLM config", slug: "docs/guides/safety-scans" },
            { label: "Scripting and CI with --json", slug: "docs/guides/automation-json" },
          ],
        },
        {
          label: "Command Reference",
          items: [
            { label: "Command reference", slug: "docs/reference/commands" },
            { label: "Configuration reference", slug: "docs/reference/configuration" },
          ],
        },
        { label: "Harness Comparison", slug: "docs/harness-comparison" },
        { label: "Exchange", slug: "docs/exchange" },
      ],
    }),
  ],
});
