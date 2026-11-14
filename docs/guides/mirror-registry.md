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

**Port 22 is the only port this needs.** One SSH connection to a controller carries both halves of the job: the Kubernetes API is read through it to find the registry, and then the push travels through it. No API port, no Kubernetes port-forward, no `kubectl`, and nothing installed on the node.

Your kubeconfig is still required, but only for **identity** -- the client credentials and the CA. Its server URL is deliberately left untouched, so TLS validates against the hostname it names while the bytes travel through SSH to the node. That means no certificate needs a SAN for the address actually dialled, and a kubeconfig whose server this machine cannot route to works fine. The port to tunnel to is read from that same URL rather than assumed, so a cluster serving the API somewhere other than 6443 needs no extra flag.

For the registry, the recorded address does double duty, which is what makes this correct rather than merely workable: it is both the service the channel opens onto and the reference host the images are pushed under, so the mirror cannot be populated at an address a node will not ask for.

Any controller will do, since the API server listens on loopback on every control-plane node and a NodePort answers on all of them, so the first one in the configuration is used. A fleet whose hosts are configured with `openSSH:` rather than `ssh:` is refused: rig's OpenSSH protocol shells out to the `ssh` binary and offers no way to open a channel.

One thing to know about it: throughput is whatever a single SSH connection gives you, so mirroring many gigabytes this way is slower than a direct push. `--oci-concurrency` opens more channels, but they share the one TCP connection.

#### Tunnelling through the API server

Pass `--via-kube` and the push is carried through the Kubernetes API server's port-forward subresource instead, needing nothing but your kubeconfig:

```
cargoship mirror ./package.tar.zst --via-kube
```

`--from-zarf-state` is implied, so it need not be passed: routing into the cluster without naming a destination has one sensible answer, which is the registry that cluster's state records. `--config` cannot imply it the same way, because `--config` on its own means something else entirely -- read the proxy a cluster configuration declares -- so that pairing stays explicit.

**This is the route for a cluster with no SSH.** k3d and kind nodes are containers with no `sshd`, and Talos runs no shell at all, so the SSH tunnel above cannot reach them however the inventory is written. It is also the route when you have a kubeconfig and no host credentials, which is the ordinary situation on a development cluster.

The bare flag reaches `zarf/zarf-docker-registry:5000`, the service Zarf's init package stands the registry up as. Give it a value to name another, as `namespace/service:port`:

```
cargoship mirror ./package.tar.zst --via-kube registry/harbor-core:8080 oci://harbor.internal
```

Naming a destination is what makes an in-cluster registry that is **not** Zarf's reachable. An `oci://` argument suppresses the implied state read, so the reference and the credentials are yours -- from the Docker configuration, as everywhere else -- and only the route comes from `--via-kube`. There is no `zarf-state` secret involved, and none has to exist.

The service has to be named because Zarf's state does not record one. What the state records is the address a **node** reaches the registry on -- a NodePort, or the per-node host port the registry proxy listens on -- and neither names anything the API server can forward to. Zarf's own marker service, the one labelled `zarf.dev/connect-name=registry`, carries the namespace and the port but has no selector and therefore no endpoints, which leaves the backing service's name as the one piece that has to be stated.

As with the SSH tunnel, the reference host stays the address Zarf recorded even though nothing dials it. The tunnel decides only where the bytes go, so the mirror is still populated under the reference a node will ask for.

What it needs that the SSH route does not is a reachable API server, which is the thing the SSH route exists to avoid. On an air-gapped fleet whose firewall admits port 22 and nothing else, `--config` remains the only route. Passing both is an error rather than a fallback: they authenticate differently and reach the registry by different routes, so a push that silently took the other one is one whose failure you cannot reason about.

Throughput is not the concern it is for SSH. A port-forward here is one websocket per connection rather than a shared session, so `--oci-concurrency` buys real parallelism, and a 500MB push measured within 5% of the same push through `kubectl port-forward`.

One certificate note: Zarf's registry serves TLS for its in-cluster name, and the reference host is the loopback address Zarf recorded for a node, so hostname validation will not match. `--insecure-skip-tls-verify` is the answer, and it is as safe here as it looks unsafe -- the connection never leaves your machine's API client, and the API server's own TLS is validated normally.

#### Naming a reachable address yourself

The remaining alternative is to give the address this machine reaches the registry on and let `--from-zarf-state` supply only the credentials. `--via-kube` above does this for you; doing it by hand is still worth knowing, because it is what to reach for when the registry is somewhere neither tunnel goes. A port-forward is the usual way to get an address, and a `127.0.0.1` address works exactly as any other:

```
kubectl port-forward -n zarf svc/zarf-docker-registry 5000:5000 &
cargoship mirror ./package.tar.zst oci://127.0.0.1:5000 --from-zarf-state --plain-http
```

The loopback address is not treated specially -- it is a destination like any other, and the only thing to watch is that it is **your** loopback rather than the one Zarf recorded for a node. `--plain-http` is worth passing **only when the deployment manages no certificates**: Zarf's internal registry without mTLS serves plain HTTP, and while HTTPS is tried first and falls back, saying so skips the failed probe. Under `mtlsStrategy: zarf-managed` the registry serves HTTPS and `--plain-http` would send the push to a port that will not answer it - see [A Zarf-managed mTLS registry](#a-zarf-managed-mtls-registry).

One sharp edge, which applies to every destination and not just this one: if the package's images already name the registry you are mirroring **to**, a repository prefix on the destination is silently dropped and the images are pushed back to their own references. That is zarf's `ImageTransformHostWithoutChecksum` treating the push as already done. It does not arise for a package built against upstream registries, which is the normal case.

