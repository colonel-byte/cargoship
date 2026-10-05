# How the OpenTofu provider will perform an install

> A plan under review, not a record of a decision. The `choice-*` documents beside it describe what was settled; this describes work that has not started, and it should either become one of them or be deleted once the provider exists.

## Context

The module is a stub: `go.mod` with no dependencies and a `main` that exits 1. The decisions around it are recorded (`docs/agent/choice-tofu-provider-layout.md`, `choice-tofu-secrets.md`) and the plumbing is open as PRs #605-#609. This is the work that makes `tofu apply` converge a cluster.

Settled by the answers to the design questions:

- **In-process via `pkg/action`.** The provider module requires `github.com/colonel-byte/cargoship` and calls `action.NewApply` / `action.NewReset` directly, so it gets `phase.RunResult` and typed errors rather than an exit code.
- **Hosts as HCL blocks**, rendered into a `cluster.ZarfCluster` the way `internal/ansibleinv.Translate` renders one for Ansible.
- **Destroy runs reset, with an opt-out.** Both "destroy runs reset" and "destroy is a no-op" were chosen; they are reconciled as a default plus a flag, since one has to be the default. `tofu destroy` resets, because that is what destroy means everywhere else in tofu, and `retain_on_destroy = true` downgrades it to dropping state with a warning. Say so in the schema description, not only here.
- **`kubeconfig` output, opt-in** per `choice-tofu-secrets.md` rule 4.
- **A separate `cargoship_prepared_hosts` resource** for the disruptive prepare phases.

## What has to change in cargoship core first

These are not provider code, and each is small. They exist because the CLI is currently the only caller.

| Gap                                                              | Where                                                                                      | Why the provider needs it                                                                                                                                                                      |
| ---------------------------------------------------------------- | ------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `NewApply` returns `nil` on an unknown distro ID                 | `pkg/action/apply.go:73-77`, same in `reset.go`, `kube-config.go`, `engine_config_sync.go` | A nil `*Apply` panics on `Run`. The constructors should return `(*Apply, error)`; the CLI call sites and the e2e harness update with them                                                      |
| `KubeConfigOptions` hardcodes `Write: true` and exposes no bytes | `pkg/action/kube-config.go:34-76`                                                          | #305 gave `phase.KubeConfig` `Write`, `Config()` and `Bytes()`; the action never got them, so a caller that wants the value has to rebuild the phase list by hand                              |
| No read-only refresh action                                      | new, beside `pkg/action/apply.go`                                                          | `Read` needs per-host facts without touching anything: `Connect, DetectOS, GatherFacts, GatherFactsDistro, Disconnect` -- five phases that all exist and all declare `ReadOnly()`              |
| Phase logs go to the zarf logger and `Manager.Writer`            | `pkg/phase/05_manager.go`                                                                  | A provider has to route both into `tflog`, which means an adapter rather than a core change -- but `Writer` being an `io.Writer` is what makes it possible, so it is worth a test pinning that |

## Provider layout

```
cmd/terraform-provider-cargoship/
  main.go                      # provider.Serve, replacing the stub
internal/tofuprovider/
  provider.go                  # provider schema: vault/age env passthrough, cache path, log level
  cluster_resource.go          # cargoship_cluster: Schema, Create, Read, Update, Delete, ImportState
  prepared_hosts_resource.go   # cargoship_prepared_hosts
  translate.go                 # HCL model -> cluster.ZarfCluster
  translate_test.go
  logging.go                   # zarf logger + Manager.Writer -> tflog
  plan.go                      # plan-time checks (downgrade, removed hosts)
  testdata/                    # fixtures for the translate tests
```

A `main` package beside `cmd/cargoship`, with the logic under `internal/` so nothing outside this module can import it. That is also what keeps the two triggers in the choice doc cheap: moving to a second module or to its own repository moves a directory, not an import graph.

## The resource

### Schema (`cargoship_cluster`)

Required: `name` (becomes `metadata.name` and the kubeconfig context), `load_balancer`, `package` (path or OCI reference), and one or more `host` blocks.

A `host` block carries `address`, `user`, `port`, `key_path`, `role`, `profile`, `private_address`, `hostname`. **`key_path` only** -- there is no attribute that accepts key material, per rule 1. Optional cluster attributes mirror the apply flags that change behaviour rather than output: `modify_hosts`, `modify_firewall`, `label_nodes`, `worker_concurrency`, `concurrency`, `allow_unmanaged_nodes`, `allow_downgrade`, `timeout`.

Computed: `id`, `engine_version` (what the package carries), and `nodes` -- one object per host with `hostname`, `os`, `arch`, `private_address`, `engine_version`. That last field is the one the plan-time downgrade check reads, and it is why it has to be in the schema before anything is published.

`kubeconfig` is computed and sensitive, populated only when `export_kubeconfig = true`.

### Create

1. `translate.go` turns the model into a `cluster.ZarfCluster`. It renders the YAML and parses it back through `clustercfg.Parse`, rather than building the struct, so the provider gets the same schema validation and the same defaults a file gets -- `load.ClusterDefinition` only adds file reading on top of that.
2. `distro.Load` extracts the package into the provider's cache directory.
3. Build the `phase.Manager`: `Config`, `Distro`, `DistroID`, `TempDirectory`, `Concurrency`, `ConcurrentUploads`, `Writer` (the tflog adapter), `SetTimeout`.
4. `action.NewApply(...).Run(ctx)` with a `phase.WithResultSink` context, so what the phases did comes back.
5. Read the facts the read-only phases left on `manager.Config.Spec.Hosts` into the computed `nodes`, and `KubeConfig.Bytes()` into `kubeconfig` when it was asked for.

