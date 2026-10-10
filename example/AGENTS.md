# Working in `example/`

Almost everything under `example/` is generated. Regenerate it with mage; do not hand-edit it. See [docs/dev/mage.md](../docs/dev/mage.md) for the full target reference.

## Generated versus hand-written

| Path                                                                            | Owner                                                                                              |
| :------------------------------------------------------------------------------ | :------------------------------------------------------------------------------------------------- |
| `example/<distro>-<flavor>/<minor>/<version>/distro.yaml`                       | `Generate.Examples` / `Generate.ExampleLine`, from `magefiles/templates/<distro>-distro.yaml.tmpl` |
| `example/<distro>-<flavor>/<minor>/<version>/values.yaml`, `values.schema.json` | the same targets, from `magefiles/templates/<distro>/*.tmpl`                                       |
| `example/shasums.json`                                                          | the same targets - a cache of the digests of the files the examples install                        |
| `example/upstream-<cni>/<minor>/<version>/distro.yaml`                          | `Generate.Examples`, from `magefiles/templates/upstream-distro.yaml.tmpl`, once per CNI flavor     |
| `example/upstream-cilium/cni/cilium-<chart>.yaml`                               | the same target, rendered with `helm template` and shared by every version of that flavor          |

To change a generated example, edit its template in `magefiles/templates/` and re-run `mage generate:examples`, which re-renders every example directory already on disk. An edit made directly to a rendered file is lost on the next run.

Every path in that table is also marked `linguist-generated` in [`.gitattributes`](../.gitattributes), so a target that starts writing a new file needs a pattern there as well. See the "Keeping `.gitattributes` in sync with the generators" section of the root [`AGENTS.md`](../AGENTS.md).

## Adding a release line

`Generate.ExampleLine` backfills a whole minor line: it lists every non-RC tag of that distro on that line and renders each one, into every flavor directory of that distro that covers the line. The leading `v` is optional.

```sh
mage generate:exampleLine k3s v1.35
mage generate:exampleLine k3s v1.36
mage generate:exampleLine rke2 v1.37
```

Once a line is on disk, `mage generate:examples` keeps it current - it renders every pinned tag plus every example directory that already exists.

## The release floor

Examples are rendered for the newest minor line and the two before it, and no further back. The floor is `exampleMinorFloor` in [`magefiles/pkg/gen/examples/tags.go`](../magefiles/pkg/gen/examples/tags.go), it is shared by every distro, and it applies to pinned tags and to directories already on disk alike - so a line that has aged out stops being re-rendered even though its examples are still committed.

Backfilling a line below the floor fails rather than rendering nothing, and raising the floor is two edits: the constant, and a `git rm` of the lines that fall below it. A render never deletes a line itself. See [`docs/agent/choice-example-release-floor.md`](../docs/agent/choice-example-release-floor.md) for why the window is N-2 and why removal is by hand.

A flavor that names minor lines is rendered only for the lines it names, so asking for a line it does not cover renders the other flavors alone. That covers the multi-architecture flavors - `example/rke2-multi-cni-canal`, `example/rke2-multi-cni-cilium`, `example/k3s-multi` - which are gated to the lines in `exampleMultiMinors` in [`magefiles/pkg/gen/examples/distros.go`](../magefiles/pkg/gen/examples/distros.go). Add a new line there before rendering it, or those flavors stay empty for it:

```go
exampleMultiMinors = []string{"v1_35", "v1_36"}
```

The list is shared by both distros, so adding a line covers the rke2 and the k3s multi-architecture flavors together. Each line added costs the shasum cache another set of arm64 artifacts, which is why the list holds the current lines rather than every line the distros render.

## The upstream CNI flavors

Upstream ships no CNI, and a cluster without one comes up with every pod stuck at `ContainerCreating`. So each upstream example carries one: `example/upstream-cilium`, `example/upstream-canal` and `example/upstream-flannel`, declared in `upstreamCNIFlavors` in [`magefiles/pkg/gen/examples/upstream_cni.go`](../magefiles/pkg/gen/examples/upstream_cni.go). The manifest goes to the controllers as a `files` entry and is named in `spec.config.manifests`, which cargoship kubectl-applies from the leader once `kubeadm init` has formed the cluster.

Three things about a flavor follow from the CNI rather than from the release, and are pinned beside it:

- **The manifest.** flannel and canal publish one, so the flavor pins its URL and cargoship verifies the download against `example/shasums.json`. Cilium publishes only a Helm chart, so its manifest is rendered with `helm template` into `example/upstream-cilium/cni/cilium-<chart>.yaml` and shared by every version of the flavor, referenced as `../../cni/…`.
- **The pod CIDR.** flannel and canal hardcode `10.244.0.0/16` in the manifest they publish, so the flavor writes the same value to `spec.config.engine.config.networking.podSubnet`. Disagreeing is not an error either side reports: the cluster forms and pod traffic silently does not route.
- **The images.** Read out of the manifest the flavor applies, not listed by hand, so a CNI that adds a sidecar does not leave a package missing an image on an air-gapped host.

`helm` is needed only to move the pinned chart version: the render is skipped while the file exists, so `mage generate:examples` on a committed tree never calls it, and neither does `refresh-examples.yaml`. A chart that generates a keypair while templating is refused rather than committed - cilium's default `hubble.tls.auto.method` does exactly that, which is why the render sets it to `cronJob` and lets cilium's own certgen job mint the certificates in the cluster.

The CNI is also what sets the release floor above: a CNI release supports a handful of Kubernetes minors and says which.

## Order of operations

Rendering a values schema needs that distro/minor's packaged-component vocabulary, which `Generate.EngineConfig` extracts from engine source under `thirdparty-src/`. A minor line whose source was never pulled fails to render rather than writing an empty enumeration. After any pin change, run:

```sh
mage generate:updatePins        # or: mage generate:latestTag <distro> <vMAJOR.MINOR>
mage generate:engineConfig
mage generate:examples
```

Adding an rke2 minor line means adding the matching k3s minor line too, because rke2's config is composed against k3s's flags at the same version.

## Network and caching

Both example targets touch the network. Each release's text assets are cached under `<zarf_cache>/examples/`, and file digests are cached in `example/shasums.json`, so re-rendering a version already on disk fetches nothing. Set `CARGOSHIP_EXAMPLES_NO_CACHE=1` to refetch a run's assets - needed when Rancher re-cuts a release's assets in place, since the URL does not change when it does.

Commit `example/shasums.json` with the examples that produced it. Delete an entry to force that file to be hashed again.

## Retired builds

Rancher removes an `rke2rN`'s RPMs once the next revision supersedes it, while the git tag and image manifests stay up. The targets probe each build before rendering it, skip the ones that can no longer be installed, and delete any example already on disk for one, along with its minor line directory if that empties it. A skipped build is not remembered: if upstream republishes it, the next run renders it again.

## Published example packages and version consistency

Example definitions are published to GHCR under `ghcr.io/colonel-byte/cargoship-examples/<package>`. Because example templates and generators evolve across commits and releases to expose new features, settings, and manifests for the same upstream engine version, published packages under the regular `<version>` tag may shift over time as new versions of Cargoship re-render and re-publish them.

To guarantee a consistent package image:
- Pull using the immutable, precise tag: `<version>-publish-example-<short git commit>` (e.g. `v1.36.4+k3s1-publish-example-35c2a83`), which pins the exact commit the package was generated and published from.
- Or build the example package locally from source using `cargoship create <example dir>`.
