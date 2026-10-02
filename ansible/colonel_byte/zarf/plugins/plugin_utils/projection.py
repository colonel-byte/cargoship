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

"""The whole of the Python in this collection.

It is deliberately the whole of it. Every rule about what a parameter means, which flag it renders,
and what counts as a valid combination lives in Go, in internal/zarfmod, where it is unit tested
without Ansible. What happens here is delivery and display: hand the task's parameters to the
wrapper binary, follow the heartbeat file it writes, and hand its one JSON object back to Ansible.

Two things here are the parts of ZEP-0072 that only exist on the Python side.

The first is that ``_execute_module()`` is not used. Ansible's module staging copies the module
file into a fresh temporary directory on every task run, and the module file is a binary. What is
used instead is ``_low_level_execute_command()``, invoking the wrapper already installed on the
management node -- the same pattern ``ansible.builtin.raw`` uses. The cost is that everything
staging provides for free has to be done here: the control keys Ansible would have injected, the
environment, and the arguments themselves.

The second is argument delivery. The proposal leans toward piping the parameters on stdin, so that
registry and git credentials never touch disk. That is what this does when the connection can
carry data on stdin. When it cannot -- an SSH connection without pipelining -- it falls back to
the proposal's other option: a 0600 file inside a 0700 directory, removed in a finally block. The
wrapper reads both, so the fallback is a different wire, not a different contract.
"""

from __future__ import absolute_import, division, print_function

__metaclass__ = type

import json
import os
import shlex
import shutil
import tempfile
import threading

from ansible import constants as C
from ansible.errors import AnsibleError
from ansible.plugins.action import ActionBase
from ansible.utils.display import Display

display = Display()

# The parameter that carries other parameters. A role assembling a module call has a dict of
# parameters and no way to know which module takes which, and templating the whole of a task's
# arguments is both warned about by Ansible and genuinely unsafe. This lets the keys stay literal
# in the task and the values be templates: everything in it is merged into the arguments here,
# before the wrapper is called, and the wrapper never sees the parameter itself.
PARAMETERS = "parameters"

# The task argument naming the wrapper binary to run. It is consumed here rather than forwarded:
# the wrapper rejects an unknown parameter by name, and this is not one of its parameters.
WRAPPER = "wrapper_binary"

# How often the monitor thread reads the heartbeat file, in seconds. ZEP-0072 specifies 500ms, on
# the grounds that a 15-minute init with no output reads as a hung play.
POLL_INTERVAL = 0.5

ENV_MODULE = "ZARF_ANSIBLE_MODULE"
ENV_STATUS_FILE = "ZARF_STATUS_FILE"


