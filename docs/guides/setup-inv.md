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
