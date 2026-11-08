# Why this directory is a symlink farm

The action plugins in the collection import their shared base the way Ansible requires:

```python
from ansible_collections.colonel_byte.cargoship.plugins.plugin_utils.projection import (
    CargoshipActionBase,
)
```

That is the only spelling Ansible accepts. It resolves at run time because Ansible installs a collection under `<collections-path>/ansible_collections/<namespace>/<name>/`, and the import path mirrors that layout exactly.

This repository does not use that layout. The collection is at `ansible/colonel_byte/cargoship/`, with no `ansible_collections` component -- which is correct, because `ansible-galaxy collection build` is pointed at the collection directory and supplies the namespace from `galaxy.yml`. The consequence is that a Python language server opening a checkout cannot resolve the import at all, and reports `reportMissingImports` on every action plugin.

`cargoship -> ../../../ansible/colonel_byte/cargoship` is the whole fix. With `hack` on the analysis path, `ansible_collections.colonel_byte.cargoship.…` resolves, and it resolves to the real files rather than to a copy -- so go-to-definition lands in `ansible/colonel_byte/cargoship/plugins/plugin_utils/projection.py`, where edits belong.

[`.vscode/settings.json`](../../.vscode/settings.json) sets `python.analysis.extraPaths` to `hack`, so VS Code and Pylance need no per-developer setup. For an editor that reads `pyrightconfig.json` instead, the equivalent is `{"extraPaths": ["hack"]}`. For anything driven by `PYTHONPATH` -- a REPL, a one-off script -- `PYTHONPATH=hack` does the same.

Nothing ships from here. `.goreleaser.yaml` names `ansible/colonel_byte/cargoship` directly everywhere it packages the collection, and `ansible-galaxy collection build` is pointed at that same directory, so this path is invisible to both.
