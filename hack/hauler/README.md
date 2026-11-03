# A smoke store for `--hauler-store`

A small [hauler](https://github.com/hauler-dev/hauler) manifest and a matching distro definition, for checking that `cargoship create --hauler-store` works without collecting a whole release. Around 19 MB, so the loop takes under a minute. See [`docs/guides/hauler-store.md`](../../docs/guides/hauler-store.md) for what the flag does and how matching works.

Every entry is a real one from [`example/rke2-multi-cni-cilium/v1_36/v1.36.5-rke2r1`](../../example/rke2-multi-cni-cilium/v1_36/v1.36.5-rke2r1) - the references, URLs, shasums and targets are copied from it rather than invented, so a build here exercises the same matching a build of that example would. The manifests that collect that example *completely* are generated beside its definition, as `hauler-{images,files}-<arch>.yaml` in each version directory of [`example/rke2-multi-cni-cilium/`](../../example/rke2-multi-cni-cilium).

| File                                               | What it is                                                                       |
| -------------------------------------------------- | -------------------------------------------------------------------------------- |
| [`rke2-cilium-smoke.yaml`](rke2-cilium-smoke.yaml) | Three images and one file. Two images from the flat list, one through the chart. |
| [`smoke-distro/`](smoke-distro)                    | A definition declaring exactly that subset, so the pair needs no network.        |

The definition is hand-written and targets amd64 alone, which is what makes a one-platform haul enough. It is not an example of that release and is not rendered by `mage generate:examples`.

## The loop

```sh
hauler store sync --filename hack/hauler/rke2-cilium-smoke.yaml \
  --store /tmp/haul/store --platform linux/amd64 --exclude-extras

cargoship create hack/hauler/smoke-distro --hauler-store /tmp/haul/store -o /tmp/out
```

Every artifact should come out of the store, which the log is the only place to confirm:

```text
INF file read from content store declared=https://rpm.rancher.io/.../rke2-selinux-0.22-1.el9.noarch.rpm
INF saving image name=docker.io/rancher/mirrored-pause:3.10.2 source=hauler store /tmp/haul/store
```

A `not held by the content store` warning means something fell through to the network, so the run proved less than it looks. To prove there is no network at all rather than reading for its absence, build in a namespace without one - this needs no privileges:

```sh
unshare -rn cargoship create hack/hauler/smoke-distro --hauler-store /tmp/haul/store -o /tmp/out
```

The archive form is what actually crosses an airlock, and works the same way:

```sh
hauler store save --filename /tmp/haul.tar.zst --store /tmp/haul/store
cargoship create hack/hauler/smoke-distro --hauler-store /tmp/haul.tar.zst -o /tmp/out
```

## Pointing it at the real example instead

The smoke store is a deliberately partial haul of that example. Building the example itself against it resolves these four artifacts and falls back to the network for everything else, logging a warning per miss - which is the fallback path, and worth seeing once:

```sh
cargoship create example/rke2-multi-cni-cilium/v1_36/v1.36.5-rke2r1 \
  --hauler-store /tmp/haul/store -o /tmp/out
```

For an offline build of that example, use the generated manifests beside its definition:

```sh
D=example/rke2-multi-cni-cilium/v1_36/v1.36.5-rke2r1
hauler store sync --store /tmp/full/store --exclude-extras \
  --filename $D/hauler-images-core.yaml \
  --filename $D/hauler-images-rke2-cilium.yaml \
  --filename $D/hauler-files-amd64.yaml \
  --filename $D/hauler-files-arm64.yaml

cargoship create $D --hauler-store /tmp/full/store -o /tmp/out
```

Those are generated from the same fields the `distro.yaml` beside them is, so they cannot drift from it - which is why they are generated rather than kept here. One manifest per file, so each is something a `hauler store sync` line can name: the core images, each chart's images, and one architecture's files.

Both architectures' file manifests are needed even for a build narrowed to one. Narrowing with `-a` narrows the images and not the files: `create` stages every file the definition declares, and the architecture selector is applied at install time.

The image manifests carry no platform, so a sync collects every platform they publish. Narrow them with `--platform linux/amd64` on the sync line rather than looking for a per-architecture manifest: hauler keys its index on the reference, so two manifests naming the same image under different platforms overwrite one another and the store keeps only whichever was synced last.
