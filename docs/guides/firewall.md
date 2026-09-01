# Host Firewall Configuration Guide

Cargoship configures the firewall on every node that runs one. It detects the firewall per node, so a single inventory can target a mix of Enterprise Linux hosts running firewalld and Debian or Ubuntu hosts running ufw. Firewall management is opt-in: it only runs when `cargoship install apply` is given the `--firewall` flag.

Every node cargoship manages gets three things: the private address of every other node in the cluster is trusted, the engine's pod and service CIDR blocks are trusted, and the ports listed in `.host.ports` are opened. Anything beyond that comes from the rules in `.host.firewall.rules`.

## Backends

| Backend | Detected when | Notes |
| --- | --- | --- |
| `firewalld` | the `firewalld` service is running | Cluster trust is applied with ipsets in the `trusted` zone; ports become a `distro-exposed-ports` service on the `public` zone |
| `ufw` | `ufw` is installed and `ufw status` reports `active` | Cluster trust and rules are applied with `ufw` commands; ports become a `cargoship-ports` application profile in `/etc/ufw/applications.d/cargoship` |

Cargoship never runs `ufw enable`. A node whose ufw is inactive is left alone, because bringing up a default-deny firewall from a remote phase would cut the SSH connection cargoship is running over. Enable ufw yourself, with a rule that keeps SSH reachable, before pointing cargoship at the node.

## Ports

The `.host.ports` list is the simple case, and it works the same on both backends. It opens a port, or an inclusive port range, to any source:

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

The two backends express the same rule differently, and two fields mean something slightly different on each.

| Rule | firewalld | ufw |
| --- | --- | --- |
| `direction: in` or `out` with an address match | A rich rule on the `public` zone | `ufw allow in ...` / `ufw allow out ...` |
| `direction: in` with only a port match | `firewall-cmd --add-port` on the `public` zone. Only `action: allow` is expressible this way | `ufw allow in from any to any port ...` |
| `direction: forward` | A policy file in `/etc/firewalld/policies`, where `ingress` and `egress` name **zones** | `ufw route allow ...`, where `ingress` and `egress` name **interfaces** |

Because `ingress` and `egress` name zones on one backend and interfaces on the other, a forward rule is the one part of the model that is not portable across a mixed-OS cluster. Set forward rules on a profile that only matches hosts of one OS family.

## Idempotency

Both backends can be applied repeatedly without accumulating rules. On firewalld, cargoship writes the same ipset, service, and policy files each run and lets `firewall-cmd` skip what is already present. On ufw, cargoship records the rules it applied in `/var/lib/cargoship/ufw.rules`; on the next run, any rule in that file that the current inventory no longer asks for is deleted from ufw before the current rules are applied. Rules cargoship owns are tagged with a `cargoship:` comment, visible in `ufw status`, so they can be told apart from rules added by hand. Rules added by hand are never touched.

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

Prefer a `direction: forward` rule under `.host.firewall.rules` for new configuration; it does the same thing on firewalld and also works on ufw hosts.

## Adding a backend

A backend is a `firewall.Backend` implementation in `src/pkg/firewall`: a `Name`, a `Detect` that reports whether the backend manages a given node, and an `Apply` that makes the node match a `firewall.Plan`. Register it in the `backends` list in `src/pkg/firewall/firewall.go`, in match order, and the apply phase picks it up with no further wiring.
