# Documentation Strategy and Agent Guidance for `colonel_byte.cargoship`

This directory tree defines the `colonel_byte.cargoship` Ansible collection. The documentation for this collection, its modules, and its roles published in the user guide under `docs/ansible/` is generated automatically from the code in this directory and validated by contract tests.

## Scope

This collection integrates Ansible with cargoship and nothing else. Every role and module here drives the `cargoship` binary from the management node: Ansible supplies the inventory, and cargoship opens every connection to the fleet itself.

A role that configures fleet hosts directly -- writing files on them, installing packages, managing services -- does not belong here, however convenient it would be to ship alongside. It belongs in the playbook that consumes this collection. See [choice-ansible-module](../../../docs/agent/choice-ansible-module.md) for why the work is split that way.

That boundary is load-bearing rather than stylistic. It is what lets `meta/runtime.yml` declare a controller range and no managed-node floor, and what the architecture note at the top of `README.md` asserts. A fleet-targeted role would falsify both.

## Markdown line formatting

Follow the line and table formatting rules in `ansible/AGENTS.md`:
- Do not hard-wrap lines in Markdown documents. Write full paragraphs and list items on single, continuous lines.
- Pad table cells so columns line up.

## Links that leave this directory

`README.md` and `galaxy.yml` ship inside the built collection, where a relative path into `docs/` does not resolve. Their links are absolute, and they point at the published book -- `https://colonel-byte.github.io/cargoship/<path>.html` -- rather than at the Markdown on GitHub. The reference pages are generated, and the book is where they are rendered with their links to one another intact.

Files that do not ship, and links between files inside `docs/`, stay relative.

## Architecture: Roles vs Direct Module Invocations

When generating playbooks, writing tasks, or answering user queries about invocation patterns, recognize the distinct layers of this collection:

| Pattern                                            | Target                     | Agent Guidance                                                                                                                                                                                                                                                                                                                                                 |
| -------------------------------------------------- | -------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **`include_role: colonel_byte.cargoship.cluster`** | High-level role            | **Default choice for standard playbooks.** Automatically applies `run_once: true`, `delegate_to: localhost`, credential protection with `no_log: true`, and merges `cargoship_cluster` / `cargoship_role_groups` into the projected inventory.                                                                                                                 |
| **`colonel_byte.cargoship.cargoship_<action>`**    | Low-level module primitive | **Execution primitives.** Symlinks directly to the `cargoship` binary. Use only when low-level control is required. If generating tasks with direct module calls, you **MUST** explicitly set `run_once: true` (or the cluster will be converged once per host in the fleet) and `delegate_to: localhost` (or the management host), as well as `no_log: true`. |

## Single Sources of Truth

Documentation content is maintained alongside the code rather than handwritten in `docs/ansible/`:

| Component                        | Source of Truth                                                             | Generated Documentation Page      |
| -------------------------------- | --------------------------------------------------------------------------- | --------------------------------- |
| Roles (e.g. `cluster`)           | `roles/<role>/meta/argument_specs.yml` and `roles/<role>/defaults/main.yml` | `docs/ansible/role_<role>.md`     |
| Modules (e.g. `cargoship_apply`) | `plugins/action/<module>.py` (`DOCUMENTATION` and `EXAMPLES` blocks)        | `docs/ansible/module_<module>.md` |
| Collection Overview              | `docs/ansible/collection.md`                                                | `docs/ansible/collection.md`      |
| Roles Overview                   | `docs/ansible/roles.md`                                                     | `docs/ansible/roles.md`           |
| Modules Overview                 | `docs/ansible/modules.md`                                                   | `docs/ansible/modules.md`         |

## Quote every description entry

In `roles/<role>/meta/argument_specs.yml` and in the `DOCUMENTATION` blocks, every entry of a `description:` list is written as a double-quoted scalar:

```yaml
description:
  - "The cargoship action to run. It selects which module the role calls."
  - "Every task carries C(run_once: true) and delegates to C(cargoship_delegate_to)."
```

An unquoted entry is a plain scalar, and a plain scalar cannot contain `: `. Descriptions here routinely do -- `C(run_once: true)`, `C(no_log: true)`, and any sentence with a colon before a clause -- and YAML reads the entry as a mapping instead, so the file either fails to parse or parses into the wrong shape. Quoting only the entries that happen to contain one means the next sentence with a colon in it breaks the file, so quote all of them.

`choices:` and `author:` entries stay unquoted. They are single tokens with no prose in them.

Nothing inside the quotes needs escaping today, because no description contains a double quote or a backslash. Rephrase rather than escape if one would.

## Updating Role Documentation

When modifying existing role variables or adding a new role to the collection:
1. Define the role variables, types, defaults, choices, descriptions, and synopsis in `roles/<role>/meta/argument_specs.yml`.
   - Double-quote every `description:` entry, per the rule above.
2. Define the corresponding default values in `roles/<role>/defaults/main.yml` (the key order in `defaults/main.yml` determines the table row order in generated documentation).
3. Run the generator to produce `docs/ansible/role_<role>.md`:

```sh
go run ./magefiles/core generate:document
```

## Updating Module Documentation

When modifying module parameters or adding a new module action plugin:
1. Update the `DOCUMENTATION = r"""..."""` block in `plugins/action/<module>.py`.
   - Each option under `options:` must specify `type:`, `description:`, and `cli_flag:` (the CLI flag the parameter renders, or `None` when it renders none -- because it is the command's positional argument, or because it is consumed before the command line is built).
   - `cli_flag:` is a cargoship extension rather than a standard Ansible documentation key. The generator reads it; `ansible-doc` would not know it. See [choice-ansible-module](../../../docs/agent/choice-ansible-module.md).
   - If `required: false` and a default value exists, provide `default:`.
   - Double-quote every `description:` entry, per the rule above.
   - Maintain the option order in the YAML mapping; the doc generator preserves declaration order for table rows.
2. Update the `EXAMPLES = r"""..."""` block in `plugins/action/<module>.py` with a realistic playbook snippet.
3. Update the matching parameter struct in Go (`internal/ansiblemod/<action>.go`).
4. Run the contract tests to ensure Go struct fields and Python docstring options match:

```sh
go test -v ./internal/ansiblemod -run TestActionPluginDocsMatchModuleParams
```

5. Regenerate the documentation:

```sh
go run ./magefiles/core generate:document
```

6. Commit the updated action plugins/specs and generated Markdown pages together.
