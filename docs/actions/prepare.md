## prepare phases
1. Connect to hosts
    - Connects to a remote host via `github.com/k0sproject/rig`
1. Detect host operating systems
    - Gathers information about the remote host, including: OS and OS version
1. Acquire exclusive host lock
    - Runs a background task that will touch a file every 30 seconds on each remote node, this prevents other `cargoships` from doing any changes until the lock file has not been touch for over a minute
1. Prepare hosts
    - Updates the remote nodes; environment variables and sysctl
1. Prepare hosts - Enterprise Linux support
    - Installs container-selinux on systems that have SELinux enabled on them
1. Prepare hosts - Enterprise Linux support - Fapolicyd
    - Creates the distro supplied FAPolicy rules to /etc/fapolicyd/rules.d/31-cargoship.rules
1. Enable the requested kernel modueles
    - Turns on the list of requested modules on the host, then reboots the box if modules are added
1. Release exclusive host lock
    - Deletes the lock file from each node, allowing other `cargoships` to run
1. Disconnect from hosts
    - Deletes any lingering temp files and disconnects from the remote node
