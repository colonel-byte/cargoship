# Host Firewall Configuration Guide

Cargoship configures the firewall on every node that runs one. It detects the firewall per node, so a single inventory can target a mix of Enterprise Linux hosts running firewalld, Debian or Ubuntu hosts running ufw, and hosts that manage nftables directly. Firewall management is opt-in: it only runs when `cargoship apply` is given the `--firewall` flag.

Every node cargoship manages gets three things: the private address of every other node in the cluster is trusted, the engine's pod and service CIDR blocks are trusted, and the ports listed in `.host.ports` are opened. Anything beyond that comes from the rules in `.host.firewall.rules`.

## Backends

| Backend | Detected when | Notes |
| --- | --- | --- |
| `firewalld` | the `firewalld` service is running | Cluster trust is applied with ipsets in the `trusted` zone; ports become a `distro-exposed-ports` service on the `public` zone |
| `ufw` | `ufw` is installed and `ufw status` reports `active` | Cluster trust and rules are applied with `ufw` commands; ports become a `cargoship-ports` application profile in `/etc/ufw/applications.d/cargoship` |
| `nftables` | `nft` is installed and either the `nftables` service is running or the host has an `/etc/nftables.conf` or `/etc/sysconfig/nftables.conf` | Everything cargoship writes lives in one table, `inet cargoship`, rendered to `/etc/cargoship/nftables.nft` and loaded in a single transaction |

Cargoship never runs `ufw enable`. A node whose ufw is inactive is left alone, because bringing up a default-deny firewall from a remote phase would cut the SSH connection cargoship is running over. Enable ufw yourself, with a rule that keeps SSH reachable, before pointing cargoship at the node.

Backends are matched in the order above, and nftables is last on purpose: firewalld and ufw are both front ends onto nftables, so a host running either would match the nftables backend as well, and the front end is the one an operator expects cargoship to configure. The nftables backend is for the hosts that never had a front end -- Debian and Arch nodes configured by hand, and the minimal or immutable images such as CoreOS and Flatcar that ship no firewall front end at all.

A node whose only nftables content comes from kube-proxy or the CNI is deliberately not a match. Every node in a running cluster has a non-empty ruleset, so matching on that would claim hosts whose operator never configured a firewall at all.

## Ports

The `.host.ports` list is the simple case, and it works the same on every backend. It opens a port, or an inclusive port range, to any source:

```yaml
spec:
  hosts:
    - hostname: distro-kc01
      role: controller
      host:
        ports:
          - port: 6443
            protocol: tcp
          - port: 30000-32767
            protocol: tcp
```

## Rules

The `.host.firewall.rules` list is the backend-neutral rule model. Each rule needs an `action`; every other field is a match, and an omitted match means "any".

| Field | Meaning |
| --- | --- |
| `name` | Names the rule. It must be unique within a host, and cargoship uses it to name the files and rule comments it writes on the node. Cargoship derives one from the match fields when it is omitted |
| `action` | `allow`, `deny`, or `reject` |
| `direction` | `in` (the default), `out`, or `forward` |
| `source` | The address or CIDR traffic comes from |
| `destination` | The address or CIDR traffic goes to |
| `ingress` | Where forward traffic enters. Forward rules only |
| `egress` | Where forward traffic leaves. Forward rules only |
| `port` | A port, or an inclusive `low-high` range. A port match also needs a `protocol` |
| `protocol` | `tcp`, `udp`, `sctp`, or `dccp` |

```yaml
spec:
  hosts:
    - hostname: distro-kc01
      role: controller
      host:
        firewall:
          rules:
            - name: allow-metrics
              action: allow
              source: 10.0.0.0/8
              port: "9100"
              protocol: tcp
            - name: ingress-http
              action: allow
              direction: forward
              ingress: public
              egress: trusted
              port: "80"
              protocol: tcp
```

### Rules on a profile

Rules can also be set on a profile, under `.spec.config.profiles.<name>.host.firewall.rules`. Every host that selects the profile gets them. This is the usual place for a rule that belongs to a role rather than to one machine, e.g. opening the API server port on every control-plane node.

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

The host above ends up with both rules. Firewall rules union with the profile's rather than replacing them, so a host can add one rule of its own without giving up the baseline its profile sets. Rules are matched by `name`, and a host rule wins over a profile rule of the same name -- that is how a host overrides one rule from its profile, or turns a profile `allow` into a `deny` for itself.

