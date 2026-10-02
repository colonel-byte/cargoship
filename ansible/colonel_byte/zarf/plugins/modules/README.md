# The modules are wrapper binaries

There are no module files in this directory in the source tree, and there is no Python module behind any of them. Each module is a compiled binary: invoked under a name carrying the `zarf_` prefix, it reads its parameters as JSON -- from the WANT_JSON arguments file a conventional caller passes, or from stdin when this collection's action plugin invokes it -- and writes one JSON object back.

The binaries are built from `cmd/zarf/init` and `cmd/zarf/deploy` in the cargoship repository:

```sh
go build -o zarf_init ./cmd/zarf/init
go build -o zarf_package_deploy ./cmd/zarf/deploy
```

Put the results somewhere on the path of the node the play runs on, or name a path with the module's `wrapper_binary` parameter. Create `zarf_init` and `zarf_package_deploy` symlinks here as well for a conventional Ansible caller that stages the module file itself -- the action plugins do not need them, because they invoke the installed binaries directly.

ZEP-0072 proposes that this dispatch live inside the `zarf` binary, with each `zarf_<action>` as a symlink to `/usr/bin/zarf` and no wrappers at all. This collection proves the shape from outside, because a proof built in this repository cannot change zarf. See [choice-zarf-ansible-module](../../../../../docs/agent/choice-zarf-ansible-module.md).
