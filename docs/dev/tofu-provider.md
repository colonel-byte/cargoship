# Running the OpenTofu provider

The provider lives at `cmd/terraform-provider-cargoship`, with its implementation in `internal/tofuprovider`. It is a package of this module rather than a module of its own; [choice-tofu-provider-layout](../agent/choice-tofu-provider-layout.md) records why, and [design-tofu-provider-install](../agent/design-tofu-provider-install.md) is the plan for what it will do. This page is how to run what exists.

## What exists

One resource, `cargoship_cluster`, and one data source, `cargoship_cluster_facts`.

`cargoship_cluster` converges a cluster on its configuration: `Create` and `Update` are the same call, because `action.NewApply` builds one phase list covering install, join and upgrade with each phase gated by its own `ShouldRun`. `Read` refreshes what the hosts report. `Delete` resets the cluster unless `retain_on_destroy` is set, in which case it drops it from state with a warning and leaves it running.

The data source, `cargoship_cluster_facts`. It runs `action.NewRefresh`, the read-only half of an apply -- `Connect`, `DetectOS`, `GatherFacts`, `GatherFactsDistro`, `Disconnect` -- and reports what each host says: operating system, version, architecture, hostname, private address, and the engine version it is running. It takes no cluster lock and writes nothing to any host. The phase list lives in `pkg/action` rather than in the provider so that "which phases are safe to run" is decided once; [docs/phases/refresh.md](../phases/refresh.md) is generated from it.

