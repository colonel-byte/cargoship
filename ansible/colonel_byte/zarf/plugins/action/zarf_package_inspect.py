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

It reads the zarf.yaml of a package tarball, an OCI reference, or a deployed package. The plumbing
is in ZarfInfoActionBase; see plugins/plugin_utils/info.py and
docs/agent/choice-zarf-info-modules.md.
"""


from __future__ import absolute_import, division, print_function

__metaclass__ = type

import yaml

from ansible_collections.colonel_byte.zarf.plugins.plugin_utils.info import (
    ZarfInfoActionBase,
)

# The cli_flag key on each option is a cargoship extension, not a standard Ansible doc key. Here it
# is held against command_parts below by internal/zarfmod/infoplugins_test.go.

DOCUMENTATION = r"""
module: zarf_package_inspect
short_description: Inspect and parse the definition of a Zarf package
description:
  - "Runs C(zarf package inspect definition) against a local package tarball, an C(oci://) reference, or a deployed package name."
  - "Signature verification is zarf's C(if-possible) unless C(verify) says otherwise, so an unsigned package inspects cleanly even when C(public_key) is given."
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
      - "Path to the public key a package signature is verified against."
      - "Supplying it does not by itself require a signature: zarf verifies when it can and passes an unsigned package otherwise. Set C(verify: always) to reject one."
    type: path
    required: false
    cli_flag: "--key"
  verify:
    description:
      - "When to verify the package signature. Left unset, zarf's own default applies, which is C(if-possible)."
    type: str
    required: false
    choices:
      - never
      - if-possible
      - always
    cli_flag: "--verify"
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
- name: Inspect a staged Zarf package tarball, rejecting it when unsigned
  colonel_byte.zarf.zarf_package_inspect:
    package: /srv/staging/zarf-package-csi-rook-ceph-amd64-v1.20.7-upstream.tar.zst
    public_key: /etc/zarf/colonel-byte-zarf-packages.pub
    verify: always
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

# The top-level keys of a zarf.yaml. The definition is found by the first line that begins one of
# them at column 0, rather than by searching the stream for "kind:": the verification line zarf
# prints ahead of the YAML when --key is given is not the only thing that can hold that substring,
# and a component's own nested YAML routinely does. Searching would silently cut the definition
# short at the wrong place and return a mapping missing everything above the match.
DEFINITION_KEYS = ("kind", "metadata", "build", "components", "constants", "variables")


def _definition_yaml(stdout):
    """Return the YAML definition in zarf's output, without whatever it printed ahead of it."""
    lines = (stdout or "").splitlines()
    for index, line in enumerate(lines):
        key = line.split(":", 1)[0]
        if key in DEFINITION_KEYS and line.startswith(key + ":"):
            return "\n".join(lines[index:])
    # No recognisable key: hand the whole stream to the parser, which reports a better error about
    # it than this function could.
    return stdout


def command_parts(params):
    """Return the zarf command line this module runs.

    A plain function of the parameters, and the only place a flag is named, so that
    internal/zarfmod/infoplugins_test.go can read the flags the module renders out of the source
    and hold them against the documented cli_flag values.
    """
    parts = [
        params["zarf_binary"],
        "package",
        "inspect",
        "definition",
        params["package"],
        # The definition is parsed as YAML below, and colour codes in it are not YAML.
        "--no-color",
    ]
    if params.get("public_key"):
        parts.extend(["--key", params["public_key"]])
    if params.get("verify"):
        # --verify=always, never --verify always. It is a flag with a no-argument default, so pflag
        # reads it the way it reads a boolean: the space form leaves the mode as a positional
        # argument and zarf rejects the command for having one too many. internal/zarfmod renders it
        # through command.valueFlag for the same reason; see
        # docs/agent/choice-zarf-ansible-module.md.
        parts.append("--verify=%s" % params["verify"])
    return parts


class ActionModule(ZarfInfoActionBase):
    """Action plugin that runs zarf package inspect definition and parses the YAML."""

    REQUIRED = ("package",)

    def zarf_argv(self, params):
        return command_parts(params)

    def interpret(self, result, stdout, params):
        try:
            definition = yaml.safe_load(_definition_yaml(stdout))
        except Exception as exc:
            result["failed"] = True
            result["msg"] = "unable to parse the inspected definition as YAML: %s" % exc
            result["module_stdout"] = stdout
            return

        if not isinstance(definition, dict):
            result["failed"] = True
            result["msg"] = "the inspected definition is not a mapping"
            result["module_stdout"] = stdout
            return

        metadata = definition.get("metadata") or {}
        build = definition.get("build") or {}

        result["definition"] = definition
        result["package_name"] = metadata.get("name", "")
        result["package_version"] = metadata.get("version", "")
        result["package_flavor"] = build.get("flavor", "")
        result["msg"] = "inspected %s (%s)" % (
            metadata.get("name") or "the package",
            metadata.get("version") or "no version",
        )
