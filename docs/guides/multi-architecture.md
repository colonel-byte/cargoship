# Multi-Architecture Packages

A distro package normally targets one CPU architecture. A package can instead target several, so that one artifact installs a cluster whose control-plane and worker hosts do not all run the same architecture. This guide covers declaring the architectures, selecting files per architecture, and how such a package is built, published, pulled, and applied.

## Declaring Architectures

`metadata.architectures` lists every architecture the package targets:

```yaml
apiVersion: zarf.dev/v1alpha1
kind: ZarfDistro
metadata:
  name: rancher-k3s-multi
  version: 1.36.4-k3s1
  architectures:
    - amd64
    - arm64
```

The supported values are `amd64`, `arm64`, and `riscv64`. Listing the same architecture twice is an error, as is listing one cargoship does not support.

`metadata.architecture`, the scalar field, stays valid for a package targeting a single architecture and means exactly the same thing as a one-entry list. Set one or the other, not both: when both are set the list wins. A definition that sets neither takes the architecture of the machine running `cargoship create`.

## Selecting Files Per Architecture

Most upstream projects publish one binary per architecture, at a different URL and with a different checksum. Write one file entry per architecture, each landing on the same target, and let `selector.arch` decide which one a host receives:

```yaml
files:
  - source: https://github.com/k3s-io/k3s/releases/download/v1.36.4%2Bk3s1/k3s
    target: /usr/local/bin/k3s
    executable: true
    shasum: 835873f37245fc615f547a2fe2af9402a347875f13fa64a1f136de644955ea3f
    selector:
      package: binary
      arch:
        - amd64
  - source: https://github.com/k3s-io/k3s/releases/download/v1.36.4%2Bk3s1/k3s-arm64
    target: /usr/local/bin/k3s
    executable: true
    shasum: c920706346d5ad4e5cd3c7bf1bb09ce71ebe07fec829e513e40f1caf98aed8bb
    selector:
      package: binary
      arch:
        - arm64
```

An entry with no `arch` selector goes to every host, the same way an entry with no `profile` selector goes to every role. That is what the architecture-independent files -- unit files, scripts, `noarch` RPMs -- want.

`selector.arch` may only name architectures the definition declares. A file selecting `x86_64` in a package targeting `amd64` fails at create time rather than silently never uploading. `example/k3s-multi` and `example/rke2-multi` are complete definitions written this way, generated from the same templates as the single-architecture examples, and `example/upstream` is a hand-written one that installs Kubernetes from upstream packages for both architectures.

## Building

`cargoship create` builds every architecture the definition declares into one package. Images are pulled for each of them, so a two-architecture package with images takes roughly twice as long to build and is roughly twice the size.

The archive is named `cargoship-<name>-<arch>-<version>.tar.zst`, and a package covering more than one architecture takes `multi` in the architecture position, since no single architecture describes what it carries:

```
cargoship-rancher-k3s-multi-multi-1.36.4-k3s1.tar.zst
```

### Narrowing With `--architecture`

`--architecture` (`-a`) narrows a definition to one of the architectures it declares, which is how you build a single-architecture package from a multi-architecture definition:

```
cargoship create example/k3s-multi/v1_36/v1.36.4-k3s1 -a arm64
```

It narrows; it does not override. Asking for an architecture the definition does not declare is an error, because the file entries for that architecture were never written. For the same reason it cannot re-target a definition that names a single architecture: `-a arm64` against an `architecture: amd64` definition fails rather than producing an amd64 package labelled arm64. A definition that names no architecture at all is the one case where the flag chooses freely, since there is nothing to contradict.

Narrowing does not invalidate the entries for the architectures left behind. A definition declaring `amd64` and `arm64` narrowed to `amd64` still carries its `arm64` file entries, and those entries are still checked against the full declared list.

## Publishing

A package is one set of blobs whatever the architecture count, so it publishes as a single manifest. The image index tagged at the package version is what makes it resolvable per architecture: it lists that one manifest digest once for every architecture the package covers.

```
cargoship publish cargoship-rancher-k3s-multi-multi-1.36.4-k3s1.tar.zst oci://registry.example.com/distros
```

Ordinary platform resolution therefore works from either architecture, and every consumer lands on the same blobs. Publishing to a tag that already holds an index leaves the entries for architectures the package does not cover alone, so publishing an `amd64` package and then an `arm64` one of the same version leaves both resolvable under the one tag.

## Pulling

`cargoship pull` resolves the architecture of the machine it runs on, or the one given with `-a`, against the index:

```
cargoship pull oci://registry.example.com/distros/rancher-k3s-multi:1.36.4-k3s1 -o .
```

A multi-architecture package resolves from any architecture it covers, and the pulled archive is identical whichever one resolved it.

When nothing in the index matches and the package holds a single manifest, that manifest is pulled anyway: it is the only thing a matching platform could have resolved to, and an `arm64` package is worth having on an `amd64` workstation to inspect, sign, or push elsewhere. Pulling a package is not running it. An index holding several distinct manifests and none for the requested architecture is still an error, since choosing between those is what platform matching is for.

## Applying

Each host in the inventory receives the files its own architecture selects, so a mixed-architecture cluster installs from the one package: an `arm64` worker gets the `arm64` binary, an `amd64` controller gets the `amd64` one, and both get the entries with no `arch` selector. Images are likewise exported per host architecture, so each host loads a tarball holding the images built for it.
