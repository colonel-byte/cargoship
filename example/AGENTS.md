# Working in `example/`

Almost everything under `example/` is generated. Regenerate it with mage; do not hand-edit it. See [docs/dev/mage.md](../docs/dev/mage.md) for the full target reference.

## Generated versus hand-written

| Path | Owner |
| :--- | :--- |
| `example/<distro>-<flavor>/<minor>/<version>/distro.yaml` | `Generate.Examples` / `Generate.ExampleLine`, from `magefiles/templates/<distro>-distro.yaml.tmpl` |
| `example/<distro>-<flavor>/<minor>/<version>/values.yaml`, `values.schema.json` | the same targets, from `magefiles/templates/<distro>/*.tmpl` |
| `example/shasums.json` | the same targets — a cache of the digests of the files the examples install |
| `example/upstream/distro.yaml` | hand-written |

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

A flavor that names minor lines (the multi-architecture ones) is rendered only for the lines it names, so asking for a line it does not cover renders the other flavors alone.

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
