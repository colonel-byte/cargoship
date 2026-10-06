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

"""Action plugin for the zarf_state_info module.

It reads the cluster's zarf-state Secret. The plumbing is in ZarfInfoActionBase; see
plugins/plugin_utils/info.py and docs/agent/choice-zarf-info-modules.md.
"""


from __future__ import absolute_import, division, print_function

__metaclass__ = type

import base64
import json

from ansible_collections.colonel_byte.zarf.plugins.plugin_utils.info import (
    ZarfInfoActionBase,
)

# The cli_flag key on each option is a cargoship extension, not a standard Ansible doc key. Here it
# is held against command_parts below by internal/zarfmod/infoplugins_test.go.

DOCUMENTATION = r"""
module: zarf_state_info
short_description: Fetch and parse the Zarf state secret from a cluster
description:
  - "Retrieves the C(zarf-state) Secret from the cluster via C(zarf tools kubectl) and parses the base64-encoded state JSON into structured output."
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
  namespace:
    description:
      - "The namespace holding the C(zarf-state) Secret."
    type: str
    default: "zarf"
    required: false
    cli_flag: None
"""

EXAMPLES = r"""
- name: Fetch and inspect the zarf state
  colonel_byte.zarf.zarf_state_info:
    kubeconfig: /etc/rancher/rke2/rke2.yaml
  delegate_to: localhost
  run_once: true
  register: zarf_state_result

- name: Display registry mode
  ansible.builtin.debug:
    msg: "{{ zarf_state_result.state.registryInfo.registryMode }}"
"""

# The key of the Secret the state is stored under, and the Secret's own name. Neither is a
# parameter: they are zarf's own names for its own state.
STATE_SECRET = "zarf-state"
STATE_KEY = "jsonpath={.data.state}"


def command_parts(params):
    """Return the zarf command line this module runs.

    A plain function of the parameters, and the only place a flag is named, so that
    internal/zarfmod/infoplugins_test.go can read the flags the module renders out of the source
    and hold them against the documented cli_flag values.

    --no-color is deliberately absent, unlike the other read-only modules: `zarf tools kubectl`
    hands its arguments to kubectl, which rejects the flag rather than ignoring it.
    """
    return [
        params["zarf_binary"],
        "tools",
        "kubectl",
        "get",
        "secret",
        STATE_SECRET,
        "-n",
        params.get("namespace") or "zarf",
        "-o",
        STATE_KEY,
    ]


class ActionModule(ZarfInfoActionBase):
    """Action plugin that fetches and decodes the zarf-state Secret."""


    def zarf_argv(self, params):
        return command_parts(params)

    def interpret(self, result, stdout):
        stdout = stdout.strip()
        if not stdout:
            result["failed"] = True
            result["msg"] = (
                "the %s Secret holds no .data.state entry, so this cluster has a Secret of that "
                "name that zarf did not write" % STATE_SECRET
            )
            return

        try:
            state = json.loads(base64.b64decode(stdout).decode("utf-8"))
        except Exception as exc:
            result["failed"] = True
            result["msg"] = "unable to decode the zarf state: %s" % exc
            return

        result["state"] = state
        result["msg"] = "retrieved and parsed %s" % STATE_SECRET
