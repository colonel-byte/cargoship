# Running the cluster suite from CI against ephemeral fleets

The cluster phase suite has one seam a CI system needs: `CARGOSHIP_E2E_BACKEND=inventory` provisions nothing and walks every phase against hosts that already exist. [e2e-external-inventory](e2e-external-inventory.md) is the reference for that backend -- the `ZarfCluster` document it reads, what each assertion does against a fleet the suite did not choose, and the join, reset and re-join sequence. **Read that page first.** This one is about everything around it: getting a pipeline from "a commit landed" to "the engines walked against mixed Ubuntu and Enterprise Linux fleets on Proxmox, vSphere or EC2, and the fleets are gone again".

[e2e-tests](e2e-tests.md) and [e2e-phase-tests](e2e-phase-tests.md) cover the suite itself, [microvm](microvm.md) is the local VM fleet, and [`test/AGENTS.md`](https://github.com/colonel-byte/cargoship/blob/main/test/AGENTS.md) is how to read a result once you have one.

## Why an ephemeral cloud fleet, when two backends already exist

Nothing in CI today runs the microvm or inventory backends, or any walk past phase 60: [e2e-cluster](../workflows/e2e-cluster.md) runs the staging half on bootloose containers and stops before an engine starts anywhere. So this is new ground rather than a port, and it is worth being precise about what it buys, because "more coverage" is not the answer.

Each existing backend is missing a different half:

*   **bootloose** carries several OS families and creates a real new host for the join walk. It reports no SELinux, runs no fapolicyd, and has no firewall of its own -- so the one branch in each of those phases that can stop an apply is the branch never run.
*   **microvm** has SELinux enforcing, fapolicyd running and firewalld present. It cannot reach the cluster API from the host, so `80_KubeConfig`, `81_LabelNodes` and `ZZ1_ClusterHealthy` skip; and it runs one distribution, so every family-routed branch collapses to one answer.

A fleet of ephemeral cloud instances is the first thing that gives **SELinux enforcing, a reachable cluster API, and a mixed-family fleet at the same time**. That combination is the whole argument for this page. It is also what makes the open questions answerable: the k3s SELinux policy gap in [microvm](microvm.md#the-shipped-k3s-packages-and-what-is-still-unproven), and the upgrade steps `test/AGENTS.md` lists as unproven rather than passing.

## The contract

The pipeline's whole job is five steps. Everything else on this page is a detail of one of them.

1.  **Provision** -- N instances, mixed families, tagged with the run ID.
2.  **Render** a `ZarfCluster` document naming them.
3.  **Export** four environment variables.
4.  **Run** `go test` against `./test/e2e/cluster/...`.
5.  **Destroy**, on the success path and the abort path both.

Step 4 has a mage wrapper and a raw form:

```sh
mage test:endToEndClusterInventory "$PWD/fleet.yaml"
```

```sh
CARGOSHIP_E2E_BACKEND=inventory \
CARGOSHIP_E2E_INVENTORY="$PWD/fleet.yaml" \
CARGOSHIP_E2E_DISTRO=rke2 \
SSH_KNOWN_HOSTS="" \
  go test -mod=vendor -count=1 -v -timeout=90m ./test/e2e/cluster/...
```

The mage target sets `CARGOSHIP_E2E_BACKEND` and `CARGOSHIP_E2E_INVENTORY` and runs with `-timeout=2h`. It does **not** set `CARGOSHIP_E2E_DISTRO`, so a matrix cell either exports it around the target or uses the raw form. The raw form is what the snippets below use, because a matrix cell wants its own timeout.

### Environment

| Variable                          | What it does                                                                                       | Default     |
| --------------------------------- | -------------------------------------------------------------------------------------------------- | ----------- |
| `CARGOSHIP_E2E_BACKEND`           | `inventory` to select this backend. An unrecognised value is an error, not a fallback              | `bootloose` |
| `CARGOSHIP_E2E_INVENTORY`         | Path to the `ZarfCluster` document. Required for this backend                                      | unset       |
| `CARGOSHIP_E2E_DISTRO`            | `k3s`, `rke2` or `upstream`: which engine the walks install                                        | `k3s`       |
| `SSH_KNOWN_HOSTS`                 | Set to `""` when the hosts' keys are not in a known-hosts file the test binary reads. Always, here | unset       |
| `CARGOSHIP_E2E_JOIN_HOST`         | `hostname` the join walk joins. Unset skips that walk                                              | unset       |
| `CARGOSHIP_E2E_UPGRADE`           | `1` opts the upgrade walk in                                                                       | off         |
| `CARGOSHIP_E2E_TMPDIR`            | Parent for the harness temp directories, alongside `TMPDIR`                                        | system temp |
| `CARGOSHIP_E2E_KEEP_CLUSTER`      | Diagnostics only. Means nothing here -- this backend creates nothing to keep                       | off         |
| `CARGOSHIP_E2E_STAGE_ONLY`        | Stop at the phase-60 staging boundary. Useful as a cheap smoke test of a new platform              | off         |
| `CARGOSHIP_E2E_KEEP_REGISTRY_LOG` | Keeps the in-memory OCI registry request log for passing tests                                     | unset       |

A `-run` filter picks a single walk: `TestClusterPhases/apply`, `/join`, `/upgrade`, `/reset` or `/dryrun`.

## Two sides, two sets of requirements

The split that surprises people the first time: the suite builds the package it installs, in-process, with `distro.Create`. So the **runner** is the machine that needs a network and the disk, and the **hosts** are the machines that do not.

**The runner needs** a Go toolchain matching `go.mod`, the vendored tree, outbound network for every image the package pulls, and tens of gigabytes of scratch. There is no binary to build -- the cluster suite calls the cargoship packages directly -- so there is nothing to download from an earlier job either. Use the same environment the existing workflows do:

```sh
export CGO_ENABLED=0 GOFLAGS=-mod=vendor GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local
```

Everything the test build needs is vendored, so turning the proxy off makes accidental network access fail immediately rather than succeed quietly against a different version than `vendor/` holds.

**The hosts need no network at all**, which is the product. What they do need:

*   **Key-based SSH as a user who can `sudo` without a password.** Every phase runs privileged commands through `Sudo()`.
*   **No engine installed.** `Test_12_GatherFactsDistro` asserts every host reports no engine version before anything is installed, which is what makes the later upgrade assertions mean anything.
*   **Node-to-node traffic permitted** on the engine's own ports, not only SSH from the runner.
*   **On Enterprise Linux: `dnf versionlock` and `container-selinux` already in the image.** Phase 21 is fatal when it cannot install the latter, and an air-gapped host cannot fetch either.
*   **Enough root volume for the package**, which runs to gigabytes and is staged on every node.

## The matrix

**Against your own fleet, the matrix is the only thing guaranteeing an Enterprise Linux host reached the RPM phase and a Debian host reached the APT phase.** The suite's family guards -- the ones that fail a fleet meant to cover a branch that lost the host for it -- stand down on this backend, because they cannot be said about hosts the suite did not choose. On bootloose a missing family is a loud failure. Here it is a silent gap, and the matrix is where it gets closed.

So the matrix is designed from what each engine actually does on each family, which is narrower and more interesting than three engines times two families:

| Engine     | On Enterprise Linux hosts                                                                                                                                       | On Ubuntu hosts                                                                                                           |
| ---------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| `rke2`     | Phase 57, a real `dnf` install, and the SELinux policy actually loaded -- the examples select `package: rpm` for it, so `semodule -l` lists it                  | Phase 59, the tarball path. Verified on a mixed fleet: no `dpkg` entry, a 133MB `/usr/local/bin/rke2`, the node `Ready`   |
| `k3s`      | **Open question.** The examples ship `k3s-selinux-*.rpm` under `selector.package: binary`, and the BIN phase runs no package manager, so its `%post` never runs | Works. No SELinux, so the gap to its left cannot bite                                                                     |
| `upstream` | **Blocked** at `Test_60_ConfigureEngine` on [#849](https://github.com/colonel-byte/cargoship/issues/849)                                                        | Blocked on the same -- and the only engine shipping a `.deb`, so the only one that would ever exercise a real apt install |

The `upstream` row is not a platform problem: `ConfigPath()` is a directory for that distro and the assertion `cat`s it. The `k3s` middle cell is the interesting one -- a node left with SELinux enforcing, an engine told to use it, and no policy for it. Nobody has run it on a real EL fleet yet.

Which gives four cells, each with a job:

| Cell | Engine     | Fleet                                         | Why it exists                                                                                                                                                                                                                                                                                                                                                      | Expect                                |
| ---- | ---------- | --------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------- |
| 1    | `rke2`     | 1 EL controller, 1 EL worker, 1 Ubuntu worker | **The regression gate.** The only cell driving phase 57 and phase 59 as real paths in one walk, with SELinux and fapolicyd enforcing, and reaching the cluster API -- so `80`, `81` and `ZZ1` assert against a real fleet. `rke2` is also the only engine declaring `tools.etcdctl`, which is what covers phase 22's generated allow rule for an executable target | green                                 |
| 2    | `k3s`      | 3 EL                                          | **Answers the open question above.** An ephemeral fleet is the cheapest way to find out, and the node's journal is where the answer is                                                                                                                                                                                                                             | may fail; the journal is the artifact |
| 3    | `k3s`      | 1 EL controller, 2 Ubuntu workers             | Family routing under the default engine, with a reachable API                                                                                                                                                                                                                                                                                                      | green                                 |
| 4    | `upstream` | 1 EL controller, 2 Ubuntu workers             | Wired now, failing at `Test_60` until #849 lands -- at which point it is the only cell covering a real `.deb` install                                                                                                                                                                                                                                              | known failure, pinned to #849         |

Cells 2 and 4 are `continue-on-error`. A cell that is expected to fail and is allowed to fail is a cell that reports a *change* in behaviour, which is the whole point of running it; a cell that is expected to fail and blocks the pipeline gets deleted within a week.

### What no cloud fleet closes

*   **musl, and phase 59 as the fallback it is.** The bootloose fleet's Alpine node is upload-only because rke2 links against glibc, and that is the only thing exercising the BIN path as a fallback rather than as a chosen route. Keep the container workflow; a cloud cell does not substitute for it.
*   **A real apt install.** Only the `upstream-*` packages carry a `.deb`, so this waits on #849, not on the pipeline. A `k3s` or `rke2` fleet with Ubuntu hosts exercises the *routing* -- which phase claims which host -- and phase 58 runs with nothing to install.
*   **Other families.** `IsEnterpriseLinux` already accepts Alma, Amazon, CentOS, Fedora, Oracle and Rocky, and `IsDebianLinux` accepts Debian proper. Each is another cell, not a flag.
*   **Architecture.** `CARGOSHIP_E2E_DISTRO` selects an engine, not an arch -- the example definition decides that. Nothing here covers arm.

### The two extra walks

Separate jobs rather than extra cells, because each changes the time and disk budget:

*   **The upgrade walk**, `CARGOSHIP_E2E_UPGRADE=1`. It installs a second complete set of engine images onto every node, so it roughly doubles the disk and adds tens of minutes. It is also where a cloud fleet earns its keep: `Test_67_UpgradeWorkers`, `Test_71_EngineConfigSyncController` and `Test_72_EngineConfigSyncWorker` have not been exercised on *any* backend, so they are unproven rather than passing.
*   **The reset and re-join round trip.** The one path through this suite where a host's second install has to behave like its first -- anything the reset left behind shows up as a phase that finds work already done, or as an engine starting against stale configuration. [e2e-external-inventory](e2e-external-inventory.md#removing-a-host-and-adding-it-back) is the worked sequence.

## Provisioning

The pipeline needs exactly three facts per host: the name the node will call itself, its role, and the address its peers reach it on. Anything that can emit those will do -- the examples below are OpenTofu because it is the common denominator across the three platforms, not because the suite cares.

Keep one thing straight: this HCL makes the *hosts*. [`terraform-provider-cargoship`](https://github.com/colonel-byte/terraform-provider-cargoship) drives the *phases*, and it is a different job -- here the e2e suite is the thing under test, so the suite is the driver and OpenTofu stops at the instances.

Every example below declares the same fleet and ends in the same output, so the rendering step in the next section is written once:

```hcl
locals {
  fleet = [
    { name = "kc0", role = "controller", family = "el" },
    { name = "kw0", role = "worker", family = "el" },
    { name = "kw1", role = "worker", family = "debian" },
  ]
}

output "hosts" {
  value = [for h in local.fleet : {
    hostname        = h.name
    role            = h.role
    address         = ...  # what the runner reaches over SSH
    private_address = ...  # what the other nodes reach
    user            = h.family == "el" ? "rocky" : "ubuntu"
  }]
}
```

### Bake these into the image

The same list for all three platforms, and every item is something an air-gapped host cannot fetch later:

*   `cloud-init`, and the CI SSH public key in the default user's `authorized_keys`.
*   `NOPASSWD` sudo for that user.
*   An NTP client, enabled. A long-lived template drifts, and the symptom is certificate errors.
*   On Enterprise Linux: `container-selinux` and the `python3-dnf-plugin-versionlock` package. Phase 21 is fatal without the first.
*   Nothing else. In particular no engine, no containerd, no `/etc/cargoship` -- the apply walk asserts their absence.

### EC2

```hcl
variable "run_id" { type = string }
variable "vpc_id" { type = string }
variable "subnet_id" { type = string }
variable "key_name" { type = string }
variable "runner_cidr" { type = string }

data "aws_ami" "el" {
  most_recent = true
  owners      = ["792107900819"] # Rocky Linux
  filter {
    name   = "name"
    values = ["Rocky-9-EC2-Base-9.*.x86_64-*"]
  }
}

data "aws_ami" "debian" {
  most_recent = true
  owners      = ["099720109477"] # Canonical
  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*"]
  }
}

# One group whose members may say anything to each other, plus SSH from the runner.
# The self-referencing rule is the one that matters: the engine's ports are not a
# list worth maintaining, and a fleet that can only be reached and cannot talk
# fails at the worker join rather than here.
resource "aws_security_group" "fleet" {
  name   = "cargoship-e2e-${var.run_id}"
  vpc_id = var.vpc_id

  ingress {
    from_port = 0
    to_port   = 0
    protocol  = "-1"
    self      = true
  }
  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = [var.runner_cidr]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_instance" "node" {
  for_each = { for h in local.fleet : h.name => h }

  ami                    = each.value.family == "el" ? data.aws_ami.el.id : data.aws_ami.debian.id
  instance_type          = "t3.xlarge"
  subnet_id              = var.subnet_id
  key_name               = var.key_name
  vpc_security_group_ids = [aws_security_group.fleet.id]

  root_block_device {
    volume_size = 60
  }

  # preserve_hostname:false plus prefer_fqdn_over_hostname:false is what makes the
  # node call itself kc0 rather than ip-10-0-1-5.ec2.internal. See the hostname
  # entry in the edge cases below -- the suite matches hosts to nodes by this name.
  user_data = <<-CLOUD
    #cloud-config
    preserve_hostname: false
    prefer_fqdn_over_hostname: false
    hostname: ${each.key}
  CLOUD

  tags = {
    Name            = each.key
    CargoshipE2ERun = var.run_id
    Role            = each.value.role
  }
}

output "hosts" {
  value = [for h in local.fleet : {
    hostname        = h.name
    role            = h.role
    address         = aws_instance.node[h.name].public_ip
    private_address = aws_instance.node[h.name].private_ip
    user            = h.family == "el" ? "rocky" : "ubuntu"
  }]
}
```

`CargoshipE2ERun` is what the reaper in the GitHub Actions section matches on, and it is not optional. Use `private_ip` for `privateAddress` and the public address only for SSH; on a fleet with no public addressing at all, point both at the private one and put the runner in the VPC.

### Proxmox

Using the `bpg/proxmox` provider, cloning per-family templates:

```hcl
variable "run_id" { type = string }
variable "node_name" { type = string }
variable "bridge" { type = string }
variable "ssh_public_key" { type = string }
variable "snippet_datastore" { type = string }

variable "template_ids" {
  type    = map(number)
  default = { el = 9001, debian = 9002 }
}

# The provider derives cloud-init's local-hostname from the VM name, and the VM
# name has to carry the run ID to stay unique across concurrent runs -- which
# would leave the node calling itself cargoship-e2e-<run>-kc0. The suite matches
# hosts to nodes by the name the node reports, so set it explicitly.
resource "proxmox_virtual_environment_file" "meta" {
  for_each = { for h in local.fleet : h.name => h }

  node_name    = var.node_name
  datastore_id = var.snippet_datastore
  content_type = "snippets"

  source_raw {
    file_name = "cargoship-e2e-${var.run_id}-${each.key}.yaml"
    data      = <<-META
      instance-id: cargoship-e2e-${var.run_id}-${each.key}
      local-hostname: ${each.key}
    META
  }
}

resource "proxmox_virtual_environment_vm" "node" {
  for_each = { for h in local.fleet : h.name => h }

  name      = "cargoship-e2e-${var.run_id}-${each.key}"
  node_name = var.node_name
  # Tags are how teardown finds the fleet if the pipeline dies holding state.
  tags = ["cargoship-e2e", var.run_id]

  clone {
    vm_id = var.template_ids[each.value.family]
    full  = true
  }

  cpu {
    cores = 4
  }
  memory {
    dedicated = 8192
  }

  # The guest agent is the only way ipv4_addresses is populated, and without it
  # the output below is empty rather than wrong -- which at least fails loudly.
  agent {
    enabled = true
  }

  initialization {
    meta_data_file_id = proxmox_virtual_environment_file.meta[each.key].id

    ip_config {
      ipv4 {
        address = "dhcp"
      }
    }
    user_account {
      username = each.value.family == "el" ? "rocky" : "ubuntu"
      keys     = [var.ssh_public_key]
    }
  }

  network_device {
    bridge = var.bridge
  }
}

output "hosts" {
  value = [for h in local.fleet : {
    hostname        = h.name
    role            = h.role
    address         = proxmox_virtual_environment_vm.node[h.name].ipv4_addresses[1][0]
    private_address = proxmox_virtual_environment_vm.node[h.name].ipv4_addresses[1][0]
    user            = h.family == "el" ? "rocky" : "ubuntu"
  }]
}
```

Two Proxmox-specific hazards, both of which present as something else:

*   **VMID allocation collides between concurrent runs.** Two pipelines cloning at once can race for the next free ID and one clone fails with a message about the template rather than about contention. Either hand each run an explicit `vm_id` range, or serialise provisioning -- the `lock()` step in the Jenkins section is there for this.
*   **The bridge must not isolate guests.** A VLAN-aware bridge with the wrong tag, or a firewall rule at the bridge, gives a fleet that answers SSH and cannot form a cluster.

`ipv4_addresses[1]` is the first non-loopback interface. On a multi-homed template that index is a guess; see `privateInterface` in the rendering section.

### vSphere

```hcl
variable "run_id" { type = string }
variable "ssh_public_key" { type = string }

variable "templates" {
  type    = map(string)
  default = { el = "rocky9-cargoship-e2e", debian = "ubuntu2404-cargoship-e2e" }
}

data "vsphere_virtual_machine" "template" {
  for_each      = var.templates
  name          = each.value
  datacenter_id = data.vsphere_datacenter.dc.id
}

# A folder per run, so teardown is folder-scoped rather than name-matched.
resource "vsphere_folder" "fleet" {
  path          = "cargoship-e2e/${var.run_id}"
  type          = "vm"
  datacenter_id = data.vsphere_datacenter.dc.id
}

resource "vsphere_virtual_machine" "node" {
  for_each = { for h in local.fleet : h.name => h }

  name             = each.key
  folder           = vsphere_folder.fleet.path
  resource_pool_id = data.vsphere_compute_cluster.cluster.resource_pool_id
  datastore_id     = data.vsphere_datastore.ds.id

  num_cpus = 4
  memory   = 8192
  guest_id = data.vsphere_virtual_machine.template[each.value.family].guest_id

  network_interface {
    network_id = data.vsphere_network.net.id
  }

  disk {
    label = "disk0"
    size  = 60
  }

  clone {
    template_uuid = data.vsphere_virtual_machine.template[each.value.family].id
  }

  # cloud-init reaches the guest through guestinfo rather than a seed ISO. The
  # metadata must be base64 and gzipped; userdata base64 alone.
  extra_config = {
    "guestinfo.metadata" = base64gzip(yamlencode({
      "local-hostname" = each.key
      "public-keys"    = [var.ssh_public_key]
    }))
    "guestinfo.metadata.encoding" = "gzip+base64"
  }

  # The suite needs an address before it needs anything else, so wait for one.
  wait_for_guest_net_timeout = 10
}

output "hosts" {
  value = [for h in local.fleet : {
    hostname        = h.name
    role            = h.role
    address         = vsphere_virtual_machine.node[h.name].default_ip_address
    private_address = vsphere_virtual_machine.node[h.name].default_ip_address
    user            = h.family == "el" ? "rocky" : "ubuntu"
  }]
}
```

`default_ip_address` is whichever address VMware Tools reports first, which on a multi-homed template is not necessarily the one the cluster should use. That is the `privateInterface` case below, and vSphere is where it comes up most.

## Rendering the document

The document the suite reads is the same `ZarfCluster` document `cargoship apply --config` takes, loaded through the CLI's own loader -- so anything cargoship accepts is accepted here, and anything it rejects fails before a host is touched. [e2e-external-inventory](e2e-external-inventory.md#the-document) has the annotated shape.

YAML is a superset of JSON, and the loader is a YAML loader, so a pipeline does not need a templating language or `yq`: `jq` emitting JSON produces a document the suite reads as-is.

```sh
tofu output -json hosts | jq \
  --arg key "$SSH_KEY_PATH" \
  --arg name "cargoship-e2e-$RUN_ID" '
  {
    apiVersion: "colonel-byte.dev/v1alpha1",
    kind: "Cluster",
    metadata: { name: $name },
    spec: {
      config: {
        loadbalancer: (map(select(.role == "controller")) | first | .private_address)
      },
      hosts: map({
        ssh: { address: .address, user: .user, port: 22, keyPath: $key },
        hostname: .hostname,
        privateAddress: .private_address,
        role: .role,
        profile: .role
      })
    }
  }' > fleet.yaml
```

Three fields decide whether the run means anything:

*   **`role`, on every host.** This backend reads roles from the document and never from the hostname -- the other two fleets name their machines `kc0` and `kw0` and let a prefix carry it, and yours are whatever your platform called them. A document with no `role: controller` is refused before a host is touched, which is the one mistake here that reports itself.
*   **`privateAddress`, on every host.** The firewall and `/etc/hosts` phases distribute these to every peer. Fact gathering will discover an address if you leave it out, and on a multi-homed host it discovers the one holding the default route.
*   **`privateInterface`, wherever the default route is not on the interface the nodes talk to each other over.** This is the field that makes a wrong fleet look right: the phases distribute addresses no peer can reach, and *everything up to the engine still passes*. On vSphere and on any instance with a second NIC, set it rather than letting discovery guess.

Validate before the provisioning cost is sunk. A dry run reads the document and reports over live state without changing anything:

```sh
cargoship apply --config fleet.yaml --dry-run <package>
```

## GitHub Actions

```yaml
---
name: E2E ephemeral fleet

on:
  workflow_dispatch:
  schedule:
    - cron: "0 3 * * *"

permissions:
  contents: read

concurrency:
  group: e2e-ephemeral-${{ github.ref }}
  cancel-in-progress: true

jobs:
  cloud-fleet:
    # Above the go test -timeout below, so an overrun is reported as a Go failure
    # naming the phase it was in rather than as a cancelled job.
    timeout-minutes: 150
    strategy:
      fail-fast: false
      matrix:
        include:
          - { cell: 1, distro: rke2, fleet: mixed-el-controller, expect: pass }
          - { cell: 2, distro: k3s, fleet: all-el, expect: investigate }
          - { cell: 3, distro: k3s, fleet: mixed-el-controller, expect: pass }
          - { cell: 4, distro: upstream, fleet: mixed-el-controller, expect: fail-849 }
    # Cells 2 and 4 report a change in behaviour rather than gate the pipeline.
    continue-on-error: ${{ matrix.expect != 'pass' }}
    runs-on: ubuntu-latest
    env:
      RUN_ID: ${{ github.run_id }}-${{ matrix.cell }}
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v7
        with:
          go-version-file: go.mod

      # The package the suite builds runs to gigabytes, which is more than the
      # ~14GB a hosted runner leaves free comfortably holds.
      - name: Free disk space
        run: |
          sudo rm -rf /usr/share/dotnet /usr/local/lib/android /opt/ghc
          df -h /

      - name: Provision
        run: |
          tofu -chdir=ci/tofu/ec2 init
          tofu -chdir=ci/tofu/ec2 apply -auto-approve \
            -var="run_id=$RUN_ID" -var="fleet=${{ matrix.fleet }}"

      # Phases 22 and 26 ask whether fapolicyd and firewalld are *running*. A fleet
      # handed over any earlier looks healthy with both of those phases skipping.
      - name: Wait for cloud-init
        run: ci/wait-for-cloud-init.sh

      - name: Render inventory
        run: ci/render-inventory.sh > fleet.yaml

      - name: Run the apply walk
        run: |
          go test -mod=vendor -count=1 -v -timeout=120m ./test/e2e/cluster/... \
            2>&1 | tee run.log
        env:
          CARGOSHIP_E2E_BACKEND: inventory
          CARGOSHIP_E2E_INVENTORY: fleet.yaml
          CARGOSHIP_E2E_DISTRO: ${{ matrix.distro }}
          SSH_KNOWN_HOSTS: ""
          CGO_ENABLED: 0
          GOFLAGS: -mod=vendor
          GOPROXY: "off"
          GOSUMDB: "off"
          GOTOOLCHAIN: local

      # A skip here is a capability the fleet does not have, not a passing test.
      # Fail the cell when more stood down than this backend is known to stand down.
      - name: Check the skip count
        run: |
          skips=$(grep -c '^ *--- SKIP' run.log || true)
          echo "stood down: $skips"
          test "$skips" -le "${EXPECTED_SKIPS:-4}"

      - name: Collect diagnostics
        if: always()
        run: ci/collect-diagnostics.sh

      - name: Upload diagnostics
        if: always()
        uses: actions/upload-artifact@v7
        with:
          name: e2e-ephemeral-cell-${{ matrix.cell }}
          path: diagnostics/
          if-no-files-found: ignore

      - name: Destroy
        if: always()
        run: |
          tofu -chdir=ci/tofu/ec2 destroy -auto-approve -var="run_id=$RUN_ID"
```

Pin every action to a 40-character SHA with the tag in a trailing comment before this goes anywhere near the repository -- the floating `@v7` above is for readability here. [`.github/AGENTS.md`](https://github.com/colonel-byte/cargoship/blob/main/.github/AGENTS.md) has the rest of the Scorecard-safe rules, including the `docs/workflows/<name>.md` summary a committed workflow needs.

Three things that bite:

*   **`if: always()` is not a teardown guarantee.** It does not reliably survive a cancelled job and never survives a killed runner, and this backend creates real instances that nothing else will clean up. A scheduled reaper matching the run tag is not optional -- it is the only thing between you and a month of orphaned instances:

    ```sh
    aws ec2 describe-instances \
      --filters "Name=tag-key,Values=CargoshipE2ERun" "Name=instance-state-name,Values=running" \
      --query 'Reservations[].Instances[?LaunchTime<=`'"$(date -u -d '6 hours ago' +%FT%TZ)"'`].InstanceId' \
      --output text | xargs -r aws ec2 terminate-instances --instance-ids
    ```

*   **Credentials and reachability.** EC2 works from a hosted runner with OIDC. Proxmox and vSphere are usually on a private network, so they need a self-hosted runner -- which changes the disk and network assumptions above, since the free-space step and the hosted runner's own 14GB no longer apply. The SSH private key is a repository secret, and the hosts it opens must be ones you are willing to destroy.
*   **The timeout ordering.** The job timeout sits above the `go test -timeout`, which sits above the suite's own phase timeouts. Invert any pair of those and a slow phase is reported as a cancelled job with no phase named.

## Jenkins

The same five steps. `post { always { ... } }` does cover an aborted build, which is the one place Jenkins is straightforwardly better here.

```groovy
pipeline {
  agent none
  options { timeout(time: 150, unit: 'MINUTES') }

  parameters {
    choice(name: 'PLATFORM', choices: ['ec2', 'proxmox', 'vsphere'])
  }

  stages {
    stage('cells') {
      matrix {
        axes {
          axis { name 'DISTRO'; values 'rke2', 'k3s', 'upstream' }
          axis { name 'FLEET';  values 'mixed-el-controller', 'all-el' }
        }
        excludes {
          // Only k3s has a question to answer on an all-EL fleet.
          exclude { axis { name 'DISTRO'; values 'rke2', 'upstream' }
                    axis { name 'FLEET';  values 'all-el' } }
        }

        agent { label 'cargoship-e2e' }

        environment {
          RUN_ID                  = "${env.BUILD_TAG}-${DISTRO}-${FLEET}"
          CARGOSHIP_E2E_BACKEND   = 'inventory'
          CARGOSHIP_E2E_INVENTORY = 'fleet.yaml'
          CARGOSHIP_E2E_DISTRO    = "${DISTRO}"
          SSH_KNOWN_HOSTS         = ''
          GOFLAGS                 = '-mod=vendor'
          GOPROXY                 = 'off'
          CGO_ENABLED             = '0'
        }

        stages {
          stage('provision') {
            steps {
              // Proxmox races for the next free VMID, and a static address pool
              // has the same shape. Serialise the clone, not the walk.
              lock(resource: "cargoship-e2e-${params.PLATFORM}") {
                sh "tofu -chdir=ci/tofu/${params.PLATFORM} init"
                sh "tofu -chdir=ci/tofu/${params.PLATFORM} apply -auto-approve -var=run_id=$RUN_ID -var=fleet=$FLEET"
              }
              sh 'ci/wait-for-cloud-init.sh'
              sh 'ci/render-inventory.sh > fleet.yaml'
            }
          }
          stage('walk') {
            steps {
              // Cells with a known failure record it rather than failing the build.
              catchError(buildResult: 'SUCCESS', stageResult: 'UNSTABLE',
                         catchInterruptions: false) {
                sh 'go test -mod=vendor -count=1 -v -timeout=120m ./test/e2e/cluster/... 2>&1 | tee run.log'
              }
            }
          }
        }

        post {
          always {
            sh 'ci/collect-diagnostics.sh || true'
            archiveArtifacts artifacts: 'diagnostics/**,run.log,fleet.yaml', allowEmptyArchive: true
            sh "tofu -chdir=ci/tofu/${params.PLATFORM} destroy -auto-approve -var=run_id=$RUN_ID"
          }
        }
      }
    }
  }
}
```

`catchInterruptions: false` matters: without it an aborted build is swallowed by `catchError` and the stage carries on, which on this suite means provisioning a fleet nothing will walk.

## Edge cases, and how each one fails

Every entry below presents as something other than what it is, which is why they are worth writing down rather than discovering.

### The ones that come out green

These are the dangerous ones. A failure gets investigated; a pass gets believed.

| Signature                                                                | Cause                                                                                                                                                 | Fix                                                                                                               |
| ------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| Phases 22 and 26 skip; everything else passes                            | The hosts answered SSH before cloud-init finished, so fapolicyd and firewalld were not yet running, and both phases gate on *running*                 | `cloud-init status --wait` on every host before rendering. This is exactly why `mage dev:vmUp` holds a fleet back |
| The run passes and the fleet was single-family                           | The EL/Debian host guards stand down on this backend, so nothing notices a cell that was meant to be mixed and came up all one family                 | Assert the families in the pipeline, after rendering, from the document                                           |
| A green run with fifteen skips reported as fifteen passing things        | A skip here almost always means a capability the fleet does not have                                                                                  | Count `--- SKIP` lines and fail the cell on an unexpected number, as the workflow above does                      |
| Everything up to the engine passes, then the join hangs                  | `privateAddress` discovered from the wrong interface on a multi-homed host, so the firewall and `/etc/hosts` phases distributed unreachable addresses | Set `privateAddress` and `privateInterface` explicitly rather than letting fact gathering guess                   |
| SELinux phases pass on a fleet that had SELinux disabled in the template | The phases correctly do nothing when there is no SELinux, the same as on a container                                                                  | `getenforce` on every EL host as a provisioning assertion, not a test assertion                                   |

### The ones that fail somewhere other than the cause

| Signature                                                           | Cause                                                                                                                 | Fix                                                                                                                   |
| ------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| The worker join times out; the test output says nothing useful      | Node-to-node traffic blocked by a security group, an NSX rule or a bridge. The real reason is in the node's journal   | The self-referencing ingress rule in the EC2 example. The phase captures the journal of the host whose wait timed out |
| The reset phase claims no hosts, or a re-join arrives as a new node | The node's name is not the document's `hostname`: an FQDN-only `/etc/hosts`, a search domain, or the EC2 IMDS default | `prefer_fqdn_over_hostname: false` in cloud-init, and check `hostname -s` matches the document                        |
| Phase 21 is fatal, naming `container-selinux`                       | A minimal EL image carries neither it nor the versionlock plugin, and an air-gapped host cannot fetch either          | Bake both into the template                                                                                           |
| Certificate errors that read like a cargoship defect                | Clock skew on a long-lived template                                                                                   | An enabled NTP client in the image                                                                                    |
| `Test_12_GatherFactsDistro` fails saying a host reports an engine   | The fleet is not clean -- a reused instance, a restored snapshot, or a second run against the fleet of the first      | Ephemeral per cell. The apply walk is one-shot by design                                                              |

### The ones that are about the pipeline

*   **The suite tears down nothing it did not create.** It did not make these hosts, so it will not destroy them, and that cuts both ways. Teardown is the pipeline's job on both the success and the abort path, with a reaper behind it.
*   **The uncached `etcd` archive.** Nothing caches the archive a `files` entry extracts `etcdctl` from, so every walk downloads it again and a momentary DNS failure fails the run at `Test_00_UpgradePackage` or `Test_00_JoinMachine` with nothing wrong in the phases at all -- three runs out of five in one session went that way. Retry the step rather than re-provisioning, and read [`todo-extractpath-archive-cache.md`](../agent/todo-extractpath-archive-cache.md) before filing it as a regression.
*   **Concurrency.** Two runs on one platform need distinct names, tags, folders and address ranges. Derive all of them from the run ID, as the examples do, and lock the step that allocates a contended resource.
*   **Spot and preemptible instances.** An interruption mid-walk reads as a phase failure on whichever phase was running. Either do not, or tag the result so nobody bisects it.
*   **The join walk needs two processes.** The suite's walks run in one process, so there is no moment in a single run at which a host can appear. Run the apply walk, provision the extra host, then run `-run 'TestClusterPhases/join'` with `CARGOSHIP_E2E_JOIN_HOST` naming it. [e2e-external-inventory](e2e-external-inventory.md#adding-a-host-and-running-the-join-walk-against-it) is the sequence.
*   **`mage test:endToEndClusterInventory` does not stop leftover bootloose containers** the way the bootloose targets do. Harmless on a fresh runner; worth knowing on a shared one.

## Reading a failure

The fleet is gone by the time anybody looks, so what the pipeline archived is all there is. Collect at least:

```sh
mkdir -p diagnostics
cp fleet.yaml run.log diagnostics/
cp -r "$HOME/.cargoship-cache/logs" diagnostics/suite-logs 2>/dev/null || true

for h in $(jq -r '.[].hostname' hosts.json); do
  ssh "$h" 'getenforce; sudo semodule -l | grep -E "k3s|rke2"' \
    > "diagnostics/$h.selinux.txt" 2>&1 || true
  ssh "$h" 'sudo journalctl -u rke2-server -u rke2-agent -u k3s -u k3s-agent --no-pager -n 500' \
    > "diagnostics/$h.journal.txt" 2>&1 || true
  ssh "$h" 'df -h /; free -h; ip -br addr' \
    > "diagnostics/$h.host.txt" 2>&1 || true
done
```

The journal is what cell 2 lives or dies on -- if the k3s SELinux policy gap is real, that is where it says so, and the test output will not. `semodule -l` is the direct question: on an `rke2` EL host it lists the policy, and the open question is whether a `k3s` one does.

Then report the run the way `test/AGENTS.md` asks: which walks passed, which steps **skipped and why**, and which failures are the fleet rather than the code. A failure here is more likely to be about the environment than about cargoship, and the suite is not in a position to tell the difference.

## The other backends

[e2e-external-inventory](e2e-external-inventory.md) is the backend this page drives, and the reference for everything about the document and the walks. [microvm](microvm.md) is the local VM fleet and the one to reach for first -- a real kernel with SELinux enforcing, a minute to bring up, and no cloud account. [e2e-tests](e2e-tests.md) and [e2e-phase-tests](e2e-phase-tests.md) cover the suite itself and the default container backend.