`Update` is the same call. `action.NewApply` already builds one phase list covering install, join and upgrade, each phase gated by its own `ShouldRun`, which is the convergent shape `Create`/`Update` want -- there is nothing to branch on.

### Read

The refresh action above, then the same fact extraction. A host that cannot be reached is a diagnostic, not a removal: `RemoveResource` on a transient SSH failure would make the next apply re-bootstrap a running cluster.

### Delete

`action.NewReset` unless `retain_on_destroy`. Reset is the whole-cluster case, which `choice-removed-hosts.md` explicitly separates from removing one host: the state holds the entire configuration as one snapshot and the instruction is unambiguous.

### Plan-time checks (`plan.go`)

`ModifyPlan` is where #307's provider half lands, and it needs no network:

- **Downgrade.** Compare the package's version against the highest `nodes[*].engine_version` in prior state. Lower, and the plan errors naming the host and both versions. This is the check the CLI can only make after connecting.
- **Removed hosts.** A `host` block gone from the configuration while state still holds it is an error with the remediation from `ErrUnmanagedNodes`, because apply reconciles no removals. The provider knows this earlier and more precisely than `DetectRemovedHosts` does, and `choice-removed-hosts.md` rules out driving a teardown from the stale state.

## `cargoship_prepared_hosts`

Same host blocks, `action.NewPrepare`, `modify_modules` included. Separate because prepare reboots a host that gained a kernel module, and that belongs on its own schedule. `Read` is a no-op beyond liveness, `Delete` is state-only -- prepare has no inverse.

## Dependency and build consequences

The provider is a package of this module, so `terraform-plugin-framework` lands in the committed `vendor/` tree. Measured: 13 modules and about 1,222 files for the runtime, 28 and about 1,998 once `terraform-plugin-testing` arrives with the acceptance tests in layer 5. Layer 2 is therefore `go get` plus `mage dev:vendor` plus whatever `dev:verifyVendor` says about new non-Go lockfiles, and that re-vendor is most of its diff.

What this does not cost is the CLI binary. The linker never loads a package the import graph does not reach, and nothing under `internal/tofuprovider` is reachable from `cmd/cargoship`, so `cargoship` stays the size it is now. The rule to hold to is that no package under `cmd/cargoship` ever imports the provider's packages - a single import reverses that, and nothing in CI would report it as anything but a larger binary.

The provider binary itself will be CLI-sized, times five platforms, in every published artifact.

## What has landed

The read-only slice: `internal/tofuprovider` with the provider block, the `translate` boundary, the `converger` seam and its fake, and one data source -- `cargoship_cluster_facts` -- that runs the five read-only phases and reports what each host says. `cmd/terraform-provider-cargoship` serves it, `mage release:tofuProviderDev` builds it into a `filesystem_mirror` to run `tofu` against, and [docs/dev/tofu-provider.md](../dev/tofu-provider.md) is how to do that.

Three things the first slice found, which the plan below did not anticipate:

- **`0.0.0` is not a usable version.** OpenTofu refuses it as the reserved "not published" version, so a development build has to be `0.1.0` or higher.
- **`tofu init` pins the binary's checksum**, so a rebuilt provider fails the next plan until `.terraform.lock.hcl` is deleted and `init` re-run. That is the development loop, not a bug.
- **The connect phase retries for ten minutes**, which is right for an apply and wrong for a plan. The provider bounds every read at `connect_timeout` (one minute by default) and says so when it gives up. An authentication failure is retried the same way a refused connection is, so a wrong key also takes the full timeout -- worth fixing in `pkg/phase/07_connect.go` rather than in the provider, since the CLI has the same problem.

## Order of work

Five layers, stacked on #609 (the stack is linear and these all depend on the package existing):

1. **`pkg/action` constructors return an error** plus `KubeConfigOptions{Write, Return}` and the refresh action. Core only, no provider code, testable on its own.
2. **Provider skeleton and `translate.go`.** `provider.Serve` wired, schema declared, translate covered by unit tests against fixtures under `testdata/`. No CRUD yet.
3. **`Create`/`Read`/`Delete`** against a real cluster, plus the tflog adapter.
4. **`ModifyPlan`**: the downgrade and removed-host checks, which are pure state comparisons and unit-testable.
5. **Acceptance tests** with `terraform-plugin-testing`, gated behind an env var, driving the bootloose cluster the e2e suite provisions. This is where a provider is actually proven, and it needs a tofu binary on the runner.

## Verification

```sh
go build ./... && go test -short ./internal/tofuprovider/...     # unit: translate, plan checks
go test -short ./pkg/action/... ./cmd/...                        # the core changes in layer 1
mage dev:vendor && go run ./magefiles/core dev:verifyVendor      # layer 2's re-vendor
mage test:endToEndClusterStage                                   # the refresh action against hosts
TF_ACC=1 CARGOSHIP_TF_ACC=1 go test ./internal/tofuprovider/...  # layer 5, needs tofu + a cluster
pre-commit run --all-files
```

Manual check once layer 3 lands: build the provider, drop it into a `filesystem_mirror`, and run `tofu apply` against the bootloose inventory the e2e suite writes.

## Open question to settle in layer 2, not now

Whether `package` accepts an OCI reference as well as a path. `distro.Load` takes both, so it costs nothing in code -- but a reference means the management node pulls at apply time, which is the one thing an airgapped operator cannot do. It may be better refused in the schema than supported and documented as a trap.
