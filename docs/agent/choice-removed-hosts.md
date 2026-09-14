# Why apply refuses a removed host instead of reconciling it

Deleting a host block from a cargoship config does not remove that machine from the cluster. Apply detects the leftover node and stops (`src/pkg/phase/13_detect_removed_hosts.go`); it does not drain it, delete it, or uninstall its engine. Reconciling the removal was the obvious alternative, and it is what every other declarative tool in this space does, so the choice needs an argument.

## Apply cannot finish the job

Removing a node is two pieces of work. Deleting the `Node` object needs the node's name, which the API server will give us. Uninstalling the engine -- stopping the service, removing the packages, deleting the data and config directories -- needs an SSH connection, and the address, user and key for that lived in the host block that was just deleted. Apply holds no memory of previous runs, so it has no way to reach a machine the config no longer describes. `UninstallEngine` is not hard to call here; it is impossible to call, because there is nothing to connect to.

Doing only the first half is worse than doing nothing. A node deleted from the API but left running keeps its certificates and keeps trying to rejoin, and for a controller `kubectl delete node` does not remove the etcd member: the cluster looks smaller while quorum is still counted against a machine nobody can reach. The failure mode of stopping is a visible leftover node and a message naming it. The failure mode of doing half is an apply that reports success and a cluster that loses quorum on the next controller restart.

## Absence is the wrong trigger for a destructive operation

Adding a host that is already there is idempotent. Removing a host that should have stayed is not. A typo in a hostname, a bad merge, a templating bug that drops a list entry -- under reconcile each of those becomes a drained and destroyed node, and under refuse each becomes a stopped apply with a message. The blast radius of a false positive is not symmetric, so the default should not be either.

## The old state is not a substitute for a connection

The tempting way to supply the missing connection details is to take them from the provider's prior state and generate a small config to run `install reset` against. It should not be built.

A state file records what was true after the last successful apply, and reset is destructive. Between then and now the machine may have been rebuilt, re-addressed, or returned to a DHCP pool, and none of that produces a diff the provider would see, because the resource is being removed and there is nothing left to refresh against. The address is the least stable identifier a host has and the one reset would connect through, so the target becomes whatever machine currently answers there. Other kinds of state drift surface as a plan diff somebody reads; this one surfaces as a wiped machine.

The generated config would also splice two points in time. The host comes from the old state, but the paths an uninstall removes come from the distro module: `CleanupPaths()` returns the data and config directories as the *current* configuration defines them. Upgrade the cluster or move the data directory and reset cleans today's paths on a machine running yesterday's engine, leaving the real files behind and removing whatever sits at the new ones.

Making removals depend on state would also settle the separate question of keeping SSH key material out of tofu state (#309) the wrong way, by making it unsolvable.

## The phases cannot express a subset anyway

Independent of where the contents come from, there is no config that makes `install reset` remove one host from a live cluster. `DeleteCommon.Prepare` finds its leader by scanning `Spec.Hosts` for a host already running the controller service, so a config holding only the removed host has no leader: both delete phases report `ShouldRun() == false` and the node is never deleted, while `UninstallEngine` runs anyway. Adding a live controller so a leader can be found is worse, because `UninstallEngine.Prepare` sets `p.hosts = p.manager.Config.Spec.Hosts` with no filter and uninstalls the engine from it too.

Neither is a bug in reset. The unfiltered list is exactly right when the instruction is "tear down this whole cluster". The gap is that no action separates the hosts it targets from the hosts that supply cluster access, which is tracked as #339.

## What the check actually does

`DetectRemovedHosts` runs after fact gathering and before anything is changed, lists the nodes joined to the cluster, and stops the apply when the cluster holds a node no host accounts for, naming each one with its role.

The list is read by running `kubectl get nodes -o json` on the leader over the existing SSH connection, which is how every other cluster command in the phases works. `LabelNodes` is the exception: it builds a Kubernetes client dialled at `https://<loadBalancer>:6443` from the machine running cargoship. That was not copied here. A cluster whose firewall admits only SSH from outside is an ordinary deployment for cargoship, and a check that runs on every apply must not be the one thing that needs the API port open. Going through the leader also drops the need for the load balancer address and for reading the admin certificates.

Two guards keep it from becoming a nuisance, and both exist because a false positive would stop an apply on a working cluster:

- Nodes are matched to hosts by any shared identifier: the node's name or any address it reports, against the host's hostname, private address, or connection address. Names alone are not enough, because an operator can set `node-name` in the engine config, and matching on names would then report every node in the cluster as removed.
- Only positive evidence stops the run. Failing to reach the cluster says nothing about whether a host was removed, so a failure to list the nodes warns and continues.

The phase is read-only, so a dry run executes it. That is the run where being told about a host you deleted from the config costs nothing.

`--allow-unmanaged-nodes` downgrades the refusal to a warning, for clusters holding nodes cargoship never joined. Cargoship leaves those nodes alone either way; the flag only chooses whether their presence is an error or a log line.

## What would justify revisiting

Not a state file, and not inferring removal from absence. The variant that survives every argument above is a tombstone: the host stays in the config for one apply, marked for removal, so the connection details are present and current rather than remembered, and the operator is editing a file that says in the present tense which machine is to be destroyed. OpenTofu's own `removed` block is precedent for stating a removal explicitly rather than inferring it.

What that needs from the phases is exactly what #339 builds -- act on this subset, use those other hosts for access -- and a subset reset does both halves on the target, so the orphaned etcd member stops being a hazard. If #339 lands, the remaining work is a per-host config marker, a quorum guard before a controller is removed, and a decision on when the marker is cleaned up. This check stays either way, as the backstop for a host deleted outright rather than tombstoned.
