# Inventory Configuration Guide

This guide explains how to author a cluster inventory file for bootstrapping or upgrading clusters with Cargoship.

## Basic Structure

A basic inventory file defines the cluster's metadata, global configuration, profiles, and individual target hosts. Start with the following template:

```yaml
---
# yaml-language-server: $schema=https://raw.githubusercontent.com/colonel-byte/cargoship/refs/heads/main/schema/zarf-v1alpha1-cluster-schema.json
kind: ZarfCluster
metadata:
  name: bubbles
```

The `.metadata.name` field sets the cluster name. Cargoship uses this to configure the context name in the resulting `kubeconfig` (e.g., `bubbles`).

## Editor Support

The `yaml-language-server` comment on the first line is what makes an editor complete and check the file as you write it. The URL above reads the schema from GitHub's default branch, which is convenient and wrong in two situations: an air-gapped machine cannot fetch it at all, and `yaml-language-server` says nothing when a fetch fails, so the file silently stops being checked; and a machine that can fetch it is checking against unreleased `main` rather than the release it is running.

`cargoship schema` writes the schema out of the binary, so it is the schema that build validates against and it needs no network:

```sh
cargoship schema inventory -o ./zarf-v1alpha1-cluster-schema.json
```

Then point the inventory at the local copy:

```yaml
---
# yaml-language-server: $schema=./zarf-v1alpha1-cluster-schema.json
kind: ZarfCluster
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

## Target Hosts

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
