## apply phases
With `--dry-run`, cargoship connects to every host and runs the preflight checks for real, then reports the phases it would have run instead of running them. Each phase below is labelled with what a dry run does with it. A phase is only run when it declares that it is safe to, so a phase added later is reported until someone says otherwise.

The report is not a static list. Every phase still prepares itself and checks whether it has anything to do, and both only read, so work that is already done is filtered out against the live hosts. A phase that could not be assessed, because it reads state an earlier reported phase would have created, is reported as `unassessed`.

A dry run takes no cluster lock, so it does not block a real run, and it can report state that a concurrent run is already changing. It does not need `--confirm`.

1. Connect to hosts
    - Connects to a remote host via `github.com/k0sproject/rig`
    - Dry run: runs, reads only. Connects opens the SSH session to each host and does nothing else. A dry run needs it, because a preflight that never reached a host would report on a cluster it never looked at.
1. Detect host operating systems
    - Gathers information about the remote host, including: OS and OS version
    - Dry run: runs, reads only. Reads `/etc/os-release` and the kernel to pick a configurer for the host. Reporting what each host runs is half of what makes a dry run worth running.
1. Acquire exclusive host lock
    - Runs a background task that will touch a file every 30 seconds on each remote node, this prevents other `cargoships` from doing any changes until the lock file has not been touch for over a minute
    - Dry run: reported, not run
1. Gather host facts
    - Gathers network related information about the remote host, including: Hostname, Private Address, Private Interface. Will also update the hosts based off the profile if configured in the config file.
    - Dry run: runs, reads only. Gather facts about each host by asks for its hostname, private interface and private address. All three are reads, and the rest of the run decides what it would do from them.
1. Validate hosts
    - Verifying that each node in the cluster has a unique name and private address, that its CPU architecture is one the package carries, and that its firewall rules are usable, 
    - Dry run: runs, reads only. Validate the hosts is the preflight itself: sudo, unique hostnames and addresses, host architecture, firewall rules and clock skew. A dry run that skipped it would check nothing.
1. Gathering facts about the distro installed
    - Gathers information relating to the specific distro being installed, including: if the distro is installed, and what version it is running
    - Dry run: runs, reads only. Gather distro related facts reads the engine version already on each host. That is what tells an upgrade apart from an install, and what catches a downgrade before a real run starts one.
1. Prepare hosts
    - Updates the remote nodes; environment variables and sysctl
    - Dry run: reported, not run
1. Prepare hosts - Enterprise Linux support
    - Installs container-selinux on systems that have SELinux enabled on them
    - Dry run: reported, not run
1. Prepare hosts - Enterprise Linux support - Fapolicyd
    - Creates the distro supplied FAPolicy rules to /etc/fapolicyd/rules.d/31-cargoship.rules
    - Dry run: reported, not run
1. Updating hosts file for clusters nodes
    - If enabled, then this will modify the `/etc/hosts` file on the remote nodes with the fully qualified domain name for each node in the cluster
    - Dry run: reported, not run
1. Updating hosts firewall
    - If enabled, this configures the firewall on each node that runs one, firewalld, ufw, or nftables, preferring the front end the node's OS ships. It trusts every other node in the cluster along with the engine's pod and service CIDRs, opens the ports in the `.host.ports` section, and applies the rules in the `.host.firewall.rules` section
    - Dry run: reported, not run
1. Upload files to hosts
    - Uploads the distro agnostic files to each remote node
    - Dry run: reported, not run
1. Upload files to hosts -- RPM
    - If the remote node is an Enterprise Linux and the Distro package includes any files for those systems
    - Dry run: reported, not run
1. Upload files to hosts -- APT
    - If the remote node is a Debian based Operating System and the Distro package includes any files for those systems
    - Dry run: reported, not run
1. Upload files to hosts -- Binaries
    - Catch all phase if the combination of Operating System and Distro don't have other install methods
    - Dry run: reported, not run
1. Configure engine
    - Runs distro specific operations
    - Dry run: reported, not run
1. Initialize Controller
    - If the remote node does not have a running controller service, and is a controller, install the engine and start each service sequentially
    - Dry run: reported, not run
1. Initialize Worker
    - If the remote node does not have a running worker service, and is not a controller, install the engine and start each service by the set concurrency limit
    - Dry run: reported, not run
1. Upgrade Controller
    - If the remote node is a controller and is running an older version of the engine, drain the node, stop the service, upgrade the engine, start the service, and uncordon the node sequentially
    - Dry run: reported, not run
1. Upgrade Worker
    - If the remote node is a worker and is running an older version of the engine, drain the node, stop the service, upgrade the engine, start the service, and uncordon the node by the set concurrency limit
    - Dry run: reported, not run
1. Sync Registry Config Controller
    - If the remote node is a controller and its engine config (registries/audit/pss) has drifted from the desired state, drain the node, stop the service, write the new config, start the service, and uncordon the node sequentially
    - Dry run: reported, not run
1. Sync Registry Config Worker
    - If the remote node is a worker and its engine config (registries/audit/pss) has drifted from the desired state, drain the node, stop the service, write the new config, start the service, and uncordon the node by the set concurrency limit
    - Dry run: reported, not run
1. Updating kubeconfig file with the current cluster
    - If enabled, this will update the local kubeconfig with the admin creds for the current distro
    - Dry run: reported, not run
1. Labeling nodes with their profile group
    - If enabled, this checks each node's `node-role.kubernetes.io/<profile>` label and adds it, set to "true", when missing or set to anything else
    - Dry run: reported, not run
1. Release exclusive host lock
    - Deletes the lock file from each node, allowing other `cargoships` to run
    - Dry run: reported, not run
1. Disconnect from hosts
    - Deletes any lingering temp files and disconnects from the remote node
    - Dry run: runs its own dry-run path
