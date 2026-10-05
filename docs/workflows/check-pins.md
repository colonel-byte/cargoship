# check-pins.yaml

**Triggers:** scheduled every 2 days at 06:00 UTC, and manual dispatch.

Two independent jobs that look for newer upstream versions and file a GitHub issue when they find one:

- **check-dnf-pins** - runs `mage dev:dnfPins` to check for newer AlmaLinux 10 DNF package versions used in containers and `.goreleaser.yaml`. If the run produces a diff, it opens or updates a tracking issue (keyed by an HTML comment marker) with the proposed diff.
- **check-engine-pins** - runs `mage generate:updatePins` to check for newer RKE2/k3s engine releases for pinned minor versions, against `thirdparty-src/pins.json`. Same open-or-update-issue pattern.

Neither job pushes a fix itself - both leave a diff in an issue for a human to apply by running the named mage target locally.
