# Building From a Local Content Store

This guide covers building a distro package from a local content store: collecting the images and files on a connected machine, carrying the result across the airlock, and building the package on the other side with nothing fetched over the network.

Two formats are read, and `--content-store` takes either:

| Format                                                   | What it is                                                                                  | How to collect one                                      |
| -------------------------------------------------------- | ------------------------------------------------------------------------------------------- | ------------------------------------------------------- |
| A [hauler](https://github.com/hauler-dev/hauler) store   | A portable store of OCI content - images, files, charts - as a directory or a saved archive | [Collecting a haul](#collecting-a-haul)                 |
| A built [zarf](https://github.com/zarf-dev/zarf) package | A `zarf-package-*.tar.zst`, carrying an image layout and one tarball per component          | [Collecting a zarf package](#collecting-a-zarf-package) |

Both are tools teams already use to move content into an air-gapped environment, and both record where each artifact came from, which is what makes the match exact. **Which format a store is comes from what it holds, not from what it is called**, so a renamed bundle still builds.

For the two narrower redirections, see [registry-override](registry-override.md) and [file-override](file-override.md). This guide explains when a content store is the better fit than either, and how the three combine.

> `--hauler-store` is the former name for `--content-store`, and reads a hauler store and nothing else. It still works and still builds, and it prints a deprecation warning. Use `--content-store`.

## Why a Content Store Rather Than an Override

All three mechanisms answer the same question - *where should this content come from* - at different levels.

| Mechanism             | What it redirects                 | What the operator writes                |
| --------------------- | --------------------------------- | --------------------------------------- |
| `--registry-override` | Image pulls, by registry prefix   | One `SOURCE=TARGET` pair per registry   |
| `--file-override`     | File downloads, by URL prefix     | One `SOURCE=TARGET` pair per URL prefix |
| `--content-store`     | Both images and files, by content | One path                                |

The overrides describe *where to fetch from instead*, so they still need something to fetch from: a reachable mirror, a running file server, or a directory of files whose layout mirrors the URLs they replace. A content store is the content itself, and it records what each artifact was collected from, so cargoship matches the definition against it with no mapping written by hand.

That recorded origin is the whole mechanism, and each format records it its own way:

| Format       | Images are matched by                                                             | Files are matched by                                                   |
| ------------ | --------------------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| Hauler       | The `hauler.dev/original-ref` annotation hauler writes on every entry it collects | The same annotation, which for a file holds its source URL             |
| Zarf package | The `org.opencontainers.image.ref.name` annotation in `images/index.json`         | The `source` each component file declares in the package's `zarf.yaml` |

Those are the same strings a distro definition writes in `imageConfig.images` and in a file's `source`, so matching the two is an exact lookup rather than a guess.

Image references are canonicalised before the lookup, so every spelling of one image resolves to the same entry: a definition naming `nginx:1.27-alpine` matches a store that recorded `index.docker.io/library/nginx:1.27-alpine`.

Reach for an override instead when the content is behind an internal mirror that the management node *can* reach, or when you want one specific file swapped and nothing else. The two work together - see [Combining a Content Store With Overrides](#combining-a-content-store-with-overrides).

### A Multi-Platform File Artifact Resolves Per Architecture

A file in a haul is usually one manifest holding one titled layer, and the reference names bytes on its own. Some vendors publish an air-gapped engine bundle differently: one tag, an image index, and one manifest per platform, each carrying that architecture's tarball as a titled layer.

```text
registry.example.com/vendor/engine:v1.37.1   -> application/vnd.oci.image.index.v1+json
  |- linux/amd64   layer title "engine.linux-amd64.tar.gz"
  |- linux/arm64   layer title "engine.linux-arm64.tar.gz"
```

The reference no longer names one file, so the entry's own `selector.arch` picks the platform child out of the index:

```yaml
- source: oci://registry.example.com/vendor/engine:v1.37.1
  extractPath: bin/engine
  target: /usr/local/bin/engine
  executable: true
  shasum: <sha256 of the extracted member>
  selector:
    package: binary
    arch:
      - amd64
```

A file is staged arch-blind during a build - every architecture's entries go into one tree, keyed on the entry's position in the list - so `selector.arch` is the only thing that says which platform an entry meant. An entry naming none of them, or naming several, is therefore **refused** rather than resolved to a guess:

```text
ERR failed to create distro package: unable to stage file 0:
    hauler store ./haul.tar.zst holds oci://registry.example.com/vendor/engine:v1.37.1 for
    amd64, arm64, and the file entry selects no single architecture: name one in its
    selector.arch
```

Write one entry per architecture, each naming its own. A store that holds the reference for some architectures but not the one an entry names is a [partial store](#a-partial-store-still-builds), not an error: that entry falls back to its declared source.

Note that **a haul synced one architecture at a time can mask this.** A single-platform `hauler store sync` leaves a bare entry under the same reference in addition to the index, and that entry carries no platform, so it answers for every architecture. A store built up one architecture at a time therefore appears to work for whichever architecture was synced last, while a clean two-platform store resolves per entry as above.

## Collecting a Haul

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

## Collecting a Zarf Package

A built zarf package needs no special handling: `zarf package create` records what it collected, and the package an operator already has is the one to pass.

Declare the same images and files the distro definition declares, by the same references and the same URLs:

```yaml
kind: ZarfPackageConfig
metadata:
  name: rke2-content
components:
  - name: payload
    images:
      - registry.k8s.io/pause:3.10
    files:
      - source: https://get.rke2.io/install.sh
        target: /usr/local/bin/install.sh
```

```bash
zarf package create . --confirm
```

A package's `zarf.yaml` keeps each component file's `source` exactly as its author wrote it, which is what the file match is made on. Two consequences:

- **A file declared from a local path records that path**, not a URL, exactly as with hauler. A definition declaring `source: https://get.rke2.io/install.sh` will not match a package that collected `./install.sh`.
- **A file zarf stored with an `extractPath` is skipped.** Zarf stores the already-extracted result, while a distro definition's own `extractPath` expects the archive, so taking those bytes would extract twice. The file falls back to its declared source, which is the one place it is still an archive.

Charts, manifests, git repositories and data injections in a zarf package are not read - see [What a Store Carries That Cargoship Ignores](#what-a-store-carries-that-cargoship-ignores).

### A Zarf Package Covers One Architecture

A zarf package carries a single `metadata.architecture`, so one package holds one architecture's images and files. A hauler store has no equivalent limit - `hauler store sync` merges any number of manifests, across architectures, into one store.

`--content-store` takes one path, so **a build that targets two architectures cannot currently be satisfied from zarf packages alone**. Two things follow, and the second is the one to watch:

- Build a single-architecture package, naming the architecture so the definition and the store agree:

  ```bash
  zarf package create ./zarf-amd64.yaml --confirm
  cargoship create ./distro-defs --architecture amd64 \
    --content-store ./zarf-package-rke2-content-amd64-1.35.0-rke2r1.tar.zst
  ```

- **A store covering fewer architectures than the definition is not used for those images.** The store is consulted per platform, so one holding only amd64 cannot stand in for the registry on a build that also wants arm64. The image falls back to its registry and says so, naming what the store could serve:

  ```text
  WRN image held by the local image source for only some architectures, falling back to its registry
      image=registry.k8s.io/pause:3.10 held=amd64 missing=arm64
  ```

  On a node with no route to that registry the build then fails naming the image, which is the documented behaviour for content that is in neither place. This used to be silent: the reference was held, so the store answered for it, and the architectures it could not serve were dropped without a word - producing a package named as multi-architecture that held one architecture's layers.

The generated examples follow this: `example/rke2-cilium-vsphere` is the flavor that ships zarf definitions, because it is amd64-only and so cannot want a second package. The multi-architecture flavors ship hauler manifests instead.

## Building From the Store

On the disconnected side, point `create` at the store. An archive or a directory works for either format:

```bash
# A hauler archive written by `hauler store save`
cargoship create ./distro-defs --content-store ./haul.tar.zst

# A hauler store directory
cargoship create ./distro-defs --content-store ./store

# A built zarf package
cargoship create ./distro-defs --content-store ./zarf-package-rke2-content-amd64.tar.zst
```

An archive is unpacked into a temporary directory for the build and removed afterwards. A directory is only ever read - cargoship does not write to it, add the `oci-layout` marker a live hauler store lacks, or otherwise modify a store you may still be adding to.

The setting belongs in the config file when a management node always builds from the same store:

```yaml
distro:
  create:
    content_store: ~/hauls/haul.tar.zst
```

A leading `~` is expanded, which it is not in every tool - see [configuration](../schema/config.md) for the rest of the section.

The log says which format was read and where each artifact's bytes came from:

```text
INF resolving package content from a local content store store=zarf package /opt/packages/zarf-package-rke2-content-amd64.tar.zst
INF file read from content store declared=https://get.rke2.io/install.sh name=install.sh store=/opt/packages/zarf-package-rke2-content-amd64.tar.zst
INF saving image name=registry.k8s.io/pause:3.10 architecture=amd64 source=zarf package /opt/packages/zarf-package-rke2-content-amd64.tar.zst
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

A content store holds bytes that whoever wrote the distro definition never saw. The `shasum` the definition declares for a file is the only thing that proves those bytes are the ones intended, so a file with no declared shasum is **refused** rather than staged unverified:

```text
ERR failed to create distro package: unable to stage file 0 (https://get.rke2.io/install.sh):
    hauler store ./haul.tar.zst holds https://get.rke2.io/install.sh, which declares no shasum:
    refusing to stage it unverified
```

This is the same rule `--file-override` applies, and for the same reason. The fix is to declare the shasum in the definition, not to work around the check.

Images need no equivalent: an image reference is resolved through its digest, so the content is verified by construction.

The store's own integrity - a hauler archive's layout, a zarf package's `checksums.txt` and signature - is not checked. The per-artifact guarantees above are what the build relies on, and they hold whatever shape the store arrived in. Verify the store with the tool that wrote it if that matters: `zarf package inspect` for a zarf package.

## A Partial Store Still Builds

Content the store does not hold falls back to the source the definition declared, and each miss is reported:

```text
WRN file not held by the content store, falling back to its declared source declared=https://get.k3s.io/k3s
WRN image not held by the local image source, falling back to its registry image=registry.k8s.io/etcd:3.7.0-0
```

This makes a store collected for one package usable for another that overlaps with it, and lets a store be topped up rather than rebuilt.

On a management node with no route to those declared sources, the fallback cannot succeed, and **an image that is in neither the haul nor a reachable registry fails the build**:

```text
ERR failed to create distro package: image registry.k8s.io/etcd:3.7.0-0 is not in hauler store
    ./haul.tar.zst and could not be fetched from its registry: ...
```

That is deliberate. The alternative - carrying on and producing a package that is quietly missing an image - defers the failure to whoever installs it, which is the worst place to find out.

So treat the warnings as the thing to read after a build that succeeded. A build that logged a dozen misses and still finished reached the network a dozen times, which on a node that is supposed to be air-gapped is worth knowing.

## Combining a Content Store With Overrides

All three can be passed together, and they resolve in a fixed order.

For **files**, an explicit `--file-override` wins over the store. An operator who named a redirect for a specific file asked for that one, and a store that happens to also hold it does not silently take precedence.

For **images**, a `--registry-override` is applied *first*, and the store is then searched for the rewritten reference. So an override that rewrites `registry.k8s.io` to `mirror.example.com` means the store is searched for `mirror.example.com/...`, which is usually not what a store collected from upstream holds. Pass the store without registry overrides unless the store was itself collected from the mirror.

### Collecting a Haul From a Mirror

The generated `hauler-images-*.yaml` beside each example declare their images unqualified, under a document-wide `hauler.dev/registry` annotation:

```yaml
metadata:
  annotations:
    hauler.dev/registry: docker.io
spec:
  images:
    - name: rancher/rke2-runtime:v1.37.0-rke2r1
```

Pointing that one line at a mirror redirects every image in the document, so collecting a release from an internal registry needs no per-image editing. Hauler supplies the annotation's registry for unqualified names only, so an image that is already qualified under some other registry stays where it is and the annotation does not touch it.

What the annotation cannot do is preserve the upstream identity. `hauler.dev/original-ref` is hauler's record of what it actually fetched, so a haul collected through the annotation records `mirror.example.com/rancher/...` and not `docker.io/rancher/...`. That is the `unless` case the section above names: the store *was* collected from the mirror, so the build has to look its images up under the same name.

```yaml
# cargoship-config.yaml
registry_override:
  docker.io: mirror.example.com
```

The config file is the right half of this rather than `--registry-override`, because which mirror a management node reaches is a standing answer for the whole fleet rather than a decision about one invocation. Either form works, and a flag replaces the config file's mappings rather than merging with them.

Two things this does not solve:

- **The mirror has to carry the same repository paths.** The annotation replaces the registry host and nothing else, so a mirror serving `mirror.example.com/rke2-runtime` rather than `mirror.example.com/rancher/rke2-runtime` still needs an entry per image.
- **Hauler's `rewrite` is not a way around the first point.** A per-image `rewrite` moves `io.containerd.image.name`, which cargoship does not match on, and leaves `original-ref` as the fetch source; the document-level `hauler.dev/rewrite` annotation had no effect at all when tried against hauler 2.1.2, in either the bare-host or the full-reference form.

Check what a haul recorded rather than assuming, since a mismatch here shows up as a store miss per image rather than as an error:

```bash
hauler store info --store ./store --output json --type image
```

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

`--content-store` exists because that is a lot of typing to restate something the store already records.

## What a Store Carries That Cargoship Ignores

A store may hold more than a distro package can use. Only images and files are read.

Charts, git repositories and wasm artifacts in a haul are skipped, as are a zarf package's charts, manifests, git repositories and data injections: a definition's charts are declared under `.spec.config.engine.manifest`, and the images they run are already collected through the definition's own image list.

Signatures, attestations and SBOMs attached to an image in a haul are also skipped. They share the original reference of the image they describe, so they are identified by what hauler recorded them as and excluded, leaving the image itself to be found. A zarf package's `sboms.tar` and `zarf-sbom/` are not read at all.

## Troubleshooting

| Symptom                                                | Cause                                                                                                                                                                   |
| ------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `is not a content store cargoship can read`            | Neither layout was found. A zarf package has `zarf.yaml` at its root; a hauler store has `index.json`. Point at the store itself, not its parent.                       |
| `is not a hauler store`                                | The path has an `index.json` that could not be read. Point at the store directory itself, not its parent, or at the archive.                                            |
| `looks like one chunk of a split hauler archive`       | A `--chunk-size` haul arrived as `haul.tar.zst.001` and friends. Rejoin it with `hauler store load`, then pass the resulting store.                                     |
| `looks like one part of a split zarf package`          | The package arrived as `...tar.zst.part000` and friends. Rejoin the parts, then pass the resulting archive.                                                             |
| `holds no content cargoship can use`                   | Nothing records what it was collected from. For a haul, the store predates `hauler.dev/original-ref`; for a zarf package, it declares no images and no component files. |
| `declares no apiVersion the vendored zarf understands` | The zarf package was built by a newer zarf than this cargoship was built against. Build the package with a zarf matching the version in `go.mod`, or update cargoship.  |
| Everything falls back, nothing matches                 | The declared sources and the collected references differ. Compare the store's contents against the definition - a file added from a local path is the usual cause.      |
| `which declares no shasum`                             | Add the `shasum` to the file's entry in the definition.                                                                                                                 |

Listing what a store actually holds is the first thing to compare against the definition when a match is not made:

```bash
# A hauler store
hauler store info --store ./store
hauler store info --store ./store --output json --type file

# A zarf package: the component files' sources, and the image references
zarf package inspect definition ./zarf-package-rke2-content-amd64.tar.zst
zarf package inspect images ./zarf-package-rke2-content-amd64.tar.zst
```
