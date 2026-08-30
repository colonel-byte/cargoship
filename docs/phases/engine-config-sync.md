## engine-config-sync phases
1. Connect to hosts
    - Connects to a remote host via `github.com/k0sproject/rig`
1. Detect host operating systems
    - Gathers information about the remote host, including: OS and OS version
1. Acquire exclusive host lock
    - Runs a background task that will touch a file every 30 seconds on each remote node, this prevents other `cargoships` from doing any changes until the lock file has not been touch for over a minute
1. Gather host facts
    - Gathers network related information about the remote host, including: Hostname, Private Address, Private Interface. Will also update the hosts based off the profile if configured in the config file.
1. Validate hosts
    - Verifying that each node in the cluster has a unique name and private address, 
1. Gathering facts about the distro installed
    - Gathers information relating to the specific distro being installed, including: if the distro is installed, and what version it is running
1. Sync Registry Config Controller
    - If the remote node is a controller and its engine config (registries/audit/pss) has drifted from the desired state, drain the node, stop the service, write the new config, start the service, and uncordon the node sequentially
1. Sync Registry Config Worker
    - If the remote node is a worker and its engine config (registries/audit/pss) has drifted from the desired state, drain the node, stop the service, write the new config, start the service, and uncordon the node by the set concurrency limit
1. Updating kubeconfig file with the current cluster
    - If enabled, this will update the local kubeconfig with the admin creds for the current distro
1. Labeling nodes with their profile group
    - If enabled, this checks each node's node-role.kubernetes.io/<profile> label and adds it, set to "true", when missing or set to anything else
1. Release exclusive host lock
    - Deletes the lock file from each node, allowing other `cargoships` to run
1. Disconnect from hosts
    - Deletes any lingering temp files and disconnects from the remote node
