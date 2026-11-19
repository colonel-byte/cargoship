# Inventory Configuration Guide

This guide explains how to author a cluster inventory file for bootstrapping or upgrading clusters with Cargoship.

## Basic Structure

A basic inventory file defines the cluster's metadata, global configuration, profiles, and individual target hosts. Start with the following template:

```yaml
---
# yaml-language-server: $schema=https://raw.githubusercontent.com/colonel-byte/cargoship/refs/heads/main/schema/cluster-v1alpha1-schema.json
kind: Cluster
metadata:
  name: bubbles
```

The `.metadata.name` field sets the cluster name. Cargoship uses this to configure the context name in the resulting `kubeconfig` (e.g., `bubbles`).

## Editor Support

The `yaml-language-server` comment on the first line is what makes an editor complete and check the file as you write it. The URL above reads the schema from GitHub's default branch, which is convenient and wrong in two situations: an air-gapped machine cannot fetch it at all, and `yaml-language-server` says nothing when a fetch fails, so the file silently stops being checked; and a machine that can fetch it is checking against unreleased `main` rather than the release it is running.

`cargoship schema` writes the schema out of the binary, so it is the schema that build validates against and it needs no network:

```sh
cargoship schema inventory -o ./cluster-v1alpha1-schema.json
```

Then point the inventory at the local copy:

```yaml
---
# yaml-language-server: $schema=./cluster-v1alpha1-schema.json
kind: Cluster
metadata:
  name: bubbles
```

## Package Values

The `.spec.config.values` section overrides the values the distro package was built with. It is the one part of an inventory whose vocabulary belongs to the package rather than to cargoship, so the schema above leaves it untyped -- and a hosted schema never could do better:

```yaml
spec:
  config:
    values:
      addons:
        disabled:
          - rke2-ingress-nginx
```

Pass `--package` to compose the package's own values schema into the inventory schema, and the block completes and checks like the rest of the file:

```sh
cargoship schema inventory --package ./package.tar.zst -o ./inventory.schema.json
```

`--package` takes anything `cargoship apply` takes -- a tarball, an `oci://` or `https://` reference -- or a package source directory, so it works before the package is built. The composed schema describes the overrides on their own, while cargoship validates them merged with the values the package ships: it catches an unknown key, a wrong type, or a name outside an enum, and does not reproduce install-time validation. See the [package values guide](package-values.md) for what a package may expose and how the merge works.

## Checking an Inventory

An editor is not the only place this check belongs. Cargoship unmarshals an inventory without strict key checking, so a misspelled key is dropped rather than reported: a file that says `loadbalancr:` parses cleanly and installs a cluster with no load balancer address. `cargoship validate` runs the same schema over a file with no editor and no network:

```sh
cargoship validate ./inventory.yaml
```

It reports every problem rather than the first, so one pass is enough to fix a file:

```
inventory.yaml does not match the inventory schema:
  spec.config: Additional property loadbalancr is not allowed
  spec.config: loadbalancer is required
  spec.hosts.0.role: must be one of the following: "controller", "worker"
```

The schema is chosen from the document's own `kind`, so a directory of inventories and package definitions can be checked in one run. A cargoship config file declares no kind and has to be named:

```sh
cargoship validate ./inventories/*.yaml
cargoship validate --kind config ./cargoship-config.yaml
```

`--package` works here too, and checks `.spec.config.values` against the package's own schema with the same caveat as above:

```sh
cargoship validate ./inventory.yaml --package ./package.tar.zst
```

This is a check you run, not one cargoship runs for you. `apply` does not validate the inventory against the schema before installing; it reads the file the way it always has. Put `cargoship validate` in CI, or in front of an install, where a typo is cheap to fix.

## Global Configuration

The `.spec.config` section configures cluster-wide settings, such as load balancer endpoints:

```yaml
spec:
  config:
    loadbalancer: bubbles-kc.test.com
```

The `loadbalancer` address is required. Cargoship adds this to the TLS Subject Alternative Names (SANs) for the Kubernetes API server. This can be a Round-Robin DNS record pointing to your control-plane nodes, or an external load balancer like an AWS NLB.

It is also what cargoship writes as `server:` on every controller that is not the first, so it is the address the second and third controllers join the cluster through, and it is what the kubeconfig `cargoship kube-config` fetches points at. All three uses are the same string, which is why getting it wrong shows up late rather than at install time.

### Pointing the name at the control plane

Three shapes work, and they differ in what DNS has to hold:

| Shape                                | The record                                 | What happens when a controller dies                                 |
| ------------------------------------ | ------------------------------------------ | ------------------------------------------------------------------- |
| Round-robin DNS                      | one `A` record per controller, same name   | clients that do not retry the next answer fail until DNS is updated |
| An external load balancer            | `A`, or `CNAME` to the balancer's own name | the balancer takes the node out, and nothing in DNS changes         |
| A VIP held on the nodes (`kube-vip`) | one `A` record, pointing at the VIP        | the VIP moves to another controller; the address does not change    |

