# Why a multi-arch package gives each architecture its own image directory

A cargoship package is fat: one tarball carries every architecture it targets, and the host's architecture selects what gets uploaded at apply time. Files fall out of that arrangement for free, because `fileGrabber` keys its output directories by the index of the entry in `spec.config.files`, so an amd64 entry and an arm64 entry are two list entries and land in two directories without anyone arranging it. Images do not. They needed a deliberate layout choice, `images/<arch>` instead of the flat `images/` directory a single-architecture package has always used, and the reason is not visible from the code that writes it.

## The OCI store tags by image reference

`images.Pull` (`src/pkg/images/pull.go`) ends by copying each pulled image into an `oci.Store` rooted at the destination directory, tagged with the image reference it was pulled as. That reference is `registry/name:tag`. It says nothing about a platform.

For a single-architecture package that is exactly right, and it is what makes the upload phase simple. `src/pkg/phase/50_uploadfiles.go` opens the package's image directory as a store and calls `src.Resolve(ctx, i)` with the same reference string that appears in `spec.config.imageConfig.images`, gets one descriptor back, and exports it as a tarball for the node.

Now pull two platforms of `registry.k8s.io/pause:3.10` into one layout. Both copies want the tag `registry.k8s.io/pause:3.10`. The second one wins, the first is left in the layout as unreferenced blobs, and `Resolve` returns whichever pull happened to finish last. Every node in the cluster would then be handed that same single platform's image, and the failure is quiet: the tarball is well-formed, the upload succeeds, and the node fails later with an exec format error from a container it cannot run.

Giving each architecture its own directory gives each one its own store, its own `index.json`, and therefore its own tag namespace. `Resolve` on that store can only return the platform the directory was pulled for.

## Alternatives that were considered

**Encode the platform in the tag.** The store would hold `registry.k8s.io/pause:3.10-amd64` and `...-arm64` in one layout. This is the smallest change to the writer, but it makes the reader worse everywhere: the upload phase no longer resolves the reference the package definition actually lists, and would have to reconstruct a mangled tag, which means the mangling scheme becomes a compatibility surface between the package format and every future reader. The image reference in `distro.yaml` should be the thing that resolves.

**One layout, resolve by platform instead of by tag.** An OCI store can hold a manifest list and let the reader pick a platform from it. This is how a registry serves multi-arch images in the first place, so it is the tempting answer. It was rejected because `images.Pull` does not put a manifest list in the layout: it pulls the platform it was asked for and copies that manifest. Making one layout hold both platforms under one tag would mean building and writing an index descriptor for each image, which is real work in the writer, and then teaching `archive.Export` in the upload phase to select from it. That is the correct end state if the package format ever needs to deduplicate across architectures, but it buys nothing today and it would have to be built and debugged before a multi-arch package could be produced at all.

**One tarball per architecture.** This was the design alternative rejected at the level of the whole feature, not just images. It solves the tag collision by never having two platforms in one package, at the cost of an operator holding several tarballs for one cluster and choosing between them per node, which is precisely the problem a fat package exists to remove.

## What it costs

Blobs are not shared between architecture directories. Two architectures of the same image duplicate whatever layers they have in common, and for images built `FROM scratch` with a single static binary that is close to nothing, while for a shared base image it is not. A future change that wants deduplication has to move to the single-layout-with-index approach described above; the per-architecture directories do not prevent that, they just do not help it.

## Why a single-architecture package stays flat

`imageDirForArch` in `src/pkg/packager/assemble/assemble.go` returns the flat `images/` directory whenever the package targets fewer than two architectures. The tag collision that motivates the split cannot happen with one architecture, so the split would be pure churn: it would change the layout of every package built today, invalidate the fixed paths in `src/pkg/coci/pull.go` that select layers for an OCI pull, and force the apply side to change in the same slice that introduced the writer.

The reader side handles both. `DistroLayout.GetImageDirPathForArch` returns `images/<arch>` when that directory exists and falls back to `images/`, so a package built before multi-arch support and a single-architecture package built after it resolve the same way, and no version marker is needed to tell them apart. The directory's existence is the marker.
