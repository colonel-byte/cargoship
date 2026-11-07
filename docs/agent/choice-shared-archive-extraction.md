# Why an archive is extracted once per build, not once per file entry

A distro definition commonly takes several files out of one archive. An engine release is the usual case: the binary, a killall script, an uninstall script and two unit files all come out of `rke2.linux-amd64.tar.gz`, and a definition targeting two architectures does that twice over - ten entries, two archives.

Each of those entries used to be staged on its own, and staging one meant walking the whole archive. The fetch was never the problem: an `oci://` source is served by the layer cache and a URL source by `utils.CachedArchive`, so the bytes arrive once either way. What was repeated was the decompression, and on a tarball of this size that is roughly 0.6s of CPU per entry.

Measured on a ten-entry definition - the five members above out of `rke2.linux-amd64.tar.gz` and `rke2.linux-arm64.tar.gz`, from the public `rancher/rke2` v1.37.1+rke2r1 release - with both archives already in the shared cache so the figure isolates the extraction:

|                            | wall  | CPU   |
| -------------------------- | ----- | ----- |
| one extraction per entry   | 6.11s | 6.23s |
| one extraction per archive | 2.80s | 2.89s |

The staged output is byte for byte identical.

## The extractor already supported it

`archive.DecompressOpts.Files` is a set, and `filterHandler` pulls every match out of a single walk. The capability was always there; what did not use it was the call structure, because `fileGrabber` stages one entry at a time and an entry names one member.

So the change is not to the extraction. It is to decide what each archive will be asked for *before* staging anything: `newArchiveExtractor` walks both file lists, groups the entries that share an archive, and records every member wanted from it. The first entry to need a member then triggers one fetch and one decompression covering all of them, and every later entry reads what that produced.

## What counts as the same archive

`archiveKey` is `{source, layer, arch}`, taken from the entry as the definition wrote it rather than from the resolved source. That is deliberate and it is sound: the resolution is a function of exactly those three fields. An override matches on the declared source, and a content store is searched by source, architecture and layer - so two entries that agree on all three resolve to the same bytes, whatever redirection is in play.

The architecture is in the key because one reference legitimately holds several. A vendor-published `oci://` artifact resolves to a different platform child per architecture, so treating the two as one archive would stage an arm64 binary for an amd64 entry. The layer is in the key for the same reason one tag can carry several.

## What this removed

The staged member is now **copied** out of the extraction directory into the entry's target path, where it used to be extracted beside the target and renamed into place. That rename had a defect of its own - an `extractPath` whose leading segment matched the target's base name created a directory at exactly the rename's destination, which is how a zarf component tarball failed with `file exists` naming a path the definition never mentions. Copying cannot collide that way, because the extraction never writes anything at the target path.

The fix for that is still a separate change with its own test, and the test still guards the behaviour. This supersedes the mechanism rather than the requirement.

## What it does not do

Nothing is cached between builds. A rebuild still fetches nothing - the layer cache and the archive cache both survive - but it decompresses again, because the extraction directory lives for the duration of one build and is removed with it. Caching the extracted member across builds is a separate change, and for an `oci://` source it has a key the URL path never had: the layer descriptor's digest is a verified digest of the archive itself, which is the thing [`todo-extractpath-archive-cache.md`](todo-extractpath-archive-cache.md) says a URL-sourced archive does not carry.
