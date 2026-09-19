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

"""Projection of an Ansible inventory into the argument a cargoship module takes.

This is the whole of the Python in this collection, and it is deliberately the whole of it. Every
translation rule -- which group carries which cargoship role, which host variable maps onto which
inventory field, what makes a host ambiguous -- lives in Go, in src/internal/ansibleinv, where it
is unit tested without Ansible. What happens here is projection and delegation: read Ansible's
resolved ``groups`` and ``hostvars`` out of the task variables, put them in the module's
``inventory`` argument, and hand the whole thing to the binary.

The plugin runs inside the ansible-playbook process, on the interpreter Ansible is already running
on. It adds no dependency to the machine that runs ``cargoship apply`` by hand.
"""

from __future__ import absolute_import, division, print_function

__metaclass__ = type

from ansible.plugins.action import ActionBase

# The Ansible connection variables cargoship reads, mirroring the constants in
# src/internal/ansibleinv/vars.go. TestActionPluginVariableAllowlist in that package reads this
# file and fails when the two drift.
ANSIBLE_VARS = (
    "ansible_host",
    "ansible_port",
    "ansible_ssh_private_key_file",
    "ansible_user",
)

# The namespace for the host variables that configure cargoship. Every variable under it is passed
# through, including ones this collection has never heard of: cargoship rejects an unknown
# cargoship_ variable by name, and it can only do that if it is shown the misspelling.
CARGOSHIP_PREFIX = "cargoship_"

# The parameter that carries other parameters. A role assembling a module call has a dict of
# parameters and no way to know which module takes which, and templating the whole of a task's
# arguments is both warned about by Ansible and genuinely unsafe. This lets the keys stay literal
# in the task and the values be templates: everything in it is merged into the arguments here,
# before the module is called, and the module never sees the parameter itself.
PARAMETERS = "parameters"


def project_host_vars(host_vars):
    """Return the variables of one host that cargoship reads.

    Everything else is dropped rather than forwarded. ``hostvars`` for a host carries its entire
    resolved variable set, which routinely includes credentials for things that have nothing to do
    with this cluster, and a module's arguments are written to a file on disk before the module
    runs. Forwarding the lot would put all of it there.
    """
    projected = {}
    for name, value in host_vars.items():
        if name in ANSIBLE_VARS or name.startswith(CARGOSHIP_PREFIX):
            projected[name] = value
    return projected


class CargoshipActionBase(ActionBase):
    """Base for the cargoship module action plugins.

    Each module's plugin is this class and nothing else. The module it delegates to is the one
    named by the task, which is the symlink the operator wrote in the playbook.
    """

    # This collection's modules are the cargoship binary. They connect to the fleet themselves,
    # from the node the task runs on, so there is nothing to copy to a managed node and no
    # temporary directory to make there.
    TRANSFERS_FILES = False

    def run(self, tmp=None, task_vars=None):
        result = super(CargoshipActionBase, self).run(tmp, task_vars)
        del tmp  # deprecated, and unused: nothing is transferred

        if task_vars is None:
            task_vars = {}

        args = dict(self._task.args)

        merged = args.pop(PARAMETERS, None) or {}
        for name in merged:
            if name in args:
                result["failed"] = True
                result["msg"] = (
                    "the %s parameter was given both directly and inside %s"
                    % (name, PARAMETERS)
                )
                return result
        args.update(merged)

        inventory = dict(args.get("inventory") or {})

        # An inventory the playbook filled in itself wins. That is how a play converges a fleet it
        # is not itself running against -- a staging inventory read from a file, say -- without
        # this plugin overwriting it with the hosts of the current play.
        if "groups" not in inventory:
            inventory["groups"] = {
                name: list(hosts) for name, hosts in (task_vars.get("groups") or {}).items()
            }
        if "hostvars" not in inventory:
            host_vars = task_vars.get("hostvars") or {}
            inventory["hostvars"] = {
                host: project_host_vars(host_vars[host]) for host in host_vars
            }

        args["inventory"] = inventory

        result.update(
            self._execute_module(
                module_name=self._task.action,
                module_args=args,
                task_vars=task_vars,
            )
        )
        return result
