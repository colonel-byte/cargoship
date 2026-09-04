# Signing Packages

This guide explains how Cargoship signs and verifies packages. It covers key pair generation, the three points where you can sign, keyless (Sigstore) signing, and verification.

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

### Key Providers Instead of Files

`--signing-key` accepts a local file path or a Cosign key provider URI. A provider URI keeps the private key off the build host.

| Provider | Example |
| --- | --- |
| Environment variable | `env://COSIGN_PRIVATE_KEY` |
| AWS KMS | `awskms:///alias/my-signing-key`, or `awskms://[ENDPOINT]/[ID/ALIAS/ARN]` |
| GCP KMS | `gcpkms://projects/[PROJECT]/locations/[LOCATION]/keyRings/[RING]/cryptoKeys/[KEY]` |
| Azure Key Vault | `azurekms://[VAULT_NAME][VAULT_URL]/[KEY_NAME]/[VERSION]` (version optional) |
| HashiCorp Vault / OpenBao | `hashivault://[KEY]`, `openbao://[KEY]` |

The Cargoship release pipeline uses the environment variable form. GitHub Actions stores the private key as a secret. GoReleaser receives the key as `--key=env://COSIGN_PRIVATE_KEY`, together with `COSIGN_PASSWORD`.

For a KMS provider, create the key with an algorithm that Cosign supports. ECDSA P-256 with SHA-256 is the usual choice. The signing identity needs sign permission and get-public-key permission on that key.

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

## Keyless Signing

`cargoship sign --keyless` signs without a private key. Cargoship exchanges an OIDC identity token with Fulcio for a signing certificate. The certificate is valid for about 10 minutes. Cargoship signs the package with the ephemeral key and records the signing identity in the bundle.

```
cargoship sign ./my-package.tar.zst --keyless
```

`--keyless` and `--signing-key` are mutually exclusive.

The flow opens a browser by default. In CI, supply a token instead. `--identity-token` accepts the token itself or a path to a file that contains one.

```
cargoship sign ./my-package.tar.zst --keyless --identity-token "$ACTIONS_ID_TOKEN" --confirm
```

`--fulcio-auth-flow` selects the flow: `normal` (browser), `device` (device code, for a headless terminal), `token`, or `client_credentials`.

### Keyless Signatures Need a Timestamp Anchor

The Fulcio certificate expires in about 10 minutes. After expiry, nobody can verify the signature without proof that you made it while the certificate was valid. Two mechanisms supply that proof:

* **Rekor transparency log.** `--tlog-upload` uploads the signature to the public Rekor log and produces an inclusion proof. Cargoship enables this flag for `--keyless` unless you pass `--tlog-upload=false`. The upload is public and permanent, so Cargoship prompts first. `--confirm` skips the prompt.
* **RFC3161 timestamp authority.** `--tsa-server-url` embeds a signed timestamp in the bundle. This mechanism publishes nothing to a public log.

```
cargoship sign ./my-package.tar.zst --keyless \
  --tsa-server-url https://timestamp.sigstore.dev/api/v1/timestamp \
  --tlog-upload=false --confirm
```

Cargoship warns you when you use neither mechanism. The signature then stops being verifiable when the certificate expires.

### Private Sigstore Deployments

`--fulcio-url`, `--rekor-url`, `--oidc-issuer`, and `--oidc-client-id` direct the keyless flow to a self-hosted Sigstore stack instead of the public instances. Everyone who verifies those signatures needs the matching `--trusted-root`. See [Transparency Log and Trusted Root](#transparency-log-and-trusted-root).

## Verifying a Package

Every command that loads a package accepts `--verify`: `sign`, `publish`, `pull`, `apply`, `prepare`, and `engine-config-sync`. The flag takes one of three modes.

| Mode | Behavior |
| --- | --- |
| `never` | Cargoship skips signature verification. |
| `if-possible` | Default. Cargoship verifies when the package has a signature and you supply verification material. It tolerates a package with nothing to verify against. Every other failure is fatal, including a tampered signature or a wrong key. |
| `always` | Cargoship requires a successful verification. It fails when the package is unsigned. It also fails when you supply no verification material. |

### Key-Based Verification

Pass the public key with `--key` or `-k`.

```
cargoship pull oci://ghcr.io/my-org/my-package:1.0.0 --verify=always --key ./cosign.pub
```

### Keyless Verification

Cargoship verifies a keyless signature against the identity claims in the signing certificate instead of a key. You must supply both the identity and the issuer.

```
cargoship pull oci://ghcr.io/my-org/my-package:1.0.0 --verify=always \
  --certificate-identity signer@example.com \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

For a package signed by a workflow, the identity is the workflow reference.

```
cargoship pull oci://ghcr.io/my-org/my-package:1.0.0 --verify=always \
  --certificate-identity 'https://github.com/my-org/my-repo/.github/workflows/release.yml@refs/heads/main' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

`--certificate-identity-regexp` and `--certificate-oidc-issuer-regexp` are the pattern-matching variants. Each variant is mutually exclusive with its literal counterpart. All four flags are mutually exclusive with `--key`.

Cargoship reports a method-specific error when you supply the wrong kind of material. This applies when you verify a key-signed package with identity flags, and in the opposite case. The message replaces a generic Cosign error.

### Verification on the Install Path

`apply`, `prepare`, and `engine-config-sync` verify the package before they touch any host. They accept the same flags as `pull`, and they read the same config keys.

```
cargoship apply ./cargoship-distro-amd64.tar.zst --config ./cluster.yaml --confirm \
  --verify=always --key /etc/cargoship/cosign.pub
```

Set `verify: always` and `public_key` in the config file to apply this policy to every install on the host. See [Configuration File and Environment Variables](#configuration-file-and-environment-variables).

Verification runs while Cargoship loads the package. A failure stops the command before it configures a node.

### Transparency Log and Trusted Root

`--insecure-ignore-tlog` defaults to `true`. A Rekor inclusion check needs network access, and an air-gapped install has none. Cargoship disables the flag when you set keyless identity flags. A keyless signature depends on the Rekor inclusion proof to stay verifiable after the certificate expires.

`--use-signed-timestamps` verifies RFC3161 timestamps in the bundle. Cargoship enables the flag when the bundle contains TSA timestamp data. You rarely set it manually.

`--trusted-root` gives the path to a Sigstore TrustedRoot JSON file. Cargoship uses the copy embedded in the binary when you omit the flag. The embedded copy makes offline verification of public Sigstore signatures possible. Supply your own file to verify signatures from a private Sigstore deployment.

## Configuration File and Environment Variables

Set signing and verification options in `cargoship-config.yaml` to avoid repeating them on every command. The `distro.verify` and `distro.public_key` keys apply to every command that loads a package, including `apply`.

```yaml
distro:
  # verification defaults, applied to any command that loads a package
  verify: always
  public_key: /etc/cargoship/cosign.pub
  # or, for keyless-signed packages:
  # certificate_identity: signer@example.com
  # certificate_oidc_issuer: https://token.actions.githubusercontent.com
  # trusted_root: /etc/cargoship/trusted-root.json
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

`signing_key_password` in the config file puts a plaintext secret on disk. Use the environment variable instead, or use a key provider such as `awskms://` or `hashivault://` that removes the local key.

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
