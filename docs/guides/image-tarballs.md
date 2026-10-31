# Image Tarballs on a Node

This guide explains what Cargoship puts in a node's image directory, what it removes from that directory and when, and how to tell it to leave files it did not put there alone.

It matters because the directory is not Cargoship's alone to reason about. An air-gapped node's disk is finite, nobody is watching it, and an image tarball is tens or hundreds of mebibytes. A directory that only ever grows is a cluster that stops scheduling pods some months after the last person touched it.

## What ends up in the directory

A package carries its container images, and `apply` exports one tarball per image and uploads it to the path the package declares in `.spec.config.imageConfig.path` -- `/var/lib/rancher/k3s/agent/images` for the k3s examples, `/var/lib/rancher/rke2/agent/images` for the rke2 ones. The engine imports them from there on start-up, which is what makes the node able to run pods with no registry to pull from.

Each tarball is named after the image reference it holds, with the characters a filename cannot carry replaced by `_`:

| Image in the package                       | Tarball on the node                            |
| ------------------------------------------ | ---------------------------------------------- |
| `docker.io/rancher/mirrored-pause:3.10.2`  | `docker.io_rancher_mirrored-pause_3.10.2.tar`  |
| `registry.example.com:5000/acme/api:1.4.0` | `registry.example.com_5000_acme_api_1.4.0.tar` |

The tag is part of the name, so two versions of one image are two files rather than one file written twice. The suffix follows `.spec.config.imageConfig.compression`: `.tar`, `.tar.gz` or `.tar.zst`.

Every file an upload phase puts on a node is also recorded in that node's upload manifest, at `/var/lib/cargoship/manifest.txt`, one `category<tab>path` line per file. The manifest is how a later run knows what it is responsible for, and it is worth reading when a node looks wrong:

```bash
ssh node-01 sudo cat /var/lib/cargoship/manifest.txt
```

## What a later apply removes

Two different cleanups run during the upload phase, and the difference between them is whether the manifest knows about the file.

**Images the manifest names are reconciled against the package.** Every tarball the previous run recorded and this run did not upload again is deleted, and its manifest line with it. So:

- an **image added** to the package is uploaded; nothing is removed
- an **image removed** from the package has its tarball deleted from every node
- an **image retagged** -- the usual case, a version bump -- is both: the new tag is uploaded and the tag it replaces is deleted

This is scoped to images. The engine binaries, RPMs and DEBs that later phases upload are reconciled by those phases, against their own manifest categories.

**Tarballs the manifest does not name are swept.** A tarball in the image directory that no manifest line accounts for is removed too. These exist: a file left by a Cargoship old enough to predate the manifest, a run interrupted between the upload and the manifest write, a tarball written under a naming scheme that no longer resolves. Nothing in the reconcile above would ever reach them, because the manifest is the only record of what is managed.

The sweep is narrow on purpose. It considers only files ending `.tar`, `.tar.gz` or `.tar.zst` -- the three a package can produce, so a package that changes compression leaves no copy of the old file behind. Anything else in the directory is left where it is, including the image **list** files an engine reads alongside the tarballs.

The first `apply` after upgrading to a Cargoship that names tarballs with their tag therefore cleans up after the old naming by itself, on every node, with no reset and nothing to delete by hand.

## Keeping tarballs Cargoship did not upload

A fleet where something other than Cargoship also puts images in that directory -- an appliance image staged by hand, a vendor's tarball dropped in by another tool -- needs the sweep turned off for those nodes. Create a file called `cargoship-keep-images` in the image directory:

```bash
sudo touch /var/lib/rancher/k3s/agent/images/cargoship-keep-images
```

Its contents are never read; its presence is the whole signal. It belongs on the nodes that need it rather than in the config file or on the command line, because which nodes those are is a property of the fleet, not of one invocation -- set it once and every later `apply` from any management node honours it.

**It does not freeze the images Cargoship owns.** Those are still uploaded, still recorded, and still reconciled against the package exactly as above; a marker that stopped that would be a marker that silently prevents upgrades from taking effect. It changes one thing: a tarball the manifest does not name is kept instead of deleted.

## How you find out that files were kept

Every kept tarball is reported, three times over, because a marker that outlives whatever needed it looks exactly like a marker nobody meant to leave.

**While the phase runs**, once per file:

```
WRN keeping an image tarball cargoship did not upload host=10.0.0.11 \
    path=/var/lib/rancher/k3s/agent/images/vendor-appliance.tar \
    marker=/var/lib/rancher/k3s/agent/images/cargoship-keep-images
```

**At the end of the run**, repeated in the recap, because on a fleet of any size the line above is a long way up the scrollback by the time the apply finishes:

```
INF finished in duration=4m12s
WRN run finished with warnings count=3
WRN warning phase=Upload files to hosts host=10.0.0.11 \
    path=/var/lib/rancher/k3s/agent/images/vendor-appliance.tar reason=kept, unmanaged
```

**In the run receipt**, if the run was given one with `--receipt-path` or `.distro.receipt_path`, under `outcome.warnings`:

```json
{
  "outcome": {
    "succeeded": true,
    "changed": true,
    "warnings": [
      {
        "phase": "Upload files to hosts",
        "host": "10.0.0.11",
        "path": "/var/lib/rancher/k3s/agent/images/vendor-appliance.tar",
        "reason": "kept, unmanaged"
      }
    ]
  }
}
```

The run still succeeds -- nothing failed, and `succeeded` stays `true`. The warnings say what the success left behind. `redact-receipt` keeps them, with the host renumbered to its alias like everywhere else in the document, so they survive into a receipt you can attach to a bug report.

## Reading an unexpected deletion

If a tarball you wanted is gone, the question is which of the two cleanups took it, and the manifest answers it.

1. **Was it ever in the manifest?** If not, the sweep removed it, and `cargoship-keep-images` is the fix.
2. **Is it in the manifest, and not in the package?** Then the reconcile removed it, which is what should happen -- the package no longer carries that image. Check `.spec.config.imageConfig.images` and the chart manifests the package enables, since a disabled chart's images are deliberately left out of the upload.
3. **Was the node ever applied with this package?** A node that joined later has only what the apply that reached it uploaded.

A `--dry-run` apply removes nothing at all: the upload phase does not run under one, so neither cleanup happens.
