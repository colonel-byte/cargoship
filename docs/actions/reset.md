## reset phases
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
1. Gathering facts about the distro installed
    - Gathers information relating to the specific distro being installed, including: if the distro is installed, and what version it is running
1. Reset Worker
    - Deletes the worker from the cluster, if enabled it will try to drain node before removing the node
1. Reload service manager
    - Runs `systemctl daemon-reload` or equivalent on all hosts.
1. Release exclusive host lock
    - Deletes the lock file from each node, allowing other `cargoships` to run
1. Disconnect from hosts
    - Deletes any lingering temp files and disconnects from the remote node
