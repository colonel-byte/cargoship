# Copyright 2026 colonel-byte
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Action plugin for the zarf_init module.

It exists to deliver the task's parameters to the wrapper binary and to display the progress the
wrapper reports. Everything it does is in ZarfActionBase; see plugins/plugin_utils/projection.py.
"""

from __future__ import absolute_import, division, print_function

__metaclass__ = type

# The cli_flag key on each option is a cargoship extension, not a standard Ansible doc key. It
# records which zarf init flag a parameter renders, and is None for a parameter that renders none:
# one consumed by the wrapper itself, or one passed to zarf as an environment variable. Stock
# ansible-doc and validate-modules are not run against this collection; see
# docs/agent/choice-zarf-ansible-module.md.
DOCUMENTATION = r"""
module: zarf_init
short_description: Initialise a cluster with a staged zarf init package
description:
  - "Runs C(zarf init) against the cluster a kubeconfig names, and follows the init package's deployment component by component."
  - "The module is a small wrapper binary, not zarf. It renders the C(zarf init) command line from these parameters, runs the installed zarf, reads the component boundaries back out of zarf's log stream, and reports one JSON result."
  - "This is a proof of ZEP-0072 (U(https://github.com/zarf-dev/proposals/pull/73)), which proposes putting this dispatch inside the zarf binary itself."
version_added: "0.0.1"
author:
  - Allen Conlon (@colonel-byte)
notes:
  - "Set C(run_once: true) and C(delegate_to) on the task. One zarf init initialises the whole cluster, so a play over the cluster's own inventory would otherwise initialise it once per host."
  - "Set C(no_log: true) on the task. A binary module has no per-parameter C(no_log), so the task-level setting is what suppresses the arguments and the result."
  - "Check mode reports the task as skipped. C(zarf init) has no dry run, so there is nothing honest to report instead."
  - "A parameter left unset renders no flag, so whatever zarf's own config file sets still applies. That is why C(plain_http: false) and omitting C(plain_http) differ."
  - "Credentials given as parameters are rendered as flags, which puts them in the process table for the life of the run. Put them in a zarf config file and name it with C(zarf_config) to keep them out of it."
  - "Needs zarf v0.72.0 or newer, which is where C(zarf init) gained the positional package source this module passes C(init_package) as. The wrapper reads C(zarf version) before it runs and fails naming both versions when the zarf it found is older."
