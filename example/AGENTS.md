# Working in `example/`

Almost everything under `example/` is generated. Regenerate it with mage; do not hand-edit it. See [docs/dev/mage.md](../docs/dev/mage.md) for the full target reference.

## Generated versus hand-written

| Path                                                                            | Owner                                                                                              |
| :------------------------------------------------------------------------------ | :------------------------------------------------------------------------------------------------- |
| `example/<distro>-<flavor>/<minor>/<version>/distro.yaml`                       | `Generate.Examples` / `Generate.ExampleLine`, from `magefiles/templates/<distro>-distro.yaml.tmpl` |
| `example/<distro>-<flavor>/<minor>/<version>/values.yaml`, `values.schema.json` | the same targets, from `magefiles/templates/<distro>/*.tmpl`                                       |
| `example/shasums.json`                                                          | the same targets — a cache of the digests of the files the examples install                        |
| `example/upstream/distro.yaml`                                                  | hand-written                                                                                       |

To change a generated example, edit its template in `magefiles/templates/` and re-run `mage generate:examples`, which re-renders every example directory already on disk. An edit made directly to a rendered file is lost on the next run.

## Adding a release line

`Generate.ExampleLine` backfills a whole minor line: it lists every non-RC tag of that distro on that line and renders each one, into every flavor directory of that distro that covers the line. The leading `v` is optional.

```sh
mage generate:exampleLine k3s v1.34
mage generate:exampleLine k3s v1.35
mage generate:exampleLine k3s v1.36
mage generate:exampleLine rke2 v1.37
```

Once a line is on disk, `mage generate:examples` keeps it current — it renders every pinned tag plus every example directory that already exists.

A flavor that names minor lines is rendered only for the lines it names, so asking for a line it does not cover renders the other flavors alone. That covers the multi-architecture flavors — `example/rke2-multi-cni-canal`, `example/rke2-multi-cni-cilium`, `example/k3s-multi` — which are gated to the lines in `exampleMultiMinors` in [`magefiles/examples.go`](../magefiles/examples.go). Add a new line there before rendering it, or those flavors stay empty for it:

```go
exampleMultiMinors = []string{"v1_35", "v1_36"}
```

The list is shared by both distros, so adding a line covers the rke2 and the k3s multi-architecture flavors together. Each line added costs the shasum cache another set of arm64 artifacts, which is why the list holds the current lines rather than every line the distros render.

## Order of operations

Rendering a values schema needs that distro/minor's packaged-component vocabulary, which `Generate.EngineConfig` extracts from engine source under `thirdparty-src/`. A minor line whose source was never pulled fails to render rather than writing an empty enumeration. After any pin change, run:

```sh
mage generate:updatePins        # or: mage generate:latestTag <distro> <vMAJOR.MINOR>
mage generate:engineConfig
mage generate:examples
```

Adding an rke2 minor line means adding the matching k3s minor line too, because rke2's config is composed against k3s's flags at the same version.

## Network and caching

Both example targets touch the network. Each release's text assets are cached under `<zarf_cache>/examples/`, and file digests are cached in `example/shasums.json`, so re-rendering a version already on disk fetches nothing. Set `CARGOSHIP_EXAMPLES_NO_CACHE=1` to refetch a run's assets — needed when Rancher re-cuts a release's assets in place, since the URL does not change when it does.

Commit `example/shasums.json` with the examples that produced it. Delete an entry to force that file to be hashed again.

## Retired builds

Rancher removes an `rke2rN`'s RPMs once the next revision supersedes it, while the git tag and image manifests stay up. The targets probe each build before rendering it, skip the ones that can no longer be installed, and delete any example already on disk for one, along with its minor line directory if that empties it. A skipped build is not remembered: if upstream republishes it, the next run renders it again.

## Published example packages and version consistency

Example definitions are published to GHCR under `ghcr.io/colonel-byte/cargoship-examples/<package>`. Because example templates and generators evolve across commits and releases to expose new features, settings, and manifests for the same upstream engine version, published packages under the regular `<version>` tag may shift over time as new versions of Cargoship re-render and re-publish them.

To guarantee a consistent package image:
- Pull using the immutable, precise tag: `<version>-publish-example-<short git commit>` (e.g. `v1.36.4+k3s1-publish-example-35c2a83`), which pins the exact commit the package was generated and published from.
- Or build the example package locally from source using `cargoship create <example dir>`.
