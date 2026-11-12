# The Cluster API provider lives in another repository, and what it needs from this one

Cargoship can stand up the Kubernetes API for a Cluster API cluster instead of kubeadm. The provider that does it is [colonel-byte/cluster-api-provider-cargoship](https://github.com/colonel-byte/cluster-api-provider-cargoship), which consumes this module as a library -- the same arrangement as [the OpenTofu provider](choice-tofu-provider-removal.md), and for the same reason: the phases a reconcile runs are the phases the CLI runs.

It is one repository holding both halves of the CAPI contract, as `rancher/cluster-api-provider-rke2` is: a bootstrap provider (`CargoshipConfig`, `CargoshipConfigTemplate`, in `bootstrap.cluster.x-k8s.io`) and a control-plane provider (`CargoshipControlPlane`, in `controlplane.cluster.x-k8s.io`). Rancher Turtles needs nothing cargoship-specific; it imports any conformant CAPI cluster.

## Why it was never a package of this module

The reasoning that kept the OpenTofu provider here for as long as it stayed does not apply. That provider was one `main` and a package the CLI did not import, so the linker loaded none of it and the CLI paid nothing. A CAPI provider is two long-running controller binaries that link `sigs.k8s.io/cluster-api` and `sigs.k8s.io/controller-runtime`, and between them those pull `k8s.io/api` in full -- the tree [choice-dynamic-kube-client](choice-dynamic-kube-client.md) measures at all 59 API groups and prices at roughly 24MB, which is why the phases here use the dynamic client and `clientcmd` rather than the typed clientset.

Keeping that in the module would put it in `go.mod` and in `vendor/`, which an air-gapped management node stages by hand, for a binary that node never runs. `cmd/cargoship/deps_test.go` holds the two prefixes that keep them out of the CLI's import graph, next to the `terraform-plugin` entry that does the same job. A pattern matching nothing is the state those entries should be in.

## What exists here for it, and is load-bearing despite having no in-tree caller

- **`pkg/action.NewManager`** and **`ManagerOptions`**. Every entry point in `pkg/action` takes a `*phase.Manager`, and the *ordering* of its assembly -- merge and schema-check the values, `ApplyValues`, `RenderFiles`, then hand the resolved values to the manager -- used to be an unexported helper in `cmd/` with a second copy in the e2e harness. The two had already drifted. An out-of-tree caller could reach neither, and could not have inferred the ordering from the parts: each step is an exported method on the layout, and nothing in their signatures says they must run in that order, once, before the manager is built. Getting it wrong is silent -- an install writes a flavor whose engine config still holds raw `{{ ... }}`, which reaches the host as invalid YAML.
- **`pkg/action.DistroModule`**. Was unexported, with a third copy in the e2e harness. The provider needs it to build an individual phase; exporting it deleted the copy.
- **`cluster.Cluster` being a plain struct with exported fields.** The provider builds its inventory in memory from the CAPI `Machine` objects that currently have an address, rather than from a file. Nothing in `pkg/action` takes a path, and that should stay true.
- **`ApplyOptions.NoKubeConfigFile`, `KubeConfigOptions.NoWrite`, `KubeConfig.Bytes` and `KubeConfig.Config`.** Added for the OpenTofu provider and reused unchanged: a controller publishes the admin credentials as the `<cluster>-kubeconfig` secret CAPI expects, and must not merge them into a `~/.kube/config` that does not exist in its pod.
- **`pkg/action.NewRefresh`**, the read-only half of an apply, for the same reason the OpenTofu provider has it.
- **`test/microvm` and the root `test` package** stay importable. The provider's end-to-end suite drives a real fleet over SSH, and CAPD containers run no sshd, so the microvm backend is the only one a real converge can be tested against. `CARGOSHIP_E2E_EXAMPLE_ROOT` points that suite at the generated distro definitions in a cargoship checkout.

## The two things it does not get, on purpose

- **`internal/receipt`.** The receipt is a projection of `phase.RunResult` plus the local username and hostname, and the last two are meaningless in a pod. A controller writes custom-resource status from `phase.RunResult` directly -- `Changed`, `Complete`, `Signal`, `HostSummaries`, `Warnings` are all exported for it. No apply path writes a receipt; only `cmd/receipt.go` does.
- **`internal/clustercfg.Keyring`.** It is the type of the exported `ApplyOptions.Keyring` field, so an external module can omit the field and apply works -- `internal` restricts imports, not keyed struct literals -- but it cannot *build* a keyring. The provider therefore cannot apply a package whose registry credentials are age- or vault-encrypted. Moving `Keyring`, `ResolveKeyring` and `DecryptRegistryAuth` to `pkg/clustercfg` is what would lift that, and it is a change with its own blast radius rather than a detail of this one.

## There is no in-memory SSH key field, and none is needed

A controller holds its key in a `Secret`, and rig's `protocol/ssh.Config.KeyPath` is a path. The field that answers this is already there and is easy to miss, because it is excluded from YAML and JSON and so appears in no generated schema: `Config.AuthMethods []ssh.AuthMethod`. When it is set, rig's connection short-circuits the whole identity-file path, so the provider fills it with `ssh.ParseSSHPrivateKey(keyBytes, nil)` per host before `Host.Connect` and the key never touches a filesystem. Nothing in this repo had to change for it, and nothing should be added -- an exported key-bytes hook here would be a second way to do what rig already does.
