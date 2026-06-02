# Changelog

## 1.0.0 (2026-06-02)


### ⚠ BREAKING CHANGES

* subcommand groups (harness/skill/library), multi-library, status
* collapse CLI to init/sync/sync-from/diff (+ list/show/doctor)
* **v0.2:** drop version + per-skill lockfile, hash is the truth

### Features

* **cli:** print command help on missing arg, unknown flag, or typo subcommand ([4a7dbd1](https://github.com/itaywol/adeptability/commit/4a7dbd15db8c53f41c697ff4f74d082c50243f7e))
* **config,llm:** adept config + LLM intent pass over safety scan ([03b02ec](https://github.com/itaywol/adeptability/commit/03b02ec166411fa9d84531bab4555e08f160b700))
* **harness:** support all 51 vercel-labs/skills agents ([dad2ba5](https://github.com/itaywol/adeptability/commit/dad2ba5aa0d1cc1788136bda0a88614a576ea0fa))
* **import:** bidirectional harness &lt;-&gt; canonical sync ([24405ff](https://github.com/itaywol/adeptability/commit/24405ff9a35187e4d04884ed68c37b510d06fca5))
* initial adeptability cross-harness skill portability CLI ([66077aa](https://github.com/itaywol/adeptability/commit/66077aa531b406f5935c6ae5b924b85b5d47580f))
* ship all v0.2 features as v0.1 — synthetic import, 3-way merge, cosign, http org ([2737928](https://github.com/itaywol/adeptability/commit/2737928e67928b6bde7838159f8924c415694c39))
* **skill:** install from skills.sh, lockfile, sandbox sniff, hash-verify ([e5ba55d](https://github.com/itaywol/adeptability/commit/e5ba55dac0f15ad33cc708dfed86d0a3668f18fa))
* **skill:** structured safety scanner + `skill check` command ([1d47e42](https://github.com/itaywol/adeptability/commit/1d47e42f8f8f4a788d236853ded6564cc3eaabf9))
* subcommand groups (harness/skill/library), multi-library, status ([40f584b](https://github.com/itaywol/adeptability/commit/40f584b6852a6827e434b14eabbd56022a3d6690))
* **v0.2:** drop version + per-skill lockfile, hash is the truth ([7650f5c](https://github.com/itaywol/adeptability/commit/7650f5c80270f799bfb0987475c6871c2254e81d))


### Bug Fixes

* **canonical:** accept vercel-style frontmatter; dir name is authoritative ([feac3af](https://github.com/itaywol/adeptability/commit/feac3aff6ad49cea47b9614594ca7a2962af217a))
* **ci:** gofmt + Windows test shell + stderr-free status ([2391fb3](https://github.com/itaywol/adeptability/commit/2391fb343f73058c628c8cd6582b10525643d4ca))
* **cli:** gate destructive writes + improve CLI UX ([8402a2e](https://github.com/itaywol/adeptability/commit/8402a2e172ed85124c9d4e0c6a1024e6365f8ca8))
* **harness:** write sidecars next to the rendered main file ([ca5b6c6](https://github.com/itaywol/adeptability/commit/ca5b6c64e313d13c41d035fe27470f9ea8e5f582))


### Code Refactoring

* collapse CLI to init/sync/sync-from/diff (+ list/show/doctor) ([157e368](https://github.com/itaywol/adeptability/commit/157e3684faf15d23b3d297034fc5da29929442ae))
