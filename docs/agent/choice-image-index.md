# Why a multi-arch package stores images under one OCI index

A cargoship package is fat: one tarball carries every architecture it targets, and the host's architecture selects what gets uploaded at apply time. Files fall out of that arrangement for free, because `fileGrabber` keys its output directories by the index of the entry in `spec.config.files`, so an amd64 entry and an arm64 entry are two list entries and land in two directories without anyone arranging it. Images do not, and the reason is not visible from the code that writes them.

## The OCI store tags by image reference

`images.Pull` (`src/pkg/images/pull.go`) copies each pulled image into an `oci.Store` rooted at the package's `images` directory, and the store is keyed by the image reference it was pulled as. That reference is `registry/name:tag`. It says nothing about a platform.

Pull two platforms of `registry.k8s.io/pause:3.10` and both copies want that one tag. Whichever finishes last wins, the other is left in the layout as unreferenced blobs, and a later `Resolve` returns a platform nobody chose. The failure is quiet: the tarball is well formed, the upload succeeds, and the node fails later with an exec format error from a container it cannot run.

## What the package does instead

`orasSave` copies a manifest and returns its descriptor without tagging anything. `tagImage` then decides what the reference points at:

- One manifest, which is every package that targets a single architecture, is tagged directly. This is byte for byte what cargoship has always written, so nothing about an existing package changes.
- Several manifests are collected into an `application/vnd.oci.image.index.v1+json` blob, and the index is tagged instead. One reference resolves to every architecture the package targets.

The store is content addressed, so the architectures share every blob they have in common. Two architectures of an image built `FROM scratch` still duplicate their one binary layer, because those layers genuinely differ, but a shared base image is stored once no matter how many architectures reference it. The pull is faster for the same reason: `oras.Copy` skips a blob the store already holds.

Each entry in the index carries a platform. A descriptor resolved through a registry's own index already has one; a registry that serves a bare manifest does not, so `ensurePlatform` reads the platform out of the image config, which is where the registry would have read it from as well. The manifests are sorted by platform before the index is marshalled, so two builds from the same inputs produce the same index digest and `--reproducible` keeps working.

## Ordering the layout's index.json

Oras builds `images/index.json` by ranging over its map of tags, so the entries land in a different order on every run even when the pull fetched exactly the same images. That file ships inside the package and is covered by `checksums.txt`, so the order alone was enough to make two otherwise identical builds differ. After the last image is tagged, `sortIndexFile` rewrites the file with its entries ordered by digest, breaking ties on the reference annotation for an image tagged under more than one name.

Entry order carries no meaning to anything that reads the layout, so the sort runs on every build rather than only under `--reproducible`. The flag stays what it always was: it pins the recorded build timestamp. Note that this only orders the index. Layer order inside a manifest is fixed by whoever built the image, since the manifest digest depends on it.

## Selecting a platform when uploading

`src/pkg/phase/50_uploadfiles.go` resolves the image reference against the package's store and exports a tarball for the node. With an index in the store, `resolveImageManifest` picks the child manifest matching the platform and hands that descriptor to `archive.Export`, so the exported tarball holds exactly one platform and looks the same as one exported from a single-architecture package.

The selection is done here rather than left to containerd, even though `archive.Export` accepts a platform matcher and filters an index's children with it. The exporter skips the blobs of the platforms it filters out, but it still writes the full index blob into the tarball, which would leave the tarball referencing manifests whose blobs are not in it. Picking the child ourselves avoids handing nodes a tarball with dangling references.

The matcher is still `platforms.DefaultStrict()`, which describes the machine running cargoship rather than the host being uploaded to. That is tracked as its own piece of work: apply time selection needs a tarball per host architecture, not one shared tarball.

## Alternatives that were considered

**A directory per architecture.** `images/amd64` and `images/arm64`, each a complete OCI layout with its own tag namespace. It is the smallest change to the writer and it needs no index at all. It was rejected because blobs are not shared between two layouts, so a shared base image is stored once per architecture, and because it gives the package format two shapes: readers have to know whether to look in `images` or `images/<arch>`, and every path that names the images directory has to learn the difference.

**Encode the platform in the tag.** The store would hold `registry.k8s.io/pause:3.10-amd64` and `...-arm64` in one layout. This dedupes blobs, but the upload phase would no longer resolve the reference the package definition actually lists, and the mangling scheme would become a compatibility surface between the package format and every future reader. The image reference in `distro.yaml` should be the thing that resolves.

**One tarball per architecture.** This was the design alternative rejected at the level of the whole feature. It solves the tag collision by never having two platforms in one package, at the cost of an operator holding several tarballs for one cluster and choosing between them per node, which is precisely the problem a fat package exists to remove.

## What it means for publishing

Publishing a package to a registry needed no changes. `LayersFromImages` in `src/pkg/coci/pull.go` reads `images/index.json`, finds the entry annotated with the image reference, and already dispatches on media type: `layersFromIndexChildren` walks an index entry and recurses, and the layer list is deduplicated before it is returned. A published multi-arch package therefore shares blobs in the registry the same way it shares them on disk.
