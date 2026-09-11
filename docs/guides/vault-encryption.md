# Vault Encryption

This guide explains how Cargoship encrypts registry credentials in a cluster configuration with Ansible Vault, and how to read them back out. It covers the four fields Cargoship decrypts, the `cargoship vault` commands, and the password-rotation workflow.

## What Cargoship Decrypts

A cluster configuration holds registry credentials in plain sight. Encrypting them lets you commit the file.

Cargoship decrypts exactly four fields per registry, and only these four:

| Path | What it holds |
| --- | --- |
| `.spec.config.registries[N].auth.user` | Registry username |
| `.spec.config.registries[N].auth.pass` | Registry password |
| `.spec.config.registries[N].auth.token` | Registry token |
| `.spec.config.registries[N].tls.ca` | Inline CA certificate |

Decryption happens during an apply, when Cargoship writes the engine's registry configuration. A field that does not carry the `$ANSIBLE_VAULT` header is passed through untouched, so a configuration can mix encrypted and plaintext values freely.

A CA certificate is public and does not need encrypting. Cargoship accepts an encrypted one anyway, so that a document can be vaulted as a whole without the parts that did not have to be causing a failure.

Encrypting any other field produces a document Cargoship can load but never unwraps: the ciphertext reaches the host verbatim, as if that were the literal value. `vault encrypt-path` warns when you ask for such a path, but does not refuse — see [Encrypting a Value Already in a File](#encrypting-a-value-already-in-a-file).

## Supplying the Password

Every `vault` command, and every command that applies a configuration, resolves the password the same way and stops at the first that is set:

1. `--vault-password-file`, a path to a file holding the password.
2. The `CARGOSHIP_VAULT_PASSWORD` environment variable.
3. The `ANSIBLE_VAULT_PASSWORD` environment variable.

A trailing newline in a password file is stripped, so a file written by `echo` works as expected.

Cargoship never prompts for the vault password. If none of the three is set, the command fails rather than treating the password as empty.

Restrict the password file to the user that runs Cargoship:

```
chmod 600 vault-pass.txt
```

## The Commands

```
cargoship vault encrypt [VALUE]
cargoship vault encrypt-path FILE YAML_PATH [YAML_PATH...]
cargoship vault encrypt-file FILE
cargoship vault decrypt [VALUE]
cargoship vault decrypt-path FILE YAML_PATH [YAML_PATH...]
cargoship vault decrypt-file FILE
cargoship vault rekey FILE
```

The `encrypt` and `decrypt` pair work on a value and print to stdout. The `-path` pair work on named values inside a configuration file and rewrite the file in place. The `-file` pair work on every registry credential in a configuration at once, and are what you want most of the time. `rekey` moves a whole configuration from one vault password to another, or re-salts it under the one it has -- see [Rotating the Vault Password](#rotating-the-vault-password).

`YAML_PATH` is a path *into the document*, not a path on the filesystem: `.spec.config.registries[0].auth.pass` names a value inside `FILE`, the way `jq` or `yq` would address it. The leading `$` that go-yaml uses is optional, so `$.spec...`, `.spec...`, and `spec...` all name the same value. Quote it in your shell -- it contains `[` and `]`.

The `-path` commands take as many of them as you like after `FILE`. The file is written once, after every value has been rewritten, so a path that is missing or in the wrong state fails the run without leaving the file half-done. Naming the same value twice -- including as two different spellings of one path -- is an error rather than a second pass over it.

### Encrypting a Value

`vault encrypt` takes the value as an argument, on stdin, or at a hidden prompt when stdin is a terminal:

```
cargoship vault encrypt my-registry-password --vault-password-file ./vault-pass.txt
```

```
printf my-registry-password | cargoship vault encrypt --vault-password-file ./vault-pass.txt
```

```
cargoship vault encrypt --vault-password-file ./vault-pass.txt < ./registry-token.txt
```

Passing a secret as an argument leaves it in your shell history and in the process list. Prefer stdin or the prompt on a shared machine.

The output is a multi-line `$ANSIBLE_VAULT` block that you then paste into the configuration yourself, at the right indentation. `vault encrypt-path` does that part for you.

### Encrypting a Value Already in a File

`vault encrypt-path` encrypts the plaintext the file already holds at a YAML path, and writes it back as a block scalar:

```
cargoship vault encrypt-path ./cluster.yaml '.spec.config.registries[0].auth.pass' --vault-password-file ./vault-pass.txt
```

Name several paths to encrypt them in one pass, which is the way to vault values `encrypt-file` does not cover -- anything outside a registry's credentials, such as an `x-tra` block of your own:

```
cargoship vault encrypt-path ./cluster.yaml '.x-tra.test.user' '.x-tra.test.pass' --vault-password-file ./vault-pass.txt
```

Given this configuration:

```
    registries:
      - name: harbor # our mirror
        auth:
          user: admin
          pass: hunter2 # rotate me
```

the command rewrites only that one value:

```
    registries:
      - name: harbor # our mirror
        auth:
          user: admin
          pass: |- # rotate me
            $ANSIBLE_VAULT;1.1;AES256
            31333438393936323663373461353533373034663339316335386139363535383531643464613539
            6235646237326332663931643964633165323934646363390a353136626562643632326566353765
            3961
```

Everything else is preserved byte for byte: comments, key order, quoting elsewhere in the document, and the trailing comment on the key's own line. The file stays yours rather than becoming a re-rendering of itself.

Two flags change what it does:

* `--dry-run` prints the resulting document to stdout and leaves the file untouched.
* `--force` encrypts a value that is `$ANSIBLE_VAULT` already, wrapping it a second time. Without it, an already-encrypted value is an error, so that re-running the command cannot bury the plaintext under a layer nothing unwraps.

If a `YAML_PATH` is not one of the four fields in [What Cargoship Decrypts](#what-cargoship-decrypts), the command warns and proceeds:

```
WRN cargoship does not decrypt this field at apply time, so the value will reach the host as ciphertext
```

### Decrypting a Value

`vault decrypt` is the inverse of `vault encrypt`. It takes the ciphertext as an argument or on stdin:

```
cargoship vault decrypt --vault-password-file ./vault-pass.txt < ./encrypted-token.txt
```

The plaintext is written to stdout as it is, with a trailing newline added only when it does not already end in one. A multi-line value such as a PEM certificate can therefore be redirected straight into a file:

```
cargoship vault decrypt --vault-password-file ./vault-pass.txt < ./encrypted-ca.txt > ./ca.pem
```

Unlike `vault encrypt`, there is no hidden prompt. Ciphertext is neither secret nor a single line, so a terminal with nothing piped into it is reported as a mistake.

### Decrypting a Value Already in a File

`vault decrypt-path` puts the plaintext back into the document, in place of the ciphertext:

```
cargoship vault decrypt-path ./cluster.yaml '.spec.config.registries[0].auth.pass' --vault-password-file ./vault-pass.txt
```

It takes several paths too, and like `encrypt-path` writes the file once at the end:

```
cargoship vault decrypt-path ./cluster.yaml '.x-tra.test.user' '.x-tra.test.pass' --vault-password-file ./vault-pass.txt
```

Encrypting a value and decrypting it again gives back the file that went in, byte for byte.

The plaintext is written in whichever scalar style holds it faithfully: plain where YAML allows it, a literal block for something multi-line such as a PEM certificate, and a quoted string for anything the other two would change on the way back in -- a value with a leading space, a trailing space, a `#`, or one that would otherwise read back as a number or a boolean.

The command warns that it has just removed the protection the file relied on:

```
WRN the file now holds this value in plaintext
```

`--dry-run` prints the resulting document to stdout and leaves the file untouched, which is the way to check that a value decrypts without writing the plaintext to disk at all:

```
cargoship vault decrypt-path ./cluster.yaml '.spec.config.registries[0].auth.pass' --vault-password-file ./vault-pass.txt --dry-run
```

There is no `--force`. A value that is not `$ANSIBLE_VAULT` has nothing to decrypt, and rewriting it would be a no-op that still rewrote the file, so that is an error.

### Encrypting a Whole Configuration

Naming each path gets tedious once a configuration has more than one registry, and it is easy to miss one. `encrypt-file` walks every registry and encrypts the four fields in the table above:

```
cargoship vault encrypt-file ./cluster.yaml --vault-password-file ./vault-pass.txt
```

It reports each path it rewrote and a count at the end. Everything else in the document is left exactly as it was, the same way `encrypt-path` leaves it.

A field that is absent, empty, or encrypted already is skipped rather than treated as an error. Two things follow from that, and both are the point of the command:

* Running it over a configuration that was vaulted by hand, or half-vaulted and then abandoned, **finishes the job** without disturbing what is already there.
* Running it **twice changes nothing** the second time, so it is safe in a script or a pre-commit hook.

This is what makes the ordinary credential rotation easy. Paste the new registry password into the file in the clear, leave the vaulted username alone, and run `encrypt-file`: it picks up the one value you changed and does not re-wrap the rest. Because Ansible Vault salts every encryption, re-wrapping them would change every one of those lines in the diff for no reason.

`--dry-run` prints the resulting document to stdout and leaves the file alone. `--force` re-encrypts values that are ciphertext already, wrapping them a second time -- which is almost never what you want, since an apply unwraps only one layer.

### Decrypting a Whole Configuration

```
cargoship vault decrypt-file ./cluster.yaml --vault-password-file ./vault-pass.txt
```

The inverse: every vaulted registry credential in the file becomes plaintext again, and anything that is not encrypted is skipped. An `encrypt-file` followed by a `decrypt-file` under the same password gives back the original document byte for byte.

If any value fails to decrypt, the command fails and the file is not touched at all -- it does not leave you with half a configuration in the clear.

### Shared Credentials (Anchors and Aliases)

A configuration that pulls from two names on the same host usually writes the credential once and points the second registry at it:

```
    registries:
      - name: docker.io
        auth: &auth
          user: robot
          pass: hunter2
      - name: test.io
        auth: *auth
```

All of the vault commands follow anchors and aliases. The value lives under the anchor, so that is what gets rewritten, and the alias is left as it is:

```
      - name: docker.io
        auth: &auth
          user: |-
            $ANSIBLE_VAULT;1.1;AES256
            ...
      - name: test.io
        auth: *auth
```

Both registries read the same ciphertext at apply time, which is what sharing the value meant in the first place. `encrypt-file` reports it once -- `count=2` for the shared `user` and `pass` above, not four -- because there is one value behind each pair of paths. Naming the aliased registry yourself works too: `encrypt-path ... '.spec.config.registries[1].auth.pass'` rewrites the anchored value, since that is the value that path resolves to. Naming both registries in one `encrypt-path` run is the one thing to avoid; the second path is the value the first has already encrypted, so it fails as already encrypted.

An alias whose anchor is missing, or comes later in the file, is an error naming the alias. Nothing in this package reports a value it cannot reach as a value that was not there.

## Rotating the Vault Password

```
cargoship vault rekey ./cluster.yaml --vault-password-file ./old-pass.txt --new-vault-password-file ./new-pass.txt
```

Every vaulted registry credential in the file moves to the new password in one command. The plaintext is never written to the file: each value is decrypted and encrypted again in memory, so there is no window in which the configuration on disk is readable, and nothing to remember to clean up if the command fails or the terminal goes away mid-rotation. `--dry-run` prints the result to stdout and leaves the file alone.

`--new-vault-password-file` is optional. Omit it and every value is re-wrapped under the password the file already carries:

```
cargoship vault rekey ./cluster.yaml --vault-password-file ./vault-pass.txt
```

That is a re-salt rather than a rotation, and it is logged as one. Ansible Vault salts each encryption, so every credential comes back as different ciphertext holding the same plaintext under the same password -- useful when the ciphertext has been somewhere you would rather it had not been, or when a value was vaulted long enough ago that you want it rewritten, and it never puts the plaintext on disk. Naming a file that holds the old password does the same thing.

Unlike `--vault-password-file`, `--new-vault-password-file` has no environment fallback. The environment holds the password the file is vaulted under *now*, so falling back to it would report a rotation onto a password nobody asked to move to.

A value that is plaintext is skipped, and so is one that is empty or absent -- rekeying does not encrypt anything that was not encrypted before. To pick up a newly added credential, run `encrypt-file` with the new password afterwards.

**Rotate the whole file, not one value at a time.** An apply decrypts all of a registry's fields with a single password, so a configuration whose values were vaulted under two different passwords is one that no password can read. `rekey` refuses to create or perpetuate that state: if any encrypted value in the file cannot be read with the old password, it stops before writing anything and names the path. `encrypt-file` refuses the same way. But rotating path by path with `decrypt-path` and `encrypt-path` will walk you into it, since those commands only ever look at the one value you named.

Ansible Vault salts each encryption, so the ciphertext differs on every run even when the password and the plaintext are unchanged. A rekey produces a diff on every value it touches, and running it twice with the same pair of *different* passwords is not idempotent -- the second run fails, because the file is no longer readable with the old password. A re-salt can be run as often as you like, since the password it reads with is the password it writes back.

## Checking Before an Apply

An apply verifies every vaulted value as soon as it has resolved the password -- before it connects to any host, and on a sync, before it starts draining nodes. A missing or wrong password fails the command immediately rather than part-way through a cluster change.

To check a configuration by hand without writing plaintext anywhere, use `decrypt-file --dry-run` and throw the output away. It decrypts every vaulted credential in the file and exits non-zero if any of them fails:

```
cargoship vault decrypt-file ./cluster.yaml --vault-password-file ./vault-pass.txt --dry-run > /dev/null
```

## Interoperability with ansible-vault

Cargoship writes the Ansible Vault 1.1 AES256 format, so `ansible-vault` reads what Cargoship writes and Cargoship reads what `ansible-vault` writes:

```
cargoship vault encrypt hunter2 --vault-password-file ./vault-pass.txt > ct.txt
ansible-vault decrypt --vault-password-file ./vault-pass.txt --output - < ct.txt
```

One difference matters. `ansible-vault encrypt_string` tags its output with `!vault`:

```
pass: !vault |
  $ANSIBLE_VAULT;1.1;AES256
  ...
```

Cargoship writes a plain block scalar with no tag, and its `-path` and `-file` commands reject a tagged value rather than strand the tag on a value that no longer matches it. Drop the `!vault` tag when moving a value across; the ciphertext underneath needs no change.

## Notes

* `cargoship vault-encrypt` is the old spelling of `cargoship vault encrypt`. It still works and still prints a deprecation notice. Use the `vault` group instead.
* A failed command never touches the file. The rewrite goes to a temporary file alongside the original and is renamed over it, so an interruption cannot leave a half-written configuration.
* The file's mode is preserved across a rewrite.