options:
  init_package:
    description:
      - "The init package to deploy, such as C(/srv/staging/zarf-init-amd64-v0.85.0.tar.zst)."
      - "It is passed as the positional package source of C(zarf init), so the file needs no particular name and need not sit in any particular directory. An C(oci://) reference or an C(https://) URL may be given instead of a path, for a cluster whose packages come from a registry rather than from disk."
      - "A directory may also be given, which leaves zarf to find a package inside it the way it does when given no source at all: a file named after zarf's own version."
      - "One of C(init_package) or C(directory) is required."
    type: str
    required: false
    cli_flag: None
  directory:
    description:
      - "The working directory to run zarf in. A relative C(init_package), and a relative path inside a zarf config file named by C(zarf_config), are resolved against it."
      - "May be given together with C(init_package): the package says what to deploy, and this says where relative paths resolve from."
    type: str
    required: false
    cli_flag: None
  kubeconfig:
    description:
      - "The kubeconfig of the cluster to initialise."
      - "Zarf has no C(--kubeconfig) flag, so it is passed as C(KUBECONFIG) in zarf's environment."
    type: str
    required: false
    cli_flag: None
  zarf_config:
    description:
      - "A zarf config file, passed as C(ZARF_CONFIG)."
      - "It is how a playbook keeps registry and git credentials out of the argument vector: zarf reads them from the file, so they never reach the process table."
    type: str
    required: false
    cli_flag: None
  zarf_binary:
    description:
      - "The zarf to run. Resolved on the path of the node the task runs on when unset."
    type: str
    required: false
    default: zarf
    cli_flag: None
  wrapper_binary:
    description:
      - "The module wrapper to run, resolved on the path when it holds no separator."
      - "The action plugin consumes this parameter; the wrapper never sees it."
    type: str
    required: false
    default: zarf_init
    cli_flag: None
  status_file:
    description:
      - "Where the wrapper writes live component progress."
      - "The action plugin creates a private temporary file and overrides this, so a task has no reason to set it. It is here for a run reproduced by hand."
    type: str
    required: false
    cli_flag: None
  parameters:
    description:
      - "Further module parameters, merged into the ones given directly. It is how a role passes a caller-supplied set without templating a whole task's arguments."
      - "A parameter given both directly and inside C(parameters) is an error naming it."
      - "The action plugin consumes this key; the wrapper never sees it."
    type: dict
    required: false
    cli_flag: None
  components:
    description:
      - "Which init components to deploy, as a comma-separated list."
      - "It is also what lets the progress display report C(component 2/4) rather than C(component 2): the wrapper cannot otherwise know how many components the package holds."
    type: str
    required: false
    cli_flag: "--components"
  storage_class:
    description:
      - "The storage class the init package's persistent volumes are created with."
    type: str
    required: false
    cli_flag: "--storage-class"
  registry_url:
    description:
      - "The URL of an external registry to use instead of the one the init package deploys."
    type: str
    required: false
    cli_flag: "--registry-url"
  registry_mode:
    description:
      - "How zarf uses the registry."
    type: str
    required: false
    cli_flag: "--registry-mode"
  registry_port:
    description:
      - "The node port the internal registry is published on."
    type: int
    required: false
    cli_flag: "--registry-port"
  registry_secret:
    description:
      - "The secret zarf derives registry credentials from."
    type: str
    required: false
    cli_flag: "--registry-secret"
  registry_push_username:
    description:
      - "The username zarf pushes images with."
    type: str
    required: false
    cli_flag: "--registry-push-username"
  registry_push_password:
    description:
      - "The password zarf pushes images with."
    type: str
    required: false
    cli_flag: "--registry-push-password"
  registry_pull_username:
    description:
      - "The username the cluster pulls images with."
    type: str
    required: false
    cli_flag: "--registry-pull-username"
  registry_pull_password:
    description:
      - "The password the cluster pulls images with."
    type: str
    required: false
    cli_flag: "--registry-pull-password"
  git_url:
    description:
      - "The URL of an external git server to use instead of the one the init package deploys."
    type: str
    required: false
    cli_flag: "--git-url"
  git_push_username:
    description:
      - "The username zarf pushes repositories with."
    type: str
    required: false
    cli_flag: "--git-push-username"
  git_push_password:
    description:
      - "The password zarf pushes repositories with."
    type: str
    required: false
    cli_flag: "--git-push-password"
  git_pull_username:
    description:
      - "The username the cluster pulls repositories with."
    type: str
    required: false
    cli_flag: "--git-pull-username"
  git_pull_password:
    description:
      - "The password the cluster pulls repositories with."
    type: str
    required: false
    cli_flag: "--git-pull-password"
  injector_image:
    description:
      - "The image the zarf injector runs."
    type: str
    required: false
    cli_flag: "--injector-image"
  injector_port:
    description:
      - "The port the zarf injector listens on."
    type: int
    required: false
    cli_flag: "--injector-port"
  agent_mutation_policy:
    description:
      - "Which resources the zarf agent mutates."
    type: str
    required: false
    cli_flag: "--agent-mutation-policy"
  agent_tls_ca:
    description:
      - "The certificate authority the zarf agent's serving certificate is issued by."
    type: str
    required: false
    cli_flag: "--agent-tls-ca"
  agent_tls_cert:
    description:
      - "The zarf agent's serving certificate."
    type: str
    required: false
    cli_flag: "--agent-tls-cert"
  agent_tls_key:
    description:
      - "The private key of the zarf agent's serving certificate."
    type: str
    required: false
    cli_flag: "--agent-tls-key"
  take_ownership:
    description:
      - "Whether zarf takes ownership of resources an earlier install created."
    type: bool
    required: false
    cli_flag: "--take-ownership"
  force_conflicts:
    description:
      - "Whether zarf forces apply conflicts."
    type: bool
    required: false
    cli_flag: "--force-conflicts"
  skip_values_schema_validation:
    description:
      - "Whether zarf skips validating values files against their schema."
    type: bool
    required: false
    cli_flag: "--skip-values-schema-validation"
  insecure_skip_tls_verify:
    description:
      - "Whether zarf skips verifying TLS certificates it connects to."
    type: bool
    required: false
    cli_flag: "--insecure-skip-tls-verify"
  plain_http:
    description:
      - "Whether zarf talks to registries over plain HTTP."
    type: bool
    required: false
    cli_flag: "--plain-http"
  retries:
    description:
      - "How many times zarf retries a failed operation."
    type: int
    required: false
    cli_flag: "--retries"
  oci_concurrency:
    description:
      - "How many OCI operations zarf runs at once."
    type: int
    required: false
    cli_flag: "--oci-concurrency"
  architecture:
    description:
      - "The architecture of the package being deployed."
    type: str
    required: false
    cli_flag: "--architecture"
  cache:
    description:
      - "Zarf's cache directory."
    type: str
    required: false
    cli_flag: "--cache"
  tmpdir:
    description:
      - "The directory zarf unpacks the init package into."
    type: str
    required: false
    cli_flag: "--tmpdir"
  timeout:
    description:
      - "How long zarf waits for a Helm operation, as a Go duration such as C(30m)."
    type: str
    required: false
    cli_flag: "--timeout"
  public_key:
    description:
      - "The public key the init package's signature is verified against."
    type: str
    required: false
    cli_flag: "--key"
  verify:
    description:
      - "When to verify the init package's signature."
    type: str
    required: false
    choices:
      - never
      - if-possible
      - always
    cli_flag: "--verify"
  values:
    description:
      - "Values files to apply, one flag per entry."
    type: list
    elements: str
    required: false
    cli_flag: "--values"
  set_values:
    description:
      - "Values to set, as a mapping. Rendered one flag per entry, sorted, so the same parameters always render the same command line."
    type: dict
    required: false
    cli_flag: "--set-values"
  set_variables:
    description:
      - "Zarf variables to set, as a mapping."
    type: dict
    required: false
    cli_flag: "--set-variables"
  log_level:
    description:
      - "Zarf's log level."
    type: str
    required: false
    cli_flag: "--log-level"
  log_format:
    description:
      - "Zarf's log format. Defaults to C(json), which is the format the component progress is read out of."
      - "Overriding it costs the progress display its component names, and changes nothing else about the run."
    type: str
    required: false
    default: json
    cli_flag: "--log-format"
