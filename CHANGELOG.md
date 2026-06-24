# Changelog

## [1.5.0](https://github.com/itaywol/adeptability/compare/v1.4.1...v1.5.0) (2026-06-24)


### Features

* **library:** add `library update` command and status update hints ([#31](https://github.com/itaywol/adeptability/issues/31)) ([ecef9fd](https://github.com/itaywol/adeptability/commit/ecef9fd989d7fd2b8bb554965262b3bfd8830d4b))

## [1.4.1](https://github.com/itaywol/adeptability/compare/v1.4.0...v1.4.1) (2026-06-23)


### Bug Fixes

* **status:** count private skills separately from libraries ([#29](https://github.com/itaywol/adeptability/issues/29)) ([e4aefc1](https://github.com/itaywol/adeptability/commit/e4aefc14eef89f8d90e85db5f981609b9285e0ae))

## [1.4.0](https://github.com/itaywol/adeptability/compare/v1.3.0...v1.4.0) (2026-06-23)


### Features

* **init:** add --as-library to initialize a publishable library ([#26](https://github.com/itaywol/adeptability/issues/26)) ([02047db](https://github.com/itaywol/adeptability/commit/02047db556201bee000985546332f65658bfa323))
* **library:** private dev-canonical + managing-library skill ([#28](https://github.com/itaywol/adeptability/issues/28)) ([9b971c5](https://github.com/itaywol/adeptability/commit/9b971c59aa4228ed4746336b000366c4e5a4ae1c))

## [1.3.0](https://github.com/itaywol/adeptability/compare/v1.2.0...v1.3.0) (2026-06-18)


### Features

* expertise-exchange skill samples the open board on start ([#24](https://github.com/itaywol/adeptability/issues/24)) ([93a40e7](https://github.com/itaywol/adeptability/commit/93a40e7e3b062f17ee56626fb78089e91281b55a))

## [1.2.0](https://github.com/itaywol/adeptability/compare/v1.1.0...v1.2.0) (2026-06-17)


### Features

* add team expertise exchange billboard ([#20](https://github.com/itaywol/adeptability/issues/20)) ([1e1b3d2](https://github.com/itaywol/adeptability/commit/1e1b3d27dae89e3d328fb6ebc31febbeecb79a4e))
* default exchange serve port to 4639 ([#23](https://github.com/itaywol/adeptability/issues/23)) ([4817665](https://github.com/itaywol/adeptability/commit/481766573ccd4edd0606b9d8b4f909ca1b061803))

## [1.1.0](https://github.com/itaywol/adeptability/compare/v1.0.3...v1.1.0) (2026-06-17)


### Features

* add git pre-commit drift hook ([#17](https://github.com/itaywol/adeptability/issues/17)) ([30c1a46](https://github.com/itaywol/adeptability/commit/30c1a46bc3327735b4f1c38501341f05796d7af0))
* seed bundled default skills on init ([#19](https://github.com/itaywol/adeptability/issues/19)) ([ac3e409](https://github.com/itaywol/adeptability/commit/ac3e409d0b30664c724b7cf2903720ea6d836be3))

## [1.0.3](https://github.com/itaywol/adeptability/compare/v1.0.2...v1.0.3) (2026-06-04)


### Bug Fixes

* stop hardcoding v1.0.0 in the manual-download docs ([#12](https://github.com/itaywol/adeptability/issues/12)) ([7e1fa96](https://github.com/itaywol/adeptability/commit/7e1fa96e1eddd23582760b05568af07efa95fa3f))

## [1.0.2](https://github.com/itaywol/adeptability/compare/v1.0.1...v1.0.2) (2026-06-04)


### Bug Fixes

* **ci:** grant attestations: write to release-please reusable job ([#10](https://github.com/itaywol/adeptability/issues/10)) ([83042ef](https://github.com/itaywol/adeptability/commit/83042ef04a7af4dd8ae9f8ae10ed9ea684265b26))
* **ci:** use id-token: write in release-please reusable job ([#9](https://github.com/itaywol/adeptability/issues/9)) ([112d90c](https://github.com/itaywol/adeptability/commit/112d90cba4b269779566ee6e74aef64ffa0b66c6))
* correct install.sh URL and document only shipped install channels ([#8](https://github.com/itaywol/adeptability/issues/8)) ([9d4d7e2](https://github.com/itaywol/adeptability/commit/9d4d7e2896e7aa1c6446615f689bc3aabbef9e4d))

## 1.0.0 (2026-06-03)


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
