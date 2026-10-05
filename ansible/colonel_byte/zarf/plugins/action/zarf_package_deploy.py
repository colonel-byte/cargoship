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

"""Action plugin for the zarf_package_deploy module.

It exists to deliver the task's parameters to the wrapper binary and to display the progress the
wrapper reports. Everything it does is in ZarfActionBase; see plugins/plugin_utils/projection.py.
"""

from __future__ import absolute_import, division, print_function

__metaclass__ = type

# The cli_flag key on each option is a cargoship extension, not a standard Ansible doc key. See
# the header of zarf_init.py and docs/agent/choice-zarf-ansible-module.md.
DOCUMENTATION = r"""
module: zarf_package_deploy
short_description: Deploy a zarf package onto an initialised cluster
description:
  - "Runs C(zarf package deploy) against the cluster a kubeconfig names, and follows the deployment component by component."
  - "The module is a small wrapper binary, not zarf. It renders the command line from these parameters, runs the installed zarf, reads the component boundaries back out of zarf's log stream, and reports one JSON result."
  - "It is the companion of M(colonel_byte.zarf.zarf_init). A cluster whose init package has no StorageClass to claim cannot finish initialising until a storage provider is deployed, and the provider cannot be deployed until the cluster is initialised far enough to hold it, so a playbook alternates between the two."
version_added: "0.0.1"
author:
  - Allen Conlon (@colonel-byte)
notes:
  - "Set C(run_once: true) and C(delegate_to) on the task. One deploy puts the package on the whole cluster, so a play over the cluster's own inventory would otherwise deploy it once per host."
  - "Set C(no_log: true) on the task when any parameter carries a credential. A binary module has no per-parameter C(no_log)."
  - "Check mode reports the task as skipped. C(zarf package deploy) has no dry run, so there is nothing honest to report instead."
  - "A parameter left unset renders no flag, so whatever zarf's own config file sets still applies."
options:
  package:
    description:
      - "The package to deploy: a path to a tarball, an C(oci://) reference, or an C(https://) URL."
      - "A relative path is resolved against the working directory the module was invoked from unless C(directory) names one, because that directory belongs to Ansible rather than to the operator."
      - "The positional argument of the command, so it renders no flag."
    type: str
    required: true
    cli_flag: None
  directory:
    description:
      - "The working directory to run zarf in. A relative C(package) is resolved against it."
    type: str
    required: false
    cli_flag: None
  kubeconfig:
    description:
      - "The kubeconfig of the cluster to deploy onto."
      - "Zarf has no C(--kubeconfig) flag, so it is passed as C(KUBECONFIG) in zarf's environment."
    type: str
    required: false
    cli_flag: None
  zarf_config:
    description:
      - "A zarf config file, passed as C(ZARF_CONFIG). It is how a playbook keeps credentials out of the argument vector."
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
    default: zarf_package_deploy
    cli_flag: None
  status_file:
    description:
      - "Where the wrapper writes live component progress. The action plugin creates a private temporary file and overrides this."
    type: str
    required: false
    cli_flag: None
  parameters:
    description:
      - "Further module parameters, merged into the ones given directly."
      - "A parameter given both directly and inside C(parameters) is an error naming it."
      - "The action plugin consumes this key; the wrapper never sees it."
    type: dict
    required: false
    cli_flag: None
  components:
    description:
      - "Which components to deploy, as a comma-separated list."
      - "It is also what lets the progress display report C(component 2/4) rather than C(component 2): the wrapper cannot otherwise know how many components the package holds."
    type: str
    required: false
    cli_flag: "--components"
  namespace:
    description:
      - "Deploy into this namespace instead of the one the package names. Alpha in zarf."
    type: str
    required: false
    cli_flag: "--namespace"
  shasum:
    description:
      - "The SHA256 checksum of a package fetched over C(https://)."
    type: str
    required: false
    cli_flag: "--shasum"
  connected:
    description:
      - "Whether to deploy without pushing the package's images and repositories, for a cluster that can already reach them."
    type: bool
    required: false
    cli_flag: "--connected"
  take_ownership:
    description:
      - "Whether zarf adopts Kubernetes resources that already exist."
    type: bool
    required: false
    cli_flag: "--take-ownership"
  force_conflicts:
    description:
      - "Whether zarf forces Helm ownership during server-side apply."
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
      - "Whether zarf talks to OCI registries over plain HTTP."
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
      - "The directory zarf unpacks the package into."
    type: str
    required: false
    cli_flag: "--tmpdir"
  timeout:
    description:
      - "How long zarf waits for a Helm operation or health check, as a Go duration such as C(15m)."
    type: str
    required: false
    cli_flag: "--timeout"
  public_key:
    description:
      - "The public key the package signature is verified against."
    type: str
    required: false
    cli_flag: "--key"
  verify:
    description:
      - "When to verify the package signature."
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
    type: str
    required: false
    default: json
    cli_flag: "--log-format"
"""

EXAMPLES = r"""
- name: Deploy a storage provider so the registry has a StorageClass to claim
  colonel_byte.zarf.zarf_package_deploy:
    package: oci://ghcr.io/colonel-byte/zarf/csi-local-path-provider:0.0.37-upstream
    components: local-path-images,local-path-chart
    kubeconfig: /etc/rancher/rke2/rke2.yaml
    public_key: /etc/zarf/colonel-byte-zarf-packages.pub
    verify: always
  delegate_to: localhost
  run_once: true

- name: Deploy a staged package
  colonel_byte.zarf.zarf_package_deploy:
    package: /srv/staging/zarf-package-monitoring-amd64.tar.zst
    kubeconfig: /etc/rancher/rke2/rke2.yaml
    set_variables:
      DOMAIN: bubbles.test
  delegate_to: localhost
  run_once: true
  no_log: true
  register: deploy_result

- name: Report which components the package deployed
  ansible.builtin.debug:
    var: deploy_result.zarf.componentsRan
"""

from ansible_collections.colonel_byte.zarf.plugins.plugin_utils.projection import (
    ZarfActionBase,
)


class ActionModule(ZarfActionBase):
    MODULE = "package_deploy"
    DEFAULT_WRAPPER = "zarf_package_deploy"
