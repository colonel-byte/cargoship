# Cargoship

Cargoship is a Go-based CLI for building, distributing, and applying offline Kubernetes distro packages. It is designed to simplify two core workflows:

1.  **Package Creation:** Building a self-contained, offline Kubernetes distro package containing everything needed for installation in disconnected or highly-regulated environments.
2.  **Cluster Lifecycle Management:** Bootstrapping, upgrading, and managing the cluster on target hosts over SSH using the packaged distribution.

Cargoship bridges the gap between offline distro packaging tools and remote cluster lifecycle managers, supporting OCI image and file packaging, secure publishing to OCI registries, and robust SSH orchestration.

---

## Supported Distributions

Cargoship currently provides native support and integration for the following Kubernetes engines:

*   **K3s**
*   **RKE2**

---

## Core Concepts

Cargoship is built around two primary architectural concepts:

### 1. Offline Distro Packages

A Cargoship package is a single, compressed archive containing all artifacts necessary to spin up or upgrade a Kubernetes cluster in air-gapped networks. It bundles:

*   Distro-specific metadata and layout descriptors.
*   Required engine configuration templates and system files.
*   OCI images and container artifacts.
*   Checksums and cryptographic signatures for integrity verification.

The package compilation process loads a local distro definition, gathers all specified assets, and produces a single, verifiable archive.

### 2. Phase-Based Orchestration

Cluster operations are modeled as ordered sequences of reusable, structured "phases." This makes high-level actions (`apply`, `prepare`, `reset`, `kube-config`) predictable, easier to debug, and simple to extend.

For example, the **`apply`** workflow comprises the following phases:

1.  **Connect:** Establish secure SSH connections to all target hosts.
2.  **OS Detection & Fact Gathering:** Identify host operating systems and system resources.
3.  **Validation:** Verify that host nodes meet the pre-requisites.
4.  **Prepare:** Install OS packages, load kernel modules, and configure system firewalls or policies.
5.  **Upload:** Securely transfer required package binaries, configuration templates, and OCI images to the nodes.
6.  **Bootstrap / Upgrade:** Initialize primary control-plane nodes and join worker nodes.
7.  **Kubeconfig Retrieval:** Fetch the generated admin credentials.
8.  **Cleanup & Disconnect:** Release locks, clean up temporary artifacts, and close SSH sessions.

---

## Usage Examples

Here are the standard workflows for compiling and deploying offline Kubernetes packages with Cargoship.

### 1. Compile an Offline Package

To build an offline archive containing all required files, binaries, and container images:

```bash
# Create a package from the current directory structure
cargoship create .

# Build from a specific path and output to a custom directory
cargoship create ./distro-defs -o ./build/
```

### 2. Prepare Target Nodes

Verify and configure OS-level prerequisites (such as kernel modules, firewall ports, `fapolicyd` rules, and `/etc/hosts`) across target machines using your cluster configuration inventory:

```bash
cargoship prepare ./build/cargoship-distro-amd64.tar.zst --config ./cargoship-config.yaml
```

### 3. Deploy or Upgrade a Cluster

Bootstrap a new cluster or upgrade an existing one from the compiled package:

```bash
cargoship apply ./build/cargoship-distro-amd64.tar.zst --config ./cargoship-config.yaml
```

### 4. Fetch the Kubeconfig

Retrieve the admin `kubeconfig` securely from the primary controller:

```bash
cargoship kube-config --config ./cargoship-config.yaml
```

### 5. Uninstall or Reset Target Nodes

Stop, uninstall, and completely purge the Kubernetes distro and its state from the target hosts:

```bash
cargoship reset --config ./cargoship-config.yaml --distro rke2
```

---

## Configuration and Schemas

Cargoship relies on strongly-typed YAML definitions to govern its operations:

*   **Cluster Inventories:** Specify SSH configurations, credentials, host roles, profiles, and load-balancer addresses.
*   **Distro Package Definitions:** Map out the required binaries, OCI images, and layout settings.
*   **Distro Runtime Configs:** Configure the underlying distribution engine.

The corresponding JSON schemas are automatically generated from Go structs into `schema/`. When authoring configurations in modern editors, refer to these schemas for real-time validation and autocompletion.

An inventory authoring guide is available in [docs/guides/setup-inv.md](./docs/guides/setup-inv.md).

---

## Development and Build Workflows

Task automation is built using **Mage**, with **Dagger** acting as the containerized execution engine.

### Mage Automation

Mage handles tasks including local compilation, e2e test execution, schema updates, and documentation generation. Key generated files include:

*   `build/cargoship_*` (Release binaries)
*   `docs/commands/*` (Cobra command references)
*   `docs/phases/*` (Orchestration phase explanations)
*   `docs/SUMMARY.md` (mdBook layout manifest)
*   `schema/*.json` (YAML validations)

### Dagger Builds

Dagger coordinates hermetic, multi-platform compilation inside containerized Go environments. It ensures that compiled binaries are reproducible and decoupled from the developer's local compiler version.

### Continuous Integration (CI) and Releases

GitHub Actions workflows run lint checks, dependency validation, cross-compilation, and end-to-end tests for every pull request. Releases are managed through **GoReleaser**, automating build signing, verification, and asset publishing.

---

## Inspiration

Cargoship draws major design and engineering inspiration from:

*   [k0sproject/k0sctl](https://github.com/k0sproject/k0sctl) — For elegant SSH-based multi-node orchestration and configuration patterns.
*   [zarf-dev/zarf](https://github.com/zarf-dev/zarf) — For air-gapped image and file packaging and offline-first design.
