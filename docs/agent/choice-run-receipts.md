# Choice: what a run receipt records, and what it refuses to

`cargoship apply --receipt-path` writes a structured record of a run, and `cargoship redact-receipt` projects it into something safe to attach to a public issue. Five decisions in that design look like oversights from the code alone. They are not.

## Host records are an allowlist, never a denylist

`internal/receipt.Host` copies in a fixed set of fields by name, and nothing anywhere marshals `cluster.ZarfHost` into a receipt.

That looks like needless transcription -- the host object already has every field, and a `json:"-"` tag on the ones that must not appear would be less code. It is deliberate. `ZarfHost` embeds a live `rig.ClientWithConfig` and a `Metadata` struct full of connection state, and a receipt built by excluding what must not appear leaks the next field somebody adds to either. An allowlist fails closed: a new fact is absent from receipts until a person adds it to `Host`, which is the safe direction to be wrong in.

`TestHostFieldsAreAnAllowlist` pins the field set so that adding one fails the build until it is classified. The precedent is `internal/tofuprovider.factsOf`, which does the same thing for the provider's attributes and for the same reason.

If you are adding a host fact to a receipt, add it to `Host` and update that test. Do not reach for reflection over `ZarfHost`.

## Diffs exist only where a phase already read the file

Four phases carry a real unified diff -- `25_modify_hosts_file`, `24_prepare_selinux_policy`, `70_engine_config_sync_common` and `89_reconfigure_survivors`. Four do not: `20_prepare_host` (sysctl), `22_prepare_fapolicyd`, `23_prepare_kernel_modules`, and the engine `config.yaml` written through `types/distrocfg`.

The split is not an unfinished job. It is exactly the line between phases that fetch the prior content anyway and phases that do not. `driftedFiles` in the sync phase holds the host's copy and the desired copy in the same scope, so a diff there costs nothing. The other four write without reading, and `ZarfHost.FileChanged` compares by sha256 without ever pulling the remote file down -- so adding a diff to them means a new `ReadFile` per file per host, every run, on every host in the fleet. On a large fleet that is a real cost paid by every operator to benefit the few who read a diff.

Those phases still report *that* they changed a host, with the file path and a reason. Only the body is missing. `HostChangeDetail.Diff` is documented to mean "not compared" when empty, never "no difference".

If you add a diff to one of the blind-write phases, that is a deliberate trade, not a cleanup -- say so, and measure it against a fleet rather than a two-node test cluster.

## Redaction happens at share time, not capture time

There is no `--receipt-redact` flag, and `--receipt-path` never writes a second, safe file beside the real one.

Both were considered. A pair of files one infix apart -- `apply.json` and `apply.redacted.json`, one carrying join tokens and one not -- is how the wrong one ends up attached to a public issue, and the failure is silent and unrecoverable once it is there. Redaction is also lossy and one way: the operator's copy is the audit record and has to stay complete, and a reporter must not have to re-run a failed apply to get something shareable.

So it is a post-processor. That also means it works on receipts already on disk, and can be tightened later without anybody re-running anything.

## Hosts are pseudonymised, not hashed

`Redact` replaces each address with `host-01`, `host-02` in the order the receipt lists them.

A digest of the real address would be the obvious choice and is the wrong one. Hostnames and RFC-1918 addresses come from a small enough space to enumerate exhaustively, so a hash of one is recoverable by anybody willing to spend an afternoon on it -- while looking like a one-way function to everyone who reads the code. A sequential counter carries no information at all.

The numbering follows the cluster configuration rather than the order phases reached hosts, so the alias a host gets in the host list is the alias it gets inside every phase, and a maintainer can still follow one machine through the ledger.

## The receipt path is templated, and only two placeholders exist

`expandReceiptPath` substitutes `{command}` and `{timestamp}` into a receipt path, and rejects anything else. A flag value that is just a filename would be simpler, and for a flag it would be right.

The templating exists for the config file, not the flag. `--receipt-path` names one file for one run; `.distro.receipt_path` is read by every `apply`, `prepare`, `reset` and `engine-config-sync` the operator ever runs, so a literal path configured there has each run truncate the record of the one before it. That failure is silent and leaves exactly one receipt on disk -- the audit trail an operator thought they had turned on is instead a single file whose contents are whichever run happened last. The two placeholders together make the name unique per run, which is what somebody setting the key meant by setting it.

An unrecognised placeholder is an error returned from `startReceipt`, before the run touches a host, rather than a literal written into the filename. Both halves are deliberate: a run that succeeds and records itself in `apply-{date}.json` is not discovered until somebody goes looking for a receipt that was written somewhere nobody expects, and failing after a fleet-wide change -- when the run is cheap to repeat before it, not after -- would be the wrong end to fail at.

There is deliberately no `{phase}` alias for `{command}`. "Phase" in this repository means a numbered step inside a run (`25_modify_hosts_file` and its siblings), and a receipt spans all of them, so a `{phase}` that expanded to `apply` would name one thing after another. Adding more placeholders is cheap -- a map entry in `expandReceiptPath` -- but each one is a name an operator has to learn, so it should answer a question the two existing ones cannot.
