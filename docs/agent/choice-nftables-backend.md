# Why the nftables backend shells out to `nft` and owns a single table

Cargoship's firewall support (`cargoship apply --firewall`) renders a backend-neutral `firewall.Plan` onto whichever firewall a node runs. `firewalld` and `ufw` cover Enterprise Linux and Debian-family hosts that use a front end. The `nftables` backend (`src/pkg/firewall/nftables.go`) covers the rest: Debian and Arch nodes configured by hand, and the minimal or immutable images such as CoreOS and Flatcar that ship no firewall front end at all. Several design choices there are not obvious from the code, and each had an alternative that was considered and rejected.

## Shelling out to `nft` rather than talking netlink

[`github.com/google/nftables`](https://github.com/google/nftables) is the native Go netlink client for the same subsystem, and it would give typed rule construction instead of string rendering. It was not used:

- **It runs in the wrong process.** Cargoship configures hosts over SSH; every other backend, and every other host-modification phase, works by executing a command on the remote node through `rig`. A netlink library would only configure the *local* machine, so using it would mean shipping and bootstrapping a helper binary onto every node -- a distribution problem cargoship does not otherwise have.
- **New dependency for no gain.** `google/nftables` is not in `go.sum` at all today, and it pulls `mdlayher/netlink` and `vishvananda/netns` with it. `nft` is already present on any host that qualifies for this backend, since `Detect` requires it.
- **The rendered ruleset is the auditable artifact.** Cargoship writes `/etc/cargoship/nftables.nft` and loads it. An operator can read exactly what cargoship intends to apply, diff it between runs, and load it by hand. A netlink client leaves nothing behind to read.

The cost is that rule construction is string formatting, so rendering bugs are not caught by the compiler. That is why `Apply` runs `nft -c -f` before `nft -f`: `-c` parses and validates without loading, so a malformed ruleset fails before it can land on a node cargoship may no longer be able to reach.

## Replacing one owned table instead of reconciling against a state file

The ufw backend records the rules it applied in `/var/lib/cargoship/ufw.rules` and, on the next run, deletes the recorded rules the current inventory no longer asks for. That is necessary there because ufw has no way to express "these are all my rules, replace them".

nftables does. `add table inet cargoship`, `delete table inet cargoship`, then the table's full definition, submitted as one `nft -f` transaction, atomically replaces everything cargoship owns. A rule dropped from the inventory is gone when the transaction commits, with no record of the previous run needed and no window in which the node has partial rules. The `add` before the `delete` is not redundant: `delete table` on a table that does not exist is an error, and `add` on one that does is a no-op, so the pair makes the script work on a node cargoship has never configured.

Deleting nothing is also how teardown works. An empty plan renders just the add/delete pair, which removes cargoship's table and leaves the host's own rules untouched.

## Never flushing the ruleset, and never reading another table

The reflex when writing an nftables integration is `flush ruleset`. That would be actively harmful here. Kube-proxy in nftables mode keeps its service and endpoint rules in this same subsystem, and so does every CNI that implements network policy. A global flush would break cluster networking until each of those components noticed and resynced -- on a node cargoship is in the middle of configuring, quite possibly the node it is reaching the rest of the cluster through.

So cargoship writes one table and reads none. Nothing outside `inet cargoship` is inspected or modified, which also means an operator's own tables survive an apply untouched.

## `policy accept` on every base chain

Every base chain cargoship writes has an accept policy. This is the same constraint that stops cargoship from running `ufw enable`: a default-drop policy applied from a remote phase would cut the SSH connection cargoship is running over, and the node would be unreachable with the phase half-finished.

There is a consequence worth stating plainly, because it makes the nftables backend weaker than the others in one respect. An `accept` verdict ends cargoship's chain, not the netfilter hook -- other base chains at other priorities still see the packet. So a rule in an operator's own table can drop traffic that a cargoship rule allowed. Firewalld's trusted zone is the last word on a packet; cargoship's nftables rules are not. A `deny` or `reject` rule does terminate evaluation, so the restrictive direction behaves as written; only the permissive direction is advisory.

## Detecting on the service or a conf file, not on a non-empty ruleset

`Detect` requires `nft` to exist, and then either the `nftables` service to be running or one of `/etc/nftables.conf` and `/etc/sysconfig/nftables.conf` to be present. Probing both paths rather than branching on the distro type keeps the check independent of `src/types/os`, since the split is Debian/Arch/Alpine versus Enterprise Linux/SUSE and does not line up with anything else cargoship distinguishes hosts by.

The tempting alternative -- treat a non-empty ruleset as evidence of a host firewall -- was rejected because it is true on every node of a running cluster. Kube-proxy and the CNI populate the ruleset on hosts whose operator never configured a firewall at all, and claiming those hosts would mean applying rules where the inventory's author expected none.

The gap that leaves is real and deliberate: a CoreOS or Flatcar node with `nft` present but no nftables service and no conf file does not match, and gets no firewall configuration. Widening the check to close that gap reopens the false-positive problem above, so the conservative direction was chosen.

## Matching last

The backend order in `src/pkg/firewall/firewall.go` is firewalld, ufw, nftables. firewalld and ufw are both front ends onto nftables, so a host running either would satisfy the nftables `Detect` as well. The front end is the one an operator expects cargoship to configure -- rules written underneath it would be invisible to `firewall-cmd` and `ufw status`, and a front-end reload could tear them out. Putting nftables last makes it the fallback it is meant to be.

## Persisting with an `include` line

Cargoship's rules are loaded into the running ruleset by `nft -f`, which does not survive a reboot. Persistence is an `include "/etc/cargoship/nftables.nft"` line appended once to whichever boot-time file the distro reads.

The alternative, dumping `nft list ruleset` into that file, would capture kube-proxy's and the CNI's rules along with cargoship's and replay them at boot as static rules -- stale ones, describing services that may no longer exist. Appending an include leaves the operator's file otherwise intact, keeps cargoship's contribution in one identifiable place, and stays correct when cargoship's ruleset is regenerated. A host with neither conf file gets a warning rather than an error: the rules are loaded for this boot, but nothing will reload them at the next one.

## One `inet` table rather than separate `ip` and `ip6` tables

The `inet` family covers IPv4 and IPv6 in a single table, so a plan renders one table, one set of chains, and one transaction regardless of what the cluster's addressing looks like. The cost is that address matches must name their family explicitly -- `ip saddr` versus `ip6 saddr` -- and that trusted addresses split into two sets, `cluster_v4` and `cluster_v6`, since a set has one type. Both are handled in rendering (`nftAddrFamily`, `nftSplitFamilies`) and are cheaper than maintaining two parallel tables and deciding which of them a dual-stack plan belongs in.
