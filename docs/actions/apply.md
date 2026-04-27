## apply phases
1. Connect to hosts
    - Connects to a remote host via github.com/k0sproject/rig
1. Detect host operating systems
    - Gathers information about the remote host, including: OS and OS version
1. Acquire exclusive host lock
    - Runs a background task that will touch a file every 30 seconds on each remote node, this prevents other `cargoships` from doing any changes until the lock file has not been touch for over a minute
1. Gather host facts
    - Gathers network related information about the remote host, including: Hostname, Private Address, Private Interface
1. Validate hosts
    - Verifying that each node in the cluster has a unique name and private address, 
1. Prepare hosts
    - Updates the remote nodes; environment variables and sysctl
1. Prepare hosts - Enterprise Linux support
    - Installs container-selinux on systems that have SELinux enabled on them
1. Prepare hosts - Enterprise Linux support - Fapolicyd
    - Creates the distro supplied FAPolicy rules to /etc/fapolicyd/rules.d/31-cargoship.rules
1. Gathering facts about the distro installed
    - Gathers information relating to the specific distro being installed, including: if the distro is installed, and what version it is running
1. Updating hosts file for clusters nodes
    - If enabled, then this will modify the `/etc/hosts` file on the remote nodes with the fully qualified domain name for each node in the cluster
1. Updating hosts firewalld service
    - If enabled, then this will create a firewalld ipsets file with the known engine cidr blocks for the pod and service networks, and create a firewalld ipsets to allow each node in cluster to access all ports on the node
1. Updating hosts firewalld ports
    - If enabled, this will open any ports in the `.ports` section for each remote node
1. Upload files to hosts
    - Uploads the distro agnostic files to each remote node
1. Upload files to hosts -- RPM
    - If the remote node is an Enterprise Linux and the Distro package includes any files for those systems
1. Upload files to hosts -- APT
    - If the remote node is a Debian based Operating System and the Distro package includes any files for those systems
1. Upload files to hosts -- Binaries
    - Catch all phase if the combination of Operating System and Distro don't have other install methods
1. Configure engine
    - Runs distro specific operations
1. Initialize Controller
    - If the remote node does not have a running controller service, and is a controller, install the engine and start each service sequentially
1. Initialize Worker
    - If the remote node does not have a running worker service, and is not a controller, install the engine and start each service by the set concurrency limit
1. Upgrade Controller
    - If the remote node is a controller and is running an older version of the engine, drain the node, stop the service, upgrade the engine, start the service, and uncordon the node sequentially
1. Upgrade Worker
    - If the remote node is a worker and is running an older version of the engine, drain the node, stop the service, upgrade the engine, start the service, and uncordon the node by the set concurrency limit
1. Release exclusive host lock
    - Deletes the lock file from each node, allowing other `cargoships` to run
1. Disconnect from hosts
    - Deletes any lingering temp files and disconnects from the remote node
