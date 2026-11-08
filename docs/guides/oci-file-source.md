# Declaring a File From an OCI Registry

A file a distro definition declares usually comes from an HTTP URL. It can also come from an OCI registry by reference, which is how a growing number of vendors publish air-gapped engine bundles: one tag per release, the artifact's content as titled layers, usually as an index over one manifest per platform.

```yaml
spec:
  config:
    os:
      files:
        - name: engine
          source: oci://registry.example.com/vendor/engine:v1.37.1
          extractPath: bin/engine
          target: /usr/local/bin/engine
          executable: true
          shasum: <sha256 of the extracted member>
          selector:
            package: binary
            arch:
              - amd64
```

This is for a build host that *can* reach the registry. For a genuinely disconnected build, stage the content into a local bundle and use [`--content-store`](content-store.md) instead - that is the better answer, and it stays the default story.

## Which Layer the Entry Wants

Two artifact shapes are in use, and they are resolved differently, so an entry says which it is rather than cargoship guessing. See [`docs/agent/choice-oci-file-source.md`](../agent/choice-oci-file-source.md) for why this is a field rather than an inference.

| Entry declares                    | What cargoship takes                                                                  |
| --------------------------------- | ------------------------------------------------------------------------------------- |
| neither `layer` nor `extractPath` | The artifact's single layer is the file. More than one layer fails, naming the titles |
| `layer`                           | The layer with that `org.opencontainers.image.title` annotation is the file           |
| `extractPath`                     | The artifact's single layer is an archive, and that member is extracted out of it     |
| both                              | The named layer is an archive, and that member is extracted out of it                 |

**One tarball per platform, as a single titled layer.** The whole release arrives as one archive and `extractPath` names a path inside it:

```text
registry.example.com/vendor/engine:v1.37.1   -> application/vnd.oci.image.index.v1+json
  |- linux/amd64   one layer, titled "engine.linux-amd64.tar.gz"
  |- linux/arm64   one layer, titled "engine.linux-arm64.tar.gz"
```

```yaml
- source: oci://registry.example.com/vendor/engine:v1.37.1
  extractPath: bin/engine
  target: /usr/local/bin/engine
  selector:
    arch:
      - amd64
```

**Every file in the release as its own titled layer.** There is no archive, and `layer` names the title:

```text
ghcr.io/cri-o/bundle:d816c7e   -> application/vnd.oci.image.index.v1+json
  |- linux/amd64   46 layers, titled "cri-o/bin/crio", "cri-o/bin/pinns", "cri-o/README.md", ...
  |- linux/arm64   the same 46 titles
```

```yaml
- source: oci://ghcr.io/cri-o/bundle:d816c7e
  layer: cri-o/bin/crio
  target: /usr/local/bin/crio
  executable: true
  selector:
    arch:
      - amd64
```

**A layer that is itself an archive.** `layer` and `extractPath` compose: name the layer, then the member inside it. A zarf package publishes every component as a titled tarball, so pulling the injector binary out of the init package is both fields at once:

```text
ghcr.io/zarf-dev/packages/init:v0.89.0   -> application/vnd.oci.image.index.v1+json
  |- multi/amd64      38 layers, titled "components/zarf-injector.tar", "images/index.json", ...
  |- multi/arm64      the same titles
  |- multi/skeleton   a source-only child, with no components
```

```yaml
- source: oci://ghcr.io/zarf-dev/packages/init:v0.89.0
  layer: components/zarf-injector.tar
  extractPath: zarf-injector/files/0/zarf-injector
  target: /usr/local/bin/zarf-injector
  executable: true
  shasum: 54c37ca19a101c795786510cb89cf4fe2aa7504d4c399985606a3d8f58f42119
  selector:
    package: binary
    arch:
      - amd64
```

The `shasum` is the **extracted member's**, not the layer's - the same rule any `extractPath` entry follows.

An entry naming a layer the artifact does not carry fails and lists the titles it does, so a typo costs one build rather than an afternoon:

```text
ERR failed to create distro package: unable to stage file 0 (crio):
    oci://ghcr.io/cri-o/bundle:d816c7e carries no layer titled "cri-o/bin/conmonrs-typo";
    it carries cri-o/LICENSE, cri-o/Makefile, cri-o/README.md, cri-o/bin/conmon,
    cri-o/bin/conmonrs, cri-o/bin/crictl, cri-o/bin/crio,
    cri-o/bin/crio-credential-provider and 38 more
```

## `selector.arch` Is the Platform

An artifact published as an image index holds more than one platform under one reference, and the entry's own `selector.arch` is what picks the child. It has to be the entry: a file is staged arch-blind during a build - every architecture's entries land in one tree, keyed on the entry's position in the list - so nothing else in the build knows which platform an entry meant.

