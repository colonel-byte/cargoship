# Ideas: other sources for `cargoship inventory from-*`

This is a shortlist, not a decision. `cargoship inventory from-ansible` established a command group whose members all answer the same question -- where does the list of machines come from -- and the obvious next question is which other sources belong in it. Nothing here is committed to; the point is to record the criteria and the ranking while the reasoning is fresh, so that the next adapter is argued against a list rather than added because someone asked.

## What every adapter has to do, and nothing more

An adapter builds the `ansibleinv.Input` shape -- `groups`, `hostvars`, `roleGroups` -- and hands it to one translator. Role derivation, controllers-first ordering so that the leader is a controller, the refusal of an unknown `cargoship_` variable, and validation against the embedded inventory schema are written once and are already fuzzed (`src/test/e2e/fuzz/ansible_inventory_fuzz_test.go`). An adapter is a decoder. When one starts wanting rules of its own -- its own notion of a role, its own precedence between two variables that disagree -- that is the signal it does not belong in the binary, and should be a script that emits the JSON contract described below.

One naming consequence, worth settling before a second adapter exists rather than after: the shared translator lives in a package called `ansibleinv`, and the moment anything but Ansible calls it the name is wrong. Rename it to something like `clusterinv` when the second caller lands.

## The four criteria

A source is worth first-party support when it meets all four.

It works offline. The management node is inside the airlock and reaches nothing. A source that needs an API call from the management node is a source that needs a script on the outside anyway, and if a script is being written the script may as well emit the contract.

The owner of the truth resolves it. Cargoship should read a resolved answer, not reproduce someone else's resolution. This is the same argument [choice-ansible-module](choice-ansible-module.md) makes for letting Ansible resolve its own inventory rather than parsing inventory files in Go: reproducing group_vars, `children` and precedence would reproduce them wrongly, and the symptom is a node installed in the wrong plane with nothing in between to say so.

The format is documented and stable. A format that changes under us turns a silent misread into an install against the wrong machines.

Roles are stated, not guessed. A host's role has to come from something the operator wrote -- a group, a column, a tag, a flag. Inferring it from a hostname pattern is guessing, and `ZarfHost.Role` is read verbatim everywhere else in this repository precisely because it is not inferable.

## The list, ranked

### 1. `from-json` -- the contract rather than a source

Publish `{groups, hostvars, roleGroups, cluster}` as a documented, versioned input shape, read from a file or from stdin. Every other item on this list collapses into "emit this JSON", which is what keeps the command group at three or four members instead of fifteen. It is also the cheapest thing here: the shape already exists as `ansibleinv.Request`, and the work is documenting it as an interface other people may target, plus a version field so it can change later without breaking the scripts that targeted it.

Ship this first, whatever else happens. It is the thing that bounds the size of the rest of the list.

### 2. `from-csv`

The rack sheet. Columns for hostname, address, role, user, port and key path, out of the spreadsheet a data center hands over, with a flag to map a header that disagrees (`--column role=Function`). No network, no dependency, no format drift, and it is the one source that exists in every air-gapped shop regardless of what tooling they otherwise run. It is unglamorous enough to be overlooked and is probably the highest-value first-party adapter on the list.

### 3. `from-ssh-config`

An SSH client configuration maps almost exactly onto `rig.SSH`: `HostName` onto the address, `User`, `Port`, `IdentityFile` onto the key path, and `ProxyJump` onto the bastion. An operator who can already reach the fleet by name has, in effect, written most of an inventory.

Do not parse the file. `Include`, wildcards and `Match` make `ssh_config` a resolver, which is the trap this document keeps naming. Shell out to `ssh -G <host>` per host and read the resolved answer, which is the owner-resolves-it rule applied. Roles come from a flag or from a list of host patterns per role, never from the name.

`IdentityFile` stays a path in the generated document. It is recorded, never read: key material in a generated inventory is always named by a path and never given by value.

