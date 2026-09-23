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

"""Action plugin for the cargoship_kube_config module.

It exists to put Ansible's resolved inventory into the module's arguments. Everything it does is
in CargoshipActionBase; see plugins/plugin_utils/projection.py.
"""

from __future__ import absolute_import, division, print_function

__metaclass__ = type

# The cli_flag key on each option is a cargoship extension, not a standard Ansible doc key. It is
# read by generateModuleDocs in magefiles/gen-docs.go to build the parameter-to-flag table on
# docs/ansible/module_*.md, and is None for a parameter that renders no flag: one that is the
# command's positional argument, or one consumed before the command line is built. Stock
# ansible-doc and validate-modules are not run against this collection; see
# docs/agent/choice-ansible-module.md.
DOCUMENTATION = r"""
module: cargoship_kube_config
short_description: Fetch the cluster kubeconfig onto the management node
description:
  - "Runs C(cargoship kube-config) against the fleet described by an Ansible inventory."
  - "It is the one module that changes the node the play runs on rather than the fleet."
  - "C(cargoship kube-config) has no dry run, so under C(--check) the module reports C(skipped: true) and runs nothing at all."
version_added: "0.26.0"
author:
  - Allen Conlon (@colonel-byte)
notes:
  - "Set C(run_once: true) and C(delegate_to) on the task. Cargoship converges the whole fleet in a single run, so a play over the fleet's own inventory would otherwise converge it once per host."
  - "Set C(no_log: true) on the task. A binary module has no per-parameter C(no_log), so the task-level setting is what suppresses the arguments and the result."
  - "A parameter left unset renders no flag, so whatever the cargoship configuration file sets still applies. That is why C(label_nodes: false) and omitting C(label_nodes) differ."
options:
  inventory:
    description:
      - "The resolved Ansible inventory the module translates into a ZarfCluster document: C(groups), C(hostvars), and a C(cluster) block."
      - "The action plugin fills C(groups) and C(hostvars) in from the play, so a task normally sets only C(cluster)."
      - "Renders no flag of its own. The translated document is passed to the command as C(--config)."
    type: dict
    required: true
    cli_flag: None
  parameters:
    description:
      - "Further module parameters, merged into the ones given directly. It is how the C(cluster) role passes a caller-supplied set without templating a whole task's arguments."
      - "A parameter given both directly and inside C(parameters) is an error naming it."
      - "The action plugin consumes this key; the module never sees it."
    type: dict
    required: false
    cli_flag: None
  distro:
    description:
      - "The distro the cluster runs."
    type: str
    required: false
    choices:
      - k3s
      - rke2
    cli_flag: "--distro"
  kubeconfig:
    description:
      - "The kubeconfig to read or write."
    type: str
    required: false
    cli_flag: "--kubeconfig"
  inventory_path:
    description:
      - "Where to write the generated ZarfCluster document. A private temporary file when unset, removed after a run that succeeded."
      - "A run that fails leaves the document on disk either way, because it is the first thing to look at when a failure reads like the wrong cluster."
    type: str
    required: false
    cli_flag: "--config"
  age_recipient:
    description:
      - "Age public keys used to encrypt registry credentials written into the generated inventory document."
      - "Consumed while that document is written, so it renders no flag."
    type: list
    elements: str
    required: false
    cli_flag: None
  age_recipients_file:
    description:
      - "Files holding age public keys, read for the same purpose as C(age_recipient)."
    type: list
    elements: str
    required: false
    cli_flag: None
  log_level:
    description:
      - "Cargoship's log level. Defaults to C(debug) when the play runs with C(-v)."
    type: str
    required: false
    cli_flag: "--log-level"
  log_format:
    description:
      - "Cargoship's log format."
    type: str
    required: false
    cli_flag: "--log-format"
  log_file:
    description:
      - "Whether to always write a full-verbosity debug log to a file on the management node, regardless of log level."
    type: bool
    required: false
    cli_flag: "--log-file"
"""

EXAMPLES = r"""
- name: Fetch the cluster kubeconfig
  colonel_byte.cargoship.cargoship_kube_config:
    distro: rke2
    kubeconfig: /srv/staging/bubbles.kubeconfig
    inventory:
      cluster:
        name: bubbles
        loadbalancer: bubbles-kc.test.com
  delegate_to: localhost
  run_once: true
  no_log: true
"""

from ansible_collections.colonel_byte.cargoship.plugins.plugin_utils.projection import (
    CargoshipActionBase,
)


class ActionModule(CargoshipActionBase):
    pass
