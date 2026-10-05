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
  - "The Secret holds the registry, git and artifact server credentials and the agent's TLS private key, so by default they are removed from C(state) and the paths that were removed are returned in C(redacted). Set C(include_credentials: true) to get them."
version_added: "0.31.0"
author:
  - Allen Conlon (@colonel-byte)
notes:
  - "Set C(run_once: true) and C(delegate_to: localhost) on the task."
  - "The returned C(state) is redacted unless C(include_credentials) is set, because zarf writes eight secrets into that one Secret: the registry push, pull and seed secrets, the git server push and pull passwords, the artifact server token, and the agent webhook's TLS private key."
  - "A run with C(include_credentials: true) censors its own task output, as though C(no_log: true) had been set on the task. The registered variable still holds the real state; it is the display that is suppressed. Note that a later task which templates the state - C(set_fact) in particular - prints what this one hid, so it needs C(no_log: true) of its own."
  - "The module runs in check mode and reports C(changed: false), because reading state changes nothing."
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
  include_credentials:
    description:
      - "Whether to return the credentials the C(zarf-state) Secret holds. Left unset, they are removed from C(state) and named in C(redacted)."
      - "A run that sets it censors its own task output. See the notes."
    type: bool
    default: false
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

- name: Report which paths were withheld
  ansible.builtin.debug:
    var: zarf_state_result.redacted

- name: Read the registry push credentials to log in with them
  colonel_byte.zarf.zarf_state_info:
    kubeconfig: /etc/rancher/rke2/rke2.yaml
    include_credentials: true
  delegate_to: localhost
  run_once: true
  register: zarf_state_credentials
"""

# The key of the Secret the state is stored under, and the Secret's own name. Neither is a
# parameter: they are zarf's own names for its own state.
STATE_SECRET = "zarf-state"
STATE_KEY = "jsonpath={.data.state}"

# The leaf names whose values are credentials. Zarf writes eight of them into the state as of
# zarf v0.85.0: registryInfo.pushPassword, registryInfo.pullPassword, registryInfo.secret,
# gitServer.pushPassword, gitServer.pullPassword, artifactServer.pushPassword,
# agentInfo.tls.key, and agentTLS.key -- the last being the deprecated copy of the one before it,
# which older zarf versions wrote and this one still does.
#
# Matching is by leaf name anywhere in the document rather than by a list of those eight paths.
# A path list is more precise and goes stale the next time zarf adds a field, and what staleness
# costs here is a leaked credential rather than a failed task, so the loose end points the safe
# way: a field zarf adds under a name like these is withheld before anybody has read this file.
#
# agentInfo.tls.ca and .cert are deliberately not matched. They are public certificates, and
# templating the CA into a trust bundle is a reason to call this module.
CREDENTIAL_SUBSTRINGS = ("password", "token")
CREDENTIAL_NAMES = ("secret", "key")


def _is_credential(name):
    """Report whether a leaf of this name holds a credential."""
    lowered = name.lower()
    if lowered in CREDENTIAL_NAMES:
        return True
    return any(part in lowered for part in CREDENTIAL_SUBSTRINGS)


def redact_state(state, prefix=""):
    """Return the state without its credentials, and the dotted paths of what was removed.

    The keys are removed rather than blanked. A missing key fails a template with "'dict object'
    has no attribute 'pushPassword'", which names the problem; a sentinel string templates cleanly
    into a registry login and fails somewhere else entirely.
    """
    if not isinstance(state, dict):
        return state, []

    kept = {}
    redacted = []
    for name, value in state.items():
        path = prefix + name
        if _is_credential(name):
            redacted.append(path)
            continue
        if isinstance(value, dict):
            value, below = redact_state(value, path + ".")
            redacted.extend(below)
        elif isinstance(value, list):
            # Nothing in the state is a list of mappings today. Descending into one anyway costs a
            # branch and means a credential zarf puts inside one is withheld without this function
            # being revisited.
            walked = []
            for index, item in enumerate(value):
                item, below = redact_state(item, "%s[%d]." % (path, index))
                redacted.extend(below)
                walked.append(item)
            value = walked
        kept[name] = value
    return kept, sorted(redacted)


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

    def interpret(self, result, stdout, params):
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

        if params.get("include_credentials"):
            result["state"] = state
            result["redacted"] = []
            # The operator asked for the credentials, which is not the same as asking for them to
            # be printed. This is the return key ansible-core censors a result by; see
            # executor/task_result.py. The registered variable still holds the real state.
            result["_ansible_no_log"] = True
            result["msg"] = "retrieved and parsed %s, credentials included" % STATE_SECRET
            return

        state, redacted = redact_state(state)
        result["state"] = state
        result["redacted"] = redacted
        result["msg"] = "retrieved and parsed %s, withholding %d credential(s)" % (
            STATE_SECRET,
            len(redacted),
        )
