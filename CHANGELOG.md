# Changelog

## [0.5.0](https://github.com/colonel-byte/cargoship/compare/v0.4.0...v0.5.0) (2026-06-25)


### ⚠ BREAKING CHANGES

* add prepare command ([#99](https://github.com/colonel-byte/cargoship/issues/99))

### Features

* add prepare command ([#99](https://github.com/colonel-byte/cargoship/issues/99)) ([9f191aa](https://github.com/colonel-byte/cargoship/commit/9f191aa097f8ffb01af7c124751c792deb695a29))
* **logging:** add better logic for distro output ([#97](https://github.com/colonel-byte/cargoship/issues/97)) ([8e96e83](https://github.com/colonel-byte/cargoship/commit/8e96e830dcc26d3b3d50549668bd80353fd4d3d6))
* update the rig version to use upstream package ([#92](https://github.com/colonel-byte/cargoship/issues/92)) ([6e34be6](https://github.com/colonel-byte/cargoship/commit/6e34be68204ee361e943be360dbdfdd37180c29e))


### CI/CD

* **dagger:** update version ([#88](https://github.com/colonel-byte/cargoship/issues/88)) ([a1643cb](https://github.com/colonel-byte/cargoship/commit/a1643cb3a38a6edda63fe5aa2cf672d87f93de54))
* update dagger to 0.21.7 ([#96](https://github.com/colonel-byte/cargoship/issues/96)) ([da2b4f8](https://github.com/colonel-byte/cargoship/commit/da2b4f857320875add6b1a61a9d36eabf2534d5b))


### Build

* **deps:** Bump actions/checkout from 6.0.2 to 6.0.3 ([#85](https://github.com/colonel-byte/cargoship/issues/85)) ([e059838](https://github.com/colonel-byte/cargoship/commit/e0598381202f9fa518e1b0ffc2f8d9564eb29cdc))
* **deps:** Bump actions/checkout from 6.0.3 to 7.0.0 ([#94](https://github.com/colonel-byte/cargoship/issues/94)) ([2c55700](https://github.com/colonel-byte/cargoship/commit/2c55700306c9bf2612ccee98ebad0b005f50e2ef))
* **deps:** Bump actions/setup-go from 6.4.0 to 6.5.0 ([#98](https://github.com/colonel-byte/cargoship/issues/98)) ([6686e60](https://github.com/colonel-byte/cargoship/commit/6686e607f59d268220478d4585ddae6c41201821))
* **deps:** Bump github.com/k0sproject/rig from 0.21.10 to 0.21.11 ([#100](https://github.com/colonel-byte/cargoship/issues/100)) ([8152958](https://github.com/colonel-byte/cargoship/commit/8152958517d74b16d37b3c2050132976d1f0fe5f))
* **deps:** Bump github.com/txn2/txeh from 1.8.0 to 1.8.1 ([#86](https://github.com/colonel-byte/cargoship/issues/86)) ([cc8160e](https://github.com/colonel-byte/cargoship/commit/cc8160ebb763501ba62860d33200084b4926cd20))
* **deps:** Bump github.com/zarf-dev/zarf from 0.76.0 to 0.77.0 ([#83](https://github.com/colonel-byte/cargoship/issues/83)) ([c9edcb8](https://github.com/colonel-byte/cargoship/commit/c9edcb8fd8e08531eeed9f9bd83ca2ba05d38c56))
* **deps:** Bump https://github.com/google/keep-sorted from v0.8.0 to 0.9.0 ([#87](https://github.com/colonel-byte/cargoship/issues/87)) ([682a63b](https://github.com/colonel-byte/cargoship/commit/682a63b555b97c04c331575cff2ac814b66416d7))
* **deps:** Bump k8s.io/client-go from 0.36.1 to 0.36.2 in the k8s group across 1 directory ([#91](https://github.com/colonel-byte/cargoship/issues/91)) ([1138748](https://github.com/colonel-byte/cargoship/commit/11387482588fbe86f9f240fd8529774e0ec85bbd))
* **deps:** Bump oras.land/oras-go/v2 from 2.6.0 to 2.6.1 ([#89](https://github.com/colonel-byte/cargoship/issues/89)) ([b4a51f8](https://github.com/colonel-byte/cargoship/commit/b4a51f8ae1e7b619fea8ad2f4f73d3d69422b8e2))

## [0.4.0](https://github.com/colonel-byte/cargoship/compare/v0.3.0...v0.4.0) (2026-05-23)


### ⚠ BREAKING CHANGES

* add dagger for ci building ([#59](https://github.com/colonel-byte/cargoship/issues/59))

### Features

* add dagger for ci building ([#59](https://github.com/colonel-byte/cargoship/issues/59)) ([2545844](https://github.com/colonel-byte/cargoship/commit/2545844143f8e6b002c1d08631e455a067ee49e0))
* add mage for make like tool ([#63](https://github.com/colonel-byte/cargoship/issues/63)) ([e35aed7](https://github.com/colonel-byte/cargoship/commit/e35aed7198b429f5f5bf3fcc5e87dc8fb749afba))
* change sysctl to create a file ([#62](https://github.com/colonel-byte/cargoship/issues/62)) ([aba7d57](https://github.com/colonel-byte/cargoship/commit/aba7d57b69239ee11aab2b4018672f1d7aa0e6e4))
* **cmd:** expand version debug info ([#68](https://github.com/colonel-byte/cargoship/issues/68)) ([29fd345](https://github.com/colonel-byte/cargoship/commit/29fd345eb2d01230f4a1e9442d6b058372cde588))
* **distro:** update the upgrade logic for rancher ([#66](https://github.com/colonel-byte/cargoship/issues/66)) ([7c4da85](https://github.com/colonel-byte/cargoship/commit/7c4da855420d44e44fc36831cec236ce4287dfbf))
* update version to include deps ([#60](https://github.com/colonel-byte/cargoship/issues/60)) ([9304ae9](https://github.com/colonel-byte/cargoship/commit/9304ae98b7c8e506f1ddd573dbc96cc71d66b9b6))


### Miscellaneous

* **deps:** updates rig version ([#69](https://github.com/colonel-byte/cargoship/issues/69)) ([cf0afc0](https://github.com/colonel-byte/cargoship/commit/cf0afc037e66ec886eb0ac52bc5d28b9b42aa877))
* **mage:** move generate logic to mage ([#70](https://github.com/colonel-byte/cargoship/issues/70)) ([413eea0](https://github.com/colonel-byte/cargoship/commit/413eea0345aed3b30adb5a97c37236598ead4d16))


### CI/CD

* allow building on host system ([20cac43](https://github.com/colonel-byte/cargoship/commit/20cac43bc4b65516f4828d488dde834f2c3ca548))
* remove the mage sub-modules ([#74](https://github.com/colonel-byte/cargoship/issues/74)) ([9c073a4](https://github.com/colonel-byte/cargoship/commit/9c073a4266601a9935e8e459a14d6b5585289a22))
* update dagger deps ([#64](https://github.com/colonel-byte/cargoship/issues/64)) ([96ad0c8](https://github.com/colonel-byte/cargoship/commit/96ad0c834feb236c169c71634c9164db994accd0))


### Build

* **deps:** Bump actions/create-github-app-token from 3.1.1 to 3.2.0 ([#61](https://github.com/colonel-byte/cargoship/issues/61)) ([ac2c0b7](https://github.com/colonel-byte/cargoship/commit/ac2c0b733af9bedef407a769a9d4d12b6314861a))
* **deps:** Bump docker/build-push-action from 7.1.0 to 7.2.0 ([#79](https://github.com/colonel-byte/cargoship/issues/79)) ([a0b4da1](https://github.com/colonel-byte/cargoship/commit/a0b4da139a540ac6440f0637e1677983f4bc05e4))
* **deps:** Bump docker/login-action from 4.1.0 to 4.2.0 ([#80](https://github.com/colonel-byte/cargoship/issues/80)) ([5d69ede](https://github.com/colonel-byte/cargoship/commit/5d69ede09a3a9980d80016e341d5beffc03f3785))
* **deps:** Bump docker/setup-buildx-action from 4.0.0 to 4.1.0 ([#82](https://github.com/colonel-byte/cargoship/issues/82)) ([f147cf5](https://github.com/colonel-byte/cargoship/commit/f147cf53a6dc6ea93507421b348430e1d314eb28))
* **deps:** Bump github.com/containerd/containerd/v2 from 2.2.3 to 2.3.0 ([#73](https://github.com/colonel-byte/cargoship/issues/73)) ([9639439](https://github.com/colonel-byte/cargoship/commit/9639439f51afc00fda7a0d601f93f4e316c02991))
* **deps:** Bump github.com/containerd/containerd/v2 from 2.3.0 to 2.3.1 ([#78](https://github.com/colonel-byte/cargoship/issues/78)) ([e6f1bd1](https://github.com/colonel-byte/cargoship/commit/e6f1bd10f13decef4e0296e5a716aae56ad8b6e5))
* **deps:** Bump github.com/invopop/jsonschema from 0.13.0 to 0.14.0 ([#77](https://github.com/colonel-byte/cargoship/issues/77)) ([d899d71](https://github.com/colonel-byte/cargoship/commit/d899d714fcda4e366b292466dcb754372a61524a))
* **deps:** Bump github.com/magefile/mage from 1.15.0 to 1.17.2 ([#75](https://github.com/colonel-byte/cargoship/issues/75)) ([9ac36d1](https://github.com/colonel-byte/cargoship/commit/9ac36d1ddd2ac990a720ac30566ba9cb0278b15a))
* **deps:** Bump github.com/stoewer/go-strcase from 1.3.0 to 1.3.1 ([#76](https://github.com/colonel-byte/cargoship/issues/76)) ([df7314c](https://github.com/colonel-byte/cargoship/commit/df7314c3b53393bde9eae1bb7882dd7f447663a4))
* **deps:** Bump github.com/zarf-dev/zarf from 0.75.1 to 0.76.0 ([#67](https://github.com/colonel-byte/cargoship/issues/67)) ([589b14f](https://github.com/colonel-byte/cargoship/commit/589b14fba97a810dc0b53d8a62941a4eb35eb2ec))
* **deps:** Bump golangci/golangci-lint-action from 9.2.0 to 9.2.1 ([#81](https://github.com/colonel-byte/cargoship/issues/81)) ([220e939](https://github.com/colonel-byte/cargoship/commit/220e9394ffec1004b8b46d04c724dea2808945b4))
* **deps:** Bump goreleaser/goreleaser-action from 7.2.1 to 7.2.2 ([#71](https://github.com/colonel-byte/cargoship/issues/71)) ([e27ed7b](https://github.com/colonel-byte/cargoship/commit/e27ed7bc1d39c2a230cd5440cc86d7c296ad58d8))
* **deps:** Bump k8s.io/client-go from 0.36.0 to 0.36.1 in the k8s group across 1 directory ([#72](https://github.com/colonel-byte/cargoship/issues/72)) ([920efb9](https://github.com/colonel-byte/cargoship/commit/920efb9976899afd81826ad96dacbc9882a91066))
* **deps:** Bump sigstore/cosign-installer from 4.1.1 to 4.1.2 ([#57](https://github.com/colonel-byte/cargoship/issues/57)) ([3aa3636](https://github.com/colonel-byte/cargoship/commit/3aa36365be6395e9c0e0e81cabd370fcd68a5c2d))

## [0.3.0](https://github.com/colonel-byte/cargoship/compare/v0.2.5...v0.3.0) (2026-05-07)


### ⚠ BREAKING CHANGES

* add proper code attribution ([#55](https://github.com/colonel-byte/cargoship/issues/55))

### Features

* add command to get kubeconfig ([#41](https://github.com/colonel-byte/cargoship/issues/41)) ([ae53fdf](https://github.com/colonel-byte/cargoship/commit/ae53fdf31401b6062552f2373f42e24bc678c118))
* add proper code attribution ([#55](https://github.com/colonel-byte/cargoship/issues/55)) ([e971e89](https://github.com/colonel-byte/cargoship/commit/e971e89a38a7422a64f803d042baec488ef8a366))
* address panic when no config is found ([#51](https://github.com/colonel-byte/cargoship/issues/51)) ([ff86d1a](https://github.com/colonel-byte/cargoship/commit/ff86d1a3cd333fdcedf3332bcfd61063ec57da0d))
* update rig fork ([#49](https://github.com/colonel-byte/cargoship/issues/49)) ([71b9124](https://github.com/colonel-byte/cargoship/commit/71b9124e516bb77ec727800f331b6ab8a4dc20f0))


### CI/CD

* **e2e:** add image files for testing cargoship ([#52](https://github.com/colonel-byte/cargoship/issues/52)) ([9d02b45](https://github.com/colonel-byte/cargoship/commit/9d02b4529c77ffe91b0fd3d546d123885f814ed0))


### Build

* **deps:** Bump github.com/zarf-dev/zarf from 0.75.0 to 0.75.1 ([#46](https://github.com/colonel-byte/cargoship/issues/46)) ([57327f8](https://github.com/colonel-byte/cargoship/commit/57327f88337bfa69ad758dc5e6dd08df1c876d7e))
* **deps:** Bump https://github.com/golangci/golangci-lint from v2.11.4 to 2.12.1 ([#45](https://github.com/colonel-byte/cargoship/issues/45)) ([d791dd0](https://github.com/colonel-byte/cargoship/commit/d791dd0686a6188e6614dfc3096364f35f187680))
* **deps:** Bump https://github.com/golangci/golangci-lint from v2.12.1 to 2.12.2 ([#56](https://github.com/colonel-byte/cargoship/issues/56)) ([8d00775](https://github.com/colonel-byte/cargoship/commit/8d007757e47c38a91f1d63d442f8aba90aecd7ec))

## [0.2.5](https://github.com/colonel-byte/cargoship/compare/v0.2.4...v0.2.5) (2026-05-01)


### CI/CD

* **release:** expand release logic ([10eb37f](https://github.com/colonel-byte/cargoship/commit/10eb37f3c65514159d09cec7b37c18e46f20f33e))


### Build

* **deps:** Bump github.com/Masterminds/semver/v3 from 3.4.0 to 3.5.0 ([#42](https://github.com/colonel-byte/cargoship/issues/42)) ([945d93b](https://github.com/colonel-byte/cargoship/commit/945d93bfc33732157960fbc89571a0e72e81183e))

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
