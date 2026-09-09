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
      - name: docker.io
        proxy:
          url: https://mirror.example.com:5000
        auth:
          user: myuser
          pass: hunter2
```

`name` is the registry the images were originally referenced from, and `proxy.url` is the mirror the engine pulls from instead. Cargoship writes both into `/etc/rancher/<engine>/registries.yaml`: the pair becomes a `mirrors` entry, and the credentials become a `configs` entry keyed by the mirror's host and port. `proxy.url` may carry a scheme and a path. A URL given without a scheme is completed to `https://`, and the host is taken from it for the `configs` key, which is what the engine matches on.

Cargoship writes `registries.yaml` as root with mode `0640`, so the engine's group can read the mirror list while the credentials in it stay off limits to everyone else. Cargoship does not copy `user` and `pass` into `registries.yaml` as they are given. It encodes the pair into a single credential -- base64 of `user:pass` -- and writes that as the engine's `auth` directive, so the password does not sit on every node as plain text. Base64 is an encoding, not encryption, so treat the resulting file as a secret regardless.

Use `token` on its own when the registry issues one directly. It is a different credential from a password, not another way to spell one, so Cargoship writes it as the engine's `identity_token` directive rather than as basic auth. A `token` given alongside `user` and `pass` wins:

```yaml
        auth:
          token: abc123
```

`proxy` is optional. Leave it out to authenticate a registry Cargoship pulls from directly, without redirecting anything -- a private registry that hosts your own images, say:

```yaml
      - name: nexus.example.com
        auth:
          user: myuser
          pass: hunter2
```

That writes a `configs` entry keyed by `nexus.example.com` and no `mirrors` entry. An entry that carries neither a `proxy.url`, credentials, nor `tls` configures nothing, and `apply` rejects it by name rather than writing an empty entry. So does a `proxy` block with no `url` in it, and a registry listed twice, which would otherwise mean the second entry quietly replacing the first.

The rest of the entry is checked the same way, when the inventory file is read and before `apply` connects to anything. `name` is the registry part of an image reference, so it carries no scheme and no repository path: `docker.io`, not `https://docker.io` and not `docker.io/library`. (`*` is the exception -- engines read it as every registry.) `proxy.url` has to name a host and use `http` or `https`, and any credentials in it belong in `auth` instead. A `proxy.rewrite` pattern has to compile as a regular expression and rewrite to something, since the engine compiles it on the node, where failing means a failed pull rather than a rejected document. `user` and `pass` are a pair; half of one authenticates nothing and reaches you as a 401 from the registry rather than as the missing half it is.

Several registries may share one mirror -- a single Nexus proxying `docker.io`, `ghcr.io`, and `quay.io` under different paths is the usual shape -- and cargoship writes one `configs` entry for the host they have in common. Those entries have to agree: if two registries resolve to the same host but ask for different credentials or different TLS settings, only one of them could survive into the file, so `apply` reports the conflict by name instead of picking one.

Writing `pass: hunter2` in plaintext works, but puts a real credential in the inventory file. Cargoship also accepts an Ansible Vault-encrypted value in `user`, `pass`, `token`, or `tls.ca` -- any field starting with `$ANSIBLE_VAULT` is decrypted automatically when the package is applied.

### Registry TLS

A registry serving a certificate the host does not already trust needs a `tls` block. It takes paths to files already on the host:

```yaml
spec:
  config:
    registries:
      - name: docker.io
        proxy:
          url: https://mirror.example.com:5000
        tls:
          caFile: /etc/pki/ca-trust/source/anchors/mirror-ca.pem
          certFile: /etc/ssl/certs/mirror-client.pem
          keyFile: /etc/ssl/private/mirror-client-key.pem
```

`caFile` verifies the registry's certificate; `certFile` and `keyFile` are the client certificate the registry authenticates the host with, and go together. Setting `insecureSkipVerify: true` turns certificate verification off for that registry -- it makes the connection trivially interceptable, so prefer distributing the CA and verifying against it.

Distributing that CA to every host beforehand is often the annoying part, so `ca` takes the PEM-encoded certificate inline instead:

```yaml
        tls:
          ca: |
            -----BEGIN CERTIFICATE-----
            MIIDdzCCAl+gAwIBAgIEbGVhZjANBgkqhkiG9w0BAQsFADBaMQswCQYDVQQGEwJV
            ...
            -----END CERTIFICATE-----
```

Cargoship writes it to `/etc/cargoship/tls/<host>.crt` on each host -- `<host>` being the same host the `configs` entry is keyed by, with anything outside `A-Za-z0-9._-` replaced by an underscore -- and sets `caFile` to that path for you. The certificate travels with the cluster configuration, so a rotated CA reaches the nodes the same way a changed mirror does: `apply` sees the file drift, then drains, rewrites, and restarts each node in turn.

Set `ca` or `caFile`, not both. `apply` rejects an entry that sets both, and one whose `ca` is not a PEM certificate, naming the registry. A certificate is public, so encrypting one buys nothing, but a `ca` given as an Ansible Vault value is accepted and decrypted like any other -- a document vaulted as a whole should not have to be picked apart. The certificate is checked once it is decrypted, which is the first point at which there is a certificate to look at.

Cargoship owns `/etc/cargoship/tls` outright: every file in it was written for a registry entry. Drop the `ca` from an entry, or drop the entry itself, and the next `apply` removes the certificate that was written for it along with the reference to it, rather than leaving it on every node for however long the cluster lives.

### Keeping the Nodes in Step

`apply` compares what it would write against what is already on each node, and syncs only the nodes that differ. Both halves of a file count: contents, and the mode it is written with. A `registries.yaml` that someone has since widened to `0644` is rewritten the same as one with the wrong mirror in it, since the mode is what keeps the credentials in it off limits.

Syncing a node means draining it, rewriting the files, restarting the engine, and uncordoning it once it is ready again -- one node at a time. The log line before each drain names the files that drifted and why, so a cluster-wide rewrite is legible while it happens.

That matters on upgrade. Cargoship 0.21 changed how `registries.yaml` is written: keys under `mirrors` and `configs` are quoted, an endpoint given without a scheme is completed to `https://`, a `user` and `pass` pair is encoded into a single `auth` credential, and the file is written `0640` instead of `0600`. None of that changes what the engine does, but all of it changes the file, so the first `apply` after upgrading rolls through every node in the cluster once. Plan for it the way you would plan for any rolling restart.

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

If a registry has a vault-encrypted credential and neither the flag nor the environment variable resolves a password, `apply` fails with an error naming the registry and the field -- `auth.user`, `auth.pass`, `auth.token`, or `tls.ca` -- rather than silently treating the ciphertext as a literal username or password. So does a `tls.ca` that decrypts to something that is not a certificate.
