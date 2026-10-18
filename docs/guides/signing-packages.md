# Signing Packages

This guide explains how Cargoship signs and verifies packages. It covers key pair generation, the three points where you can sign, and verification.

A Cosign key pair is the only signing method. Keyless (Fulcio/OIDC) signing and verification, and cloud KMS key references, were removed: all of them need to reach a cloud endpoint, and the management node Cargoship runs from is air-gapped. If you need keyless back, say so on [issue #848](https://github.com/colonel-byte/cargoship/issues/848).

## What Gets Signed

A Cargoship package is a `.tar.zst` archive. It contains a `distro.yaml` manifest, a `checksums.txt` file, and the image and file layers.

Cargoship signs `distro.yaml` only. The signing step sets `build.signed: true` in the manifest. It then signs the manifest bytes with Cosign and writes the signature beside the manifest as `distro.bundle.sig`, a Sigstore bundle.

One signature covers the whole package. `distro.yaml` records `metadata.aggregateChecksum`, the checksum of `checksums.txt`. `checksums.txt` lists a checksum for every layer in the package. Signing does not modify the checksums.

Signing is optional. Unsigned packages build, publish, pull, and apply normally. Verification then has nothing to check.

## Generating a Cosign Key Pair

Cargoship does not generate keys. It uses keys in Cosign's format. Use the [cosign CLI](https://github.com/sigstore/cosign):

```
cosign generate-key-pair
```

The command prompts for a password. It writes two files into the current directory:

* `cosign.key` -- the encrypted private key. Keep this file secret. Treat it like any other signing credential.
* `cosign.pub` -- the public key. Give this file to everyone who verifies your packages.

To skip the prompt in CI, set the password in the environment first:

```
COSIGN_PASSWORD='...' cosign generate-key-pair
```

Restrict the private key file to the signing user:

```
chmod 600 cosign.key
```

### Keeping the Key Off Disk

`--signing-key` accepts a local file path, or `env://NAME` to read the PEM out of an environment variable. The environment form keeps the private key off the build host's filesystem, which is what a CI pipeline wants.

```sh
export COSIGN_PRIVATE_KEY="$(cat cosign.key)"
export COSIGN_PASSWORD=...
cargoship sign ./my-package.tar.zst --signing-key env://COSIGN_PRIVATE_KEY
```

`--key` takes the same two forms when verifying, so a public key can come from `env://COSIGN_PUBLIC_KEY` as well as from a file.

Cloud KMS references -- `awskms://`, `gcpkms://`, `azurekms://`, `hashivault://`, `openbao://` -- are **not** supported. Each resolves a key by reaching a cloud endpoint, which an air-gapped management node cannot do. Passing one fails naming the scheme rather than reporting a missing file.

## Signing a Package

You can sign a package at three points. Each point accepts the same `--signing-key` and `--signing-key-pass` flags.

### At Create Time

```
cargoship create ./distro-defs --signing-key ./cosign.key
```

Cargoship prompts for the key password when you omit `--signing-key-pass`. Use `--confirm` to skip the prompt in a non-interactive environment. The password must then come from `--signing-key-pass`, the config file, or the environment.

```
cargoship create ./distro-defs --signing-key ./cosign.key --signing-key-pass "$COSIGN_PASSWORD" --confirm
```

### At Publish Time

`publish` signs an unsigned package as it pushes to a registry. It also re-signs a signed package with a different key.

```
cargoship publish ./build/cargoship-rancher-rke2-amd64-1.0.0.tar.zst oci://ghcr.io/my-org \
  --signing-key ./cosign.key --confirm
```

Use this form for a two-stage pipeline. One job builds the package. A second job holds the key material and signs.

### As a Separate Step

`cargoship sign` signs an existing package without a rebuild. The source is a local tarball or an OCI reference.

```
cargoship sign cargoship-rancher-rke2-amd64-1.0.0.tar.zst --signing-key ./cosign.key
```

`--output` sets the destination of the signed package. Cargoship applies a default when you omit the flag. For a local source, the default is the source directory. For an OCI source, the default is the repository part of the source reference. The output can also be an OCI URL. Cargoship then signs and publishes in one step.

```
cargoship sign oci://ghcr.io/my-org/my-package:1.0.0 --signing-key ./cosign.key --output ./signed/

cargoship sign ./my-package.tar.zst --signing-key ./cosign.key --output oci://ghcr.io/my-org/signed-packages
```

### Re-signing

Cargoship refuses to sign a package that already has a signature. It reports `package is already signed, use --overwrite to re-sign`. Use `--overwrite` to replace the existing signature.

```
cargoship sign ./my-package.tar.zst --signing-key ./new-cosign.key --overwrite
```

Add `--verify=always --key ./old-cosign.pub` to check the existing signature against the old key first. Use this form for key rotation. It proves the package is the one you expect before the new signature replaces the old one.

## Verifying a Package

Every command that loads a package accepts `--verify`: `sign`, `publish`, `pull`, `apply`, `prepare`, and `engine-config-sync`. The flag takes one of three modes.

| Mode          | Behavior                                                                                                                                                                                                                               |
| ------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `never`       | Cargoship skips signature verification.                                                                                                                                                                                                |
| `if-possible` | Default. Cargoship verifies when the package has a signature and you supply verification material. It tolerates a package with nothing to verify against. Every other failure is fatal, including a tampered signature or a wrong key. |
| `always`      | Cargoship requires a successful verification. It fails when the package is unsigned. It also fails when you supply no verification material.                                                                                           |

### Key-Based Verification

Pass the public key with `--key` or `-k`.

```
cargoship pull oci://ghcr.io/my-org/my-package:1.0.0 --verify=always --key ./cosign.pub
```

### Verification on the Install Path

`apply`, `prepare`, and `engine-config-sync` verify the package before they touch any host. They accept the same flags as `pull`, and they read the same config keys.

```
cargoship apply ./cargoship-distro-amd64.tar.zst --config ./cluster.yaml --confirm \
  --verify=always --key /etc/cargoship/cosign.pub
```

Set `verify: always` and `public_key` in the config file to apply this policy to every install on the host. See [Configuration File and Environment Variables](#configuration-file-and-environment-variables).

Verification runs while Cargoship loads the package. A failure stops the command before it configures a node.

## Configuration File and Environment Variables

Set signing and verification options in `cargoship-config.yaml` to avoid repeating them on every command. The `distro.verify` and `distro.public_key` keys apply to every command that loads a package, including `apply`.

```yaml
distro:
  # verification defaults, applied to any command that loads a package
  verify: always
  public_key: /etc/cargoship/cosign.pub
  publish:
    signing_key: /etc/cargoship/cosign.key
    signing_key_password: hunter2
```

Every key also reads from the environment. Use the `DISTRO_` prefix and the full config path, with each `.` replaced by `_`.

```
export DISTRO_DISTRO_PUBLISH_SIGNING_KEY=./cosign.key
export DISTRO_DISTRO_PUBLISH_SIGNING_KEY_PASSWORD="$COSIGN_PASSWORD"
export DISTRO_DISTRO_PUBLIC_KEY=./cosign.pub
```

Note the doubled prefix. `DISTRO_` is the environment prefix. The second `DISTRO` is the `distro` section of the config file.

Command-line flags take precedence over the config file and the environment.

`signing_key_password` in the config file puts a plaintext secret on disk. Use the environment variable instead, and `env://` for the key itself, so neither the key nor its password is written to the build host.

## Recommended CI Pattern

Use this pattern for a pipeline that builds and signs with a stored key.

```yaml
env:
  COSIGN_PASSWORD: ${{ secrets.COSIGN_PASSWORD }}
  COSIGN_PRIVATE_KEY: ${{ secrets.COSIGN_PRIVATE_KEY }}

# ...

- run: |
    cargoship create ./distro-defs -o ./build \
      --signing-key env://COSIGN_PRIVATE_KEY \
      --signing-key-pass "$COSIGN_PASSWORD" \
      --confirm

    cargoship publish ./build/cargoship-*.tar.zst oci://ghcr.io/my-org \
      --verify=always --key ./cosign.pub
```

`--verify=always` on the publish step matters. The step fails instead of pushing a package whose signature does not validate against the public key you distribute.
