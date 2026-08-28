# Per-Profile Update Concurrency

This guide explains why a cluster admin may want to set a different concurrency limit per node
profile, and how to configure it.

## The problem with one global concurrency limit

`-c/--concurrency` and `-w/--work-concurrency` cap how many hosts, or how many workers, Cargoship
touches at once, but that limit applies flat across the whole cluster. A cluster is rarely
uniform, though. It often mixes roles with very different blast radii if something goes wrong
mid-update:

- **Infra nodes** may run stateful services, ingress, or storage where taking more than one down
  at a time risks an outage or data unavailability.
- **General worker nodes** may be numerous, stateless, and behind a load balancer, where updating
  a quarter of them at once is safe and finishes far faster than one at a time.

A single global limit forces a tradeoff: set it low enough to protect the sensitive infra nodes,
and worker upgrades crawl; set it high enough for fast worker upgrades, and infra nodes risk being
updated too aggressively.

## Setting concurrency per profile

Every profile under `.spec.config.profiles` accepts its own `concurrency`, which overrides the
CLI's `-c`/`-w` value for hosts using that profile only. It accepts either a fixed count or a
percentage of the hosts that share the profile:

```yaml
spec:
  config:
    profiles:
      infra:
        concurrency: "1"
      worker:
        concurrency: "25%"
```

With this configuration:

- Hosts with `profile: infra` are always updated one at a time, regardless of the `-w` flag value.
- Hosts with `profile: worker` are updated 25% at a time, rounded up, with a minimum batch size of
  1 so a small worker pool never stalls at "0 at a time."
- A profile that sets no `concurrency` (or a host with no profile) falls back to the `-w` flag
  value, same as before this feature existed.

A percentage is resolved against the number of hosts in that profile, not the whole cluster, so
`25%` on a 4-host `worker` profile means 1 host at a time, and `25%` on a 40-host `worker` profile
means 10 at a time.

This applies everywhere Cargoship batches worker operations: initializing, upgrading, draining,
deleting, and uninstalling the engine.

## Choosing values

- Use a fixed low count (`"1"`, `"2"`) for profiles running singleton or quorum-sensitive
  services, where losing more than N nodes at once breaks availability.
- Use a percentage (`"25%"`, `"50%"`) for horizontally scaled, stateless profiles where speed
  matters more than caution, and the remaining capacity can absorb the load while a batch updates.
- Use `"100%"` (or leave `concurrency` unset with `-w 0`) for profiles where all hosts can safely
  update simultaneously, e.g. a dev/test cluster.
