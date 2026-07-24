// Generates public/llms.txt and public/llms-full.txt from the Starlight docs.
//
// - llms.txt      : spec-compliant (llmstxt.org) curated index. H1, one-paragraph
//                   summary, then H2 sections of [title](url): description links.
// - llms-full.txt : the full markdown of every docs page, concatenated, with
//                   frontmatter stripped and a per-page H1 + canonical URL line.
//
// Wired as an npm "prebuild" step so both files regenerate on every build and
// cannot go stale. No dependencies: plain Node + fs.

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(__dirname, "..");
const DOCS_DIR = path.join(ROOT, "src/content/docs/docs");
const PUBLIC_DIR = path.join(ROOT, "public");
const SITE = "https://adeptability.itaywol.tools";

// One-paragraph project summary. Authored copy, kept in sync with the landing page.
const SUMMARY =
  "adept is an open source Go CLI (MIT licensed) that lets you author an AI coding " +
  "skill or subagent once, in a single canonical format, then render it accurately " +
  "into Claude Code, Cursor, GitHub Copilot, OpenAI Codex, OpenCode, and 50+ other AI " +
  "coding agents. Think of it as dotfiles for your AI coding agents: one source of " +
  "truth, and the correct on-disk format everywhere. A hash-based 3-way detector flags " +
  "drift, and libraries let a team share a versioned set of skills by reference.";

// Curated section order for llms.txt. Slugs are relative to /docs/ (no extension).
const GROUPS = [
  {
    name: "Getting started",
    slugs: [
      "getting-started/install",
      "getting-started/quickstart",
      "getting-started/first-skill",
    ],
  },
  {
    name: "Concepts",
    slugs: [
      "concepts/canonical-skills",
      "concepts/agents",
      "concepts/harnesses",
      "concepts/libraries",
      "concepts/scopes",
      "concepts/layouts",
      "concepts/drift",
    ],
  },
  {
    name: "Guides",
    slugs: [
      "guides/authoring",
      "guides/sharing-a-library",
      "guides/importing",
      "guides/vendoring",
      "guides/git-hook",
      "guides/loops",
      "guides/safety-scans",
    ],
  },
  {
    name: "Reference",
    slugs: ["reference/commands", "harness-comparison", "exchange"],
  },
];

function walk(dir) {
  const out = [];
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, entry.name);
    if (entry.isDirectory()) out.push(...walk(p));
    else if (/\.(md|mdx)$/.test(entry.name)) out.push(p);
  }
  return out;
}

function relSlug(file) {
  return path.relative(DOCS_DIR, file).replace(/\\/g, "/").replace(/\.(md|mdx)$/, "");
}

function slugToUrl(slug) {
  return slug === "index" ? `${SITE}/docs/` : `${SITE}/docs/${slug}/`;
}

