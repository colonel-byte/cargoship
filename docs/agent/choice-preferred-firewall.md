# Why the firewall backend is chosen by the node's OS, not by what is running

Cargoship's firewall support (`cargoship apply --firewall`) renders a backend-neutral `firewall.Plan` onto whichever firewall a node runs. Three backends exist: `firewalld`, `ufw`, and `nftables`. Selection lives in `firewall.Select` (`src/pkg/firewall/firewall.go`).

The first implementation matched a host against the backends in a fixed order -- firewalld, then ufw, then nftables -- taking the first whose `Detect` was true. Ordering firewalld and ufw ahead of nftables was already deliberate, since both are front ends onto nftables and a host running either would match the nftables backend as well. What that ordering could not express is the case the current implementation is about: a node whose front end is installed but stopped.

## The OS decides, and `Detect` only confirms

Each OS module now names the front end its distribution ships, through `PreferredFirewall` on the `Configurer` interface (`src/types/os/interface.go`): firewalld on Enterprise Linux and SUSE, ufw on Debian and Ubuntu, nothing on Alpine, Arch, CoreOS, Flatcar, and Slackware. `Select` looks for that backend first and uses it when it is running. Only a node whose preferred front end is absent, or whose distribution ships none, falls through to the ordered `Detect` match.

The alternative was to keep selection entirely inside the firewall package and infer the front end from what `Detect` finds. That is what the ordered match already did, and it cannot distinguish "this distribution has no front end" from "this distribution's front end is not up". Cargoship already resolves an OS module per host and already asks it distribution-specific questions, so the distribution's own answer is both available and more trustworthy than a probe.

An inventory field to override the choice was considered and rejected. It would be a second source of truth for something the OS already answers, and the failure mode -- an operator naming a backend the node does not run -- produces a worse outcome than any case it would fix.

## An installed but stopped front end means cargoship configures nothing

When the preferred backend is installed and not running, `Select` returns it as `Selection.Skipped` and cargoship makes no change to that node. Two other options were available and both were rejected:

- **Start the service.** Bringing up a firewall from a remote phase is the same hazard that stops cargoship from running `ufw enable`: a default-deny policy applied over the SSH connection cargoship is running on can cut it, leaving the node unreachable with the phase half-finished.
- **Fall through to nftables.** The front end owns the nftables ruleset on that node. Rules written underneath a stopped firewalld or ufw would be overwritten the moment the operator started it, so the node would silently lose the configuration cargoship reported as applied.

An operator who installed a front end and left it down has made a decision about the node's firewall posture. Cargoship logs the node it skipped and leaves that decision with them.

The cost is that a stock Ubuntu node, which ships ufw installed and inactive, gets no firewall configuration at all. That is a real change in behaviour: the ordered match would have configured such a node through the nftables backend if `nft` was present and the `nftables` service or an `/etc/nftables.conf` existed. The rules it wrote there were the ones ufw would have discarded on its next start, so what is lost is a configuration that was never durable.

## `Installed` is a separate method rather than a mode of `Detect`

`Detect` answers "does this backend manage the firewall on this node", which requires the firewall to be running -- the nftables backend deliberately reads it as "the operator configured a persistent ruleset", not "nft is present". Overloading it would have changed what every existing caller means. `Installed` is a plain presence check (`firewall-cmd`, `ufw`, `nft` on `PATH`), and only the skip decision uses it.

## `PreferredFirewall` is declared on each leaf OS module

The obvious place for the method is a shared base: `EnterpriseLinux` for the seven Enterprise Linux modules, `Debian` for Ubuntu, `SLES` for OpenSUSE. It works for the second and third, whose modules embed only their base, but not for the first. `RockyLinux` and its siblings embed both `linux.EnterpriseLinux` and `configurer.Linux`, so a method on `EnterpriseLinux` and the default on `configurer.Linux` are promoted at the same depth, and Go leaves the selector ambiguous -- the modules stop satisfying `Configurer`. Declaring the method on each leaf keeps it unambiguous, at the price of seven one-line methods.