### 4. `from-terraform` (or OpenTofu)

Useful, with one decision that determines whether it is cheap or endless.

Reading `resources[]` out of state means reading provider-specific attributes -- `aws_instance.public_ip`, `proxmox_vm_qemu.default_ipv4_address`, `libvirt_domain.network_interface[0].addresses[0]`, `vsphere_virtual_machine.default_ip_address` -- and grouping by tags, name prefixes, `for_each` keys or module paths, all per-shop convention. That is a provider mapping table in Go which is never finished.

Reading outputs is not. The operator writes one `output` block whose value is the contract from item 1, in HCL, where they already know their own provider's attribute names:

```hcl
output "cargoship_inventory" {
  value = {
    groups = {
      controller = [for k, v in aws_instance.kc : v.private_ip]
      worker     = [for k, v in aws_instance.kw : v.private_ip]
    }
    hostvars = { for k, v in aws_instance.kc : v.private_ip => {
      ansible_user       = "ubuntu"
      cargoship_hostname = v.tags["Name"]
    }}
  }
}
```

`tofu output -json` then runs outside the airlock, the JSON crosses it, and the management node reads a file. No backend access, no network, no tofu binary on the inside, and no provider knowledge in cargoship. At that point `from-terraform` is barely distinct from `from-json`: unwrap `.<name>.value` and decode.

A middle tier exists if writing an output block is too much to ask -- read `terraform show -json`, which is documented and carries a `format_version`, with the mapping supplied by flags such as `--address-attribute private_ip` and `--controller-filter 'tags.role=controller'`. That keeps the provider table out of Go but gives cargoship a small query language of its own, and small query languages grow. Do it on real demand, not in advance.

HCL itself is out. The hosts are not in it: `count`, `for_each`, variables, modules and data sources mean the configuration describes a shape rather than a fleet, and `hashicorp/hcl/v2` would be a dependency bought in order to get a wrong answer.

Security, because it is easy to get wrong here specifically: a Terraform state file is secret-bearing. It holds provider credentials and every `sensitive` value in plaintext. An adapter that reads raw state must not log the file, must not echo attributes into error messages, and must refuse any output marked `"sensitive": true` rather than copy it into a generated inventory that lands on disk unencrypted. Reading outputs rather than state keeps that blast radius small, which is a second reason to prefer it.

### 5. `from-kubernetes`

The reverse direction, for a cluster cargoship did not build. `kubectl get nodes -o json` gives an InternalIP per node and `node-role.kubernetes.io/*` labels, which is exactly the controller and worker split, and that is enough to run `engine-config-sync` or `reset` against a brownfield cluster.

What decides its priority is that it yields half an inventory. Addresses and roles, yes; SSH user, port and key, no -- those are not in the Kubernetes API and have to arrive by flag. It is a discovery step rather than a source.

### 6. `from-netbox`, `from-maas`

NetBox is the real source of truth in a good number of the environments this tool targets, and its model maps cleanly: devices, device roles, primary IPs, tags. The obstacles are that it needs network from the management node, which the airlock does not give, and an API client in the binary, which is dependency weight for one integration. Better served by a script that emits item 1. Worth reconsidering only if several users ask for the same field mapping.

## Explicitly not

`from-inventory`, reading an Ansible INI or YAML inventory file directly without Ansible installed. Tempting because it would drop the Python dependency the action plugin adds; it reintroduces group_vars, host_vars, `children` and precedence, which is the thing the module architecture deliberately does not reimplement.

`from-vsphere`, `from-proxmox`, and the cloud providers. Provider attribute tables that are never finished, and network from a node that has none.

Scanning: DHCP leases, ARP tables, a subnet sweep. These guess which machine is a controller, and a wrong guess installs a control plane on someone else's server.

## Suggested order

`from-json`, then `from-csv`, then `from-ssh-config`, then stop until someone asks. `from-terraform` slots in wherever demand puts it, built on the outputs path rather than the state path.
