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

"""Action plugin for the zarf_package_info module.

It lists the packages deployed to the cluster. The plumbing is in ZarfInfoActionBase; see
plugins/plugin_utils/info.py and docs/agent/choice-zarf-info-modules.md.
"""


from __future__ import absolute_import, division, print_function

__metaclass__ = type

import json

from ansible_collections.colonel_byte.zarf.plugins.plugin_utils.info import (
    ZarfInfoActionBase,
)

# The cli_flag key on each option is a cargoship extension, not a standard Ansible doc key. Here it
# is held against command_parts below by internal/zarfmod/infoplugins_test.go.

DOCUMENTATION = r"""
module: zarf_package_info
short_description: List all Zarf packages deployed to a cluster
description:
  - "Retrieves the list of packages deployed to the cluster via C(zarf package list --output-format json) and returns structured package records."
  - "Runs on the node the task is delegated to (typically localhost), reaching the cluster through a kubeconfig."
version_added: "0.31.0"
author:
  - Allen Conlon (@colonel-byte)
notes:
  - "Set C(run_once: true) and C(delegate_to: localhost) on the task."
options:
  kubeconfig:
    description:
      - "Path to the kubeconfig file used to reach the cluster."
    type: path
    required: false
    cli_flag: None
  zarf_binary:
    description:
      - "Path to the C(zarf) CLI binary on the delegated host. When omitted, C(zarf) on C(PATH) is used."
    type: path
    default: "zarf"
    required: false
    cli_flag: None
"""

EXAMPLES = r"""
- name: List deployed zarf packages
  colonel_byte.zarf.zarf_package_info:
    kubeconfig: /etc/rancher/rke2/rke2.yaml
  delegate_to: localhost
  run_once: true
  register: zarf_packages_result

- name: Display package names and versions
  ansible.builtin.debug:
    msg: "{{ zarf_packages_result.packages | map(attribute='package') | list }}"
"""

def command_parts(params):
    """Return the zarf command line this module runs.

    A plain function of the parameters, and the only place a flag is named, so that
    internal/zarfmod/infoplugins_test.go can read the flags the module renders out of the source
    and hold them against the documented cli_flag values.
    """
    return [
        params["zarf_binary"],
        "package",
        "list",
        "--output-format",
        "json",
        # Colour codes in the stream would reach json.loads as syntax.
        "--no-color",
    ]


class ActionModule(ZarfInfoActionBase):
    """Action plugin that lists the packages deployed to a cluster."""


    def zarf_argv(self, params):
        return command_parts(params)

    def interpret(self, result, stdout):
        stdout = stdout.strip()
        packages = []
        if stdout:
            try:
                parsed = json.loads(stdout)
            except ValueError as exc:
                result["failed"] = True
                result["msg"] = "unable to parse the package list as JSON: %s" % exc
                result["module_stdout"] = stdout
                return
            # zarf prints a list, and null for a cluster with no packages deployed.
            if isinstance(parsed, list):
                packages = parsed
            elif parsed is not None:
                packages = [parsed]

        result["packages"] = packages
        result["msg"] = "retrieved %d deployed zarf package(s)" % len(packages)
