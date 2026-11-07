# Why an `oci://` file source names its layer explicitly

A file a distro definition declares can come from an `oci://` reference, and the entry says which layer of that artifact it wants in a `layer` field rather than having it inferred. The field looks redundant from one artifact and essential from the next, which is the whole reason it exists: there are two shapes in the wild, they resolve differently, and nothing in a reference distinguishes them.

## The two shapes

Both of these are real, both are public, and both are what the feature was built and tested against:

| Artifact                                       | Shape                                                                                                                                 | What names the bytes                                    |
| ---------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------- |
| `oci://ghcr.io/cri-o/bundle:d816c7e`           | An index over four platform manifests, each carrying **46** layers, one per file in the release, titled `cri-o/bin/crio` and the like | The layer's own title                                   |
| `oci://ghcr.io/zarf-dev/packages/init:v0.89.0` | An index over three manifests, each carrying **38** layers that are themselves archives, titled `components/zarf-injector.tar`        | The title picks the archive, a path picks what is in it |

So `extractPath` cannot answer both. On the second it is a member of an archive; on the first there is no archive at all, and the same string would have to mean a layer title. Both are reachable with no credentials, so anything claimed here can be checked:

```bash
oras manifest fetch ghcr.io/cri-o/bundle:d816c7e-amd64 |
  jq -r '.layers[].annotations["org.opencontainers.image.title"]' | head
```

A third shape is a platform manifest carrying a **single** titled archive - the whole release as one `rke2.linux-<arch>.tar.gz` - where `layer` can be omitted because there is nothing to choose between. Rancher Carbide publishes RKE2 Government that way, and it is the artifact that prompted this work, but it sits behind a subscription and a reader cannot verify it; the two public artifacts above cover every resolution rule it exercises.

## Why not infer it

The tempting rule is to try `extractPath` as a layer title first and fall back to treating a sole layer as an archive. It works for both artifacts above, needs no new field, and is about fifteen lines.

It was rejected because the field would then mean different things depending on what the registry served, and the two meanings are not distinguishable by reading the definition. A reviewer looking at `extractPath: bin/engine` cannot tell whether that is a path inside a tarball or a layer title without fetching the artifact, and an operator debugging a failure has to work out which branch they are on before the error makes sense. A vendor who retitles their layers between releases would silently move an entry from one branch to the other.

`layer` and `extractPath` are therefore separate, each means one thing, and they compose: name a layer, and extract a member out of it, for an artifact that publishes several tarballs as titled layers.

| Entry declares | What happens                                                                            |
| -------------- | --------------------------------------------------------------------------------------- |
| neither        | The artifact's one layer is the file. More than one layer is an error naming the titles |
| `layer`        | That layer's bytes are the file                                                         |
| `extractPath`  | The artifact's one layer is an archive; the member is extracted                         |
| both           | The named layer is an archive; the member is extracted                                  |

## The platform comes from `selector.arch`, and nowhere else

An entry's `selector.arch` is what picks a child out of an index, because it is the only thing that can. A file is staged arch-blind at build time - `fileGrabber` keys its output on the entry's position in the list, and `selector.arch` is not read again until install time - so there is no per-architecture context for the fetch to inherit. An entry naming no single architecture, whose source is an index, is refused and named; see [`#1033`](https://github.com/colonel-byte/cargoship/issues/1033), which is the same conclusion reached from the content-store side.

### Three spellings of the platform OS, and why there is no `selector.os`

The child is selected by `resolveManifest` matching on the architecture and accepting any of three operating systems, rather than by handing a platform to `oras.Resolve`. That is not fussiness - it is the only way to read the artifacts that exist:

| Spelling | Who writes it                                                                                       | Example                          |
| -------- | --------------------------------------------------------------------------------------------------- | -------------------------------- |
| `linux`  | A vendor publishing real binaries, and what `pkg/images` asks for when pulling an image             | `ghcr.io/cri-o/bundle`           |
| `multi`  | zarf and cargoship, on a package index whose children are package architectures, not host platforms | `ghcr.io/zarf-dev/packages/init` |
| `""`     | An artifact recording an architecture and no OS at all                                              |                                  |

Matching on `linux` alone cannot read a zarf init package, and matching on `multi` alone cannot read a vendor bundle - and nothing in a reference says which kind of publisher produced it. `oci.PlatformForArch` is therefore the wrong helper here despite being the one every other caller in this repository uses: it hardcodes `multi`, because every other caller is resolving a zarf or cargoship package.

**An explicit `selector.os` was considered and rejected.** It would let an entry say `multi`, which reads as configuration for something that is not a choice: the OS a child declares is a property of how the artifact was published, not of what the operator wants, and an operator would have to go and read the manifest to find out which value to write. That is the opposite of the `layer` field, which exists precisely because there the entry is resolving a genuine ambiguity that cargoship cannot resolve for it.

The case that would earn the field is an artifact publishing `linux/amd64` *and* `windows/amd64` as file content under one tag. Nothing does today, and until something does it fails loudly - `resolveManifest`'s `default` branch reports that the reference holds two manifests for the architecture - rather than silently picking one. A child naming any other OS is not a candidate at all, since what cargoship stages is going to a linux node.

### Two traps in staging the bytes

- **An extraction identifies an archive by its file name, not its bytes.** `archive.Decompress` resolves a format by suffix (`findArchiver`), so the layer is staged under the name the artifact's title gives it, extension and all. A layer carrying no title and an entry declaring `extractPath` is refused for that reason, rather than failing inside the extractor with "unsupported archive extension" naming a temporary path.
- **An `extractPath` can collide with its own target.** `fileGrabber` extracts into the staged file's parent directory and then renames the member onto the staged name, so an `extractPath` whose leading segment equals the target's base name has the extraction create a directory at exactly the rename's destination. zarf publishes its component tarballs that way - `zarf-injector/files/0/zarf-injector` for a target of `/usr/local/bin/zarf-injector` - and the rename failed with `file exists`, naming a path the definition never mentions. The member is now moved out to a temporary name, the extracted tree removed, and the temporary renamed into place. This was never specific to an `oci://` source; the same local or HTTP archive had always failed the same way, and nothing in the tree had that shape until one of these artifacts did.

## No second endpoint concept

There is no `ociEndpoint`-style config key for file sources. Credentials, TLS, plain-HTTP and mirror rewriting are all configured already for the registry paths cargoship has, and `--registry-override` applies to an `oci://` file source for the same reason it applies to an image pull: an operator who redirects a registry means it for everything cargoship fetches from there. A second endpoint surface would duplicate all of it and still not help a disconnected build, which is what `--content-store` is for.

One consequence worth knowing: `--file-override` never matches an `oci://` source, because `pkg/fileoverride`'s `parseHTTPURL` accepts http and https only. A redirect for one of these entries is a registry override.

## What a redirected fetch has to prove

An overridden `oci://` source is refused unless it declares a `shasum` or pins its reference with a digest. This is [`choice-file-override-shasum.md`](choice-file-override-shasum.md)'s argument applied one layer along: an unrewritten reference is the definition author's own choice, and carries the trust already placed in the definition, but a rewritten one is a substitution chosen by whoever ran `create`, and something has to tie the bytes to what upstream published.

A digest in the reference does that by itself, which is why it is accepted in place of a `shasum` - a registry serving different bytes under `@sha256:...` fails the digest check. That is a genuine difference from `--file-override`, where a URL has no equivalent and the `shasum` is mandatory with no alternative.
