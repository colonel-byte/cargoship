# Building From a Hauler Store

This guide covers building a distro package from a [hauler](https://github.com/hauler-dev/hauler) store: collecting the images and files on a connected machine, carrying the result across the airlock, and building the package on the other side with nothing fetched over the network.

Hauler is a tool for collecting OCI content - images, files, charts - into a single portable store, and it is what many teams already use to move that content into an air-gapped environment. Cargoship reads such a store directly, so a haul that has already crossed the gap can be handed to `create` as the build's content source.

For the two narrower redirections, see [registry-override](registry-override.md) and [file-override](file-override.md). This guide explains when a haul is the better fit than either, and how the three combine.

## Why a Haul Rather Than an Override

All three mechanisms answer the same question - *where should this content come from* - at different levels.

| Mechanism             | What it redirects                 | What the operator writes                |
| --------------------- | --------------------------------- | --------------------------------------- |
| `--registry-override` | Image pulls, by registry prefix   | One `SOURCE=TARGET` pair per registry   |
| `--file-override`     | File downloads, by URL prefix     | One `SOURCE=TARGET` pair per URL prefix |
| `--hauler-store`      | Both images and files, by content | One path                                |

The overrides describe *where to fetch from instead*, so they still need something to fetch from: a reachable mirror, a running file server, or a directory of files whose layout mirrors the URLs they replace. A haul is the content itself, and it records what each artifact was collected from, so cargoship matches the definition against it with no mapping written by hand.

That recorded origin is the whole mechanism. When hauler adds an artifact it stores a `hauler.dev/original-ref` annotation naming where the artifact came from: a `registry/repo:tag` for an image, and the source URL for a file. Those are the same strings a distro definition writes in `imageConfig.images` and in a file's `source`, so matching the two is an exact lookup rather than a guess.

Reach for an override instead when the content is behind an internal mirror that the management node *can* reach, or when you want one specific file swapped and nothing else. The two work together - see [Combining a Haul With Overrides](#combining-a-haul-with-overrides).

## Collecting the Haul

On a machine that can reach the upstream registries and URLs, add everything the distro definition declares. **Add files by the same URL the definition names**, because that URL is what the match is made on:

```bash
# The files a definition declares, added by their declared URL
hauler store add file https://github.com/flannel-io/flannel/releases/download/v0.28.10/kube-flannel.yml

# The images it declares
hauler store add image registry.k8s.io/kube-apiserver:v1.37.0
hauler store add image registry.k8s.io/pause:3.10.2
```

A hauler manifest is the practical way to do this for a real package, rather than one command per artifact:

```bash
hauler store sync --filename hauler-manifest.yaml
```

Manifests collecting everything a shipped example declares are generated beside its definition as `hauler-files-<arch>.yaml` and `hauler-images-<component>.yaml` - see [`example/rke2-multi-cni-cilium/`](https://github.com/colonel-byte/cargoship/tree/main/example/rke2-multi-cni-cilium) - and [`hack/hauler/`](https://github.com/colonel-byte/cargoship/tree/main/hack/hauler) holds a roughly 19 MB subset with a matching definition - useful for checking the mechanism before collecting a whole release.

Then save the store as a single archive to carry:

```bash
hauler store save --filename haul.tar.zst
```

### Adding a File From a Local Path Does Not Match a URL

`hauler store add file ./install.sh` records the **absolute path on the machine that built the haul** as the artifact's origin, not a URL. A definition that declares `source: https://get.rke2.io/install.sh` will not match that entry, and the file falls back to being downloaded.

So collect files by their declared URL. Where the bytes are only available locally, the mechanism that fits is `--file-override` pointing at a directory, not a haul.

## Building From the Haul

On the disconnected side, point `create` at the store. Both a store directory and an archive work:

```bash
# From an archive written by `hauler store save`
cargoship create ./distro-defs --hauler-store ./haul.tar.zst

# From a store directory
cargoship create ./distro-defs --hauler-store ./store
```

An archive is unpacked into a temporary directory for the build and removed afterwards. A store directory is only ever read - cargoship does not write to it, add the `oci-layout` marker a live store lacks, or otherwise modify a store you may still be adding to.

The setting belongs in the config file when a management node always builds from the same haul:

```yaml
distro:
  create:
    hauler_store: ~/hauls/haul.tar.zst
```

A leading `~` is expanded, which it is not in every tool - see [configuration](../schema/config.md) for the rest of the section.

The log says where each artifact's bytes came from:

```text
INF resolving package content from a hauler store store=/opt/hauls/haul.tar.zst
INF file read from content store declared=https://get.rke2.io/install.sh name=install.sh store=/opt/hauls/haul.tar.zst
INF saving image name=registry.k8s.io/pause:3.10 architecture=amd64 source=hauler store /opt/hauls/haul.tar.zst
```

The same information is recorded into the built package, under `.build.fileSources`, so a package discloses which of its files came from a store rather than from the source the definition declared:

```yaml
build:
  fileSources:
    - path: files/0/install.sh
      declared: https://get.rke2.io/install.sh
      resolved: install.sh
      store: /opt/hauls/haul.tar.zst
      shasum: 393bdbce53eb50c8eb97dd513b218518b7d6e7985dc7b889be956e112a711084
```

## A File Taken From a Store Needs a Declared Shasum

A haul holds bytes that whoever wrote the distro definition never saw. The `shasum` the definition declares for a file is the only thing that proves those bytes are the ones intended, so a file with no declared shasum is **refused** rather than staged unverified:

```text
ERR failed to create distro package: unable to stage file 0 (https://get.rke2.io/install.sh):
    hauler store ./haul.tar.zst holds https://get.rke2.io/install.sh, which declares no shasum:
    refusing to stage it unverified
```

This is the same rule `--file-override` applies, and for the same reason. The fix is to declare the shasum in the definition, not to work around the check.

Images need no equivalent: an image reference is resolved through its digest, so the content is verified by construction.

## A Partial Haul Still Builds

Content the store does not hold falls back to the source the definition declared, and each miss is reported:

```text
WRN file not held by the content store, falling back to its declared source declared=https://get.k3s.io/k3s
WRN image not held by the local image source, falling back to its registry image=registry.k8s.io/etcd:3.7.0-0
```

This makes a haul collected for one package usable for another that overlaps with it, and lets a haul be topped up rather than rebuilt.

On a management node with no route to those declared sources, the fallback cannot succeed, and **an image that is in neither the haul nor a reachable registry fails the build**:

```text
ERR failed to create distro package: image registry.k8s.io/etcd:3.7.0-0 is not in hauler store
    ./haul.tar.zst and could not be fetched from its registry: ...
```

That is deliberate. The alternative - carrying on and producing a package that is quietly missing an image - defers the failure to whoever installs it, which is the worst place to find out.

So treat the warnings as the thing to read after a build that succeeded. A build that logged a dozen misses and still finished reached the network a dozen times, which on a node that is supposed to be air-gapped is worth knowing.

## Combining a Haul With Overrides

All three can be passed together, and they resolve in a fixed order.

For **files**, an explicit `--file-override` wins over the haul. An operator who named a redirect for a specific file asked for that one, and a store that happens to also hold it does not silently take precedence.

For **images**, a `--registry-override` is applied *first*, and the haul is then searched for the rewritten reference. So an override that rewrites `registry.k8s.io` to `mirror.example.com` means the haul is searched for `mirror.example.com/...`, which is usually not what a haul collected from upstream holds. Pass the haul without registry overrides unless the haul was itself collected from the mirror.

## Serving a Haul Instead

Hauler can also serve a store, and that path needs nothing from this guide - it is the existing override mechanism:

```bash
# On the disconnected side
hauler store serve registry &      # an OCI registry on :5000
hauler store serve fileserver &    # a file server on :8080

cargoship create ./distro-defs --plain-http \
  --registry-override registry.k8s.io=localhost:5000 \
  --file-override https://get.rke2.io/install.sh=http://localhost:8080/install.sh
```

This works today and is a reasonable fit when something else on the network also needs the registry. Two things to know if you choose it:

- The file server serves artifacts by **name**, flat, while `--file-override` appends the remainder of the declared URL's path to the target. So a prefix override will usually miss, and each file needs its own exact-URL override as above.
- `--plain-http` is required, because both servers are HTTP by default.

`--hauler-store` exists because that is a lot of typing to restate something the haul already records.

## What a Haul Carries That Cargoship Ignores

A haul may hold more than a distro package can use. Charts, git repositories and wasm artifacts are skipped: a definition's charts are declared under `.spec.config.engine.manifest`, and the images they run are already collected through the definition's own image list.

Signatures, attestations and SBOMs attached to an image are also skipped. They share the original reference of the image they describe, so they are identified by what hauler recorded them as and excluded, leaving the image itself to be found.

## Troubleshooting

| Symptom                                          | Cause                                                                                                                                                             |
| ------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `is not a hauler store`                          | The path has no `index.json`. Point at the store directory itself, not its parent, or at the archive.                                                             |
| `looks like one chunk of a split hauler archive` | A `--chunk-size` haul arrived as `haul.tar.zst.001` and friends. Rejoin it with `hauler store load`, then pass the resulting store.                               |
| `holds no content cargoship can use`             | Nothing in the index records what it was collected from. The store was not written by a hauler version that sets `hauler.dev/original-ref`.                       |
| Everything falls back, nothing matches           | The declared sources and the collected references differ. Compare `hauler store info` against the definition - a file added from a local path is the usual cause. |
| `which declares no shasum`                       | Add the `shasum` to the file's entry in the definition.                                                                                                           |

`hauler store info` lists what a store actually holds, which is the first thing to compare against the definition when a match is not made:

```bash
hauler store info --store ./store
hauler store info --store ./store --output json --type file
```
