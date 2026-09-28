# codeql.yaml

**Triggers:** push to `main`, pull requests, merge groups, weekly schedule (Mondays 05:37 UTC).

Runs GitHub CodeQL static analysis over two matrix targets: Go source (`build-mode: manual`) and GitHub Actions workflow files (`build-mode: none`).

For Go, CodeQL analyzes by building the code, so the `go build` step's package list - spelled out explicitly rather than `./...` - decides what gets extracted. Two directories are deliberately excluded from that list:

- `thirdparty-src/` - a second, intentionally non-compiling module holding verbatim upstream k3s/RKE2 source (see `docs/dev/thirdparty-src.md`). Anything that walks the checkout for module roots, including CodeQL's autobuilder, would otherwise try to resolve its unvendored imports and fetch modules from the network.
- `magefiles/` - build-tag-gated (`//go:build mage`) dev tooling with no `main` function of its own; it can't be compiled with plain `go build`. `magefiles/pkg/` under it is ordinary Go and would build, but it is dev tooling too and ships in no binary, so the whole tree is left unanalyzed.

The build runs fully offline (`GOPROXY=off`, `GOSUMDB=off`, vendored modules only) so any accidental network dependency fails loudly.
