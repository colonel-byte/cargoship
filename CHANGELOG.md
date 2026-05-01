# Changelog

## [0.2.5](https://github.com/colonel-byte/cargoship/compare/v0.2.4...v0.2.5) (2026-05-01)


### CI/CD

* **release:** rework release logic ([#37](https://github.com/colonel-byte/cargoship/issues/37)) ([6f28838](https://github.com/colonel-byte/cargoship/commit/6f2883800dabe96b516033d11a71a8714a483fae))

## [0.2.4](https://github.com/colonel-byte/cargoship/compare/v0.2.3...v0.2.4) (2026-04-29)


### CI/CD

* **release:** change cosign cert ([#35](https://github.com/colonel-byte/cargoship/issues/35)) ([ce4c5be](https://github.com/colonel-byte/cargoship/commit/ce4c5beaec3b56e09216581c9661d9b294f492cc))

## [0.2.3](https://github.com/colonel-byte/cargoship/compare/v0.2.2...v0.2.3) (2026-04-28)


### Features

* rework cosign logic ([#34](https://github.com/colonel-byte/cargoship/issues/34)) ([9c5687f](https://github.com/colonel-byte/cargoship/commit/9c5687f6e9b28676856c4cc1fc7cd6612dfd1635))


### Build

* **deps:** Bump sigstore/cosign-installer from 4.1.0 to 4.1.1 ([#32](https://github.com/colonel-byte/cargoship/issues/32)) ([25cbcdd](https://github.com/colonel-byte/cargoship/commit/25cbcdd7b35821bf2d588ec1679b4636bfd5a19e))

## [0.2.2](https://github.com/colonel-byte/cargoship/compare/v0.2.1...v0.2.2) (2026-04-28)


### CI/CD

* **release:** remove cosign key ([#30](https://github.com/colonel-byte/cargoship/issues/30)) ([2f0f9b0](https://github.com/colonel-byte/cargoship/commit/2f0f9b0045ec76672ac94543d64a5858751b11be))

## [0.2.1](https://github.com/colonel-byte/cargoship/compare/v0.2.0...v0.2.1) (2026-04-28)


### CI/CD

* **release:** add cosign binary ([#28](https://github.com/colonel-byte/cargoship/issues/28)) ([6ba9cee](https://github.com/colonel-byte/cargoship/commit/6ba9ceec196459650de74b8fc501cb545407d2e8))

## [0.2.0](https://github.com/colonel-byte/cargoship/compare/v0.1.0...v0.2.0) (2026-04-27)


### CI/CD

* **release:** add syft action ([#24](https://github.com/colonel-byte/cargoship/issues/24)) ([3b9293e](https://github.com/colonel-byte/cargoship/commit/3b9293ee2bfba660e100fe6bcc1b8df6eb39114b))

## 0.1.0 (2026-04-27)


### Features

* add logic for allowing distro to manage stopping service ([#11](https://github.com/colonel-byte/cargoship/issues/11)) ([73010f5](https://github.com/colonel-byte/cargoship/commit/73010f5d887405c7fb9636de1a402f374d70f08d))
* add reset action ([#19](https://github.com/colonel-byte/cargoship/issues/19)) ([5b3bc07](https://github.com/colonel-byte/cargoship/commit/5b3bc07bd0317788113285412453d40d9c9e52a8))
* **core:** basic logic for bootstrapping ([#1](https://github.com/colonel-byte/cargoship/issues/1)) ([03e1704](https://github.com/colonel-byte/cargoship/commit/03e17041a0d86e5b8ac3074dcfaa56749e36a325))
* **distro:** add kill-all script for k3s ([#12](https://github.com/colonel-byte/cargoship/issues/12)) ([d0bc761](https://github.com/colonel-byte/cargoship/commit/d0bc7612597eaeecd3da6a1d53f63abeff930e2e))
* **distro:** add rke2 1.35.4 ([#13](https://github.com/colonel-byte/cargoship/issues/13)) ([fcf4616](https://github.com/colonel-byte/cargoship/commit/fcf4616eeeadf73b0cf04b584b69641135c30c1f))
* init ([7918478](https://github.com/colonel-byte/cargoship/commit/7918478d83435d4158e5847bf13794a4924a8301))
* run golangci lint ([#16](https://github.com/colonel-byte/cargoship/issues/16)) ([f1be9fa](https://github.com/colonel-byte/cargoship/commit/f1be9fa5b004167c76955fc12e63cb875770ff30))
* **schema:** rework config schema ([#23](https://github.com/colonel-byte/cargoship/issues/23)) ([444214f](https://github.com/colonel-byte/cargoship/commit/444214f7d65a92937f083a6e6a0c7cbf662f58cb))


### CI/CD

* **action:** add check for go mod delta's ([#15](https://github.com/colonel-byte/cargoship/issues/15)) ([9b1a40d](https://github.com/colonel-byte/cargoship/commit/9b1a40d25a055d3efdba753b39354caa62e437c4))
* **commit:** add pr title check ([#14](https://github.com/colonel-byte/cargoship/issues/14)) ([3e071da](https://github.com/colonel-byte/cargoship/commit/3e071daaf965d785256ce756cab2c18d440e8a43))
* **release:** configure to bump minor ([7bbed97](https://github.com/colonel-byte/cargoship/commit/7bbed972b00b2819495523156082f184337d81c6))
* **release:** fix config settings ([88a8fd1](https://github.com/colonel-byte/cargoship/commit/88a8fd18e88acfcfcae54fcbbbee81087e264bc0))
* **release:** remove versioning ([30efbff](https://github.com/colonel-byte/cargoship/commit/30efbff506acffa34ccaab13d9fae9ddd9875490))


### Build

* **deps:** Bump actions/setup-node from 6.3.0 to 6.4.0 ([#21](https://github.com/colonel-byte/cargoship/issues/21)) ([846530b](https://github.com/colonel-byte/cargoship/commit/846530bf1cfc67b9f755e819bc05ace871f860a8))
* **deps:** Bump github.com/invopop/jsonschema from 0.13.0 to 0.14.0 ([#3](https://github.com/colonel-byte/cargoship/issues/3)) ([51e8ace](https://github.com/colonel-byte/cargoship/commit/51e8ace6fde4be678e46b0eafb4bad879f978e6c))
* **deps:** Bump github.com/stoewer/go-strcase from 1.3.0 to 1.3.1 ([#4](https://github.com/colonel-byte/cargoship/issues/4)) ([9ea3635](https://github.com/colonel-byte/cargoship/commit/9ea363549e635a526c1e0be6b5d41b1ab470e164))
* **deps:** Bump googleapis/release-please-action from 4.4.0 to 5.0.0 ([#2](https://github.com/colonel-byte/cargoship/issues/2)) ([0e30fa2](https://github.com/colonel-byte/cargoship/commit/0e30fa282e404f0135edc105708e187abde343c1))
* **deps:** Bump goreleaser/goreleaser-action from 7.1.0 to 7.2.1 ([#22](https://github.com/colonel-byte/cargoship/issues/22)) ([5d094f0](https://github.com/colonel-byte/cargoship/commit/5d094f0285f1012e8357c581368e62fd057a45ca))
