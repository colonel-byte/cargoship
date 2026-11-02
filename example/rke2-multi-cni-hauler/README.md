# Hauler manifests for the rke2 multi-CNI cilium example

[hauler](https://github.com/hauler-dev/hauler) manifests that collect everything [`example/rke2-multi-cni-cilium/v1_36/v1.36.5-rke2r1`](../rke2-multi-cni-cilium/v1_36/v1.36.5-rke2r1) declares, so that example can be built with `cargoship create --hauler-store` and nothing fetched over the network. See [`docs/guides/hauler-store.md`](../../docs/guides/hauler-store.md) for what the flag does and how matching works.

**These are hand-written.** Everything else under `example/` is rendered from a template by `mage generate:examples` - see [`example/AGENTS.md`](../AGENTS.md) - and these are not. They live in a directory of their own, beside the flavor directories rather than inside one, for a specific reason: a render deletes a version directory outright once upstream stops publishing that build (`removeExample` in `magefiles/pkg/gen/examples/examples.go`), and Rancher drops an `rke2rN`'s RPMs as soon as the next revision supersedes it. Anything hand-written next to the `distro.yaml` would go with it. Nothing visits this directory, because flavor paths are a declared list in `magefiles/pkg/gen/examples/distros.go` and this is not one of them.

| File                                                             | What it collects                                                                  |
| ---------------------------------------------------------------- | --------------------------------------------------------------------------------- |
| [`v1.36.5-rke2r1-images.yaml`](v1.36.5-rke2r1-images.yaml)       | All 25 images - the flat `imageConfig.images` list plus the `rke2-cilium` chart's |
| [`v1.36.5-rke2r1-files.yaml`](v1.36.5-rke2r1-files.yaml)         | All 11 unique file sources, both architectures                                    |

The version is in the filename rather than the path, so a second release line is another pair of files here.

## Collecting and building

```sh
hauler store sync --store /tmp/haul/store --exclude-extras \
  --filename example/rke2-multi-cni-hauler/v1.36.5-rke2r1-images.yaml \
  --filename example/rke2-multi-cni-hauler/v1.36.5-rke2r1-files.yaml

cargoship create example/rke2-multi-cni-cilium/v1_36/v1.36.5-rke2r1 \
  --hauler-store /tmp/haul/store -o /tmp/out
```

This collects every platform of 25 images and both architectures of 11 files, so expect it to be large and slow. For a fast check of the mechanism instead, [`hack/hauler/`](../../hack/hauler) holds a roughly 19 MB subset with a matching definition.

`--platform` takes one platform rather than a list, and omitting it collects all of them. This example targets amd64 and arm64, so passing `--platform linux/amd64` produces a store a build of it cannot finish from.

A build that succeeded is not proof the store was used - check the log, where every artifact says where its bytes came from:

```text
INF file read from content store declared=https://rpm.rancher.io/.../rke2-selinux-0.22-1.el9.noarch.rpm
INF saving image name=docker.io/rancher/mirrored-pause:3.10.2 source=hauler store /tmp/haul/store
```

A `not held by the content store` warning means that artifact came from the network instead. To prove there is no network rather than reading for its absence, build in a namespace without one - this needs no privileges:

```sh
unshare -rn cargoship create example/rke2-multi-cni-cilium/v1_36/v1.36.5-rke2r1 \
  --hauler-store /tmp/haul/store -o /tmp/out
```

## Keeping these in step with the example

The example they mirror is generated and its pins move: a refresh changes image tags, RPM versions and shasums. These manifests will not follow, and a stale entry does not announce itself - the reference stops matching, the build falls back to the network, and that reads as a slow success rather than a failure. So when a build warns about content that should have been in the store, compare the manifests against the definition before looking anywhere else:

```sh
hauler store info --store /tmp/haul/store --output json --type image
hauler store info --store /tmp/haul/store --output json --type file
```

## Why the entries are spelled the way they are

Matching is by the reference hauler recorded each artifact as having come from, so three things are load-bearing rather than cosmetic:

- **File URLs are byte-exact copies of the definition's `source`.** `v1.36.5%2Brke2r1` is not interchangeable with `v1.36.5+rke2r1`; hauler stores the URL as written and the match is a string comparison.
- **Files are added as URLs, not local paths.** `hauler store add file ./rke2.tar.gz` records the absolute path of the machine that built the haul, which no definition declares.
- **Images are spelled as the definition spells them.** Hauler normalises a Docker Hub reference to `index.docker.io/...`; cargoship matches either spelling, so do not rewrite `docker.io/...` to match what `hauler store info` prints.

A file is only taken from a store when the definition declares a `shasum` for it - store content is bytes the definition's author never saw, and the shasum is the only thing that verifies it. Every file in this example declares one.
