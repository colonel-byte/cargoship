# Working in `magefiles/`

This directory houses the repository's task automation layer using Mage.

## Layout and Organization

The top level of `magefiles/` is strictly for Mage namespace declarations, targets, and flag parsers in `package main`. Do not place heavy domain logic, parsers, HTTP scrapers, code generators, or template walkers directly in `package main` files at the root of `magefiles/`.

All implementation logic belongs in modular Go packages under `magefiles/pkg/`:

| Path                        | Purpose                                                                                                             |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------- |
| `magefiles/build.go`        | Mage entrypoint for the `Build` namespace (`build:binary`, `build:all`, `build:examples`)                           |
| `magefiles/dev.go`          | Mage entrypoint for the `Dev` namespace (`dev:clean`, `dev:tidy`, `dev:vendor`, `dev:dnfPins`, etc.)                |
| `magefiles/generate.go`     | Mage entrypoint for the `Generate` namespace (`generate:document`, `generate:schema`, etc.)                         |
| `magefiles/release.go`      | Mage entrypoint for the `Release` namespace (`release:tofuProvider`, `release:tofuProviderLayout`)                  |
| `magefiles/test.go`         | Mage entrypoint for the `Test` namespace (`test:endToEnd`, `test:fuzz`, etc.)                                       |
| `magefiles/core/core.go`    | Bootstrapping main package for `go run ./magefiles/core`                                                            |
| `magefiles/pkg/build/`      | Binary building, compiler/linker flag assembly, and example package compilation                                     |
| `magefiles/pkg/devtools/`   | Developer tooling: AlmaLinux repodata parsing (`dnfpins`), Scorecard vendor overrides (`osv`)                       |
| `magefiles/pkg/gen/`        | Generators: `completion/`, `docs/`, `engineconfig/` (k3s/rke2 codegen & pins), `examples/`, `schema/`, `zarfflags/` |
| `magefiles/pkg/release/`    | Release artifacts goreleaser does not build: the provider's OCI mirror artifact                                     |
| `magefiles/pkg/testrunner/` | End-to-end and fuzz test runners                                                                                    |
| `magefiles/pkg/util/`       | Shared helper functions (aligned table formatting, git commit resolution, clean utilities)                          |

## Target Structure Rules

1. **Keep Root Namespace Targets Thin**: Root Mage methods (`build.go`, `dev.go`, `generate.go`, `release.go`, `test.go`) should only perform argument defaults, environmental setup, or high-level status output, delegating immediately to the appropriate package under `magefiles/pkg/`.
2. **Encapsulate and Test**: Logic under `magefiles/pkg/` should be written as standard Go packages with unit test files (`*_test.go`) adhering to the table-driven test formatting guidelines in the root `AGENTS.md`.
