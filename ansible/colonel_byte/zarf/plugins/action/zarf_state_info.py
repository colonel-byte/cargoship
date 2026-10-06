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

Reads and parses the cluster's zarf-state secret using `zarf tools kubectl`.
"""

from __future__ import absolute_import, division, print_function

import base64
import json
import os
import shlex
import shutil

from ansible.errors import AnsibleActionFail
from ansible.plugins.action import ActionBase

__metaclass__ = type

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


class ActionModule(ActionBase):
    """Action plugin that fetches and decodes the zarf-state secret."""

    def run(self, tmp=None, task_vars=None):
        if task_vars is None:
            task_vars = {}

        result = super(ActionModule, self).run(tmp, task_vars)
        del tmp

        kubeconfig = self._task.args.get('kubeconfig')
        zarf_binary = self._task.args.get('zarf_binary', 'zarf')
        namespace = self._task.args.get('namespace', 'zarf')

        # Check if zarf binary exists or is on PATH
        resolved_bin = zarf_binary
        if os.path.sep not in zarf_binary:
            found = shutil.which(zarf_binary)
            if found:
                resolved_bin = found

        cmd_parts = []
        if kubeconfig:
            cmd_parts.extend(["env", "KUBECONFIG=%s" % shlex.quote(str(kubeconfig))])

        cmd_parts.extend([
            shlex.quote(resolved_bin),
            'tools',
            'kubectl',
            'get',
            'secret',
            'zarf-state',
            '-n',
            shlex.quote(str(namespace)),
            '-o',
            shlex.quote("jsonpath={.data.state}"),
        ])

        res = self._low_level_execute_command(
            cmd=' '.join(cmd_parts),
            executable='/bin/sh',
        )

        rc = res.get('rc', 0)
        stdout = res.get('stdout', '').strip()
        stderr = res.get('stderr', '').strip()

        if rc != 0:
            result['failed'] = True
            result['msg'] = (
                "Failed to fetch zarf-state Secret in namespace %s: %s"
                % (namespace, stderr or stdout)
            )
            result['rc'] = rc
            result['stderr'] = stderr
            return result

        if not stdout:
            result['failed'] = True
            result['msg'] = (
                "The zarf-state Secret in namespace %s contained no .data.state entry"
                % namespace
            )
            return result

        try:
            raw_json = base64.b64decode(stdout).decode('utf-8')
            state = json.loads(raw_json)
        except Exception as exc:
            result['failed'] = True
            result['msg'] = "Failed to decode zarf state JSON: %s" % exc
            return result

        result['changed'] = False
        result['state'] = state
        result['msg'] = "Retrieved and parsed zarf-state"
        return result