Write one entry per architecture, each naming its own. An entry that names none, or names several, is refused rather than resolved to a guess:

```text
ERR failed to create distro package: unable to stage file 0 (engine):
    unable to resolve oci://registry.example.com/vendor/engine:v1.37.1: ... not found: no
    matching manifest was found in the manifest list (the file entry selects no single
    architecture, so a multi-platform artifact cannot be resolved from it: name one in its
    selector.arch)
```

An artifact published as a single manifest carries no platform at all, and an entry for it needs no `selector.arch` for this reason - nor does an index that holds exactly one manifest, since there is nothing there to be ambiguous about. An entry may of course still want `selector.arch` for the usual reason, to control which hosts receive the file.

The architecture is matched regardless of how the artifact spells its platform's operating system, because there are three spellings in circulation: a vendor publishing binaries writes `linux`, zarf and cargoship write `multi` on a package index, and some artifacts record an architecture and no OS. There is no `selector.os` to write, and nothing to configure. A child naming an OS a linux node cannot run - `windows`, `darwin` - is not a candidate, and an architecture available only under one of those is reported with its OS so the message does not read as though the architecture were there:

```text
ERR ... oci://registry.example.com/vendor/tool:v1 holds no amd64 manifest;
    it holds windows/amd64, windows/arm64
```

An index child for something that is not an architecture at all is skipped the same way - the zarf init package carries a `skeleton` child, which is why a miss lists what the reference does hold:

```text
ERR ... oci://ghcr.io/zarf-dev/packages/init:v0.89.0 holds no riscv64 manifest;
    it holds amd64, arm64, skeleton
```

## Shasums, Digests, and Redirects

The layer is verified against the digest the manifest gives it as it is read, so a truncated or substituted transfer fails whether or not the entry declares a `shasum`. A declared `shasum` is still checked, against the file that is finally staged - which for an `extractPath` entry is the extracted member, not the archive.

`shasum` is therefore **optional** for an `oci://` source, exactly as it is for an HTTP URL: the reference is the definition author's choice, so it carries the trust already placed in the definition.

That changes when the reference is redirected. `--registry-override` applies to an `oci://` file source for the same reason it applies to an image pull:

```bash
cargoship create ./distro-defs \
  --registry-override registry.example.com=registry.internal.example.net
```

The bytes now come from a registry the definition's author never named, so something has to tie them to what upstream published. A redirected entry is refused unless it declares a `shasum` **or** pins its reference with a digest:

```text
ERR failed to create distro package: unable to stage file 0 (engine):
    registry override registry.example.com=registry.internal.example.net applies to
    oci://registry.example.com/vendor/engine:v1.37.1, which declares no shasum and is not
    pinned by digest: refusing to fetch it unverified
```

Pinning by digest is the better of the two for an artifact you expect to be mirrored, because it is checked by construction:

```yaml
- source: oci://ghcr.io/cri-o/bundle@sha256:fe1f8c9bef946d9a8c819748aae2501394d723edf8b20c8db80dc6ca304b2de0
  layer: cri-o/bin/pinns
  target: /usr/local/bin/pinns
```

Either way, the redirect is recorded in the package's `.build.fileSources` beside the declared source, so the package says where each file actually came from.

Note that **`--file-override` does not apply to an `oci://` source.** It matches http and https URL prefixes only, so it will silently never fire on one of these entries. A redirect for an OCI reference is a registry override.

## Credentials and Transport

Registry credentials come from the Docker credential store, the same place every other registry cargoship talks to reads them from - `docker login` or `podman login` against the registry is enough. `--plain-http` and `--insecure-skip-tls-verify` apply to these fetches too.

Fetched layers go into the shared blob cache under `~/.cache/cargoship/images`, keyed by digest, so an artifact's 43 MiB tarball is fetched once per cache rather than once per build - including when several entries extract different members out of the same archive.

## Finding the Values to Write

Reading an artifact's shape needs no cargoship:

```bash
# the index, and the platforms it covers
oras manifest fetch ghcr.io/cri-o/bundle:d816c7e | jq '.manifests[] | {digest, platform}'

# the layer titles of one platform child
oras manifest fetch ghcr.io/cri-o/bundle:d816c7e-amd64 |
  jq -r '.layers[] | "\(.size)\t\(.annotations["org.opencontainers.image.title"])"'
```

The `shasum` an entry declares comes from `cargoship dev sha256sum`, which takes the same three selectors the entry does:

```bash
# a titled layer
cargoship dev sha256sum oci://ghcr.io/cri-o/bundle:d816c7e \
  --layer cri-o/bin/pinns --arch amd64

# a member of a layer that is itself an archive
cargoship dev sha256sum oci://ghcr.io/zarf-dev/packages/init:v0.89.0 \
  --layer components/zarf-injector.tar \
  --extract-path zarf-injector/files/0/zarf-injector --arch amd64
```

