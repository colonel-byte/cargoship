# How a host is removed through the OpenTofu provider

[choice-removed-hosts](choice-removed-hosts.md) decided that an apply refuses a removal rather than reconciling one, and named the single variant that survives every argument against it: a **tombstone**, where the host stays in the configuration for one apply with a marker saying to remove it. That is what `state = "absent"` is, and [#339](https://github.com/colonel-byte/cargoship/issues/339) built the thing it needed from the phases -- a reset that acts on a named subset while driving the node deletions through a controller that stays.

This records the decisions taken while wiring the two together, because most of them are about failure rather than about the happy path, and each one is a shape the obvious code gets wrong.

## Why the marker and not the missing block

Deleting a `host` block deletes the address, the user and the key path with it. The provider still has them in prior state for one plan, and driving a teardown from that is the thing `choice-removed-hosts` argues against at length: a state file records what was true after the last apply, reset is destructive, and the address is the least stable identifier a host has. The tombstone keeps the machine described in the present tense while it is being removed.

What the earlier document could not settle was "a decision on when the marker is cleaned up after the apply that acted on it". That is the next section.

## The marker stays, and `removed` is why

The first implementation pruned the host from state once it was torn down, so the operator had to delete the block afterwards. That is one edit, one apply, one more edit -- and if they forgot the last one, the configuration and the state disagreed on every subsequent plan, each of which would attempt the teardown again. Attempting it again means reaching a machine that has been removed, which works until the machine is decommissioned and then fails forever.

So the block stays, and a computed `removed` attribute records that the work is done. `Update` reads the prior state, skips any host already recorded as removed, and the configuration can keep the tombstone indefinitely. Deleting the block is tidying, not a step.

The cost is a marker in the configuration that outlives its meaning. It is a smaller cost than a plan that never converges.

## A failed removal keeps everything

Three rules, all learned from the same mistake:

- **The host stays in state.** Pruning it after a failed teardown leaves a machine that is still running and still joined, described nowhere -- and the block that was pruned held the only address and key path that could reach it. That rebuilds, from inside the provider, exactly the dead end `choice-removed-hosts` exists to prevent.
- **The failure is a diagnostic, not a warning.** `tflog` output is invisible without `TF_LOG`, so a warning meant an apply that printed "Apply complete!" over a cluster that still held the node.
- **The apply stops.** Converging on top of a half-removed cluster -- drained and deleted but not uninstalled, or the reverse -- is the other way to report success over a broken state.

## What a destroy does with the markers

It reads them and tears down what is still there. A host marked absent whose removal **failed** is included, because the machine is still running; one whose removal **finished** is excluded, because reaching for a decommissioned machine would fail. The markers are not something to clean up before destroying.

This is deliberately not the list an apply uses. An apply converges the hosts that are staying, so it drops every absent host; a destroy has to act on what exists. Taking the apply's list for a destroy walks past the one machine a failed removal left behind.

## Refusals, and why each one is not pedantry

Each of these is a case where the natural behaviour is a silent no-op or a quiet half-change:

| Refused                                                  | What would otherwise happen                                                                                                 |
| :------------------------------------------------------- | :-------------------------------------------------------------------------------------------------------------------------- |
| A `--target-hosts` entry naming no host                  | Every phase filters down to nothing and the reset reports success having removed nothing. A typo is the ordinary route      |
| Every running controller targeted, some host kept        | Nodes drained and deleted through a controller the same run is about to uninstall, with the outcome depending on host order |
| Every host marked `absent`                               | The whole cluster reset through the apply path, then a translation failure for a cluster with no hosts                      |
| More than one running controller removed in one run      | The second removal starts from a membership with a member already stopped, and hangs with the cluster unavailable           |

The third is raised twice -- once from `ModifyPlan`, once during the apply -- because a saved plan is applied later and the apply is also where hosts already recorded as removed are known. It is a destroy written the long way, and an apply cannot do the part that matters: dropping the resource. It points at `tofu destroy`.

## The limit worth knowing: `retain_on_destroy` is the wrong shape

When the machines are OpenTofu resources too, a `tofu destroy` deletes them as well, and the cluster teardown is minutes of draining and uninstalling nodes that are about to cease to exist. `retain_on_destroy = true` skips it.

But it is a **resource attribute**, so it is decided when the configuration is written, while "am I deleting the machines in this run?" is a property of the run. OpenTofu gives a resource no signal about what else is being destroyed -- there is no "the thing I depend on is also going" in the provider protocol -- so the provider cannot infer it, and a `-destroy`-time flag does not exist.

What is left is to make the attribute a variable, so the decision is a `-var` rather than a code edit:

```hcl
variable "retain_cluster_on_destroy" {
  type        = bool
  description = "Leave the cluster running when the resource is destroyed. Set it when the machines are being destroyed in the same run: draining and uninstalling a node that is about to be deleted is work nobody sees the result of"
  default     = false
}

resource "cargoship_cluster" "prod" {
  # ...
  retain_on_destroy = var.retain_cluster_on_destroy
}
```

```sh
tofu destroy -var retain_cluster_on_destroy=true
```

That is documented rather than solved, and it is the honest state of it.

## Removing the first controller is a configuration problem, not an etcd one

The membership arithmetic above is the part that looks dangerous, and it is handled in the phases. What is specific to the *first* controller is a file: every other controller's `config.yaml` carries `server: https://<first controller>:9345`, written by `ConfigureEngine` when it joined (`types/distrocfg/rancher_common.go`), while workers and the kubeconfig both point at the load balancer instead.

A running controller never reads that key -- it is the join address, read at startup -- so removing the machine it names breaks nothing until something restarts. `ConfigureEngine` re-renders it on every apply from the remaining hosts, with no restart, so the fix is an apply rather than new code. `EngineConfigSync` does not cover it: its desired-file set is registries, audit, PSS and chart manifests, and `config.yaml` is not in it.

That is why the provider does removals and then convergence in one `tofu apply`, and why the CLI guide tells an operator to follow a targeted reset with an apply rather than treating the reset as finished. The window between the two is the only time the cluster holds a join address that resolves to nothing.

## What is not done

- **Only one plan-time check.** `ModifyPlan` raises the emptied cluster, which is the one refusal visible from the configuration alone. The others -- a target naming no host, every running controller targeted, more than one controller in a run -- all depend on which engines are answering, and a plan does not connect. They stay apply-time.
- **No e2e coverage of a targeted reset.** [#339](https://github.com/colonel-byte/cargoship/issues/339)'s own "done when" asks for a named worker and a named controller removed from a live cluster, with a host used only for leader selection keeping its engine. The unit tests cover the filtering and the refusals; nothing yet proves it against bootloose.
- **A quorum guard that counts hosts, not etcd members.** A run that keeps the cluster and targets more than one running controller is refused (`ErrControllersNotOneAtATime`), and the removal that leaves a single controller keeps its engine up through the node deletion so the member can commit its own removal. Both count controllers whose engine answers, which is not the same as etcd's view of the membership: a cluster already one member down looks whole to this check. Asking etcd directly needs an etcdctl path onto a controller, which no phase has today.
