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

A node is matched to a host by any identifier they share: the node's name or any address it reports, against the host's hostname, private address, or connection address. That is deliberately generous, since a false positive would stop an apply on a working cluster -- it means a node whose name you overrode with `node-name` in the engine config still matches its host through its address. If the cluster cannot be reached at all, the check warns and the apply continues, because failing to reach it is not evidence that a host was removed. The node list is read with `kubectl` on a controller over the existing SSH connection, so the check works on a cluster whose firewall admits nothing but SSH from outside.

## Why apply refuses instead of reconciling

Removing a node properly is two jobs, and an apply can only do one of them.

Draining a node and deleting its `Node` object needs the node's name, which the API server will happily give us. Uninstalling the engine -- stopping the service, removing the packages, deleting the data directory and certificates -- needs an SSH connection to the machine, and the address, user and key for that lived in the `hosts` entry you just deleted. An apply has no memory of previous runs, so it has no way to reach a machine the config no longer describes.

Doing only the first half would be worse than doing nothing, because it looks like the removal worked. The machine keeps its certificates and keeps trying to rejoin, and if it was a controller it stays an etcd member: the cluster looks smaller while quorum is still counted against a member nobody can reach.

So an apply reports the gap and leaves the cluster alone.

## Removing a host in OpenTofu

In OpenTofu, mark the host as `state = "absent"` in your `cargoship_cluster` resource:

```hcl
resource "cargoship_cluster" "prod" {
  # ...

  hosts = {
    controller1 = {
      address  = "10.0.0.11"
      role     = "controller"
      key_path = "~/.ssh/id_ed25519"
      profile  = "control"
    }

    # worker3 is marked for removal:
    worker3 = {
      address  = "10.0.0.31"
      role     = "worker"
      key_path = "~/.ssh/id_ed25519"
      state    = "absent"
    }
  }
}
```

One `tofu apply` does the whole removal:

1. The node is drained and deleted from the cluster, through a controller that is staying. The run refuses rather than guessing if every running controller is itself marked absent -- removing all of them is a teardown, and `tofu destroy` is what does that.
2. Cargoship connects to `10.0.0.31` over SSH, stops the engine and uninstalls it.
3. Every other host is left alone, and the apply then converges them as usual.
4. The host's `removed` attribute becomes `true`.

**Leave the entry where it is.** `removed` is what tells later applies the work is done, so they neither repeat the teardown nor try to reach a machine that may since have been decommissioned. Delete the entry whenever it suits; nothing depends on it going, and deleting it changes nothing on any host.

If the removal fails, the apply stops there and says so. The entry stays in state with `removed` still false, which is deliberate: the machine is still running and still joined, and that entry holds the only address and key path that can reach it. Fix the cause and apply again. The rest of the convergence does not run, because converging on top of a half-removed cluster would report success over a node that is still in it.

### What a destroy does with the markers

Nothing, beyond reading them. `tofu destroy` tears down everything the cluster still holds: a host marked absent whose removal failed is included, because the machine is still running and still joined, and one whose removal finished is left out, because reaching for a decommissioned machine would fail. The markers are not something to clean up before destroying.

If the machines are OpenTofu resources too and are being destroyed in the same run, consider `retain_on_destroy = true` -- draining and uninstalling a node that is about to be deleted is minutes spent on a result nobody sees. See [opentofu-terragrunt](opentofu-terragrunt.md#destroying-the-machines-as-well).

## Removing the machine by hand (CLI)

With the CLI, use targeted reset with `--target-hosts`:

```console
$ cargoship reset --config ./cargoship-config.yaml --distro rke2 --target-hosts worker3 --confirm
```

Cargoship automatically selects a surviving running controller as the leader to delete `worker3` from the cluster, then uninstalls the engine only on `worker3`, leaving your surviving controllers and workers untouched. After the targeted reset finishes, delete the host block from `cargoship-config.yaml`.

Removing a controller is the same command with more rules attached, and a run that targets two of them is refused rather than left to etcd. See [Removing a controller](#removing-a-controller).

## Removing a controller

A worker leaves the cluster by being drained, deleted and uninstalled. A controller does that and also gives up an etcd member, so the order matters and the run is more constrained.

**Cargoship stops the engine on the target before deleting its node.** That is what makes the deletion stick: k3s and RKE2 remove the etcd member when the Node object is deleted, but only once the engine behind it has stopped, and an engine that is still running re-registers the node seconds later. The one exception is a removal that leaves a single controller, where the member being removed has to still be up to form the majority of two that commits its own removal -- there the engine stays running through the deletion and is uninstalled immediately after.

**One controller per run.** A run that keeps the cluster and targets two running controllers is refused. Each removal is a raft reconfiguration that needs a majority of the membership it starts from; the second one in a run would start from three members with one already stopped, and taking the next one down leaves one of two, which hangs with the cluster unavailable rather than failing cleanly. Removing them in separate runs is safe, because the cluster is whole again at the start of each.

### Removing the first controller

There is nothing special about the first controller at runtime -- it is the first in the host list, re-elected on every run, and it holds no state the others do not. Three things point at it by name, and only one of them needs attention:

| Points at                                | Removing the first controller                                                                                                                                     |
| :--------------------------------------- | :---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| The kubeconfig Cargoship writes          | Unaffected. Its server is `https://<load_balancer>:6443`, never a node address, so admin access survives any single controller                                    |
| Workers                                  | Unaffected. They join through `https://<load_balancer>:9345` for the same reason                                                                                  |
| The other controllers' `config.yaml`     | `server:` names the first controller, written when they joined. A running controller never reads it -- it is the join address, used at startup -- so the cluster keeps working, and the next restart is what would fail |

So the step that makes it safe is re-rendering `config.yaml` on the controllers that stayed, which `cargoship apply` does: it re-elects the first controller from the remaining hosts and writes the new `server:` to every one of them. The engines are not restarted for it, which is the point -- the file is correct again before anything reads it.

In OpenTofu this is already one operation. `tofu apply` removes the host and then converges what is left, in that order, in the same run.

With the CLI, the targeted reset and the apply are two commands, and the apply is not optional:

```console
$ cargoship reset --config ./cargoship-config.yaml --distro rke2 --target-hosts controller1 --confirm
$ cargoship apply ./package.tar.zst --config ./cargoship-config.yaml --confirm
```

Between them the surviving controllers hold a join address that no longer resolves to a machine. They keep serving, and a reboot in that window is what turns it into an outage.

Two things Cargoship does not do for you:

- **The load balancer.** `load_balancer` is a name you own -- a VIP, a DNS record, an HAProxy backend list. If it still routes to the controller you removed, a share of every API call fails. Update it in whatever manages it.
- **A quorum check against live etcd.** The refusals above count controllers from the configuration and from whether their engine answers, not from etcd's own view of the membership. Removing a controller from a cluster that is already one member down is a removal from a membership that cannot commit it, and nothing here will say so. Confirm the cluster is healthy first.

## When the extra nodes are deliberate

A cluster can hold nodes Cargoship never joined, and refusing on those would block every apply on a working setup. Pass `--allow-unmanaged-nodes` to downgrade the refusal to a warning:

```console
$ cargoship apply ./package.tar.zst --config ./cargoship-config.yaml --allow-unmanaged-nodes --confirm
```

It is also available as `distro.allow_unmanaged_nodes` in the Cargoship config file, and as `DISTRO_DISTRO_ALLOW_UNMANAGED_NODES` in the environment. Cargoship still leaves those nodes alone either way; the flag only chooses whether their presence is an error or a log line.
