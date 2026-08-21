## kube-config phases
1. Connect to hosts

    - Connects to a remote host via `github.com/k0sproject/rig`

1. Detect host operating systems

    - Gathers information about the remote host, including: OS and OS version

1. Acquire exclusive host lock

    - Runs a background task that will touch a file every 30 seconds on each remote node, this prevents other `cargoships` from doing any changes until the lock file has not been touch for over a minute

1. Gather host facts

    - Gathers network related information about the remote host, including: Hostname, Private Address, Private Interface. Will also update the hosts based off the profile if configured in the config file.

1. Updating kubeconfig file with the current cluster

    - If enabled, this will update the local kubeconfig with the admin creds for the current distro

1. Release exclusive host lock

    - Deletes the lock file from each node, allowing other `cargoships` to run

1. Disconnect from hosts

    - Deletes any lingering temp files and disconnects from the remote node