It resolves the artifact exactly as a build does - same index descent, same layer selection, same extraction - so what it prints is the digest of the bytes `create` would stage. For an `extractPath` entry that is the **extracted member's** digest, not the archive's, which is what the entry has to declare.

`--arch` is **not** defaulted to the architecture of the machine you run it on. The digest is going into an entry that names its own `selector.arch`, and an operator on an arm64 workstation filling in an amd64 entry would otherwise get a plausible wrong answer with nothing to notice. A reference that resolves to a real index says so and names the flag:

```text
ERR unable to compute the SHA256SUM hash
    oci://ghcr.io/cri-o/bundle:d816c7e is an index over amd64, arm64, ppc64le, s390x:
    no single architecture was selected: give one with --arch
```

An artifact published as a single manifest needs no `--arch` at all.

Tab completion on `--arch` reads the reference you are checksumming and offers the architectures that artifact actually holds, which is usually not the list cargoship itself builds for:

```console
$ cargoship dev sha256sum oci://ghcr.io/cri-o/bundle:d816c7e --arch <TAB>
amd64    linux/amd64
arm64    linux/arm64
ppc64le  linux/ppc64le
s390x    linux/s390x

$ cargoship dev sha256sum oci://ghcr.io/zarf-dev/packages/init:v0.89.0 --arch <TAB>
amd64     multi/amd64
arm64     multi/arm64
skeleton  multi/skeleton
```

The description is the platform as the artifact declares it, so a zarf package reading `multi/amd64` where a vendor bundle reads `linux/amd64` is visible rather than surprising. Anything that stops the reference from being read - a local path, an unreachable registry, credentials you do not have, a reference resolving to a single manifest - falls back to cargoship's own architectures, and the read is given three seconds before it does.

`--layer` completes the same way, from the titles the artifact carries:

```console
$ cargoship dev sha256sum oci://ghcr.io/cri-o/bundle:d816c7e --arch amd64 --layer cri-o/bin/<TAB>
cri-o/bin/conmon    cri-o/bin/crictl  cri-o/bin/crio-credential-provider  cri-o/bin/pinns
cri-o/bin/conmonrs  cri-o/bin/crio    cri-o/bin/crun                      cri-o/bin/runc
```

**Give `--arch` first.** A layer title is entirely a property of the artifact, so unlike `--arch` there is no generic list to fall back to - and on a reference that is an index over several architectures, there is no one manifest to read titles from until you say which. Rather than offer nothing, cargoship reads the first one and says so:

```console
$ cargoship dev sha256sum oci://ghcr.io/cri-o/bundle:d816c7e --layer <TAB>
cri-o/LICENSE   (from the amd64 manifest; pass --arch)
cri-o/Makefile  (from the amd64 manifest; pass --arch)
cri-o/README.md (from the amd64 manifest; pass --arch)
...
```

The note is said twice, through two channels, because neither reaches every shell. bash and zsh also show it once as a non-selectable hint line; fish does not, because cobra's generated fish script runs the completion with ActiveHelp switched off. The description carried on each entry is what fish renders.

Once both `--arch` and `--layer` are settled, `--extract-path` completes from the members of that layer:

```console
$ cargoship dev sha256sum oci://ghcr.io/zarf-dev/packages/init:v0.89.0 \
    --arch amd64 --layer components/zarf-registry.tar --extract-path <TAB>
zarf-registry/charts/docker-registry-1.0.3.tgz        zarf-registry/manifests/registry-connect-0.yaml
zarf-registry/manifests/kep-1755-registry-annotation-0.yaml  zarf-registry/values/docker-registry-1.0.3-0
```

This one reads the **layer itself** rather than a manifest - 43 MiB for that artifact - which is why it waits for both other flags and offers nothing until it has them. Two things make that reasonable: it is the same blob the command you are completing is about to fetch, into the same cache, so following the suggestion through costs nothing extra; and a second tab is served from that cache without touching the network. An abandoned transfer is dropped rather than cached, because the blob is verified against its digest before the cache commits it. On a link slow enough that three seconds is not enough, you get no suggestions rather than a wait.

That is a good guess rather than an answer. Every artifact this has been tried against titles its layers identically across platforms, so in practice the suggestions are right - but they need not be. The zarf init package is the counter-example: its `skeleton` child carries `components/k3s.tar` and `components/zarf-registry.tar`, which its `amd64` child does not carry at all. Nothing is decided by the suggestion either way, because the title is resolved for real against the architecture you actually request when the digest is computed, and a title that is not there fails with the full list.