Rules with no `name` are matched by their fields instead, so a host that repeats a profile rule verbatim still only gets it once. Name your rules if you intend to override them.

The rest of `.host` still replaces rather than unions: a host that lists any `ports` of its own inherits none of its profile's, and the same holds for the legacy `policy` map.

### How rules land on each backend

The backends express the same rule differently, and two fields mean something slightly different on each.

| Rule | firewalld | ufw | nftables |
| --- | --- | --- | --- |
| `direction: in` or `out` with an address match | A rich rule on the `public` zone | `ufw allow in ...` / `ufw allow out ...` | A rule in the `input` or `output` chain, matching on `ip saddr` or `ip6 saddr` |
| `direction: in` with only a port match | `firewall-cmd --add-port` on the `public` zone. Only `action: allow` is expressible this way | `ufw allow in from any to any port ...` | A `tcp dport` or `udp dport` rule in the `input` chain |
| `direction: forward` | A policy file in `/etc/firewalld/policies`, where `ingress` and `egress` name **zones** | `ufw route allow ...`, where `ingress` and `egress` name **interfaces** | A rule in the `forward` chain, where `ingress` and `egress` name **interfaces** matched with `iifname` and `oifname` |

Because `ingress` and `egress` name zones on firewalld and interfaces on the other two, a forward rule is the one part of the model that is not portable across a mixed-OS cluster. Set forward rules on a profile that only matches hosts of one OS family.

## Idempotency

Every backend can be applied repeatedly without accumulating rules. On firewalld, cargoship writes the same ipset, service, and policy files each run and lets `firewall-cmd` skip what is already present. On ufw, cargoship records the rules it applied in `/var/lib/cargoship/ufw.rules`; on the next run, any rule in that file that the current inventory no longer asks for is deleted from ufw before the current rules are applied. Rules cargoship owns are tagged with a `cargoship:` comment, visible in `ufw status`, so they can be told apart from rules added by hand. Rules added by hand are never touched.

On nftables, idempotency needs no state file: the rendered ruleset is cargoship's whole desired state, and it is loaded with an `add table`, `delete table`, and redefinition of `inet cargoship` in one transaction, so a rule the current inventory dropped is gone once the transaction commits. The `add` before the `delete` is what keeps the script working on a node cargoship has not configured before.

## nftables and the cluster's own rules

Kube-proxy in nftables mode, and every CNI, keep their service and network policy rules in the same subsystem cargoship writes to. Cargoship never flushes the ruleset and never reads or writes a table other than its own, because a global flush would break cluster networking until those components resynced.

Two consequences are worth knowing. Every base chain cargoship writes has an `accept` policy, the same reason cargoship never runs `ufw enable`: a default-drop policy applied from a remote phase would cut the connection cargoship is running over. And an `accept` in cargoship's chain ends only that chain, not the netfilter hook, so a rule in an operator's own table can still drop traffic cargoship allowed. Unlike firewalld's trusted zone, cargoship's trust is not the last word on a packet.

Cargoship's rules survive a reboot through an `include "/etc/cargoship/nftables.nft"` line added to whichever boot-time file the distro reads, `/etc/nftables.conf` or `/etc/sysconfig/nftables.conf`. That file is appended to, once, and never rewritten. A host with neither file gets a warning rather than an error: the rules are loaded for this boot, but nothing will reload them at the next one.

## Legacy firewalld policies

The older `.host.policy` map still works and is still firewalld-only. It maps a policy name to a firewalld policy written verbatim to `/etc/firewalld/policies/<name>.xml`:

```yaml
spec:
  hosts:
    - hostname: distro-kc01
      role: controller
      host:
        policy:
          k8s-forward:
            target: ACCEPT
            ingress:
              name: public
            egress:
              name: trusted
            ports:
              - port: "80"
                protocol: tcp
```

Prefer a `direction: forward` rule under `.host.firewall.rules` for new configuration; it does the same thing on firewalld and also works on ufw and nftables hosts.

## Adding a backend

A backend is a `firewall.Backend` implementation in `src/pkg/firewall`: a `Name`, a `Detect` that reports whether the backend manages a given node, and an `Apply` that makes the node match a `firewall.Plan`. Register it in the `backends` list in `src/pkg/firewall/firewall.go`, in match order, and the apply phase picks it up with no further wiring.