"""

EXAMPLES = r"""
- name: Initialise the cluster
  colonel_byte.zarf.zarf_init:
    init_package: /srv/staging/zarf-init-amd64-v0.85.0.tar.zst
    kubeconfig: /etc/rancher/rke2/rke2.yaml
    storage_class: local-path
    components: zarf-registry,zarf-agent
    timeout: 30m
  delegate_to: localhost
  run_once: true
  no_log: true
  register: init_result

- name: Report which components the init package deployed
  ansible.builtin.debug:
    var: init_result.zarf.componentsRan

- name: Initialise the cluster with credentials zarf reads from its own config file
  colonel_byte.zarf.zarf_init:
    init_package: /srv/staging/zarf-init-amd64-v0.85.0.tar.zst
    kubeconfig: /etc/rancher/rke2/rke2.yaml
    zarf_config: /etc/zarf/zarf-config.yaml
  delegate_to: localhost
  run_once: true
  no_log: true

- name: Initialise the cluster from a custom init package under a name of its own
  colonel_byte.zarf.zarf_init:
    init_package: /srv/staging/our-init-package.tar.zst
    kubeconfig: /etc/rancher/rke2/rke2.yaml
  delegate_to: localhost
  run_once: true
  no_log: true

- name: Initialise the cluster from an init package held in a registry
  colonel_byte.zarf.zarf_init:
    init_package: oci://registry.bubbles.test/zarf/init:v0.85.0
    kubeconfig: /etc/rancher/rke2/rke2.yaml
  delegate_to: localhost
  run_once: true
  no_log: true
"""

from ansible_collections.colonel_byte.zarf.plugins.plugin_utils.projection import (
    ZarfActionBase,
)


class ActionModule(ZarfActionBase):
    MODULE = "init"
    DEFAULT_WRAPPER = "zarf_init"
