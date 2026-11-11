# Mirroring to a Registry Guide

This guide explains how to publish the contents of a built package into an OCI registry with `cargoship mirror`, what reference each item lands under, and when this is the right tool rather than [`--registry-override`](registry-override.md) or [`--content-store`](content-store.md).

## What Mirroring Does

`cargoship publish` uploads a package as a single OCI artifact, to be pulled back as a package. `cargoship mirror` does the opposite: it unpacks what the package carries into a registry as individually pullable content.

```
cargoship mirror ./cargoship-rke2-amd64-0.1.0.tar.zst oci://harbor.internal
```

That pushes two kinds of thing:

- **every image the package carries**, under the same repository path and tag it had upstream, with only the registry host swapped
- **every file the package stages**, as a single-layer OCI artifact

Nothing is fetched from the network. The package already holds all of it, which is the point: a package that was built, signed, and carried across the gap is the content the fleet was tested against, and mirroring from it puts exactly those bytes in the registry.

## Why the Host Swap Is the Whole Design

An image keeps its path and tag:

| In the package                            | In the mirror                                     |
| ----------------------------------------- | ------------------------------------------------- |
| `registry.k8s.io/pause:3.10`              | `harbor.internal/pause:3.10`                      |
| `ghcr.io/colonel-byte/cargoship/agent:v1` | `harbor.internal/colonel-byte/cargoship/agent:v1` |

This is not a cosmetic choice. When a cluster configuration declares a registry proxy, `apply` writes that address into each host's containerd `hosts.toml` (or wharfie `registries.yaml`), and containerd then asks the mirror for **the repository path unchanged**. A reference rewritten any other way is one no node ever requests -- it would sit in the registry while every node fell back to an upstream it cannot reach.

For the same reason there is no option to append a checksum to the tag. Zarf's `mirror-resources` does that by default, writing `pause:3.10-zarf-1234567890`; that suits the way Zarf's own agent rewrites pod specs, and it would break a containerd mirror.

A multi-architecture package tags its images as an OCI image index, and the index is pushed whole, so the registry holds every architecture the package did.

## Naming the Destination

Give the registry as an `oci://` reference, optionally with a repository prefix:

```
cargoship mirror ./package.tar.zst oci://harbor.internal/cargoship
```

A prefix is what makes this work against a registry that will not accept a push at its root, which includes most hosted ones. It sits ahead of the image's own path, so `registry.k8s.io/pause:3.10` becomes `harbor.internal/cargoship/pause:3.10`. A containerd mirror endpoint may carry a path too, so the two still line up.

### Reading It from the Cluster Configuration

The fleet's answer is already written down. Pass `--config` and the destination is read from the one entry under `spec.config.registries` that declares a proxy:

```yaml
spec:
  config:
    registries:
      - name: registry.k8s.io
        proxy:
          url: https://harbor.internal/cargoship
```

```
cargoship mirror ./package.tar.zst --config ./cluster.yaml
```

Prefer this. It is the same field `apply` writes into every host, so what gets pushed and what the nodes ask for are read from one place and cannot drift. The scheme is dropped, and a path becomes the repository prefix.

The argument and `--config` are alternatives rather than layers: passing both is an error, because silently preferring one would mirror to an address you did not name. (`--config` does compose with `--from-zarf-state`, where it names the nodes rather than the destination -- see below.) A configuration declaring more than one proxy is also an error -- which mirror a package belongs in is not a question the command can answer, and guessing would populate the wrong one quietly.

### Reading It from a Zarf Deployment

