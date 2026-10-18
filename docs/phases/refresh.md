## refresh phases
1. Connect to hosts
    - Connects to a remote host via `github.com/k0sproject/rig`
1. Detect host operating systems
    - Gathers information about the remote host, including: OS and OS version
1. Gather host facts
    - Gathers network related information about the remote host, including: Hostname, Private Address, Private Interface. Will also update the hosts based off the profile if configured in the config file.
1. Gathering facts about the distro installed
    - Gathers information relating to the specific distro being installed, including: if the distro is installed, and what version it is running
1. Disconnect from hosts
    - Deletes any lingering temp files and disconnects from the remote node