function parse(file) {
  const raw = fs.readFileSync(file, "utf8");
  const m = raw.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n?/);
  const fm = {};
  let body = raw;
  if (m) {
    body = raw.slice(m[0].length);
    for (const line of m[1].split(/\r?\n/)) {
      const kv = line.match(/^(\w[\w-]*):\s*(.*)$/);
      if (kv) fm[kv[1]] = kv[2].trim().replace(/^["']|["']$/g, "");
    }
  }
  return { fm, body };
}

// llms.txt prose: replace em dash (U+2014) and en dash (U+2013) with commas to
// keep the curated index clean. Built from char codes so no literal dash lives
// in this source. Used for both link titles and one-line descriptions.
const DASHES = String.fromCharCode(0x2014, 0x2013);
const DASH_RE = new RegExp("\\s*[" + DASHES + "]\\s*", "g");
function clean(s) {
  return (s || "").replace(DASH_RE, ", ").replace(/\s{2,}/g, " ").trim();
}

// llms-full.txt bodies: strip MDX imports and unwrap <Tabs>/<TabItem> into headings
// so the corpus stays clean, faithful markdown.
function cleanBody(body) {
  const lines = body.split(/\r?\n/);
  const out = [];
  for (const line of lines) {
    const t = line.trim();
    if (/^import\s.+from\s+['"].+['"];?\s*$/.test(t)) continue;
    if (/^<\/?Tabs>\s*$/.test(t)) continue;
    if (/^<\/TabItem>\s*$/.test(t)) continue;
    const tab = t.match(/^<TabItem\s+label=["'](.+?)["']\s*>\s*$/);
    if (tab) {
      out.push(`#### ${tab[1]}`);
      continue;
    }
    out.push(line);
  }
  return out.join("\n").replace(/\n{3,}/g, "\n\n").trim();
}

const files = walk(DOCS_DIR);
const bySlug = new Map();
for (const f of files) bySlug.set(relSlug(f), f);

// --- Build llms.txt ---------------------------------------------------------
const seen = new Set();
let llms = `# adeptability\n\n> ${SUMMARY}\n\n`;
llms += `The full text of every documentation page is available at ${SITE}/llms-full.txt\n\n`;

// Docs home first.
if (bySlug.has("index")) {
  const { fm } = parse(bySlug.get("index"));
  llms += `## Overview\n\n`;
  llms += `- [Documentation home](${SITE}/docs/): ${clean(fm.description)}\n\n`;
  seen.add("index");
}

for (const group of GROUPS) {
  llms += `## ${group.name}\n\n`;
  for (const slug of group.slugs) {
    const file = bySlug.get(slug);
    if (!file) {
      console.warn(`[gen-llms] WARN: manifest slug not found: ${slug}`);
      continue;
    }
    const { fm } = parse(file);
    const title = clean(fm.title || slug);
    llms += `- [${title}](${slugToUrl(slug)}): ${clean(fm.description)}\n`;
    seen.add(slug);
  }
  llms += `\n`;
}

llms += `## Links\n\n`;
llms += `- [GitHub repository](https://github.com/itaywol/adeptability): Source code, issues, and discussion. Written in Go, MIT licensed.\n`;
llms += `- [Releases](https://github.com/itaywol/adeptability/releases): Signed binaries and the changelog for every version.\n`;
llms += `- [Landing page](${SITE}/): Project overview, feature tour, and install options.\n`;

// Staleness guard: warn if any docs page is missing from the curated index.
for (const slug of bySlug.keys()) {
  if (!seen.has(slug)) console.warn(`[gen-llms] WARN: docs page not in llms.txt manifest: ${slug}`);
}

fs.writeFileSync(path.join(PUBLIC_DIR, "llms.txt"), llms.trimEnd() + "\n");

// --- Build llms-full.txt ----------------------------------------------------
// Ordered: curated pages first (in manifest order), then any stragglers.
const order = ["index", ...GROUPS.flatMap((g) => g.slugs)];
for (const slug of bySlug.keys()) if (!order.includes(slug)) order.push(slug);

const parts = [];
parts.push(`# adeptability documentation`);
parts.push(``);
parts.push(`> ${SUMMARY}`);
parts.push(``);
parts.push(`This file concatenates the full text of all ${bySlug.size} documentation pages.`);
parts.push(`Source: ${SITE}/  |  Generated by scripts/gen-llms.mjs`);
parts.push(``);

for (const slug of order) {
  const file = bySlug.get(slug);
  if (!file) continue;
  const { fm, body } = parse(file);
  const title = fm.title || slug;
  parts.push(`---`);
  parts.push(``);
  parts.push(`# ${title}`);
  parts.push(``);
  parts.push(`URL: ${slugToUrl(slug)}`);
  if (fm.description) parts.push(`Description: ${fm.description}`);
  parts.push(``);
  parts.push(cleanBody(body));
  parts.push(``);
}

fs.writeFileSync(path.join(PUBLIC_DIR, "llms-full.txt"), parts.join("\n").replace(/\n{3,}/g, "\n\n").trimEnd() + "\n");

console.log(
  `[gen-llms] wrote public/llms.txt and public/llms-full.txt from ${bySlug.size} docs pages`
);
