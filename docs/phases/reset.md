## reset phases
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
1. Gather host facts
    - Gathers network related information about the remote host, including: Hostname, Private Address, Private Interface. Will also update the hosts based off the profile if configured in the config file.
    - Dry run: runs, reads only
1. Validate hosts
    - Verifying that each node in the cluster has a unique name and private address, that its CPU architecture is one the package carries, and that its firewall rules are usable, 
    - Dry run: runs, reads only
1. Gathering facts about the distro installed
    - Gathers information relating to the specific distro being installed, including: if the distro is installed, and what version it is running
    - Dry run: runs, reads only
1. Reset Worker
    - Deletes the worker from the cluster, if enabled it will try to drain node before removing the node
    - Dry run: reported, not run
1. Reset Controller
    - Deletes the controller from the cluster, if enabled it will try to drain node before removing the node
    - Dry run: reported, not run
1. Uninstalling Engine
    - Remove the rpm, apt, or binary files from all the hosts
    - Dry run: reported, not run
1. Reload service manager
    - Runs `systemctl daemon-reload` or equivalent on all hosts.
    - Dry run: reported, not run
1. Release exclusive host lock
    - Deletes the lock file from each node, allowing other `cargoships` to run
    - Dry run: reported, not run
1. Disconnect from hosts
    - Deletes any lingering temp files and disconnects from the remote node
    - Dry run: runs its own dry-run path
