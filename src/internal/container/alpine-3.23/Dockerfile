# Copyright 2023 bootloose authors
# Copyright 2026 colonel-byte
#
# This file contains code derived from bootloose:
# https://github.com/k0sproject/bootloose
#
# Modifications Copyright 2026 colonel-byte.
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

FROM docker.io/library/alpine:3.23.4

RUN apk add --no-cache \
  alpine-base \
  openssh-server \
  doas \
  curl \
  && rc-update add syslog boot \
  && rc-update add machine-id boot \
  && rc-update add sshd default \
  && rc-update add local default \
# disable ttys
  && sed -i -e 's/^\(tty[0-9]\)/# \1/' /etc/inittab \
# prevent start-stop-daemon from hanging when max_fds is huge
  && sed -Ei -e 's/^[# ](rc_ulimit)=.*/\1="-n 16384"/' /etc/rc.conf \
# no greetings
  && truncate -c -s0 /etc/issue /etc/motd \
# allow root to use doas
  && echo permit :wheel >> /etc/doas.d/doas.conf \
  && echo permit nopass keepenv root >> /etc/doas.d/doas.conf

# Start a shell if no command is given, otherwise exec the command verbatim.
ENTRYPOINT ["/bin/sh", "-c", "[ $# -gt 0 ] || set -- /bin/sh; exec \"$@\"", "--"]
