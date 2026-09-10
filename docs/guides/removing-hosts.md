# Removing a Host from the Cluster

This guide explains what happens when you delete a host from your Cargoship config, why an apply stops rather than reconciling the removal, and how to actually remove the machine.

## What an apply does with a removed host

Nothing, and that is deliberate. Every phase after the fact-gathering ones reads `.spec.hosts` and acts on what it finds there, so a host you delete from the config becomes invisible to all of them. The machine keeps running the engine and stays joined to the cluster while the apply that was supposed to remove it reports success.

An apply catches this instead of walking past it. Right after it gathers facts, and before it changes anything, it lists the nodes joined to the cluster and compares them to the hosts in the config. If the cluster holds a node no host accounts for, the apply stops:

```
the cluster holds nodes the config does not: add the host back to the config,
run `cargoship install reset` against it, or pass --allow-unmanaged-nodes to
apply anyway: worker3 (worker)
```

Each node is named with its role, because a leftover controller is the more dangerous case. The check is read-only, so `--dry-run` runs it too, which is where you want to find out about a removal you did not intend.

A node is matched to a host by any identifier they share: the node's name or any address it reports, against the host's hostname, private address, or connection address. That is deliberately generous, since a false positive would stop an apply on a working cluster -- it means a node whose name you overrode with `node-name` in the engine config still matches its host through its address. If the cluster cannot be reached at all, the check warns and the apply continues, because an unreachable API server is not evidence that a host was removed.

## Why apply refuses instead of reconciling

Removing a node properly is two jobs, and an apply can only do one of them.

Draining a node and deleting its `Node` object needs the node's name, which the API server will happily give us. Uninstalling the engine -- stopping the service, removing the packages, deleting the data directory and certificates -- needs an SSH connection to the machine, and the address, user and key for that lived in the host block you just deleted. An apply has no memory of previous runs, so it has no way to reach a machine the config no longer describes.

Doing only the first half would be worse than doing nothing, because it looks like the removal worked. The machine keeps its certificates and keeps trying to rejoin, and if it was a controller it stays an etcd member: the cluster looks smaller while quorum is still counted against a member nobody can reach.

So an apply reports the gap and leaves the cluster alone. The layer that will eventually close it is the OpenTofu provider, which still holds the removed host's connection details in its state file when it plans the destroy, and can hand them to a targeted teardown.

## Removing the machine by hand

Removing a node is two steps, and they have to happen in that order. `install reset` uninstalls the engine, but it cannot delete the node from the cluster unless the config it is given also contains a controller that is still running -- and `UninstallEngine` acts on every host in that config, so adding one would uninstall the engine from your working controller too. Delete the node first, then reset the machine.

1. From a controller that is staying, drain and delete the node:

   ```console
   $ kubectl drain worker3 --ignore-daemonsets --delete-emptydir-data
   $ kubectl delete node worker3
   ```

2. Copy your config and cut `.spec.hosts` down to the host being removed, leaving the rest of the cluster settings as they are.

3. Run reset against it. With the node already gone from the cluster, the deletion phases have nothing to do and the uninstall is all that remains:

   ```console
   $ cargoship install reset ./package.tar.zst --config ./removal-config.yaml --confirm
   ```

4. Delete the host block from your real config.

5. Run `cargoship apply` as usual. The check now finds nothing left over.

Remove controllers one at a time, and only while the remaining controllers still form a quorum. Removing two of three at once leaves etcd unable to elect a leader, and no amount of applying will fix that from the outside.

## When the extra nodes are deliberate

A cluster can hold nodes Cargoship never joined, and refusing on those would block every apply on a working setup. Pass `--allow-unmanaged-nodes` to downgrade the refusal to a warning:

```console
$ cargoship apply ./package.tar.zst --config ./cargoship-config.yaml --allow-unmanaged-nodes --confirm
```

It is also available as `distro.allow_unmanaged_nodes` in the Cargoship config file, and as `DISTRO_DISTRO_ALLOW_UNMANAGED_NODES` in the environment. Cargoship still leaves those nodes alone either way; the flag only chooses whether their presence is an error or a log line.
