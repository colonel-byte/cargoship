# A smoke store for `--hauler-store`

A small [hauler](https://github.com/hauler-dev/hauler) manifest and a matching distro definition, for checking that `cargoship create --hauler-store` works without collecting a whole release. Around 19 MB, so the loop takes under a minute. See [`docs/guides/hauler-store.md`](../../docs/guides/hauler-store.md) for what the flag does and how matching works.

Every entry is a real one from [`example/rke2-multi-cni-cilium/v1_36/v1.36.5-rke2r1`](../../example/rke2-multi-cni-cilium/v1_36/v1.36.5-rke2r1) - the references, URLs, shasums and targets are copied from it rather than invented, so a build here exercises the same matching a build of that example would. The manifests that collect that example *completely* are in [`example/rke2-multi-cni-hauler/`](../../example/rke2-multi-cni-hauler).

| File                                                   | What it is                                                                       |
| ------------------------------------------------------ | -------------------------------------------------------------------------------- |
| [`rke2-cilium-smoke.yaml`](rke2-cilium-smoke.yaml)     | Three images and one file. Two images from the flat list, one through the chart. |
| [`smoke-distro/`](smoke-distro)                        | A definition declaring exactly that subset, so the pair needs no network.        |

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

For an offline build of that example, use the full manifests in [`example/rke2-multi-cni-hauler/`](../../example/rke2-multi-cni-hauler).
