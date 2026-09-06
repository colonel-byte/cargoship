# Changelog

## [0.19.1](https://github.com/colonel-byte/cargoship/compare/v0.19.0...v0.19.1) (2026-09-06)


### Features

* assemble multi-arch packages with an OCI image index ([#263](https://github.com/colonel-byte/cargoship/issues/263)) ([db8d106](https://github.com/colonel-byte/cargoship/commit/db8d106e0710e4dbd835143512b44e9bb2d00716))
* assert the firewall and the three upload phases ([#272](https://github.com/colonel-byte/cargoship/issues/272)) ([e5bc4c2](https://github.com/colonel-byte/cargoship/commit/e5bc4c2cd7e89fb71a0b6defb1cf52324c93b794))
* reject a host the package carries no architecture for ([#267](https://github.com/colonel-byte/cargoship/issues/267)) ([fe73d68](https://github.com/colonel-byte/cargoship/commit/fe73d68f237935a562958925c127ea13eea37c29))
* run the staging phases against a smaller cluster in CI ([#293](https://github.com/colonel-byte/cargoship/issues/293)) ([2b62d54](https://github.com/colonel-byte/cargoship/commit/2b62d54cfe65e6c5bb482b6ea66ab6e16bc5dd54))
* start work on multi-arch support ([#261](https://github.com/colonel-byte/cargoship/issues/261)) ([d5661d1](https://github.com/colonel-byte/cargoship/commit/d5661d11b090f81dc10fbfcfe4cf8af6a3501a46))
* walk the apply phases against a live cluster, through ModifyHosts ([#271](https://github.com/colonel-byte/cargoship/issues/271)) ([5886029](https://github.com/colonel-byte/cargoship/commit/5886029296576fd359dc1ac7c4ec42ebdeb96b3b))


### Bug Fixes

* export image tarballs for the package architecture ([#266](https://github.com/colonel-byte/cargoship/issues/266)) ([dbe65d7](https://github.com/colonel-byte/cargoship/commit/dbe65d7fa8bb12243566ee443e896955678c408a))
* honour --architecture on package create and package pull ([#262](https://github.com/colonel-byte/cargoship/issues/262)) ([4665c85](https://github.com/colonel-byte/cargoship/commit/4665c850b71f89d0f7472e659f805fd70f313217))


### Documentation

* play with cleaner print-able ([#254](https://github.com/colonel-byte/cargoship/issues/254)) ([c751bf4](https://github.com/colonel-byte/cargoship/commit/c751bf4a4f661fc006772eafd3a34723190d82e1))


### CI/CD

* add extra label for release ([#295](https://github.com/colonel-byte/cargoship/issues/295)) ([fb865a0](https://github.com/colonel-byte/cargoship/commit/fb865a0f98fa8e3c2114d100bda3e50b43aaf7a4))

## [0.19.0](https://github.com/colonel-byte/cargoship/compare/v0.18.0...v0.19.0) (2026-09-01)


### ⚠ BREAKING CHANGES

* rework to add ufw support ([#249](https://github.com/colonel-byte/cargoship/issues/249))
* drop the update- prefix from the host modification flags ([#250](https://github.com/colonel-byte/cargoship/issues/250))

### Features

* add an nftables firewall backend ([#251](https://github.com/colonel-byte/cargoship/issues/251)) ([7541115](https://github.com/colonel-byte/cargoship/commit/7541115416e50159d54e351de0f87cb959e35772))
* drop the update- prefix from the host modification flags ([#250](https://github.com/colonel-byte/cargoship/issues/250)) ([39d0d47](https://github.com/colonel-byte/cargoship/commit/39d0d4734939093f563e81752f7ad3720584c0f9))
* rework to add ufw support ([#249](https://github.com/colonel-byte/cargoship/issues/249)) ([8576563](https://github.com/colonel-byte/cargoship/commit/8576563efae70748eb21ba019651df29e979829b))

## [0.18.0](https://github.com/colonel-byte/cargoship/compare/v0.17.0...v0.18.0) (2026-09-01)


### ⚠ BREAKING CHANGES

* add package verification ([#248](https://github.com/colonel-byte/cargoship/issues/248))

### Features

* add package verification ([#248](https://github.com/colonel-byte/cargoship/issues/248)) ([231030b](https://github.com/colonel-byte/cargoship/commit/231030b60eea38478c9903a0b8dc6025e0dcf01b))


### Build

* **deps:** Bump github.com/docker/docker-credential-helpers from 0.9.8 to 0.9.9 ([#241](https://github.com/colonel-byte/cargoship/issues/241)) ([0c0d8e8](https://github.com/colonel-byte/cargoship/commit/0c0d8e81b9834c8fe85a99f790a621898096a39d))
* **deps:** Bump golangci-lint from v2.13.1 to 2.13.2 in the core group across 1 directory ([#239](https://github.com/colonel-byte/cargoship/issues/239)) ([e9bdab8](https://github.com/colonel-byte/cargoship/commit/e9bdab85d00a5086bb19ba0e7762aaba3af594b5))
* **deps:** Bump google.golang.org/api from 0.293.0 to 0.295.0 ([#247](https://github.com/colonel-byte/cargoship/issues/247)) ([a42d408](https://github.com/colonel-byte/cargoship/commit/a42d408f32ddb7fd8c39b8d210566d324d93ebb8))
* **deps:** Bump the core group across 1 directory with 2 updates ([#242](https://github.com/colonel-byte/cargoship/issues/242)) ([2d16203](https://github.com/colonel-byte/cargoship/commit/2d16203465a88c083b87a74d7a9cc9c792e775ac))
* **deps:** Bump the cosign group across 1 directory with 20 updates ([#244](https://github.com/colonel-byte/cargoship/issues/244)) ([b31baad](https://github.com/colonel-byte/cargoship/commit/b31baad73a9b46b343a2b546bde450b5e783486c))
* **deps:** Bump the k8s group across 1 directory with 7 updates ([#243](https://github.com/colonel-byte/cargoship/issues/243)) ([afb47df](https://github.com/colonel-byte/cargoship/commit/afb47df68d62f5da2260d44c0f68371fe698f68a))
* **deps:** Bump the opentelemetry group across 1 directory with 11 updates ([#245](https://github.com/colonel-byte/cargoship/issues/245)) ([5d15c69](https://github.com/colonel-byte/cargoship/commit/5d15c695e518b4b6bce5598c0eee6f7340205edf))

## [0.17.0](https://github.com/colonel-byte/cargoship/compare/v0.16.0...v0.17.0) (2026-08-31)


### ⚠ BREAKING CHANGES

* make package signature verification actually run on publish and local sources ([#236](https://github.com/colonel-byte/cargoship/issues/236))

### Bug Fixes

* add logging into debug on config ([#238](https://github.com/colonel-byte/cargoship/issues/238)) ([28a9aba](https://github.com/colonel-byte/cargoship/commit/28a9aba305574e8e6c13976fff3ec16a4362e06c))
* make package signature verification actually run on publish and local sources ([#236](https://github.com/colonel-byte/cargoship/issues/236)) ([bc34895](https://github.com/colonel-byte/cargoship/commit/bc348957d657c2780fc68a5d363dc22cadf4166f))
* remove no-op --skip-sbom flag, unblock vault-encrypt env fallbacks ([#235](https://github.com/colonel-byte/cargoship/issues/235)) ([8a5c5bc](https://github.com/colonel-byte/cargoship/commit/8a5c5bc3faa0f34f64c01c756dc518493bdcefe3))


### CI/CD

* add commit into mage build ([#237](https://github.com/colonel-byte/cargoship/issues/237)) ([abca83e](https://github.com/colonel-byte/cargoship/commit/abca83e06d0b5769cb3ba156523c944972391dd3))
* update actions ([#232](https://github.com/colonel-byte/cargoship/issues/232)) ([c62b748](https://github.com/colonel-byte/cargoship/commit/c62b748a1f3bf4e79e0e60b58aae3082f902f537))

## [0.16.0](https://github.com/colonel-byte/cargoship/compare/v0.15.1...v0.16.0) (2026-08-30)


### ⚠ BREAKING CHANGES

* generate rke2 and k3s example packages per CNI flavor ([#228](https://github.com/colonel-byte/cargoship/issues/228))

### Features

* generate rke2 and k3s example packages per CNI flavor ([#228](https://github.com/colonel-byte/cargoship/issues/228)) ([d9e5c0d](https://github.com/colonel-byte/cargoship/commit/d9e5c0d2dec0be80a48cdac082b4d5f4fbb91126))
* generate typed engine config structs per distro and version ([#226](https://github.com/colonel-byte/cargoship/issues/226)) ([c2649b3](https://github.com/colonel-byte/cargoship/commit/c2649b3e10c2f5f88139dd8d841dce70b76997db))
* pull pinned k3s and rke2 source into thirdparty-src ([#225](https://github.com/colonel-byte/cargoship/issues/225)) ([b8ee106](https://github.com/colonel-byte/cargoship/commit/b8ee10659b87616d67108cacacf7dcdd66aca27e))
* validate engine config keys against the generated schemas ([#227](https://github.com/colonel-byte/cargoship/issues/227)) ([b959ef5](https://github.com/colonel-byte/cargoship/commit/b959ef581a873d2188625daf8fac90ae845ecb50))


### Documentation

* add usage examples to every cargoship subcommand ([#230](https://github.com/colonel-byte/cargoship/issues/230)) ([d4d7528](https://github.com/colonel-byte/cargoship/commit/d4d7528940ee174847782d754f3a532743e309ed))
* expand the mage target reference ([#224](https://github.com/colonel-byte/cargoship/issues/224)) ([84c60f7](https://github.com/colonel-byte/cargoship/commit/84c60f77bc3a0758b73b02c3ee66faaa8c2f8501))


### CI/CD

* add commit hash ([#231](https://github.com/colonel-byte/cargoship/issues/231)) ([b9ac48a](https://github.com/colonel-byte/cargoship/commit/b9ac48aacab5675b1af960d5503d60a2ee78a20a))

## [0.15.1](https://github.com/colonel-byte/cargoship/compare/v0.15.0...v0.15.1) (2026-08-29)


### Features

* reduce binary ([#221](https://github.com/colonel-byte/cargoship/issues/221)) ([43a17fc](https://github.com/colonel-byte/cargoship/commit/43a17fc5e96d73277d82ced969b462a86876987a))

## [0.15.0](https://github.com/colonel-byte/cargoship/compare/v0.14.0...v0.15.0) (2026-08-28)


### ⚠ BREAKING CHANGES

* add profile concurrency ([#219](https://github.com/colonel-byte/cargoship/issues/219))
* allow registry overrides ([#215](https://github.com/colonel-byte/cargoship/issues/215))

### Features

* add profile concurrency ([#219](https://github.com/colonel-byte/cargoship/issues/219)) ([1abcc66](https://github.com/colonel-byte/cargoship/commit/1abcc66b9a50168c30fb9ac388a7a9800b4df872))
* allow registry overrides ([#215](https://github.com/colonel-byte/cargoship/issues/215)) ([cac56ee](https://github.com/colonel-byte/cargoship/commit/cac56ee52a83071d4a0dbc414c5d9d4b25387ac9))


### Bug Fixes

* logging on rig ([#217](https://github.com/colonel-byte/cargoship/issues/217)) ([4e00e99](https://github.com/colonel-byte/cargoship/commit/4e00e9959943955149f8ded82075995e55a8cddf))

## [0.14.0](https://github.com/colonel-byte/cargoship/compare/v0.13.0...v0.14.0) (2026-08-27)


### ⚠ BREAKING CHANGES

* add logging to file ([#213](https://github.com/colonel-byte/cargoship/issues/213))
* add logic for keeping track of files uploaded ([#212](https://github.com/colonel-byte/cargoship/issues/212))
* add registry overrides ([#211](https://github.com/colonel-byte/cargoship/issues/211))

### Features

* add ability to sign packages ([#208](https://github.com/colonel-byte/cargoship/issues/208)) ([3646edf](https://github.com/colonel-byte/cargoship/commit/3646edff8d641eabd9570db483b1c1eef5dd45bc))
* add logging to file ([#213](https://github.com/colonel-byte/cargoship/issues/213)) ([b9ee105](https://github.com/colonel-byte/cargoship/commit/b9ee105cfc6f99d681c7f480e89b9961ee60ccb0))
* add logic for keeping track of files uploaded ([#212](https://github.com/colonel-byte/cargoship/issues/212)) ([de757a9](https://github.com/colonel-byte/cargoship/commit/de757a9b0376a0d73bc53ba961651dc83e2dad29))
* add registry overrides ([#211](https://github.com/colonel-byte/cargoship/issues/211)) ([cb65777](https://github.com/colonel-byte/cargoship/commit/cb65777bcf77f2c9bd6e0a8a43547d6380d1f6ac))
* add the ability to create reproducible packages ([#209](https://github.com/colonel-byte/cargoship/issues/209)) ([2737a87](https://github.com/colonel-byte/cargoship/commit/2737a873bc441c4ef49eec1b45e9bdd4973a57f8))

## [0.13.0](https://github.com/colonel-byte/cargoship/compare/v0.12.0...v0.13.0) (2026-08-24)


### ⚠ BREAKING CHANGES

* add caching of artifacts ([#204](https://github.com/colonel-byte/cargoship/issues/204))

### Features

* add caching of artifacts ([#204](https://github.com/colonel-byte/cargoship/issues/204)) ([854111d](https://github.com/colonel-byte/cargoship/commit/854111dd521c03b7eb2b275c5906dc23c0e2a77a))
* add shell completion for arch ([#207](https://github.com/colonel-byte/cargoship/issues/207)) ([e81b2fb](https://github.com/colonel-byte/cargoship/commit/e81b2fbf3befe062c8e39adf04141bb50b560f7e))
* fix permissions when pulling package ([#206](https://github.com/colonel-byte/cargoship/issues/206)) ([8990299](https://github.com/colonel-byte/cargoship/commit/89902995bdc4fc75aa2d4f08864e04adf6bf62ac))


### CI/CD

* update dependabot config ([#196](https://github.com/colonel-byte/cargoship/issues/196)) ([3ca50fd](https://github.com/colonel-byte/cargoship/commit/3ca50fddde487d5916b35823bdc77bf9602cf412))


### Build

* **deps:** Bump google.golang.org/grpc from 1.83.0 to 1.83.1 ([#195](https://github.com/colonel-byte/cargoship/issues/195)) ([8878453](https://github.com/colonel-byte/cargoship/commit/88784531588e37eeb94694e8327bf60cd370230a))
* **deps:** Bump the core group across 1 directory with 2 updates ([#198](https://github.com/colonel-byte/cargoship/issues/198)) ([1b6324e](https://github.com/colonel-byte/cargoship/commit/1b6324e93ad3a94422e62d77c351959eb9ed52c1))
* **deps:** Bump the cosign group across 1 directory with 16 updates ([#199](https://github.com/colonel-byte/cargoship/issues/199)) ([3bbb43d](https://github.com/colonel-byte/cargoship/commit/3bbb43d4e09ffd3c238a270883a48eae20f2f3d3))
* **deps:** Bump the k8s group across 1 directory with 8 updates ([#194](https://github.com/colonel-byte/cargoship/issues/194)) ([9b49268](https://github.com/colonel-byte/cargoship/commit/9b49268de160d6ce6040570cc161050dab95f90b))
* **deps:** Bump the misc group across 1 directory with 28 updates ([#201](https://github.com/colonel-byte/cargoship/issues/201)) ([dfb9e29](https://github.com/colonel-byte/cargoship/commit/dfb9e2973390e93c8f4949f53203237f072d9341))

## [0.12.0](https://github.com/colonel-byte/cargoship/compare/v0.11.0...v0.12.0) (2026-08-23)


### ⚠ BREAKING CHANGES

* rework viper config ([#191](https://github.com/colonel-byte/cargoship/issues/191))

### Features

* add shell completion for flags ([#190](https://github.com/colonel-byte/cargoship/issues/190)) ([9c4046a](https://github.com/colonel-byte/cargoship/commit/9c4046a77ee96911930cae4c0df9b379b0158a2d))
* rework viper config ([#191](https://github.com/colonel-byte/cargoship/issues/191)) ([5513027](https://github.com/colonel-byte/cargoship/commit/5513027781368449747342ad12f685dfb3ae9318))


### CI/CD

* move mage main to allow vendoring ([#188](https://github.com/colonel-byte/cargoship/issues/188)) ([75c6d46](https://github.com/colonel-byte/cargoship/commit/75c6d464621ea3e128087d4376ec315d92fefe8f))

## [0.11.0](https://github.com/colonel-byte/cargoship/compare/v0.10.2...v0.11.0) (2026-08-21)


### ⚠ BREAKING CHANGES

* update zarf version ([#187](https://github.com/colonel-byte/cargoship/issues/187))

### Features

* update zarf version ([#187](https://github.com/colonel-byte/cargoship/issues/187)) ([13c9595](https://github.com/colonel-byte/cargoship/commit/13c95955fecc3fca242f847befc0f1f65dc0069c))


### Build

* **deps:** Bump the cosign group across 1 directory with 20 updates ([#183](https://github.com/colonel-byte/cargoship/issues/183)) ([eb4a6ff](https://github.com/colonel-byte/cargoship/commit/eb4a6ff3669d650d29a7591b137e0defe8915074))
* **deps:** Bump the golang group across 1 directory with 10 updates ([#182](https://github.com/colonel-byte/cargoship/issues/182)) ([a9deafc](https://github.com/colonel-byte/cargoship/commit/a9deafcedd1cce6addcd103d0cc16b2a370a1a69))
* **deps:** Bump the misc group across 1 directory with 17 updates ([#186](https://github.com/colonel-byte/cargoship/issues/186)) ([b20ea24](https://github.com/colonel-byte/cargoship/commit/b20ea24431fc0e3ac99f414ed4f5a00f22cb8bac))
* **deps:** Bump the opentelemetry group across 1 directory with 5 updates ([#184](https://github.com/colonel-byte/cargoship/issues/184)) ([61ce493](https://github.com/colonel-byte/cargoship/commit/61ce4938316fc0388463542ec11c9e685fa04207))

## [0.10.2](https://github.com/colonel-byte/cargoship/compare/v0.10.1...v0.10.2) (2026-08-14)


### Features

* remove ingests folder from tarball ([#180](https://github.com/colonel-byte/cargoship/issues/180)) ([29ded7b](https://github.com/colonel-byte/cargoship/commit/29ded7b16f47e9434f9e721b82af055ca29db074))


### Documentation

* add asd ste1000 ([#179](https://github.com/colonel-byte/cargoship/issues/179)) ([6fed7f5](https://github.com/colonel-byte/cargoship/commit/6fed7f5c709087d33403eac0fa5793abe739226e))

## [0.10.1](https://github.com/colonel-byte/cargoship/compare/v0.10.0...v0.10.1) (2026-08-12)


### Documentation

* simplify markdown ([#177](https://github.com/colonel-byte/cargoship/issues/177)) ([37511a5](https://github.com/colonel-byte/cargoship/commit/37511a5ca3c8db665ea3d5f8b1bda99cbfe7a91d))

## [0.10.0](https://github.com/colonel-byte/cargoship/compare/v0.9.0...v0.10.0) (2026-08-11)


### ⚠ BREAKING CHANGES

* add profiles for the config ([#176](https://github.com/colonel-byte/cargoship/issues/176))
* schema generator includes golang comments ([#165](https://github.com/colonel-byte/cargoship/issues/165))

### Features

* add profiles for the config ([#176](https://github.com/colonel-byte/cargoship/issues/176)) ([cda1112](https://github.com/colonel-byte/cargoship/commit/cda1112ef8c1f284b7abff2a71d33380910b7d4a))
* add the built dagger artifacts ([#158](https://github.com/colonel-byte/cargoship/issues/158)) ([9f61630](https://github.com/colonel-byte/cargoship/commit/9f6163021dcd1300a522b44101d652ee7c6a6e23))
* schema generator includes golang comments ([#165](https://github.com/colonel-byte/cargoship/issues/165)) ([68364ff](https://github.com/colonel-byte/cargoship/commit/68364ff101da6177193702a1d82ac68c978a53da))
* standardize the location distro interaction ([#159](https://github.com/colonel-byte/cargoship/issues/159)) ([3ba592e](https://github.com/colonel-byte/cargoship/commit/3ba592eb0e1549cc36eba7cd21e8991bcb958de4))


### Documentation

* expand info about the project ([#166](https://github.com/colonel-byte/cargoship/issues/166)) ([4d672f5](https://github.com/colonel-byte/cargoship/commit/4d672f52f66f4b715946dc2ee681f80866b640d9))


### CI/CD

* update dependabot to update indirect goland modules ([#138](https://github.com/colonel-byte/cargoship/issues/138)) ([5cd3b69](https://github.com/colonel-byte/cargoship/commit/5cd3b69b1e7e98afa7a686601eae25fa5c1e0dbf))
* update the dependabot ([#174](https://github.com/colonel-byte/cargoship/issues/174)) ([8061c61](https://github.com/colonel-byte/cargoship/commit/8061c61c8d755c168e61b15a6c0c2bab8a78b547))
* update the dependabot config ([#172](https://github.com/colonel-byte/cargoship/issues/172)) ([aa6652a](https://github.com/colonel-byte/cargoship/commit/aa6652a756d87b1fc52d029c14eb1a9b8a123478))
* update the dependabot logic ([#150](https://github.com/colonel-byte/cargoship/issues/150)) ([d7b2fef](https://github.com/colonel-byte/cargoship/commit/d7b2fef1d05514ff6dab86cba825d3768caf0ec9))


### Build

* **deps:** Bump github.com/alibabacloud-go/debug from 1.0.0 to 1.0.1 ([#149](https://github.com/colonel-byte/cargoship/issues/149)) ([6891f31](https://github.com/colonel-byte/cargoship/commit/6891f31721473f27187f5ff8d6e79f6e57b5c1b1))
* **deps:** Bump github.com/aws/aws-sdk-go-v2/internal/configsources from 1.4.29 to 1.4.34 ([#144](https://github.com/colonel-byte/cargoship/issues/144)) ([45d3e40](https://github.com/colonel-byte/cargoship/commit/45d3e40af149ad24077947d705e4a0eb33635e66))
* **deps:** Bump github.com/Azure/go-autorest/autorest/date from 0.3.0 to 0.3.1 ([#143](https://github.com/colonel-byte/cargoship/issues/143)) ([e3e647f](https://github.com/colonel-byte/cargoship/commit/e3e647fdd29828a90462870d2c2a53b06764d805))
* **deps:** Bump github.com/docker/cli in the docker group across 1 directory ([#167](https://github.com/colonel-byte/cargoship/issues/167)) ([8ef1011](https://github.com/colonel-byte/cargoship/commit/8ef1011324325f20ba402ba926daeefbfcab4eba))
* **deps:** Bump github.com/felixge/httpsnoop from 1.0.4 to 1.1.0 ([#154](https://github.com/colonel-byte/cargoship/issues/154)) ([be7dd60](https://github.com/colonel-byte/cargoship/commit/be7dd60e0ff6411047cb9f518cfd20b7c9f334a6))
* **deps:** Bump github.com/gabriel-vasile/mimetype from 1.4.13 to 1.4.15 ([#142](https://github.com/colonel-byte/cargoship/issues/142)) ([e620467](https://github.com/colonel-byte/cargoship/commit/e6204675d75d92cb628809fbd702f539db785fdd))
* **deps:** Bump github.com/go-git/go-git/v5 from 5.19.1 to 5.19.2 ([#163](https://github.com/colonel-byte/cargoship/issues/163)) ([8300cdd](https://github.com/colonel-byte/cargoship/commit/8300cdd909c8d1776da9681f8efee81fccf252eb))
* **deps:** Bump github.com/go-openapi/swag/pools from 0.27.3 to 0.28.0 ([#145](https://github.com/colonel-byte/cargoship/issues/145)) ([f712628](https://github.com/colonel-byte/cargoship/commit/f71262804b2d5bf8c85ad4fb38a29e617439a78c))
* **deps:** Bump github.com/lestrrat-go/dsig from 1.2.1 to 1.3.0 ([#141](https://github.com/colonel-byte/cargoship/issues/141)) ([b280e61](https://github.com/colonel-byte/cargoship/commit/b280e611deca47e94afe143790513bef47876abb))
* **deps:** Bump github.com/lestrrat-go/jwx/v3 from 3.1.1 to 3.2.0 ([#157](https://github.com/colonel-byte/cargoship/issues/157)) ([b077c77](https://github.com/colonel-byte/cargoship/commit/b077c77cf1c1389d7cf4e11d79403f815a27f3ac))
* **deps:** Bump github.com/moby/sys/sequential from 0.6.0 to 0.7.0 ([#156](https://github.com/colonel-byte/cargoship/issues/156)) ([081a016](https://github.com/colonel-byte/cargoship/commit/081a016e156054bfab5c53560dc38354a641f7ff))
* **deps:** Bump go.opentelemetry.io/.../google.golang.org/grpc/otelgrpc from 0.68.0 to 0.69.0 ([#155](https://github.com/colonel-byte/cargoship/issues/155)) ([457dfa7](https://github.com/colonel-byte/cargoship/commit/457dfa73706876bc6e83fd61c0f5321aafc2f192))
* **deps:** Bump go.opentelemetry.io/otel/metric from 1.44.0 to 1.45.0 ([#162](https://github.com/colonel-byte/cargoship/issues/162)) ([91435aa](https://github.com/colonel-byte/cargoship/commit/91435aade9ee9314128098209cca29802cac24f9))
* **deps:** Bump go.opentelemetry.io/otel/metric from 1.44.0 to 1.45.0 in /.dagger ([#164](https://github.com/colonel-byte/cargoship/issues/164)) ([268f75a](https://github.com/colonel-byte/cargoship/commit/268f75a576f0eaafc73c43e94220d6e4a22c4add))
* **deps:** Bump the aws group across 1 directory with 14 updates ([#151](https://github.com/colonel-byte/cargoship/issues/151)) ([4af8fdf](https://github.com/colonel-byte/cargoship/commit/4af8fdf824d75e0560dbca32fd3b9e789aa698ff))
* **deps:** Bump the aws group across 1 directory with 15 updates ([#168](https://github.com/colonel-byte/cargoship/issues/168)) ([56ea0cf](https://github.com/colonel-byte/cargoship/commit/56ea0cf5a6ff7394db5d4cf61ea44cf21cd69909))
* **deps:** Bump the azure group across 1 directory with 6 updates ([#152](https://github.com/colonel-byte/cargoship/issues/152)) ([26a3688](https://github.com/colonel-byte/cargoship/commit/26a36884bff38842e998da83e38b3b337ea3e999))
* **deps:** Bump the docker group across 1 directory with 2 updates ([#140](https://github.com/colonel-byte/cargoship/issues/140)) ([9fce9c0](https://github.com/colonel-byte/cargoship/commit/9fce9c0f27321a350e03bf049c34765b2ae70928))
* **deps:** Bump the misc group across 1 directory with 28 updates ([#175](https://github.com/colonel-byte/cargoship/issues/175)) ([031dc81](https://github.com/colonel-byte/cargoship/commit/031dc819298f708ae3d18e598635c97bbc62d6f4))
* **deps:** Bump the openapi group across 1 directory with 11 updates ([#153](https://github.com/colonel-byte/cargoship/issues/153)) ([8273afb](https://github.com/colonel-byte/cargoship/commit/8273afb4ab79b0501994c6d5843b68a4c9702d4b))
* **deps:** Bump the sigstore group across 1 directory with 6 updates ([#161](https://github.com/colonel-byte/cargoship/issues/161)) ([26f65d3](https://github.com/colonel-byte/cargoship/commit/26f65d3919ddee72f2a3feeecce74bbb61b4cf33))

## [0.9.0](https://github.com/colonel-byte/cargoship/compare/v0.8.0...v0.9.0) (2026-08-05)


### ⚠ BREAKING CHANGES

* add framework for publishing and pulling packages ([#134](https://github.com/colonel-byte/cargoship/issues/134))

### Features

* add framework for publishing and pulling packages ([#134](https://github.com/colonel-byte/cargoship/issues/134)) ([9f97253](https://github.com/colonel-byte/cargoship/commit/9f972535442cd50c596424ae6610bd7fbc8671ea))

## [0.8.0](https://github.com/colonel-byte/cargoship/compare/v0.7.0...v0.8.0) (2026-08-04)


### ⚠ BREAKING CHANGES

* add sha256sum command ([#136](https://github.com/colonel-byte/cargoship/issues/136))

### Features

* add sha256sum command ([#136](https://github.com/colonel-byte/cargoship/issues/136)) ([a1b5f8b](https://github.com/colonel-byte/cargoship/commit/a1b5f8b8b67c78a6d8daa3fbe7db6a9a0eec0425))


### Documentation

* add mdbook site builder ([#135](https://github.com/colonel-byte/cargoship/issues/135)) ([fa31b78](https://github.com/colonel-byte/cargoship/commit/fa31b78f796838e57b97afe233269be13e45900a))


### CI/CD

* fix gitignore settings ([#133](https://github.com/colonel-byte/cargoship/issues/133)) ([47d77bc](https://github.com/colonel-byte/cargoship/commit/47d77bc842aa90f0c9d45639de6f3d3c471f2046))
* remove bootloose images ([#131](https://github.com/colonel-byte/cargoship/issues/131)) ([49bc60c](https://github.com/colonel-byte/cargoship/commit/49bc60cf344ad9b22bb22a4f425dc46501cef11d))

## [0.7.0](https://github.com/colonel-byte/cargoship/compare/v0.6.0...v0.7.0) (2026-07-28)


### ⚠ BREAKING CHANGES

* vendor the golang dep ([#128](https://github.com/colonel-byte/cargoship/issues/128))

### CI/CD

* add envrc config ([#123](https://github.com/colonel-byte/cargoship/issues/123)) ([8e3154d](https://github.com/colonel-byte/cargoship/commit/8e3154d0b04026e3dbff7f63eeaea9ad5ee3e6f6))
* address dependabot issues ([#129](https://github.com/colonel-byte/cargoship/issues/129)) ([07c3a04](https://github.com/colonel-byte/cargoship/commit/07c3a04402cd70ea0631b5416731884affb8c681))
* vendor the golang dep ([#128](https://github.com/colonel-byte/cargoship/issues/128)) ([832f376](https://github.com/colonel-byte/cargoship/commit/832f376f9f47c88db9e514f626a8b389091be36f))


### Build

* **deps:** Bump actions/checkout from 7.0.0 to 7.0.1 ([#122](https://github.com/colonel-byte/cargoship/issues/122)) ([0d77578](https://github.com/colonel-byte/cargoship/commit/0d7757876902877b16439e69cc63873b699a591d))
* **deps:** Bump actions/setup-go from 6.5.0 to 7.0.0 ([#120](https://github.com/colonel-byte/cargoship/issues/120)) ([4276e9d](https://github.com/colonel-byte/cargoship/commit/4276e9d88ea8c3e30bf368505b81d86732a81ec4))
* **deps:** Bump actions/setup-node from 6.4.0 to 7.0.0 ([#119](https://github.com/colonel-byte/cargoship/issues/119)) ([bde75d1](https://github.com/colonel-byte/cargoship/commit/bde75d1f5d46cc7d2c58bef43eab9cbfe4b847dc))
* **deps:** Bump docker/build-push-action from 7.2.0 to 7.3.0 ([#109](https://github.com/colonel-byte/cargoship/issues/109)) ([d201529](https://github.com/colonel-byte/cargoship/commit/d201529be28daf91a5765633064b0a8ae87b3ef0))
* **deps:** Bump docker/login-action from 4.2.0 to 4.3.0 ([#112](https://github.com/colonel-byte/cargoship/issues/112)) ([902d370](https://github.com/colonel-byte/cargoship/commit/902d37052e58910ba9055e56e3bf37add29b9d43))
* **deps:** Bump docker/login-action from 4.3.0 to 4.4.0 ([#114](https://github.com/colonel-byte/cargoship/issues/114)) ([a6ae875](https://github.com/colonel-byte/cargoship/commit/a6ae87501ccccf51016de905a24d3c13938b816c))
* **deps:** Bump docker/login-action from 4.4.0 to 4.5.1 ([#126](https://github.com/colonel-byte/cargoship/issues/126)) ([3d087d1](https://github.com/colonel-byte/cargoship/commit/3d087d13fd762d4999918343fb801b7444700ad2))
* **deps:** Bump docker/login-action from 4.5.1 to 4.5.2 ([#130](https://github.com/colonel-byte/cargoship/issues/130)) ([e93e7c1](https://github.com/colonel-byte/cargoship/commit/e93e7c1aa0105a0b0afe034c674c5030dceb1846))
* **deps:** Bump docker/setup-buildx-action from 4.1.0 to 4.2.0 ([#113](https://github.com/colonel-byte/cargoship/issues/113)) ([92a726b](https://github.com/colonel-byte/cargoship/commit/92a726b183c79ff65960b43db5c6c24736643b78))
* **deps:** Bump github.com/containerd/containerd/v2 from 2.3.2 to 2.3.3 ([#118](https://github.com/colonel-byte/cargoship/issues/118)) ([8992739](https://github.com/colonel-byte/cargoship/commit/8992739a282470c05fdab24716cf5d16366c8b87))
* **deps:** Bump golang.org/x/sync from 0.21.0 to 0.22.0 ([#115](https://github.com/colonel-byte/cargoship/issues/115)) ([1ec8814](https://github.com/colonel-byte/cargoship/commit/1ec8814a986568b0d776e088e47b7032e6f2a397))
* **deps:** Bump https://github.com/google/keep-sorted from v0.9.0 to 0.9.1 ([#110](https://github.com/colonel-byte/cargoship/issues/110)) ([773b5f3](https://github.com/colonel-byte/cargoship/commit/773b5f3dd4f97527208a0c2b744033442653bee7))
* **deps:** Bump k8s.io/client-go from 0.36.2 to 0.36.3 in the k8s group across 1 directory ([#125](https://github.com/colonel-byte/cargoship/issues/125)) ([7126818](https://github.com/colonel-byte/cargoship/commit/7126818bf2719da6bd692a49583353ca41889d7f))
* **deps:** Bump oras.land/oras-go/v2 from 2.6.1 to 2.6.2 ([#116](https://github.com/colonel-byte/cargoship/issues/116)) ([7140d42](https://github.com/colonel-byte/cargoship/commit/7140d42428c8fa09ba6ceaa3202a7227c91a1e4d))

## [0.6.0](https://github.com/colonel-byte/cargoship/compare/v0.5.0...v0.6.0) (2026-06-30)


### ⚠ BREAKING CHANGES

* add firewalld policy management ([#107](https://github.com/colonel-byte/cargoship/issues/107))
* reduce binary size ([#101](https://github.com/colonel-byte/cargoship/issues/101))

### Features

* add firewalld policy management ([#107](https://github.com/colonel-byte/cargoship/issues/107)) ([4a52030](https://github.com/colonel-byte/cargoship/commit/4a520304207ee9c7ebe1fe555cd2a9917198259f))
* reduce binary size ([#101](https://github.com/colonel-byte/cargoship/issues/101)) ([81bd7ea](https://github.com/colonel-byte/cargoship/commit/81bd7ea8d26f8bb180a494d72c837f6b6b9a338e))


### Build

* **deps:** Bump golangci/golangci-lint-action from 9.2.1 to 9.3.0 ([#105](https://github.com/colonel-byte/cargoship/issues/105)) ([a314b7c](https://github.com/colonel-byte/cargoship/commit/a314b7c9b40a75c0ab5dd363f0bb51d2dea656a0))
* **deps:** Bump goreleaser/goreleaser-action from 7.2.2 to 7.2.3 ([#106](https://github.com/colonel-byte/cargoship/issues/106)) ([0b32174](https://github.com/colonel-byte/cargoship/commit/0b32174b9bb006681159c561909c5e918184d8ea))

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
