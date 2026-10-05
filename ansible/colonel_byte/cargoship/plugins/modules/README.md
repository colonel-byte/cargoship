# The modules are symlinks to the cargoship binary

There are no module files in this directory in the source tree, and there is no Python module behind any of them. The cargoship binary *is* the module: invoked under a name carrying the `cargoship_` prefix, it reads its parameters from the WANT_JSON arguments file and writes one JSON object back. See [choice-ansible-module](../../../../../docs/agent/choice-ansible-module.md).

The cargoship `.rpm`, `.deb`, and `.apk` packages install this collection to `/usr/share/ansible/collections/ansible_collections/colonel_byte/cargoship/`, which is on Ansible's default collections path, and create the five symlinks here pointing at `/usr/bin/cargoship`. Installing the package is all that is needed.

For a collection installed some other way -- `ansible-galaxy collection install`, or a checkout -- create the symlinks against whichever cargoship is on your path:

```sh
./hack/ansible-link-modules.sh ~/.ansible/collections/ansible_collections/colonel_byte/cargoship
```

The script is in the cargoship repository. It creates one symlink per action and nothing else.
