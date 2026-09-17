# age Encryption

This guide explains how Cargoship encrypts registry credentials in a cluster configuration with [age](https://github.com/FiloSottile/age), and how to read them back out. age is an alternative to Ansible Vault, not a replacement for it: the same commands write both formats, one configuration can hold both, and an apply reads whichever format each value carries.

For the Ansible Vault side of the same commands, see [vault-encryption](vault-encryption.md). Everything that guide says about YAML paths, anchors and aliases, dry runs, and the in-place rewrite is true here as well; this guide covers only what differs.

## Why age

The draw is the key model rather than the cipher.

Ansible Vault is a shared passphrase. Everyone who has to run an apply has the same secret, which means the secret has to be distributed, and revoking one person's access means rotating it for everybody.

age is public key encryption. You encrypt a credential to a set of public keys -- the *recipients* -- and it can only be read with one of the matching private keys, the *identities*. A machine that only has to encrypt needs no secret at all: a CI job that commits a configuration holds the public keys and nothing more. Revoking access means re-encrypting to a smaller recipient set, which is one command over the whole file and touches no other key.

Ansible Vault remains the better fit when a single operator or a single shared credential store is the whole story, and it is what an existing configuration already uses. Neither format is going away.

## What Cargoship Decrypts

The same four fields per registry, in either format:

| Path | What it holds |
| --- | --- |
| `.spec.config.registries[N].auth.user` | Registry username |
| `.spec.config.registries[N].auth.pass` | Registry password |
| `.spec.config.registries[N].auth.token` | Registry token |
| `.spec.config.registries[N].tls.ca` | Inline CA certificate |

Cargoship tells the formats apart by the value's own header. Ansible Vault ciphertext starts with `$ANSIBLE_VAULT`; age ciphertext is armored, so it starts with `-----BEGIN AGE ENCRYPTED FILE-----` and ends with `-----END AGE ENCRYPTED FILE-----`. A value carrying neither is plaintext and is passed through untouched.

Nothing records the format in the schema, and no flag selects it for reading. That is what lets one document mix the two.

## Keys

### Making a Key Pair

Cargoship makes a key pair itself, so the [`age`](https://github.com/FiloSottile/age) distribution is not something you have to install first:

```
cargoship vault keygen --output ~/.age/cargoship.key
```

It writes the identity -- the private key, `AGE-SECRET-KEY-1...` -- to the file with mode `0600`, and prints the recipient -- the public key, `age1...` -- to stderr. It will not overwrite a file that already exists: replacing an identity makes every value encrypted to the old key unreadable, by anyone, permanently. Move the old file aside if you mean to replace it.

`age-keygen -o ~/.age/cargoship.key` does the same thing and writes the same file. The format is age's, not Cargoship's, which is the point: the key stays usable with `age --decrypt` and anything else that speaks it.

The public key is also in the file, on a `# public key:` comment line, so you can recover it later:

```
cargoship vault keygen --public-key ~/.age/cargoship.key
```

`age-keygen -y ~/.age/cargoship.key` is the equivalent, and `-y` is the short flag here too.

Omitting `--output` writes the private key to stdout instead, which makes the redirect work:

```
cargoship vault keygen > ~/.age/cargoship.key
chmod 600 ~/.age/cargoship.key
```

The `chmod` matters in that form and not the other: a redirect creates the file with whatever mode your shell gives it. Run bare, with no redirect, the command warns -- the key is then on the screen and in that terminal's scrollback.

The public key is not a secret. Commit it, publish it, put it in a ticket.

### Recipients: the Keys That Encrypt

Three flags name recipients, and all three are repeatable:

```
--age-recipient age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p
--age-recipients-file ./recipients.txt
```

A recipients file holds one public key per line. Blank lines are ignored, and a line beginning with `#` is a comment, so the file doubles as the list of who can read the configuration:

```
# alice, platform team
age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p

# bob, platform team
age19h0ngeasgxd5vpcfavgavma2m39cmq3a2xlhggs6u0r5rtscx5ms0gw4jh

# the CI runner's deploy key
age1lggyhqrw2nlhcxprm67z43rta597azn8gknawjehu9d9dl0jq3yqqvfafg
```

The comment has to be on its own line. A `#` after a key on the same line is read as part of the key, and the file fails to parse with the line number. This is age's own format, and `age --recipients-file` reads the same file.

SSH public keys go in the same flags and the same file; see [Using an SSH Key](#using-an-ssh-key).

Every recipient named across every flag is encrypted to, so a value can be readable by several people at once. This is the normal case, not a migration state.

When no recipient flag is given, Cargoship reads `CARGOSHIP_AGE_RECIPIENTS`, which holds whitespace-separated public keys:

```
export CARGOSHIP_AGE_RECIPIENTS="age1ql3z... age19h0n..."
```

A recipients *file* that parses to no keys at all is an error rather than nothing to do, because carrying on would fall back to writing Ansible Vault ciphertext under a password the operator did not ask to use.

### Identities: the Keys That Decrypt

```
--age-identity-file ~/.age/cargoship.key
```

Repeatable, and a single file may hold several identities. Every identity Cargoship has been given is tried against a value, so holding keys for two teams in one file works.

When no identity flag is given, Cargoship reads `CARGOSHIP_AGE_IDENTITY_FILE`, which holds exactly one path. A list would need a separator, and every separator worth choosing is legal in a file name.

### Using an SSH Key

An `ssh-ed25519` or `ssh-rsa` key works anywhere an age key does, and goes in the same three flags. There are no `--ssh-*` flags, because there is no point in making an operator work out which kind of key they hold before choosing a flag:

```
cargoship vault encrypt-file ./cluster.yaml --age-recipient "$(cat ~/.ssh/id_ed25519.pub)"
cargoship vault encrypt-file ./cluster.yaml --age-recipients-file ./authorized_keys
cargoship vault decrypt-file ./cluster.yaml --age-identity-file ~/.ssh/id_ed25519
```

This is worth doing because the keys already exist. A team that maintains an `authorized_keys` file can encrypt a configuration to it as it stands -- options in front of a key and comments behind it parse fine -- and nobody has to generate, distribute, or lose a new key.

A recipients file may mix the two kinds freely, which is what makes a gradual move onto native age keys possible:

```
# alice, on her age key already
age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p

# bob, still on his SSH key
ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample bob@example.com
```

A line that is neither -- an `ecdsa-sha2-nistp256` key, say, which age does not support -- fails the whole file with its line number rather than being skipped. Skipping it would encrypt to fewer people than the file lists, and the one who finds out is the one who cannot decrypt.

Three things differ from a native age key:

* **A passphrase-protected private key is prompted for on a terminal, and fails without one.** The prompt happens only once a value turns out to be encrypted to that key, and the decrypted key is then reused for the rest of the run. Under CI, or with input piped in, the command fails immediately and says so; it does not read the passphrase out of standard input and does not hang. If that is where the key has to be used, decrypt it first with `ssh-keygen -p`.
* **The public key is not derived from the private one.** `cargoship vault keygen --public-key` reads age identity files only. `ssh-keygen` already wrote the public key into the `.pub` file beside the key, and `ssh-keygen -y -f ~/.ssh/id_ed25519` prints it again.
* **SSH recipients are not anonymous.** See [Recipients Are Anonymous](#recipients-are-anonymous) below.

Upstream's own advice is that these key types exist for compatibility with keys you already have, and that a native age key is preferable otherwise. `cargoship vault keygen` makes one.

### Nothing Is Discovered

Cargoship reads key material from the flags, the config file, and those two environment variables. It reads nothing else. It does not look in `~/.config/sops/age/keys.txt`, it does not look in `$XDG_CONFIG_HOME/age`, it does not read an `age` agent, and it does not pick your SSH key up out of `~/.ssh` because it happens to be there.

The one path derived rather than given is the `.pub` file beside a passphrase-protected SSH key, which older key formats do not carry their public key inside. That path comes from the identity file the operator named on the command line, and is read only when that key needs a passphrase.

This matches `ResolveVaultPassword`, which has always behaved the same way, and it is deliberate: an apply that succeeds on one operator's machine because of a file the configuration never mentions is an apply nobody can reason about.

### Keys in the Config File

All three flags can be defaulted from the `age` section of `cargoship-config.yaml`, so a team can commit its recipient set once instead of restating it on every command:

```
age:
  recipients:
    - age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p
    - age19h0ngeasgxd5vpcfavgavma2m39cmq3a2xlhggs6u0r5rtscx5ms0gw4jh
  recipients_files:
    - /etc/cargoship/recipients.txt
  identity_files:
    - /home/operator/.age/cargoship.key
```

Each key is a list, and each is the equivalent of repeating its flag. A recipient in the config file counts as choosing age exactly as the flag does -- see below.

Put only public keys in a committed config file. `identity_files` holds *paths*, not keys, so it is safe to commit when the path is a per-machine convention; it is still a per-operator choice, and the environment variable is often the better home for it.

## Choosing the Format Cargoship Writes

Reading never involves a choice: the value's header decides. Writing does, and Cargoship resolves it like this:

1. An age recipient given on the command line or in the config file wins, **even when `CARGOSHIP_VAULT_PASSWORD` or `ANSIBLE_VAULT_PASSWORD` is set in the environment.** Those variables are typically set once in a shell profile on a machine that already uses vault, and an operator who passes `--age-recipient` is asking for age, not for a complaint about a variable they set months ago.
2. An age recipient *and* an explicit `--vault-password-file` together is an error:

   ```
   age recipients and a vault password file were both given; pass one or the other,
   since a value is written in a single format
   ```

   Both flags name the key to write with. Picking one would mean writing a secret under a key the operator did not choose, which is worth stopping for rather than guessing well.
3. Otherwise Ansible Vault, exactly as before.

With no key material at all, every `vault` command fails up front:

```
no encryption key found: set --vault-password-file or the
CARGOSHIP_VAULT_PASSWORD/ANSIBLE_VAULT_PASSWORD environment variable, or pass
--age-recipient and --age-identity-file
```

An apply is the exception. A configuration holding no encrypted credential needs no key, so an apply accepts an empty keyring and only fails if it meets a value it cannot read.

## The Commands

The `cargoship vault` group is the same group, and it keeps its name -- renaming it would break every operator's scripts:

```
cargoship vault encrypt [VALUE]
cargoship vault encrypt-path FILE YAML_PATH [YAML_PATH...]
cargoship vault encrypt-file FILE
cargoship vault decrypt [VALUE]
cargoship vault decrypt-path FILE YAML_PATH [YAML_PATH...]
cargoship vault decrypt-file FILE
cargoship vault rekey FILE
cargoship vault keygen [IDENTITY_FILE]
```

`keygen` is the only one of them that takes no key material, since it is the command that makes some.

### Encrypting a Value

```
cargoship vault encrypt my-registry-password --age-recipient age1ql3z...
```

The output is an armored age block that you paste into the configuration at the right indentation, the same way a `$ANSIBLE_VAULT` block is pasted. `vault encrypt-path` does that part for you.

Passing a secret as an argument leaves it in your shell history and in the process list. Prefer stdin, or the hidden prompt you get when stdin is a terminal:

```
cargoship vault encrypt --age-recipient age1ql3z... < ./registry-token.txt
```

### Decrypting a Value

```
cargoship vault decrypt --age-identity-file ~/.age/cargoship.key < ./encrypted-token.txt
```

**An armored age value begins with dashes, so passing it as an argument does not work** -- the shell hands it to Cargoship intact, but the flag parser reads the leading `-----` as a flag and fails with `bad flag syntax`. Use stdin, as above, or put `--` after all of the flags so that everything following it is read as an argument:

```
cargoship vault decrypt --age-identity-file ~/.age/cargoship.key -- "$CIPHERTEXT"
```

`$ANSIBLE_VAULT` values do not have this problem, which is why it only comes up now.

When no configured identity can read the value, the error says what the value was encrypted to:

```
none of the configured age identities can decrypt this value; it is encrypted to
X25519 recipients
```

That is as specific as it can be. See [Recipients Are Anonymous](#recipients-are-anonymous).

### Encrypting a Whole Configuration

```
cargoship vault encrypt-file ./cluster.yaml --age-recipient age1ql3z...
```

Every registry credential in the file that is plaintext is encrypted to the recipients; anything absent, empty, or **encrypted already in either format** is skipped. Comments, key order, and the rest of the document survive byte for byte.

Running it twice changes nothing the second time, so it is safe in a script or a pre-commit hook, and running it over a half-encrypted configuration finishes the job.

There is one consequence of that worth stating plainly. `encrypt-file` **never moves a credential between formats, and never re-encrypts one to a new recipient set.** Running it with new recipients against a file whose credentials are already encrypted does nothing to the file. It does not do it quietly, though -- every credential it left alone is reported, with the reason:

```
WRN left this credential as it was: it is encrypted to age recipients already, and cargoship
    cannot tell whether they are the ones you named, because age ciphertext does not record its
    recipients; run 'cargoship vault rekey' with an identity to re-encrypt it to the recipients
    you want  file=./cluster.yaml path=$.spec.config.registries[0].auth.pass
INF nothing to encrypt: every registry credential is encrypted already, or there are none to encrypt
```

The same warning appears when the formats differ, naming the one the value is in and the `rekey` invocation that moves it. Warnings are written to stderr, so `--dry-run` piped to a file still produces a clean document.

To change *which* keys a configuration is encrypted to, use `rekey`.

### Decrypting a Whole Configuration

```
cargoship vault decrypt-file ./cluster.yaml --age-identity-file ~/.age/cargoship.key
```

Every encrypted registry credential becomes plaintext again, whatever format it was in, and anything that is not encrypted is skipped. If any value fails to decrypt, the command fails and the file is not touched at all.

Give both `--age-identity-file` and `--vault-password-file` to decrypt a document holding both formats. That combination is fine here: it is only *encrypting* that has to choose one format, and only then that giving both is an error.

## Mixed Documents

A configuration can hold both formats at once, and not only while migrating:

```
    registries:
      - name: harbor.internal
        auth:
          user: robot
          pass: |-
            $ANSIBLE_VAULT;1.1;AES256
            31333438393936323663373461353533373034663339316335386139363535383531643464613539
            ...
      - name: ghcr.io
        auth:
          user: ci
          pass: |-
            -----BEGIN AGE ENCRYPTED FILE-----
            YWdlLWVuY3J5cHRpb24ub3JnL3YxCi0+IFgyNTUxOSBB...
            -----END AGE ENCRYPTED FILE-----
```

An apply given both a vault password and an identity file reads this document without complaint. A legacy credential nobody wants to touch can stay vaulted indefinitely while everything new is written to age.

## Migrating from Ansible Vault

`rekey` is the migration, and there is no other command for it:

```
cargoship vault rekey ./cluster.yaml \
  --vault-password-file ./vault-pass.txt \
  --age-recipient age1ql3z...
```

Every vaulted registry credential in the file is read with the password and written back to the recipients, in one pass. The plaintext is never written to the file: each value is decrypted and encrypted again in memory, so there is no window in which the configuration on disk is readable, and nothing to clean up if the command fails partway. `--dry-run` prints the result to stdout and leaves the file alone.

The same rule that has always governed `rekey` applies: **rotate the whole file, not one value at a time.** If any encrypted value in the file cannot be read with the key given, the command stops before writing anything and names the path.

`--new-vault-password-file` and `--age-recipient` together is an error. Each names the key to rekey onto, and honouring one would mean silently ignoring the other.

To go the other way -- age back to Ansible Vault -- read with the identity and write with a new password:

```
cargoship vault rekey ./cluster.yaml \
  --age-identity-file ~/.age/cargoship.key \
  --new-vault-password-file ./vault-pass.txt
```

## Revoking Access

Removing someone's key from a recipients file changes nothing on its own. The ciphertext in the configuration was encrypted to them and still is. Re-encrypt it:

```
cargoship vault rekey ./cluster.yaml \
  --age-identity-file ~/.age/cargoship.key \
  --age-recipients-file ./recipients.txt
```

Reading needs an identity that is still in the file; writing uses the new, smaller recipient set. Afterwards the removed key can no longer read the configuration.

Treat the credential itself as compromised anyway if the person had it. Re-encryption removes their access to the *file*; it does not un-tell them a password they have already read. The registry credential should be rotated at the registry too.

Naming the recipient set the file already uses re-encrypts every value to those same recipients:

```
cargoship vault rekey ./cluster.yaml \
  --age-identity-file ~/.age/cargoship.key \
  --age-recipients-file ./recipients.txt
```

age re-randomizes every encryption, so each value comes back as different ciphertext holding the same plaintext for the same recipients -- useful when the ciphertext has been somewhere you would rather it had not been.

Cargoship reports this as a rekey rather than as the re-salt it is, because it cannot tell the two apart: the recipients you name may or may not be the ones the values already carry, and the ciphertext does not say. With Ansible Vault, where omitting `--new-vault-password-file` unambiguously means "the key this file already has", the same command is reported as a re-salt.

An identity on its own is not enough. `rekey --age-identity-file key.txt` with no recipients names nothing to write with and fails:

```
no vault password and no age recipients were provided
```

## Checking Before an Apply

An apply verifies every encrypted value as soon as it has resolved the keys -- before it connects to any host, and on a sync, before it starts draining nodes. A missing identity fails the command immediately rather than part-way through a cluster change.

With age this check matters more than it did with a shared password. A value encrypted to the wrong recipient set is indistinguishable from a correct one until something tries to read it, and nothing else in the pipeline will.

To check a configuration by hand without writing plaintext anywhere:

```
cargoship vault decrypt-file ./cluster.yaml --age-identity-file ~/.age/cargoship.key --dry-run > /dev/null
```

Or run the real preflight without touching a host:

```
cargoship apply ./cluster.yaml --age-identity-file ~/.age/cargoship.key --dry-run
```

## Recipients Are Anonymous

An age header written to a native `age1...` recipient does not say who the value was encrypted to. The X25519 stanza holds an ephemeral share and nothing else -- no key identifier, no fingerprint -- by design, so that ciphertext does not leak its audience.

**SSH recipients are the exception.** An `ssh-ed25519` or `ssh-rsa` stanza carries a short 32-bit hash of the public key, so someone holding a copy of the ciphertext and a list of candidate public keys can tell which of them it was encrypted to. If who can read a configuration is itself something you do not want in a committed file, use native age keys.

Either way, two things follow, because Cargoship reads no stanza at all -- age keeps that parser internal, so nothing here can see even the recipient hash:

* Cargoship cannot tell you whether a value is already encrypted to the recipients you have in hand. It can only tell you that it is encrypted. This is why `encrypt-file` skips an encrypted value rather than checking it, why it warns about every value it skipped rather than staying quiet, and why changing recipients is `rekey`'s job.
* An error about a value you cannot read can never name the key you are missing. `none of the configured age identities can decrypt this value` is the whole of what is knowable.

`docs/agent/choice-age-encryption.md` records the reasoning in full.

## Interoperability with age

Cargoship writes the same armored format `age --armor` writes, so the tools read each other:

```
cargoship vault encrypt hunter2 --age-recipient age1ql3z... > ct.txt
age --decrypt --identity ~/.age/cargoship.key ct.txt
```

```
printf hunter2 | age --encrypt --armor --recipient age1ql3z... > ct.txt
cargoship vault decrypt --age-identity-file ~/.age/cargoship.key < ct.txt
```

## What Is Not Supported

* **`ssh-agent`.** A passphrase-protected SSH key is prompted for directly; a key held in an agent is not reachable, because age's SSH support needs the private key itself rather than a signing oracle.
* **SSH key types other than `ssh-ed25519` and `ssh-rsa`.** `ecdsa-*` and `sk-*` keys are not supported by age.
* **age passphrases (scrypt).** age can encrypt to a passphrase instead of a public key. Cargoship does not expose that, because it is the thing Ansible Vault already does here -- a shared secret -- and having two ways to spell it would help nobody.
* **Plugins** (`age-plugin-yubikey` and friends). Cargoship links the age library rather than shelling out to the `age` binary, and plugin support is a property of the binary.

## Notes

* A failed command never touches the file. The rewrite goes to a temporary file alongside the original and is renamed over it, so an interruption cannot leave a half-written configuration.
* The file's mode is preserved across a rewrite.
* Armored age ciphertext is longer than the plaintext it holds by a fixed overhead of a couple of hundred bytes, so a short password becomes a block several lines long. This is normal, and why every command writes it as a YAML block scalar rather than a quoted string.