class ZarfActionBase(ActionBase):
    """Base for the zarf module action plugins.

    A subclass sets ``MODULE`` to the action it drives and ``DEFAULT_WRAPPER`` to the binary that
    answers as it. Everything else is here.
    """

    # The wrapper connects to the cluster itself, from the node the task runs on, so there is
    # nothing to copy to a managed node and no temporary directory to make there.
    TRANSFERS_FILES = False

    MODULE = None
    DEFAULT_WRAPPER = None

    def run(self, tmp=None, task_vars=None):
        result = super(ZarfActionBase, self).run(tmp, task_vars)
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

        wrapper = args.pop(WRAPPER, None) or self.DEFAULT_WRAPPER
        resolved = shutil.which(wrapper) if os.path.sep not in wrapper else wrapper
        if not resolved:
            result["failed"] = True
            result["msg"] = (
                "%s is not on the path of the node this task runs on. Install the zarf Ansible "
                "wrapper, or set the %s parameter to its path." % (wrapper, WRAPPER)
            )
            return result

        # The control keys ``_execute_module()`` would have injected. Check mode is the one that
        # changes behaviour: the wrapper reports the task as skipped rather than initialising a
        # cluster during a run an operator asked to be told about.
        args["_ansible_check_mode"] = bool(self._task.check_mode)
        args["_ansible_diff"] = bool(self._task.diff)
        args["_ansible_no_log"] = bool(self._task.no_log)
        args["_ansible_verbosity"] = display.verbosity

        payload = json.dumps(args)

        status_file = None
        args_dir = None
        stop_monitor = threading.Event()
        monitor = None

        try:
            status_fd, status_file = tempfile.mkstemp(prefix="zarf-status-", suffix=".json")
            os.close(status_fd)
            monitor = threading.Thread(
                target=self._monitor_status, args=(status_file, stop_monitor)
            )
            monitor.daemon = True
            monitor.start()

            env = {ENV_MODULE: self.MODULE, ENV_STATUS_FILE: status_file}

            in_data = None
            if self._pipelining_available():
                # Option B: the parameters stay in memory for the lifetime of the pipe. The
                # connection writes in_data to the child's stdin without encoding it, so it is
                # handed over as bytes.
                in_data = payload.encode("utf-8")
                command = self._command(resolved, env, args_file=None)
            else:
                # Option A: a private directory, a 0600 file, and a finally block below.
                args_dir = tempfile.mkdtemp(prefix="zarf-ansible-")
                args_path = os.path.join(args_dir, "args")
                with open(args_path, "w") as f:
                    os.fchmod(f.fileno(), 0o600)
                    f.write(payload)
                command = self._command(resolved, env, args_file=args_path)

            rc, stdout, stderr = self._execute_wrapper(command, in_data)
        except AnsibleError as err:
            result["failed"] = True
            result["msg"] = "unable to run %s: %s" % (wrapper, err)
            return result
        finally:
            if monitor is not None:
                stop_monitor.set()
                monitor.join(timeout=1.0)
            _remove_file(status_file)
            _remove_tree(args_dir)

        parsed = _parse_result(stdout)
        if parsed is None:
            result["failed"] = True
            result["rc"] = rc
            result["msg"] = (
                "%s did not return one JSON object, which is what every Ansible module must "
                "write on stdout. Its output is in module_stdout." % wrapper
            )
            result["module_stdout"] = stdout
            result["module_stderr"] = stderr
            return result

        # Zarf's own output is the diagnosis for a failed run, and the wrapper sends all of it to
        # stderr so that stdout carries nothing but the result.
        parsed.setdefault("module_stderr", stderr)
        result.update(parsed)
        return result

    def _command(self, binary, env, args_file):
        """Return the shell command line that runs the wrapper.

        The environment is part of the command rather than passed separately, because
        ``_low_level_execute_command()`` takes a command and not an environment. Every part is
        quoted: a path holding a space is an operator's choice, not an injection.
        """
        parts = ["env"]
        for name in sorted(env):
            parts.append("%s=%s" % (name, shlex.quote(str(env[name]))))
        parts.append(shlex.quote(binary))
        if args_file:
            parts.append(shlex.quote(args_file))
        return " ".join(parts)

    def _execute_wrapper(self, command, in_data):
        """Run the wrapper and return its exit status and streams."""
        # sudoable is left at its default: zarf needs no privilege escalation of its own, and an
        # operator who set become: true for an unrelated reason -- a root-owned kubeconfig, say --
        # still gets it, because the connection plugin applies it.
        res = self._low_level_execute_command(command, in_data=in_data)
        return (
            res.get("rc", 0),
            res.get("stdout", "") or "",
            res.get("stderr", "") or "",
        )

    def _pipelining_available(self):
        """Report whether the connection can hand the wrapper its parameters on stdin.

        A local connection always can. An SSH connection can only when pipelining is enabled,
        and asking it to anyway fails the task rather than falling back, so the question is asked
        before the attempt rather than after it.
        """
        if not getattr(self._connection, "has_pipelining", False):
            return False
        transport = getattr(self._connection, "transport", "")
        if transport == "local":
            return True
        return bool(getattr(self._play_context, "pipelining", False) or C.ANSIBLE_PIPELINING)

    def _monitor_status(self, path, stop):
        """Display each heartbeat the wrapper writes, until the run is over.

        The wrapper writes a whole file and renames it into place, so a read either sees the
        previous heartbeat or the next one. A read that fails anyway is dropped: a progress
        display must not be able to fail a task.
        """
        last = None
        while not stop.is_set():
            try:
                if os.path.exists(path) and os.path.getsize(path) > 0:
                    with open(path, "r") as f:
                        st = json.load(f)
                    key = (st.get("phase"), st.get("status"))
                    if key != last:
                        last = key
                        self._display_status(st)
            except Exception:
                pass
            stop.wait(POLL_INTERVAL)

    def _display_status(self, st):
        phase = st.get("phase")
        status = st.get("status")
        index = st.get("index", 0)
        total = st.get("total", 0)

        # A total of 0 is the wrapper saying it does not know how many components this init
        # package holds. "component 3" is honest where "component 3/0" is not.
        position = "%s/%s" % (index, total) if total else "%s" % index

        if status == "running":
            display.display("[zarf] component %s: %s [running]" % (position, phase),
                            color=C.COLOR_VERBOSE)
        elif status == "completed":
            display.display("[zarf] component %s: %s [done]" % (position, phase),
                            color=C.COLOR_OK)
        elif status == "failed":
            message = "[zarf] component %s: %s [failed]" % (position, phase)
            if st.get("error"):
                message = "%s: %s" % (message, st["error"])
            display.display(message, color=C.COLOR_ERROR)


def _parse_result(stdout):
    """Return the module result in stdout, or None when it does not hold one JSON object."""
    stdout = (stdout or "").strip()
    if not stdout:
        return None
    try:
        parsed = json.loads(stdout)
    except ValueError:
        return None
    if not isinstance(parsed, dict):
        return None
    return parsed


def _remove_file(path):
    if not path:
        return
    try:
        os.remove(path)
    except OSError:
        pass


def _remove_tree(path):
    if not path:
        return
    try:
        shutil.rmtree(path, ignore_errors=True)
    except OSError:
        pass
