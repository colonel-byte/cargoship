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

"""Action plugin for the zarf_package_inspect module.

Inspects the definition of a Zarf package tarball, OCI reference, or cluster package.
"""

from __future__ import absolute_import, division, print_function

import os
import shlex
import shutil
import yaml

from ansible.errors import AnsibleActionFail
from ansible.plugins.action import ActionBase

__metaclass__ = type

DOCUMENTATION = r"""
module: zarf_package_inspect
short_description: Inspect and parse the definition of a Zarf package
description:
  - "Runs C(zarf package inspect definition) against a local package tarball, an C(oci://) reference, or a deployed package name."
  - "Parses the resulting YAML definition into structured output containing package metadata, build data, and components."
  - "Runs on the node the task is delegated to (typically localhost)."
version_added: "0.31.0"
author:
  - Allen Conlon (@colonel-byte)
notes:
  - "Set C(run_once: true) and C(delegate_to: localhost) on the task."
options:
  package:
    description:
      - "Path to a local package tarball, an C(oci://) reference, an C(https://) URL, or a deployed package name."
    type: str
    required: true
    cli_flag: None
  public_key:
    description:
      - "Path to a public key file used to validate signed packages."
    type: path
    required: false
    cli_flag: "--key"
  kubeconfig:
    description:
      - "Path to the kubeconfig file, used when inspecting a package already deployed to a cluster."
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
- name: Inspect a staged Zarf package tarball
  colonel_byte.zarf.zarf_package_inspect:
    package: /srv/staging/zarf-package-csi-rook-ceph-amd64-v1.20.7-upstream.tar.zst
    public_key: /etc/zarf/colonel-byte-zarf-packages.pub
  delegate_to: localhost
  run_once: true
  register: pkg_inspect

- name: Display package version and flavor
  ansible.builtin.debug:
    msg:
      name: "{{ pkg_inspect.definition.metadata.name }}"
      version: "{{ pkg_inspect.definition.metadata.version }}"
      flavor: "{{ pkg_inspect.definition.build.flavor | default('default') }}"
"""


class ActionModule(ActionBase):
    """Action plugin that runs zarf package inspect definition and parses YAML."""

    def run(self, tmp=None, task_vars=None):
        if task_vars is None:
            task_vars = {}

        result = super(ActionModule, self).run(tmp, task_vars)
        del tmp

        package = self._task.args.get('package')
        if not package:
            result['failed'] = True
            result['msg'] = "missing required argument: package"
            return result

        public_key = self._task.args.get('public_key')
        kubeconfig = self._task.args.get('kubeconfig')
        zarf_binary = self._task.args.get('zarf_binary', 'zarf')

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
            'package',
            'inspect',
            'definition',
            shlex.quote(str(package)),
            '--no-color',
        ])

        if public_key:
            cmd_parts.extend(['--key', shlex.quote(str(public_key))])

        res = self._low_level_execute_command(
            cmd=' '.join(cmd_parts),
            executable='/bin/sh',
        )

        rc = res.get('rc', 0)
        stdout = res.get('stdout', '') or ''
        stderr = res.get('stderr', '') or ''

        if rc != 0:
            result['failed'] = True
            result['msg'] = "Failed to inspect package %s: %s" % (package, stderr.strip() or stdout.strip())
            result['rc'] = rc
            result['stderr'] = stderr
            return result

        # zarf package inspect definition prints "Verified OK" before YAML when --key is supplied
        yaml_content = stdout
        if "kind:" in yaml_content:
            idx = yaml_content.find("kind:")
            yaml_content = yaml_content[idx:]

        try:
            definition = yaml.safe_load(yaml_content)
        except Exception as exc:
            result['failed'] = True
            result['msg'] = "Failed to parse inspect output as YAML: %s" % exc
            result['stdout'] = stdout
            return result

        if not isinstance(definition, dict):
            result['failed'] = True
            result['msg'] = "Parsed definition is not a dictionary"
            result['stdout'] = stdout
            return result

        metadata = definition.get('metadata', {}) or {}
        build = definition.get('build', {}) or {}

        result['changed'] = False
        result['definition'] = definition
        result['package_name'] = metadata.get('name', '')
        result['package_version'] = metadata.get('version', '')
        result['package_flavor'] = build.get('flavor', '')
        result['msg'] = "Inspected package %s (%s)" % (
            metadata.get('name', package),
            metadata.get('version', 'unknown'),
        )
        return result
