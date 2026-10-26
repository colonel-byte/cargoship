# The engine's disable key does not gate a chart's images

`spec.config.engine.manifest.<chart>.enabled` decides whether a chart's images are exported and uploaded. `.config.disable` -- the engine's own key, which every example exposes as `addons.disabled` -- decides whether the engine installs the chart at all. They are not wired together, and the asymmetry is visible to an operator:

| How the chart is turned off | `HelmChartConfig` written | Images uploaded |
| --------------------------- | ------------------------- | --------------- |
| `enabled: false`            | no                        | no              |
| `addons.disabled: [...]`    | no                        | **yes**         |

So a cluster that disables cilium the documented way, `addons.disabled: [rke2-cilium]`, still has all eleven cilium images exported to tarballs and pushed to every node. `helmChartConfigs` (`types/distrocfg/rancher_common.go`) consults both keys; `Config.EnabledImages` (`api/colonel-byte.dev/v1alpha1/distro/chart.go`) consults only `enabled`.

## Why it was left

Wiring the disable list into `EnabledImages` is not a one-liner, and the reasons are worth writing down before someone decides it is:

- **The normalization is not where it would be needed.** `disabledCharts` handles a YAML list, a bare string, and surrounding whitespace, and it is unexported in `types/distrocfg`, which imports the distro API package. Reusing it from `EnabledImages` means moving it rather than calling it, and duplicating that parsing is how the two switches would drift in a second, quieter way.
- **It would make the API package opinionated about an engine key.** `disable` is rke2's and k3s's; `manifest` is already only read by those two distros, but `Config.EnabledImages` is on the shared type and upstream distros call it too.
- **The right answer may be fewer switches rather than more plumbing.** `enabled` arrived to gate images. If it becomes a real install switch -- which a user-supplied Helm chart would need, since a disabled supplied chart should not be installed at all rather than merely have its images withheld -- then one of the two keys should probably defer to the other rather than both being read everywhere.

## What to decide

Whether `enabled` and `disable` stay two mechanisms or become one. Revisit alongside the user-supplied chart work, which is what forces the question: until then the cost is wasted upload for a chart disabled through `addons.disabled`, which is real but is no worse than the behaviour before per-chart images existed.

See [package-values](../guides/package-values.md) for what each key does today.
