# Inventory

To create a basic inventory file for bootstrapping or upgrading an existing cluster, it will start off looking like the following:

```yaml
---
# yaml-language-server: $schema=https://raw.githubusercontent.com/colonel-byte/cargoship/refs/heads/main/schema/zarf-v1alpha1-cluster-schema.json
kind: ZarfCluster
metadata:
  name: bubbles
```

The `.metadata.name` will be used by `cargoship` set the cluster name, so in this case the kube-config context will be `bubbles`.

Next section is:

```yaml
spec:
  config:
    loadbalancer: bubbles-kc.test.com
```

This is required as it will be added to the valid TLS Subject Alternative Names used by the Kubernetes API server, this can be the Round-Robin DNS record for the control-plane nodes, or it could be a cloud load-balancer like AWS NLB.

The largest section like be the `.spec.hosts` array:

```yaml
spec:
  hosts:
    - hostname: distro-kc01
      ssh:
        address: 10.0.3.114
        user: root
        port: 22
        keyPath: ~/.ssh/id_ed25519
      profile: control-plane
      role: controller
```
