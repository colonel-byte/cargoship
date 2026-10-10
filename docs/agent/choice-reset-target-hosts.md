# Why a targeted reset names its hosts separately from the hosts that provide access

`cargoship reset --target-hosts` removes named hosts from a live cluster and leaves the rest alone. The gap it closes is recorded in [#339](https://github.com/colonel-byte/cargoship/issues/339): reset was all-or-nothing by construction, and the two shapes available to an operator who wanted one node gone were both wrong.

A configuration holding only the host being removed has no controller, so `DeleteCommon.Prepare` finds no leader, `DeleteWorkers` and `DeleteControllers` both report `ShouldRun() == false`, and the node is never deleted from the cluster -- while `UninstallEngine` runs regardless and takes the engine off anyway. That is an orphaned `NotReady` node object. Adding a live controller to supply the leader fixes the deletion and uninstalls the engine from the controller too, because `UninstallEngine.Prepare` set `p.hosts` to the whole host list with no filter. That is a destroyed control plane.

Neither was a defect. The unfiltered list is exactly right when the instruction is "tear this cluster down", which is what `reset` had always meant. What was missing was a way to say which hosts an action acts *on*, separately from which hosts it reaches the cluster *through*.

## Two host lists, not one

`TargetHosts` is that second list. The delete phases and `UninstallEngine` act on it; leader selection keeps scanning the full configuration. So a controller can supply cluster access without being torn down, which is the one thing neither earlier shape could express. An empty target list means every host, so `reset` with no flag behaves exactly as it did.

Hosts are matched generously -- hostname, private address, or connection address -- for the same reason the removed-host check in `apply` is generous: an operator naming a host by the address they SSH to it on, when the configuration calls it by hostname, has not made a mistake worth failing over.

## Three refusals, each replacing a silent half-change

The natural behaviour in each of these cases is a no-op or a partial teardown that reports success, which is worse than stopping.

| Refused | What would otherwise happen |
| :------ | :-------------------------- |
| A `--target-hosts` entry matching no host | Every phase filters down to nothing and the reset reports success having removed nothing. A typo is the ordinary route into this, so it is checked in `NewReset` -- before a single connection is opened -- rather than in a phase |
| Every running controller targeted, some host kept | Nodes drained and deleted through a controller the same run is about to uninstall, with the outcome depending on the order the hosts happened to be in. Removing all of them is a teardown, and plain `reset` is what does that |
| More than one running controller targeted in one run | The second removal starts from a membership with a member already stopped, and hangs with the cluster unavailable. One run per controller |

The second and third are about etcd membership rather than about cargoship, which is why they are refusals and not retries: there is no ordering of the work that makes either safe.

## The engine stops before the node is deleted, except once

k3s and RKE2 drop the etcd member when the `Node` object is deleted, but only once the engine behind it has stopped -- an engine still running re-registers its node seconds later and the deletion does not stick. So the engine comes down first.

The exception is a removal that leaves a single controller behind. There the member being removed has to still be up to form the majority of two that commits its own removal, so the engine stays running through the deletion and is uninstalled immediately after. `keepEngineForDelete` is that case, and it is the reason the ordering is decided in `Prepare` from the controller membership rather than being fixed.

## What is not covered

The refusals count controllers whose engine answers, not etcd's own membership, so a cluster that is already one member down looks whole to them. A quorum check against etcd would be the stronger test and is not here.