Where the fleet pulls through a registry [Zarf](https://zarf.dev) stands up, `--from-zarf-state` reads that registry out of the `zarf-state` secret in the `zarf` namespace:

```
cargoship mirror ./package.tar.zst oci://node1.internal:31999 --from-zarf-state --plain-http
```

The push credentials always come from there, and that is the half worth having: Zarf generates them at `zarf init` and writes them nowhere an operator would otherwise look, so the alternative is extracting them by hand and running `docker login`.

The **address** only comes from there when the registry is external, which is the one mode whose recorded address is routable from somewhere other than a cluster node:

| `registryMode` | What the address is                        | What `--from-zarf-state` does               |
| -------------- | ------------------------------------------ | ------------------------------------------- |
| `external`     | A registry outside the cluster             | Uses it, and the credentials                |
| `nodeport`     | A node-local address behind a NodePort     | Tunnels with `--config`, else names the fix |
| `proxy`        | A per-node host port served by a DaemonSet | Tunnels with `--config`, else names the fix |

There are two ways to reach an internal registry, and the error names both.

#### Tunnelling through a node

Pass `--config` alongside `--from-zarf-state` and the push is carried over an SSH channel to one of the cluster's controllers:

```
cargoship mirror ./package.tar.zst --from-zarf-state --config ./cluster.yaml
```

Nothing else is needed: cargoship already holds SSH to every host, so a `direct-tcpip` channel reaches `127.0.0.1:31999` on a node without the API port, a Kubernetes port-forward, or any tooling on the node itself. The registry's recorded address does double duty here, which is what makes it correct rather than merely workable -- it is both the service the channel opens onto and the reference host the images are pushed under, so the mirror cannot be populated at an address a node will not ask for.

Any controller will do, since a NodePort answers on every node and a proxy-mode host port is served on each one, so the first one in the configuration is used. A fleet whose hosts are configured with `openSSH:` rather than `ssh:` is refused: rig's OpenSSH protocol shells out to the `ssh` binary and offers no way to open a channel.

Two things to know about it. Throughput is whatever one SSH connection gives you, so mirroring many gigabytes this way is slower than a direct push -- `--oci-concurrency` opens more channels but they share the one TCP connection. And the state itself is still read through your kubeconfig, not the tunnel, so this solves reaching the registry rather than reaching the cluster.

#### Naming a reachable address yourself

The alternative is to give the address this machine reaches the registry on and let `--from-zarf-state` supply only the credentials. A port-forward is the usual way to get one, and a `127.0.0.1` address works exactly as any other:

```
kubectl port-forward -n zarf svc/zarf-docker-registry 5000:5000 &
cargoship mirror ./package.tar.zst oci://127.0.0.1:5000 --from-zarf-state --plain-http
```

The loopback address is not treated specially -- it is a destination like any other, and the only thing to watch is that it is **your** loopback rather than the one Zarf recorded for a node. `--plain-http` is worth passing: Zarf's internal registry without mTLS serves plain HTTP, and while HTTPS is tried first and falls back, saying so skips the failed probe.

One sharp edge, which applies to every destination and not just this one: if the package's images already name the registry you are mirroring **to**, a repository prefix on the destination is silently dropped and the images are pushed back to their own references. That is zarf's `ImageTransformHostWithoutChecksum` treating the push as already done. It does not arise for a package built against upstream registries, which is the normal case.

`--context` picks the kubeconfig context the state is read from, defaulting to the current one, and completes from your kubeconfig on tab. The file itself is the `kubeconfig` setting (`KUBECONFIG`, else the standard location). `--context` without `--from-zarf-state` is an error rather than being ignored, since nothing else here reads a cluster.

A registry configured for Zarf-managed mTLS is refused: the push would need a client certificate, which this does not read.

Note that the images land under their original path and tag, as everywhere else in this guide -- **not** Zarf's `pause:3.10-zarf-1234567890` form. That is correct for nodes reaching the mirror through a containerd `hosts.toml`, and it means a workload whose image reference Zarf's own agent rewrites will not find these copies.

## How Files Are Named

A file's source URL is not a valid repository path, so it is turned into one:

| Declared `source`                | In the mirror                                   |
| -------------------------------- | ----------------------------------------------- |
| `https://get.rke2.io/install.sh` | `harbor.internal/files/install-sh:d4b7535d9ac2` |

The name is the file's basename, slugged, so a repository listing is legible. The tag is the first twelve hex characters of a SHA-256 of the **whole source URL**, which is what makes it correct: every rke2 and etcd release archive shares a filename across versions, so naming by basename alone would serve one version's file for another's.

The artifact itself is written in hauler's shape -- a config blob with hauler's file media type, the content as one layer titled with its filename, and the declared source recorded on the manifest under `hauler.dev/original-ref`. That means `hauler` and `oras` can both read what cargoship writes, and no new format is involved.

A file with no declared `source` is not mirrored. Inline `data:` content is the usual case, and it travels inside the package with nothing for a reader to match it against.

## Selecting What to Push

Both halves go by default. `--images` and `--files` are mutually exclusive and each narrows it to one:

```
cargoship mirror ./package.tar.zst oci://harbor.internal --images
```

Every image in the package is pushed, including those belonging to a chart that is currently disabled. A mirror's job is to hold the content, and enabling that chart later then needs no second pass -- the alternative fails on a node at pull time.

## Credentials

Credentials come from the Docker configuration file, the same as `publish`, `pull`, and `create`. `docker login harbor.internal` before mirroring to a registry that needs authentication.

The one exception is `--from-zarf-state`, which reads the push credentials Zarf generated along with the registry -- see [above](#reading-it-from-a-zarf-deployment).

The credentials a cluster configuration carries under `spec.config.registries[].auth` are **not** used for the push, even when `--config` supplied the address. Those exist for the nodes to pull with, they are commonly encrypted at rest with age or Ansible Vault, and reusing them here would mean decrypting a fleet's secrets during what is otherwise a local operation.

Use `--plain-http` for a registry serving plain HTTP, and `--insecure-skip-tls-verify` for one with a certificate the management node does not trust. Without `--plain-http`, HTTPS is tried first and plain HTTP only if the registry does not serve it.

## How This Compares

Three features move content toward an air-gapped fleet, in different directions:

| Feature               | Direction                               | When                                                               |
| --------------------- | --------------------------------------- | ------------------------------------------------------------------ |
| `mirror`              | package to registry                     | A fleet pulls through a mirror and the mirror needs filling        |
| `--content-store`     | local bundle to package, at create time | Building a package where the upstream registries are unreachable   |
| `--registry-override` | redirects a create-time pull            | Building a package against a mirror that already holds the content |

`mirror` and `--content-store` are inverses, and they compose: mirror a package into a registry on one network, then build from that registry on another. Note that `--registry-override` is applied *before* a content store is searched, so a store collected upstream is looked up under the rewritten reference -- see [content-store](content-store.md#combining-a-content-store-with-overrides). A mirror does not have that problem, because it is keyed on the original reference.

## What You Would Do Instead

Mirroring needs no special tooling to replace -- `hauler` will do it, and `mage generate:examples` writes hauler manifests beside each example definition:

```
hauler store sync -f hauler-images-core.yaml
hauler store copy registry://harbor.internal
```

What that does not give you is the package as the source of truth. A haul is collected from a definition and may drift from what was actually built; a package is what shipped. Mirroring from the package also carries the per-architecture index the build synthesized, which no hauler manifest describes.
