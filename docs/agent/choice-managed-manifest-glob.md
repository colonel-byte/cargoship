# Why the engine's manifest directory is pruned through a glob

Cargoship prunes the directories it owns. `StaleFiles` (`types/distrocfg/managed_files.go`) lists every file under each of a distro's `ManagedDirs` and removes the ones the current configuration no longer asks for, so a registry CA certificate dropped from a cluster inventory does not sit on every node forever. Pruning runs while a node is stopped and about to be restarted, which is the one moment a file can be removed without the engine noticing it go.

That works because a managed directory holds only files cargoship put there. Two of the three qualify outright: `/etc/cargoship/tls` and the state directory were created by cargoship and are cargoship's to empty. The third is not cargoship's at all.

## The manifest directory is shared

`<data>/server/manifests` belongs to the engine. RKE2 and K3s ship their own manifests there -- the CNI chart, CoreDNS, metrics-server, the ingress controller -- and lay them down again on startup. Cargoship writes into the same directory because that is where a `HelmChartConfig` has to live for the engine's helm controller to read it: the values a package's `spec.config.engine.manifest` subtree produces become one `<chart>-config.yaml` per chart (`types/distrocfg/rancher_common.go`).

Pruning that directory the way the other two are pruned would delete every manifest the engine put there on the first sync, which takes the cluster apart. Not pruning it at all would leave a `HelmChartConfig` behind for a chart a package no longer configures, and the helm controller would keep reconciling the chart to values nobody asks for any more -- the exact failure `StaleFiles` exists to prevent, on the files where it matters most, since these are the only files cargoship writes that something else then acts on.

## The glob

`ManagedDir` therefore carries an optional `Glob`, and the manifest directory sets it to `*-config.yaml` -- the same suffix cargoship names its own `HelmChartConfig` files with. An empty glob means the whole directory is cargoship's, which is what the other two use.

The suffix is the whole mechanism for telling the two sets of files apart, so it is a single constant, `helmChartConfigSuffix`, used both by the writer and by the glob. A rename in one place that missed the other would either start pruning the engine's files or stop pruning cargoship's.

## Alternatives considered

**A separate directory for cargoship's manifests.** There is no such thing: the helm controller watches one path, and a `HelmChartConfig` outside it is inert.

**A marker inside each file, read before deleting.** Pruning would then have to read every file in the directory over SSH on every sync, on every node, to decide about a handful of them. The name already carries the same information, and it carries it without a read.

**Recording what was written, as the ufw backend does.** The ufw firewall backend keeps a state file of the rules it applied, because ufw cannot express "replace everything I own". Here the desired set is already in hand at prune time -- `filesFor(host)` is what drift detection just compared against -- so a state file would add a thing to keep in sync for no new information.

**A prefix instead of a suffix.** A prefix would have to be cargoship's own, which would rename every `HelmChartConfig` the project has ever written and orphan the files already on upgraded nodes. The suffix is what the files are already called.

A malformed pattern matches nothing rather than everything (`ManagedDir.prunes`). Pruning is cleanup, and cleanup is not worth deleting something on a guess.

See [package-values](../guides/package-values.md) for how a package produces the `HelmChartConfig` files this glob scopes pruning to.
