# Why an image tarball's name carries the image's tag

`phase/50_uploadfiles.go` exports one tarball per image in the package and uploads it to the engine's image directory. The filename comes from the image reference, and for most of this repository's history it came from the reference with the tag cut off: `docker.io/rancher/mirrored-pause:3.10` was uploaded as `docker.io_rancher_mirrored-pause.tar`.

That was deliberate, and the reason was never written down, which is why this file exists. A name with no tag in it is a stable path across versions, so an upgrade overwrote the tarball it superseded instead of adding a second one beside it. Nothing accumulated in the image directory, and nothing had to clean up after a version bump.

It is no longer how the name is built. `imageTarBallName` now keeps the whole reference and replaces only the characters a filename may not carry, so the same image is uploaded as `docker.io_rancher_mirrored-pause_3.10.tar`.

## What was wrong with dropping the tag

Cutting the tag off made the name ambiguous, and two references that are not the same image could produce one path:

| In the package                                      | Filename both resolved to |
| --------------------------------------------------- | ------------------------- |
| `pause:3.9` and `pause:3.10`                        | `pause.tar`               |
| `registry:5000/foo:1.0` and `registry:5000/bar:2.0` | `registry.tar`            |

The second row is the worse one and is not about tags at all: the expression stripped everything from the **first** colon, which in a reference carrying a registry port is the port. Every image from a registry with a port in its hostname collapsed onto a single filename.

What happened then depended on how the exporter opened the file, and neither answer was acceptable. It opened for appending, so the second export concatenated a second archive onto the first and the host was sent a file that was not an image tarball. Opening it with `O_TRUNC` instead -- which it now is, for separate reasons, see the comment at the call -- would have silently discarded every colliding image but the last. A collision has to be impossible rather than handled, and the only way to get there is for the name to keep everything that distinguishes one reference from another.

## What replaces the property that was lost

The old name was doing real work: it is what kept superseded images from piling up on a host. A tagged name does not overwrite anything, so on its own it would leave every image an upgrade replaced sitting in the engine's image directory forever, on an air-gapped host whose disk nobody is watching.

**The prune is what replaces it, and it already existed.** `UploadFiles.cleanStaleUploads` reads each host's upload manifest as it was before the run touched it, scopes it to the `image` category, and deletes every entry the run did not upload again. So:

- an **image added** to the package is a new entry and prunes nothing
- an **image removed** from the package is in the prior manifest and not in this run's uploads, so its tarball is deleted and its entry dropped
- a **tag bumped** is both at once -- the new path is uploaded, the old path is pruned -- which is the case the old naming handled by overwriting

The scoping to one category is not incidental: the engine install phases record into `engine` and run after this one, so a prune comparing the whole manifest would read their files as stale and delete the binary the node is running. That is [#942](https://github.com/colonel-byte/cargoship/issues/942), and `TestCleanStaleUploadsLeavesASiblingPhasesFilesAlone` is its regression test.

The first apply after this change therefore cleans up after the old naming by itself. A host carrying `mirrored-pause.tar` from a previous install has that path in its manifest and no longer in the uploads, so it is pruned in the same pass that uploads `mirrored-pause_3.10.tar`. No reset is needed, and nothing has to be deleted by hand.

## The gap the manifest cannot see, and the sweep that closes it

The prune reaches what the manifest names, which leaves a gap: a tarball in the image directory that no manifest records. One left by a version that predates the manifest, one left by a run interrupted between the upload and the manifest write, one written under a naming scheme nothing resolves any more. Nothing in the manifest would ever remove those, and nobody is watching the disk on an air-gapped host.

`sweepUnmanagedImages` closes it, and the reasoning is about who owns the directory. The engine reads that directory and cargoship populates it: in practice everything in it arrived through the upload phase, so a tarball in it that the manifest does not name is not somebody else's file, it is one of ours that lost its record. So the sweep removes it.

It is scoped two ways, because "remove what we do not recognise" is a sentence worth being careful with:

- **only the three tarball suffixes** `.tar`, `.tar.gz` and `.tar.zst` -- every ending `ImageConfig.TarballSuffix` can produce, so a package that switches compression leaves no copy of the old file behind, and anything else in the directory is left alone. The image **list** files an engine reads alongside the tarballs are not tarballs and are not touched.
- **only what this run did not upload.** The images cargoship owns are managed by the manifest prune above, not by the sweep.

## The escape hatch, and why it warns

A fleet where something other than cargoship also puts images in that directory needs the sweep off, and `KeepImagesMarker` -- a file named `cargoship-keep-images` in the image directory -- is how such a host says so. It is a file on the host rather than a flag or a config key on purpose: the hosts that need it are a property of the fleet, not of the invocation, and an operator who sets it once should not have to remember it on every apply from every management node.

It applies only to tarballs cargoship does not own. The images in the package are still uploaded, still recorded, and still pruned when the package stops carrying them -- a marker that froze those would be a marker that silently stops upgrades from taking effect.

**And it warns, every run, for every file.** A marker left behind after whatever needed it is gone is indistinguishable from a marker nobody meant to leave, and the failure mode is a disk filling up over months with nothing in any log to explain it. So each kept tarball is logged where it happens, and then repeated in the run's recap through `warningReporter` -- which is also how it reaches the run receipt, under `outcome.warnings`. The recap exists because a line logged during the upload phase is thousands of lines above the prompt by the time a fleet-sized apply finishes.

`requireNoOrphanImages` in the cluster e2e suite asserts every walk leaves nothing in the directory the manifest has lost track of, and `Test_50c_UploadSweep` plants a tarball cargoship did not upload to check both halves against real hosts: removed without the marker, kept and warned about with it.

## Keeping this true

Five tests hold the halves together, and a change to any of them should fail one:

- `TestImageTarBallName` pins the name, including the two collision pairs above.
- `TestCleanStaleUploadsPrunesImagesThisRunDidNotUpload` pins the prune, over an added image, a removed one and a bumped tag.
- `Test_50b_UploadRoundTrip` in the cluster suite re-runs the upload phase against hosts it already staged and asserts nothing changed, locally or on the host, which is what proves a re-export is reproducible rather than merely idempotent-looking.
- `TestSweepUnmanagedImages` pins the sweep and the marker, including the files it must not touch.
- `Test_50c_UploadSweep` plants an unmanaged tarball on every host in the walk and checks it is removed, then checks the marker keeps it and that keeping it warns.
