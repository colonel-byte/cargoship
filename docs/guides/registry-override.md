# Registry Override Guide

This guide explains how to redirect the images Cargoship pulls when creating a package, and how to authenticate to the resulting registry at apply time using an Ansible Vault-encrypted credential.

## What a Registry Override Does

By default, `cargoship create` pulls images from the registries referenced in your source manifests (e.g. `docker.io`, `ghcr.io`). A registry override rewrites the registry portion of an image reference at package-create time, so images are instead pulled from a mirror -- for example an internal pull-through cache or an air-gapped registry.

This only affects where `create` pulls *from*. It doesn't touch the cluster the package is later applied to; for authenticating the cluster's container engine to a registry, see [Authenticating to the Registry](#authenticating-to-the-registry) below.

## Setting an Override

Overrides are specified as `source=override` pairs, either on the command line or in the config file.

### Flag

```
cargoship create --registry-override docker.io=mirror.example.com
```

The flag is repeatable, so multiple registries can be overridden in one `create`:

```
cargoship create \
  --registry-override docker.io=mirror.example.com \
  --registry-override ghcr.io=mirror.example.com/ghcr
```

### Config File

The same mapping can be set under `.distro.create.registry_override` in your config file, so it doesn't need to be repeated on every invocation:

```yaml
distro:
  create:
    registry_override:
      docker.io: mirror.example.com
      ghcr.io: mirror.example.com/ghcr
```

Flag values take precedence over the config file when both are set.

## Prefix Matching

A source can be as broad as a registry (`docker.io`) or as narrow as a specific repository prefix (`docker.io/library`). When more than one override could match an image, the longest matching source wins:

```
cargoship create \
  --registry-override docker.io=mirror.example.com \
  --registry-override docker.io/library=mirror.example.com/library-cache
```

Here, `docker.io/library/nginx` resolves against the more specific `docker.io/library` override, while `docker.io/someorg/app` falls back to the broader `docker.io` override.

## Authenticating to the Registry

Once images are being pulled from your mirror, the cluster's container engine also needs credentials for it at apply time. This is configured separately, in the cluster inventory file's `.spec.config.registries` (see the [inventory guide](setup-inv.md)):

```yaml
spec:
  config:
    registries:
      - name: mirror.example.com
        auth:
          user: myuser
          pass: hunter2
```

Writing `pass: hunter2` in plaintext works, but puts a real credential in the inventory file. Cargoship also accepts an Ansible Vault-encrypted value in `user`, `pass`, or `token` -- any field starting with `$ANSIBLE_VAULT` is decrypted automatically when the package is applied.

### Encrypting the Credential

Use `cargoship vault-encrypt` to produce the encrypted value. It reads the plaintext from an argument, from stdin, or -- if stdin is a terminal -- prompts for it with hidden input so the secret is never echoed or left in shell history:

```
cargoship vault-encrypt --vault-password-file ./vault-pass.txt
Value to encrypt:
$ANSIBLE_VAULT;1.1;AES256
62306432326630316632646366363136303161316635343463306637643137646336363634363338
...
```

Paste the resulting block into the inventory file:

```yaml
spec:
  config:
    registries:
      - name: mirror.example.com
        auth:
          user: myuser
          pass: |
            $ANSIBLE_VAULT;1.1;AES256
            62306432326630316632646366363136303161316635343463306637643137646336363634363338
            ...
```

### Applying with the Vault Password

`cargoship apply` needs the same vault password to decrypt the credential at run time, supplied the same way as encryption -- via `--vault-password-file`, or the `CARGOSHIP_VAULT_PASSWORD` environment variable if the flag is omitted:

```
cargoship apply --vault-password-file ./vault-pass.txt cluster.tar.zst
```

If a registry has a vault-encrypted credential and neither the flag nor the environment variable resolves a password, `apply` fails with an error naming the registry rather than silently treating the ciphertext as a literal username or password.
