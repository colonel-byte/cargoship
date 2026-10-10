# Why a package's SELinux policy is CIL, applied fatally, and removed from a record on the host

`spec.config.os.selinux` lets a Cargo package carry the SELinux policy its workload needs: CIL policy modules, booleans, and file context mappings. `pkg/phase/24_prepare_selinux_policy.go` applies it, and `pkg/phase/89_remove_selinux_policy.go` takes it back off. Four decisions in there are not recoverable from the code, and this is the record of them.

## CIL, not Type Enforcement source and not a compiled module

A policy can reach a host in three shapes, and only one of them fits a management node driving an air-gapped fleet.

Type Enforcement source -- the familiar `.te` and `.fc` pair -- has to be compiled on the host, with `checkmodule` and `semodule_package` out of `policycoreutils-devel` and `selinux-policy-devel`. Neither is on a minimal Enterprise Linux install, and neither is staged inside the package. Cargoship installs nothing on a host beyond what a phase uploads, so a feature whose first requirement is two development packages from a repository the host cannot reach is a feature that does not work on the fleet it was written for.

A compiled `.pp` module avoids the toolchain by moving the build off-host, and pays for it in portability: a binary module is bound to the policy version it was built against, so a package carrying one works on the hosts whose policy matches and fails on the rest. For a package that is built once and applied to whatever an operator has, that is the wrong trade.

CIL is what `semodule -i` accepts directly. No build tooling, no version binding, and it is plain text, so it lives in `distro.yaml` the way the fapolicyd rules already do and shows up in a review as the policy it is. The cost is that CIL is lower-level than `.te` and less pleasant to write by hand. That cost falls on the few packages that need a module at all, and booleans and file contexts -- which need no policy language whatsoever -- cover the common cases without it.

## Applying is fatal, removing is tolerant

The rule set in [`choice-unpinnable-hosts.md`](choice-unpinnable-hosts.md) is prerequisite against convenience: a failure that leaves the install impossible stops the run, and a failure that leaves it merely weaker warns. A policy written into `distro.yaml` by hand is the first kind. Nobody adds a CIL module or a `container_var_lib_t` mapping speculatively; it is there because the workload is denied without it. A host that quietly skipped it is an enforcing host running a workload whose policy is absent, and the operator finds out from an AVC denial somewhere downstream rather than from the phase that could have told them. So `semodule`, `setsebool` and `semanage` failures return, and the phase stops the run.

A host that cannot take a policy at all is a different question, and is filtered rather than failed -- `selinuxPolicyCapable` requires both SELinux enforcing and `semodule` present, the same shape as `PrepareFapolicy` filtering on the service running. A Debian host with no SELinux is not a failure to apply a policy; it is a host the policy was never about.

Removal inverts both. `RemoveSelinuxPolicy` warns and carries on through every step. A reset has to be re-runnable, and the state it is undoing can legitimately be half gone -- a module an operator removed by hand, a context already deleted, a boolean already back at its default. Returning an error on the first absent thing would strand the host with the state file still claiming the policy is installed, which is worse than the inconsistency it was reporting.

## Reset reads a record on the host, because it has no package to read

A reset loads no distro package. `cmd/install_reset.go` builds its `phase.Manager` by hand precisely because there is nothing to load, and `Manager.Distro` is nil for the whole run. So the removal phase cannot ask `spec.config.os.selinux` what to remove: whatever it undoes, it has to learn from the host.

That is what `/var/lib/cargoship/selinux/cargoship-state.json` is for. The apply phase writes it before it changes anything, recording the modules and their priorities, the file context mappings, and -- the part that cannot be reconstructed any other way -- the value each boolean held beforehand. `getsebool` after the fact reports cargoship's own value, so without the record "unwind" could only mean guessing at a distribution default, and guessing wrong on a boolean is a security change made silently in the wrong direction. A boolean already present in the record keeps the value written the first time, so a second apply does not overwrite the host's original with cargoship's.

A host with no record is skipped. It has nothing cargoship applied, or it has a policy applied by something else, and neither is the removal phase's to touch.

## No command-line flag

The phase gates on two things only: what the hosts can take, and what the package asks for. There is no `--selinux-policy` flag, and this is deliberate rather than unfinished.

`apply` and `prepare` already bind `-f/--fapolicyd`, and no phase reads it -- `PrepareFapolicy` has no `Enabled` field and `PrepareOptions` has no fapolicyd member. `CHANGELOG.md` records the matching cleanup on the phase side, `drop the unused InstallFapolicy phase`. A flag is not one flag: it is `types/config.go`, `cmd/root.go`, `cmd/viper.go`, both `cmd/install_*.go` files, `internal/ansiblemod/params.go`, and the `DOCUMENTATION` block in both Ansible action plugins. Adding that surface for a toggle nothing threads through is how the dead `-f` happened. An operator who does not want the policy applied removes it from the package, which is where it is declared.

## Dry run

The phase is classified `DryRunSkip`, like `PrepareSelinux` and `PrepareFapolicy` beside it: a dry run reports it and does not run it. Its mutations are still wrapped in `GenericPhase.Wet`, which under that classification never executes, and the wrapping is there so the phase does not have to be rewritten if it ever declares a dry-run path. Claiming `readOnly` would be wrong -- the phase changes hosts -- and a `DryRun()` that reported the commands it would run would have to re-derive the host filtering against live hosts to say anything true, which is work with no caller asking for it yet.
