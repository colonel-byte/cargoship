# Why a missing dnf versionlock plugin warns instead of failing the install

After installing the engine packages, cargoship pins them at the installed version so a host-level package manager update cannot move the engine out from under it (`pkg/phase/51_package_pin.go`). On Debian-family hosts that is `apt-mark hold`; on RHEL-family hosts it is `dnf versionlock add`. The two are not equally available, and that asymmetry is the whole of this decision.

## apt-mark is always there; versionlock is not

`apt-mark` ships inside the `apt` package itself. Any Debian-family host that just installed a `.deb` has it, so a failing `apt-mark hold` means something is genuinely wrong and `holdAPTPackages` returns the error.

`dnf versionlock` is a plugin in a separate package -- `python3-dnf-plugin-versionlock` on dnf4, `dnf5-plugin-versionlock` on dnf5 -- and a minimal RHEL-family install does not include it. On such a host `dnf versionlock add` exits non-zero with `No such command: versionlock`, which under fail-fast turned a perfectly good install into a failed apply.

## Cargoship cannot fix it, and cannot fix it later

The obvious repair is to install the plugin first, and cargoship cannot. It runs from a management node against an air-gapped fleet, so a package that is not staged inside the package being applied has to come from a repository the host can reach, and the versionlock plugin is not staged.

Cargoship does install `container-selinux` by name, in `pkg/phase/21_prepare_selinux.go`, and that phase is fatal on failure. It looks like a counterexample and is in fact the rule this decision follows. The staged RPM set carries `k3s-selinux` / `rke2-selinux`, and those declare `Requires: container-selinux`. With no repository to resolve it from, dnf installing the staged file fails on the unmet dependency unless `container-selinux` is already on the host, which is why that phase runs at 21, ahead of the install, and why it stops the run when it cannot succeed.

The distinction is not by-name against staged, it is prerequisite against convenience. Without `container-selinux` there is no engine install to pin. Without versionlock the install completes and is merely unpinned. Failing one and warning the other is the same rule applied to two different stakes.

So the choice on an unpinnable host is between installing unpinned and not installing at all.

Failing is also badly timed. By the time pinning runs, `InstallPackage` has already succeeded and the engine packages are on disk. Returning an error there does not undo the install; it reports a failure for a host that is in fact installed, and the operator's only route forward is to stop the run from trying to pin. An unpinned host is a weaker guarantee than a pinned one, but it is a working host, and the warning names it and the packages it left unpinned.

## What is still fatal

The tolerance is deliberately narrow, and it lives in `holdRPMPackages` rather than in the shared `installAndPinPackagesFor`, so it cannot spread to the apt path:

- A host that *has* versionlock and still fails to pin is a real error. The probe (`dnf versionlock --help`, chosen because it reads no repository metadata and touches no existing lock) is what separates "this tooling is absent" from "this tooling failed".
- A staged file whose package name cannot be read fails the phase on either package manager. That is a property of the artifact, not of the host's pinning tooling, and `packageNames` gives up on the first file it cannot read -- so continuing would leave *every* package on the host unpinned while reporting success.

## What would justify revisiting

Staging the versionlock plugin as part of the package, so a RHEL-family host can be given the plugin before the pin instead of excused from it. That turns an unpinnable host back into a fatal error, which is the stronger behavior, and it is the only change that should reverse this.

Note also that nothing unpins yet: the comment on `holdAPTPackages` defers the unhold needed before an upgrade installs a newer set to the upgrade phase, which does not exist. Until it does, the warning is the only record that a host's engine is not pinned.
