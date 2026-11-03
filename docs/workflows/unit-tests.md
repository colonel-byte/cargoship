# unit-tests.yaml

**Triggers:** pull requests, merge groups.

Runs the fast test suites on a single `ubuntu-latest` runner, in one `unit` job with two steps.

The first step is `go run ./magefiles/core test:unit`, which is everything except the two e2e groups - those have runners of their own in `e2e.yaml` and `e2e-cluster.yaml`, drive `./test/...`, and need Docker and the better part of an hour. See `Test.Unit` in `magefiles/test.go` for the package list that leaves.

The second step replays the fuzz seed corpus with `test:fuzz`. It takes about a second and needs nothing the first step did not already need, so it rides along here rather than paying for a runner of its own. This is the only job that runs the `fuzz/` package - see [`../dev/fuzz-tests.md`](../dev/fuzz-tests.md).

Both steps run fully offline (`GOFLAGS=-mod=vendor`, `GOPROXY=off`, `GOSUMDB=off`, `GOTOOLCHAIN=local`), so accidental network access fails fast rather than passing on a runner and failing on an air-gapped management node. Both invoke the mage target through `go run ./magefiles/core` rather than a `mage` binary, which builds from `vendor/` and needs nothing installed on the runner.

Concurrency is grouped per ref with `cancel-in-progress`, so a new push supersedes an in-flight run. Default permissions are `contents: read`.