What is **not** there yet: `ModifyPlan`, so the plan-time downgrade and removed-host checks of [#307](https://github.com/colonel-byte/cargoship/issues/307) do not run; `ImportState`, which cannot work from an ID alone because the host blocks and their key paths are not recoverable from a running cluster; and acceptance tests. The `engine_version` of each node is in state, which is the input the plan-time check needs.

## The development loop

```sh
mage release:tofuProviderDev 0.1.0
```

That builds the provider for this machine into a `filesystem_mirror` layout under `build/`, and prints the directory to point OpenTofu at:

```
build/tofu-provider/mirror/registry.opentofu.org/colonel-byte/cargoship/0.1.0/linux_amd64/terraform-provider-cargoship_v0.1.0
```

Three things about that path are not arbitrary. The registry hostname is part of it because a configuration resolves a provider by source address and a mirror redirects that address rather than replacing it. The version directory has to hold a version OpenTofu accepts: **`0.0.0` is refused** -- it is reserved for a provider that is not published, and `tofu init` says so and points at `dev_overrides` -- so use `0.1.0` or anything higher. And the binary is named `terraform-provider-cargoship_v<version>`, which is the name OpenTofu looks for inside the directory.

Point a CLI configuration at the mirror:

```hcl
# ~/.tofurc, or anywhere named by TF_CLI_CONFIG_FILE
provider_installation {
  filesystem_mirror {
    path    = "/path/to/cargoship/build/tofu-provider/mirror"
    include = ["registry.opentofu.org/colonel-byte/*"]
  }
  direct {
    exclude = ["registry.opentofu.org/colonel-byte/*"]
  }
}
```

The `direct` block with the same exclusion is what stops OpenTofu reaching for the registry for this provider while leaving every other provider alone.

Then a configuration:

```hcl
terraform {
  required_providers {
    cargoship = {
      source  = "colonel-byte/cargoship"
      version = "0.1.0"
    }
  }
}

provider "cargoship" {
  concurrency     = 10
  connect_timeout = "2m"
}

data "cargoship_cluster_facts" "fleet" {
  name          = "bubbles"
  load_balancer = "10.0.0.10"
  distro        = "k3s"

  host {
    address  = "10.0.0.11"
    role     = "controller"
    user     = "operator"
    key_path = "~/.ssh/id_ed25519"
  }
}

output "nodes" {
  value = data.cargoship_cluster_facts.fleet.nodes
}
```

```sh
export TF_CLI_CONFIG_FILE=$PWD/.tofurc
tofu init
tofu plan
```

A successful read looks like this, against a host with no engine installed:

```
nodes = [
  {
    + address         = "127.0.0.1:2223"
    + arch            = "amd64"
    + engine_version  = "v0.0.0"
    + hostname        = "b67d7a425ad4"
    + os              = "ubuntu"
    + os_version      = "26.04"
    + private_address = "172.17.0.2"
    + role            = "controller"
  },
]
```

`engine_version` is `v0.0.0` for a host running no engine, which is the sentinel `GatherFactsDistro` records. It is the value a plan-time downgrade check will compare a package against, which is why it is in the schema before there is anything to compare.

### Rebuilding

**`tofu init` records a checksum of the provider binary in `.terraform.lock.hcl`, so a rebuilt binary fails the next plan** with "does not match any of the checksums recorded in the dependency lock file". Delete the lock file and re-init:

```sh
mage release:tofuProviderDev 0.1.0 && rm -f .terraform.lock.hcl && tofu init && tofu plan
```

That is the whole loop. Bump the version instead if you want two builds side by side.

### Attaching a debugger

OpenTofu launches a provider as a child process over a plugin protocol, so there is no `go run` path into it. Build it and run it with `--debug`, which prints a `TF_REATTACH_PROVIDERS` value to export in the shell that runs `tofu`:

```sh
go build -o /tmp/provider ./cmd/terraform-provider-cargoship
/tmp/provider --debug
```

## A host to read, without a cluster

The data source needs something that answers SSH, not a Kubernetes cluster. One bootloose container is enough, and it is the same image the cluster e2e suite uses:

```sh
ssh-keygen -q -t ed25519 -N "" -f /tmp/facts-key

docker run -d --name facts-host --privileged --tmpfs /run --tmpfs /tmp \
  -v /sys/fs/cgroup:/sys/fs/cgroup:rw --cgroupns=host -p 2223:22 \
  ghcr.io/colonel-byte/bootloose/ubuntu-26:latest /sbin/init

docker exec facts-host bash -c 'mkdir -p /root/.ssh && chmod 700 /root/.ssh'
docker cp /tmp/facts-key.pub facts-host:/root/.ssh/authorized_keys
docker exec facts-host bash -c 'chown root:root /root/.ssh/authorized_keys && chmod 600 /root/.ssh/authorized_keys'
docker exec facts-host systemctl start ssh
```

Both the `chown` and the mode matter: `docker cp` leaves the file owned by whichever uid the image's build left in place, and sshd refuses a key file it does not trust. The symptom is an authentication failure that looks like a wrong key, and it retries for the whole `connect_timeout` before saying so.

Then point a host block at `127.0.0.1` port `2223` with `key_path = "/tmp/facts-key"`, and `SSH_KNOWN_HOSTS=""` so the container's regenerated host key is not checked against a known-hosts file. Remove it with `docker rm -f facts-host` when finished.

For a fleet rather than one host, the cluster e2e suite writes a full inventory while it runs -- see [e2e-tests](e2e-tests.md) -- and the addresses, user and key path in it are what a host block needs.

## Applying for real

An apply needs a package and a fleet, so the loop is longer than the data source's:

```sh
mage build:binary
./build/cargoship_linux_amd64 package create example/k3s-flannel/v1_36/v1.36.4-k3s1 --output /srv/staging
```

That pulls the engine's artifacts and images, which is the one step that needs a network and around 1.5GB. Point `package` at the `.tar.zst` it writes, and the host blocks at a fleet -- the cluster e2e suite's bootloose inventory is the cheapest one to hand, see [e2e-tests](e2e-tests.md).

Two things to expect from the first apply. It is long and it looks silent: the phases log through cargoship's own logger, and OpenTofu shows a resource as "Still creating..." until the whole run returns. And a run that fails part way through **still writes state** -- deliberately, because an apply that failed has already changed hosts, and a resource that returned without writing would leave the next plan deciding nothing is installed and re-bootstrapping a live cluster. The error says so, and the nodes it reached are in state.

A destroy resets the cluster by default. `retain_on_destroy = true` drops the resource and leaves the cluster running, with a warning naming it and the `cargoship install reset` that would tear it down.

## `connect_timeout`, and why it exists

cargoship's connect phase retries for **ten minutes** (`pkg/phase/07_connect.go`), because a host rebooting into a new kernel mid-apply is ordinary. That is the wrong answer for a plan: a typo in an address would make `tofu plan` sit silent for ten minutes before failing. The provider bounds every read at `connect_timeout`, one minute by default, and says so when it gives up:

```
cargoship could not gather facts for bubbles: gave up after 1m0s: failed on 1 hosts:
 - 127.0.0.1:1: context deadline exceeded
client connect: retry: context done after 8 attempts: context deadline exceeded:
ssh dial: dial tcp 127.0.0.1:1: connect: connection refused
(raise connect_timeout if the fleet is slow to answer)
```

Raise it for a fleet that is genuinely slow to answer. Note that an authentication failure is retried the same way a refused connection is, so a wrong key also takes the full timeout to report.

## Running the tests

```sh
go test ./internal/tofuprovider/...     # unit: translation, state mapping, diagnostics
go test ./pkg/action/...                # the actions the provider calls, including the read-only one
go test ./cmd/cargoship/                # holds that the CLI does not reach the provider's packages
```

The unit tests need no cluster, no SSH and no `tofu` binary, and they are the ones that cover most of what a provider gets wrong. They can, because the provider calls cargoship through one interface -- `converger` -- which a fake implements from a table: everything below it needs a fleet, and everything above it is the state mapping and the diagnostics.

`TestCargoshipDoesNotDependOnTheProvider` is what keeps the provider's dependencies out of the CLI. The property it holds is that the linker never loads a package the import graph does not reach, so `cargoship` is the same size whatever the provider depends on -- and one import from a package the CLI already uses would undo that silently. It reads `go list -deps` and fails on any dependency under the provider's packages or `terraform-plugin`.

Acceptance tests with `terraform-plugin-testing` come with the resource, since what they are for is asserting that an apply converges a cluster. When they land they will need a `tofu` binary on the runner, and `TF_ACC_TERRAFORM_PATH` pointing at it -- the framework's harness looks for `terraform` by default.

## Publishing

`mage release:tofuProvider <version>` builds every published platform and pushes an OCI image index to `ghcr.io` as an OpenTofu provider mirror; `mage release:tofuProviderLayout <version>` stops before the push and leaves the artifact under `build/` to inspect. See [mage](mage.md) and [release-tofu-provider](../workflows/release-tofu-provider.md).
