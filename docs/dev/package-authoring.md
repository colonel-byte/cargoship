# Authoring a Distro Package

This page is about the loop you run while writing or changing a `distro.yaml`: what each step costs, what is cached between runs, and how `cargoship dev apply` shortens it. It is a development page -- for what a distro definition can contain, see the [distro schema](../schema/distro.md), and for the shipped examples see [`example/AGENTS.md`](https://github.com/colonel-byte/cargoship/blob/main/example/AGENTS.md).

## The Loop Without `dev apply`

Authoring a definition means building a package and installing from it, over and over:

```sh
cargoship create ./distro-defs -o ./build
cargoship apply ./build/cargoship-distro-amd64.tar.zst --config ./inventory.yaml --confirm
```

Three things happen in there, and only one of them is work the edit actually needs:

| Step                                | Cost on a real distro package                           | Needed after an edit?                                                |
| ----------------------------------- | ------------------------------------------------------- | -------------------------------------------------------------------- |
| Fetch the declared images and files | Large on the first run, near zero after it -- see below | Only for content the edit changed                                    |
| Compress the layout to `.tar.zst`   | Most of the wall clock on a multi-gigabyte package      | No, when the package is about to be installed from this same machine |
| Decompress it again in `apply`      | The same bytes a second time                            | No, for the same reason                                              |

The compress-and-decompress round trip is pure overhead in a loop: the bytes are written, read and discarded without ever leaving the machine.

## `cargoship dev apply`

`dev apply` does the build and the install in one process and leaves the artifact out:

```sh
cargoship dev apply ./distro-defs --config ./inventory.yaml --confirm
```

The build stays in its staging directory and the apply phases read it where it sits. Everything after the build is the same code `apply` runs -- values resolution, engine-configuration mapping, template rendering, then the phases themselves -- so what reaches the hosts is what the built package would have put there. That is deliberate: a dev loop that resolved values differently from a real apply would be worse than no dev loop.

It takes the build-side flags from `create` that change what gets built, so a loop can resolve content from a local bundle -- a hauler store or a built zarf package -- instead of the network:

```sh
cargoship dev apply ./distro-defs --config ./inventory.yaml --content-store ./haul.tar.zst --confirm
```

Like `apply`, it changes every host it is pointed at, so it needs `--confirm`. Pass `--dry-run` instead to build the definition, connect to every host and run the preflight checks for real without changing anything -- which is also the quickest way to find out whether a definition builds and resolves at all.

### What It Deliberately Does Not Do

`dev apply` is for a definition under active edit, not for shipping. A release still goes through `create`, `sign` and `apply`.

- **It writes no artifact.** There is nothing to carry across an airlock, keep, or hand to `publish`. The flags describing one -- `--output`, `--tag`, `--reproducible` -- are not registered.
- **It signs nothing**, so `--signing-key` is not registered either. A run cannot stand in for a check that a release build would pass.
- **It verifies no signature or checksum**, and `--key` and `--verify` are not registered. This is not a gate being skipped: `apply` checks a package's integrity and signature because those bytes travelled, and here they were produced by this process a moment earlier. A flag accepted and silently ignored would be worse than one that is absent.

## What the Build Caches Between Runs

A second build after an edit usually reaches the network for nothing it already has. Three separate caches do that, all under the cache root (`--cargoship-cache`, `~/.cache/cargoship` by default):

| Content                               | Cache                          | Keyed on                             |
| ------------------------------------- | ------------------------------ | ------------------------------------ |
| Container image blobs                 | `<cache>/images`               | Digest, through the ORAS layer cache |
| A file declared with a plain `source` | `<cache>/files`                | The shasum the definition declares   |
| An archive a file is extracted from   | `<cache>/files`, `url-` prefix | A digest of the resolved URL         |

The third is keyed differently from the second for a reason worth knowing. When a `files` entry sets `extractPath`, its `shasum` is the digest of the *extracted member*, not of the archive carrying it -- so the archive has no declared digest to key a cache on, and it is keyed on its URL instead. The member extracted from it is still checked against the declared shasum, so a bad cache entry cannot produce a bad package; but an archive served from this cache is trusted on its URL rather than on its content, which is weaker than what cargoship does elsewhere.

Two consequences:

- **The cache has no eviction, and these entries are whole archives.** An etcd or RKE2 release tarball is hundreds of megabytes, and every version a definition has ever named stays. `du -sh ~/.cache/cargoship/files` is worth a look occasionally; removing the directory is safe and costs one slow build.
- **A definition that declares no shasum for an extracted file is unverified** either way, cached or not. That is unchanged by the caching, and it is a good reason to declare one.

### Reclaiming It

`cargoship dev clean-cache` reports every area with its size and removes the fetchable ones. Nothing else reclaims the space: the cache has no size limit and no expiry.

```sh
# see what is there and what would go, removing nothing
cargoship dev clean-cache --dry-run

# keep anything fetched in the last fortnight, drop the rest
cargoship dev clean-cache --older-than 336h

# remove every fetchable area, including the image layout
cargoship dev clean-cache
```

An age window is the one to reach for on a machine still in use, and it has one limit worth knowing: it cannot prune the image layout. That area is an index over shared blobs, so removing some of them by age would leave the index naming blobs that are gone -- it is reported as skipped, and comes out whole when `--older-than` is not given.

The run logs and the dev fleet's base images under `microvm/` are never touched. Neither is a directory this version does not recognise, so a cache written by a newer cargoship is safe from an older one.

There is no `--no-cache` flag. To force a single refetch, remove that entry.

## Checking a Definition Without Hosts

Two commands check a definition and touch nothing:

```sh
# schema-validate the definition itself
cargoship validate ./distro-defs/distro.yaml

# build it and run every preflight check against the fleet, changing nothing
cargoship dev apply ./distro-defs --config ./inventory.yaml --dry-run
```

`validate` is the fast one and needs no network or fleet. The dry run is the one that proves the content resolves, the values validate against the schema, and the hosts are reachable and in a state the phases accept.

For a local fleet to point either at, see [MicroVM Fleets](microvm.md) and `mage dev:vmUp`.
