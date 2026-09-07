## prepare phases
With `--dry-run`, cargoship connects to every host and runs the preflight checks for real, then reports the phases it would have run instead of running them. Each phase below is labelled with what a dry run does with it. A phase is only run when it declares that it is safe to, so a phase added later is reported until someone says otherwise.

The report is not a static list. Every phase still prepares itself and checks whether it has anything to do, and both only read, so work that is already done is filtered out against the live hosts. A phase that could not be assessed, because it reads state an earlier reported phase would have created, is reported as `unassessed`.

A dry run takes no cluster lock, so it does not block a real run, and it can report state that a concurrent run is already changing. It does not need `--confirm`.

1. Connect to hosts
    - Connects to a remote host via `github.com/k0sproject/rig`
    - Dry run: runs, reads only
1. Detect host operating systems
    - Gathers information about the remote host, including: OS and OS version
    - Dry run: runs, reads only
1. Acquire exclusive host lock
    - Runs a background task that will touch a file every 30 seconds on each remote node, this prevents other `cargoships` from doing any changes until the lock file has not been touch for over a minute
    - Dry run: reported, not run
1. Prepare hosts
    - Updates the remote nodes; environment variables and sysctl
    - Dry run: reported, not run
1. Prepare hosts - Enterprise Linux support
    - Installs container-selinux on systems that have SELinux enabled on them
    - Dry run: reported, not run
1. Prepare hosts - Enterprise Linux support - Fapolicyd
    - Creates the distro supplied FAPolicy rules to /etc/fapolicyd/rules.d/31-cargoship.rules
    - Dry run: reported, not run
1. Enable the requested kernel modueles
    - Turns on the list of requested modules on the host, then reboots the box if modules are added
    - Dry run: reported, not run
1. Release exclusive host lock
    - Deletes the lock file from each node, allowing other `cargoships` to run
    - Dry run: reported, not run
1. Disconnect from hosts
    - Deletes any lingering temp files and disconnects from the remote node
    - Dry run: runs its own dry-run path