`--context` picks the kubeconfig context the state is read from, defaulting to the current one, and completes from your kubeconfig on tab. The file itself is the `kubeconfig` setting (`KUBECONFIG`, else the standard location). `--context` without `--from-zarf-state` is an error rather than being ignored, since nothing else here reads a cluster. Under `--config` the context still chooses which cluster's credentials and API port are used, and the tunnel supplies the route to it.

### The registry pod's memory limit counts what you push

Mirroring several gigabytes into Zarf's in-cluster registry can kill its pod, and the reason is not the registry's own memory. Zarf's init chart gives the deployment a **2Gi memory limit**, and cgroup v2 charges a pod's limit for the **page cache** of every file it writes -- so the blobs a mirror lands become memory charged to the registry, whether or not the registry is still reading them. Where the node is short of free memory the kernel OOM-kills the container rather than reclaiming, and because each restart touches the files again it comes back as a crash loop.

The signature is unmistakable once you know to look, and it is the reason to read the cgroup rather than `kubectl top`, which reports the working set and so shows a healthy figure:

```
$ kubectl -n zarf get pod -l app=docker-registry
zarf-docker-registry-...  0/1  CrashLoopBackOff  6

$ # on the node, the pod's cgroup
memory.max      2147483648   # the 2Gi limit
memory.current  2134671360   # pinned at it
anon                 24576   # 24KB -- the registry process itself
file            2127904768   # 1.98GB -- page cache from the blobs
```

Deleting the pod clears it immediately, because the cgroup goes with the pod and the replacement starts with nothing charged:

```
kubectl -n zarf delete pod -l app=docker-registry
```

To stop it recurring, give the limit room for both the process and the cache of what it writes. `zarf init` has no flag for this, so it is a change to the deployment:

```
kubectl -n zarf set resources deploy/zarf-docker-registry --limits=memory=4Gi
```

None of this is affected by how the push is routed -- `--via-kube`, `--config` and a directly dialled address all write the same bytes -- and none of it indicates anything wrong with the package or the mirror. A push interrupted this way is safe to repeat: content already landed is skipped by digest.

### A Zarf-managed mTLS registry

`zarf init` can manage the registry's certificates itself, recording `mtlsStrategy: zarf-managed` in state - and from Zarf v1.0.0 that is what a default deployment does. Two things follow, and `--from-zarf-state` handles both without a flag.

The registry serves **HTTPS**, never plain HTTP, with a server certificate signed by a private CA named `Zarf Registry CA`. Nothing in the management node's trust store covers it, so the CA is read out of the cluster - the `ca.crt` of the `zarf-registry-server-tls` secret, the same secret Zarf mounts into the registry pod - and trusted for the push. Under `--config` that read travels the same SSH channel as the state read, so a fleet with a closed API port is unaffected. `--insecure-skip-tls-verify` remains available for a deployment whose CA cannot be read, but it should not be needed.

No client certificate is presented, and that is deliberate. Zarf does issue one - the `zarf-registry-client-tls` secret, common name `zarf-registry-client` - but it belongs to the per-node registry proxy, which presents it on a node's behalf: a node reaches `127.0.0.1:5000` in plain HTTP, the proxy on that node terminates it and speaks mTLS to the registry. The registry does not require a client certificate from a direct client, so a push authenticates with the push credentials from state and nothing else. Earlier versions of cargoship refused this case outright on the premise that a client certificate was needed; that premise was wrong.

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

## Reading a Mirror Back

Nothing else in a mirror says which package filled it. Images keep their original repository paths and files are named from their source URL, so a registry holding a few hundred repositories answers "what images are here" and not "is rke2 0.1.0 loaded". A finished mirror therefore records itself:

```
cargoship ls oci://harbor.internal
```

```
 PACKAGE  VERSION  ARCH         ENGINE  MIRRORED          BY
 rke2     0.1.0    amd64,arm64  rke2    2026-11-13 09:41  0.41.1
 rke2     0.0.9    amd64        rke2    2026-10-02 14:08  0.40.2
```

The record is one small OCI artifact per package and version, at `cargoship/NAME:VERSION` under whatever the mirror's own target was. Its config blob is the row above as JSON; its single layer is the package's own `distro.yaml`, so a reader that wants the full definition has it without pulling the package back. The prefix is reserved: `cargoship publish` writes a package to `NAME:VERSION`, and a record written there would be the same reference.

Two versions of one package sit side by side, because the record's tag is the version. That is a property of a registry rather than of a cluster -- Zarf's deployed-package state is keyed on the package name alone, so it records one version per name and a redeploy overwrites it.

`ls` names its registry the same three ways `mirror` does, and `--from-zarf-state` with `--config` tunnels the read over SSH exactly as the push is tunnelled:

```
cargoship ls --from-zarf-state --config ./cluster.yaml
cargoship ls oci://harbor.internal --name rke2
cargoship ls oci://harbor.internal -o json
```

`--name` matters for more than brevity. Listing the whole registry means asking it for its catalogue, which is a wider permission than pushing -- the push credentials Zarf generates are not always allowed to enumerate -- while `--name` reads one repository and needs no more than a pull.

Two limits worth knowing:

- **A record is a claim, not a check.** Nothing reconciles it against the images and files it describes, so content deleted from the registry by hand leaves a record that still lists.
- **It is not a trust anchor.** The `distro.yaml` a record carries is a copy; the signature that makes a package's own `distro.yaml` the root of trust lives in the package bundle. A record says what was mirrored, never that it is intact. Verify a package with `cargoship verify` before mirroring it, which is where that check belongs.

A package that declares no version has nothing to tag a record with. The mirror still runs -- its content is what matters -- and says in the log that it recorded nothing.

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
