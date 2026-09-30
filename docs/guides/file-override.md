# File Override Guide

This guide explains how to redirect the files Cargoship downloads when creating a package, so a build can run against an internal mirror or against assets staged on disk ahead of time.

It is the file-download counterpart to the [registry override guide](registry-override.md), which covers the same idea for container images.

## What a File Override Does

A distro definition declares two kinds of external artifact. Container images are pulled from OCI registries. Files and OS packages are downloaded over HTTP from whatever URLs `.spec.config.files` and `.spec.config.os.files` name -- `https://rpm.rancher.io/...`, `https://github.com/...`, and so on.

`--registry-override` has always covered the first kind. A file override covers the second: it rewrites the front of a file's `source` URL at package-create time, so the bytes come from a mirror instead of the upstream host. On a build host that cannot reach the public internet, this is the difference between building a package and hand-editing every URL in the distro definition first.

This only affects where `create` downloads from. What ends up in the package is byte-for-byte what the definition declared, because the declared checksum is still enforced -- see [Integrity](#integrity) below.

## Setting an Override

Overrides are specified as `source=target` pairs, either on the command line or in the config file. `source` is a URL prefix. `target` is either a replacement URL prefix or a local directory.

### Flag

```
cargoship create ./distro-defs --file-override https://rpm.rancher.io=https://mirror.example.com/rpm-rancher
```

The flag is repeatable, so several hosts can be redirected in one `create`:

```
cargoship create ./distro-defs \
  --file-override https://rpm.rancher.io=https://mirror.example.com/rpm-rancher \
  --file-override https://github.com=https://mirror.example.com/github-releases
```

A comma-separated value works too, which is occasionally easier to pass through a wrapper script:

```
cargoship create ./distro-defs --file-override https://rpm.rancher.io=https://mirror.example.com/rpm,https://github.com=https://mirror.example.com/gh
```

### Config File

The same mapping can be set under `.distro.create.file_override` in your config file, so it doesn't need to be repeated on every invocation:

```yaml
distro:
  create:
    file_override:
      https://rpm.rancher.io: https://mirror.example.com/rpm-rancher
      https://github.com: https://mirror.example.com/github-releases
      https://pkgs.k8s.io: /srv/staged/pkgs-k8s
```

Flag values take precedence over the config file. They replace it rather than merging with it: passing any `--file-override` on the command line means the config file's mappings are not used for that run, so a command line that overrides one host has to name every host it still wants redirected. This is the same behavior as `--registry-override`.

## Building From a Local Directory

A target that is not an `http://` or `https://` URL names a directory of files already on disk, which lets a build run with no network at all and without standing up an internal HTTP server:

```
cargoship create ./distro-defs --file-override https://rpm.rancher.io=/srv/staged/rpm-rancher
```

The path under the source prefix is appended to the directory. A file declared as:

```yaml
source: https://rpm.rancher.io/public/centos/9/x86_64/k3s-selinux.rpm
```

is then read from `/srv/staged/rpm-rancher/public/centos/9/x86_64/k3s-selinux.rpm`. Mirroring the upstream layout with `wget --mirror`, or with whatever your artifact store exports, is usually the least effort way to populate the directory.

A relative target is resolved against the working directory when the flag is parsed, not against the distro definition, so it means what it looks like it means from the shell you typed it in.

## Prefix Matching

A source can be as broad as a host (`https://github.com`) or as narrow as a path prefix (`https://github.com/rancher`). When more than one override could match a file, the longest matching source wins:

```
cargoship create ./distro-defs \
  --file-override https://github.com=https://mirror.example.com/gh \
  --file-override https://github.com/rancher=https://mirror.example.com/rancher
```

Here, `https://github.com/rancher/rke2/releases/...` resolves against the more specific `https://github.com/rancher` override, while `https://github.com/k3s-io/k3s/releases/...` falls back to the broader one.

Matching is done on the parsed URL rather than on the raw string, and a path prefix has to end at a path separator. Both of those matter in practice:

- An override for `https://github.com` does **not** match `https://github.com.example.invalid/...`, which a plain string prefix would have captured.
- An override for `https://github.com/rancher` does **not** match `https://github.com/rancher-dev/...`.

The scheme, host, and port all have to match exactly. An override for `https://files.example.com` does not apply to `http://files.example.com` or to `https://files.example.com:8443`.

A file whose `source` is already a local path is never overridden. There is nothing to redirect.

## Integrity

Rewriting where a file comes from does not change what the file has to be.

Every overridden file is checked against the `shasum` its distro definition declares, exactly as it would be without the override. If a mirror serves a modified, corrupted, truncated, or simply out-of-date artifact, the build fails with a checksum mismatch naming the file, and nothing is written into the package. A file that fails for any reason -- a failed download, a failed extraction, a missing local file -- fails the whole `create` rather than being skipped.

**A file that declares no `shasum` cannot be overridden.** Cargoship refuses the build and names the file. Without a declared checksum there is nothing to compare a mirror's bytes against, and "the file came from wherever this flag pointed" is not a property worth shipping in an offline package. If you hit this, add the upstream checksum to the distro definition; that is the fix, and it improves the definition whether or not you use an override.

When a file declares an `extractPath`, the override applies to the archive that is downloaded, and the `shasum` continues to be checked against the extracted file rather than the archive -- unchanged from how extraction already worked.

## Auditing a Built Package

The overrides configured for a build are recorded in the package's `distro.yaml` under `.build.fileOverrides`, alongside `.build.registryOverrides`:

```yaml
build:
  fileOverrides:
    https://rpm.rancher.io: https://mirror.example.com/rpm-rancher
```

This records the overrides that were configured for the build, not which one resolved any particular file. Combined with the enforced checksums, it tells you how a package was built without being the thing that makes the package trustworthy -- the checksums are.