The third is what [the kube-vip examples](../../example/rke2-kube-vip) set up, and it is the one with a DNS requirement worth stating explicitly:

- **One `A` record for the name, pointing at the VIP** - not one per controller. The whole point of the VIP is that the address outlives any one node, so round-robin records alongside it defeat it.
- **Create the record before the install.** `kubeVip.address` is empty by default, so the pod is told to advertise the name from `loadbalancer`, and kube-vip resolves a name rather than rejecting it. The resolution has to succeed on the controller, from its own resolver, by the time the kubelet starts the static pod - and cargoship writes nothing into `/etc/hosts` for the load balancer, only for the fleet's own hosts.
- **Where it will not resolve at bootstrap, set the IP explicitly.** `spec.config.values.kubeVip.address` takes the address the record points at; leave `loadbalancer` as the name. That is the air-gapped case, where the name is real but the cluster's own resolver does not answer for it yet.
- **No `PTR` record is needed.** Nothing in cargoship or kube-vip does a reverse lookup on it.
- **A low TTL buys nothing.** The address does not move - the node holding it does - so there is nothing for a resolver to re-learn when a controller fails.

### The certificate only names what the inventory names

Cargoship writes `tls-san` itself, from `loadbalancer` plus each controller's short and long hostname, and it *assigns* the key rather than adding to it - so a `tls-san` a package definition sets is replaced. Where `loadbalancer` is an FQDN, that means **the VIP's IP address is not on the API server's certificate** unless you name it: reaching the cluster at `https://<vip>:6443` fails verification even though the address answers.

`.spec.config.tlsSan` is where the rest go. Cargoship appends them to the derived names, so the list holds only what nothing can derive - the VIP's address, an external DNS alias, a bastion or proxy a client dials:

```yaml
spec:
  config:
    loadbalancer: bubbles-kc.test.com
    tlsSan:
      - 10.0.0.1
      - bubbles.external.test.com
```

Either a name or an IP address is accepted, and duplicates of a derived name are dropped rather than written twice. A load balancer or proxy in front of the API server does not change this: the certificate is still the API server's own, so every name a client uses to reach it has to be on it. A SAN added after the cluster is up only takes effect once the API server's serving certificate is reissued, and cargoship does not do that for you.

## Node Profiles

The optional `.spec.config.profiles` section defines reusable groups of node configurations (e.g., `control`, `infra`, or `worker`). Profiles simplify management by applying standard node labels, taints, or host firewall rules across matching hosts.

```yaml
spec:
  config:
    profiles:
      control:
        host:
          ports:
            - port: 6443
              protocol: tcp
          firewall:
            rules:
              - name: allow-metrics
                action: allow
                source: 10.0.0.0/8
                port: "9100"
                protocol: tcp
        engine:
          labels:
            adrp.xyz/purpose-control: "true"
          taints:
            - CriticalOnly=True:NoExecute
```

### Inheriting from another profile

A host selects one profile, and a profile's settings apply only where the host left them empty. That means a cluster whose profiles differ in one field each has to spell every one of them out in full, and the `profiles` block grows with the number of combinations rather than with the fleet. `inherit` is how to write the difference instead of the whole:

```yaml
spec:
  config:
    profiles:
      base:
        host:
          ports:
            - port: "6443"
              protocol: tcp
        engine:
          labels:
            adrp.xyz/tier: "standard"
      infra:
        inherit: base
        engine:
          taints:
            - adrp.xyz/infra=true:NoSchedule
```

`infra` opens the same port and carries the same label as `base`, because it said nothing about either. It adds a taint. Had it named labels of its own they would have replaced `base`'s entirely rather than being added to -- the same whole-field rule a host's settings follow against a profile's.

A chain may be any length, and the nearest ancestor to set a field is the one that wins. Cargoship folds the chain in when it loads the document, so every phase sees a profile that already carries what it inherits. An `inherit` naming a profile the cluster does not define is refused, and so is a cycle, both naming the profiles involved.

The `.spec.hosts` array is the main body of the inventory. It defines each host node, its connection details, role, and profile:

```yaml
spec:
  hosts:
    - hostname: distro-kc01
      ssh:
        address: 10.1.2.3
        user: root
        port: 22
        keyPath: ~/.ssh/id_ed25519
      profile: control
      role: controller
```

## Host Firewall

The optional `.host.firewall` section, on a host or on a profile, declares firewall rules cargoship applies to the node. Cargoship detects whether the node runs firewalld, ufw, or nftables directly, and renders the same rules onto any of them. A host's own rules union with its profile's, so the host below gets `allow-metrics` from the `control` profile as well as its own `allow-backup`. See the [firewall guide](firewall.md) for the rule model and the per-backend details.

```yaml
spec:
  hosts:
    - hostname: distro-kc01
      role: controller
      profile: control
      host:
        firewall:
          rules:
            - name: allow-backup
              action: allow
              source: 10.0.9.4
              port: "2049"
              protocol: tcp
```
