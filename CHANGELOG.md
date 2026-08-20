# Changelog

## [1.0.0](https://github.com/mm503/tautulli-exporter/compare/v0.2.7...v1.0.0) (2026-08-20)


### ⚠ BREAKING CHANGES

* /ready no longer returns 503 when Tautulli is unreachable. Alerts that watched for the pod going NotReady must move to `plex_up == 0`.
* rewrite exporter in Go

### Features

* add Helm chart ([cea7f19](https://github.com/mm503/tautulli-exporter/commit/cea7f192c4e208fd8c2492a4607476c4474747aa))
* expose Tautulli scrape health as metrics ([fc89372](https://github.com/mm503/tautulli-exporter/commit/fc893720001fcd36efbd6a5c27aada206ce25c2c))
* log when collection resumes after failures ([e988174](https://github.com/mm503/tautulli-exporter/commit/e9881742d0fbabb2209c068c767153150123f97c))
* rewrite exporter in Go ([5f8436d](https://github.com/mm503/tautulli-exporter/commit/5f8436dd728ff15f725ef5ea82f7c569a379cb95))


### Bug Fixes

* **chart:** harden pod security and drop unused scaffolding ([e4a6d56](https://github.com/mm503/tautulli-exporter/commit/e4a6d562e364eca3f517472ee1de48231eb7a4fc))
* **deps:** update actions/setup-go action to v7 ([56ce9e4](https://github.com/mm503/tautulli-exporter/commit/56ce9e4c9001946f65c2b4f3abeb4d94a4a4a9a1))
* **deps:** update golang docker tag to v1.26.6 ([36101e6](https://github.com/mm503/tautulli-exporter/commit/36101e6acc9e3dac66809a586011066d2834a653))
* **deps:** update golang docker tag to v1.27.0 ([6356028](https://github.com/mm503/tautulli-exporter/commit/63560286a2abf1010c05d54be405d4d25e5dfb88))
* **deps:** update module github.com/prometheus/client_golang to v1.24.0 ([0ffcb74](https://github.com/mm503/tautulli-exporter/commit/0ffcb749d6ca31cca8adeee9b69d113002848ed3))
* **deps:** update module github.com/prometheus/client_golang to v1.24.1 ([e72ed66](https://github.com/mm503/tautulli-exporter/commit/e72ed66225150aeb035d50cd9a93fe99991cf2c4))

## [0.2.7](https://github.com/mm503/tautulli-exporter/compare/v0.2.6...v0.2.7) (2026-07-13)


### Bug Fixes

* **deps:** update actions/checkout action to v7 ([c02f7de](https://github.com/mm503/tautulli-exporter/commit/c02f7de0f55a7185cfaebf967168ec371db915fc))

## [0.2.6](https://github.com/mm503/tautulli-exporter/compare/v0.2.5...v0.2.6) (2026-06-14)


### Bug Fixes

* **deps:** update python docker tag to v3.14.6 ([03573b2](https://github.com/mm503/tautulli-exporter/commit/03573b2472e7d00a241c4f80ea3b93344b1b31df))

## [0.2.5](https://github.com/mm503/tautulli-exporter/compare/v0.2.4...v0.2.5) (2026-05-18)


### Bug Fixes

* **deps:** update dependency requests to v2.34.2 ([58b691d](https://github.com/mm503/tautulli-exporter/commit/58b691d4a5608d0f680b340b098988e62825ba04))

## [0.2.4](https://github.com/mm503/tautulli-exporter/compare/v0.2.3...v0.2.4) (2026-05-12)


### Bug Fixes

* circuit breaker never recovers after Tautulli comes back up ([19911dc](https://github.com/mm503/tautulli-exporter/commit/19911dc52613a4f445dc8c3417f4647b90a4d326))
* deps will increase patch version ([8d0dcb4](https://github.com/mm503/tautulli-exporter/commit/8d0dcb4e82738bc676384ecb27eec7fda2804575))
* **deps:** update actions/setup-python action to v6 ([011189a](https://github.com/mm503/tautulli-exporter/commit/011189ad2dc548ca3862dc3c4501adb6b0a1fc0e))
* **deps:** update dependency python to 3.14 ([eb756c4](https://github.com/mm503/tautulli-exporter/commit/eb756c4543d49de377738053c2dcd8886f151800))
* **deps:** update python docker tag to v3.14.5 ([33a1fea](https://github.com/mm503/tautulli-exporter/commit/33a1fead0f83d65567476b12282b61affdbc7a1e))

## [0.2.3](https://github.com/mm503/tautulli-exporter/compare/v0.2.2...v0.2.3) (2026-03-21)


### Bug Fixes

* **ci:** don't keep pip caches when building img ([a323c21](https://github.com/mm503/tautulli-exporter/commit/a323c21aaff7953357a5465b613fb714788af26f))
* **ci:** image has correct tag in metadata ([b64e11a](https://github.com/mm503/tautulli-exporter/commit/b64e11a66f387d5325a682f5e3e6e10949d1a656))

## [0.2.2](https://github.com/mm503/tautulli-exporter/compare/v0.2.1...v0.2.2) (2026-03-20)


### Bug Fixes

* **ci:** strip v from docker tags ([27ded87](https://github.com/mm503/tautulli-exporter/commit/27ded874b80a3541e99e00a4c17add0239b114b8))

## [0.2.1](https://github.com/mm503/tautulli-exporter/compare/v0.2.0...v0.2.1) (2026-03-20)


### Bug Fixes

* **ci:** actually push the release ([5b6c1f2](https://github.com/mm503/tautulli-exporter/commit/5b6c1f223ee95b37482d71790db51b574b35a49e))

## [0.2.0](https://github.com/mm503/tautulli-exporter/compare/v0.1.0...v0.2.0) (2026-03-20)


### Features

* add bandwidth and direct stream metrics ([0ea84ed](https://github.com/mm503/tautulli-exporter/commit/0ea84edf94b409e752401b2d18e02a6d8ffb2eb6))
* add proper ci ([268fef2](https://github.com/mm503/tautulli-exporter/commit/268fef2d1ee97c49674474b32e7ad4d0e35b47a3))
* **ci:** make dev build names better ([dfbf850](https://github.com/mm503/tautulli-exporter/commit/dfbf8502a368a3b38fbbe565851f534006c8dbdd))


### Bug Fixes

* preserve backwards compatibility for plex_active_streams_direct ([4c87480](https://github.com/mm503/tautulli-exporter/commit/4c87480a1e95afeafe7efb93c763ae252b61c6e5))
