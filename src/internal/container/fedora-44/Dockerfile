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

FROM docker.io/library/fedora:44

ENV container docker

RUN dnf -y install sudo openssh-server procps-ng hostname net-tools iproute iputils wget && dnf clean all

EXPOSE 22

RUN echo -e '#!/bin/sh\n[ -f "/etc/machine-id" ] && rm -f /etc/machine-id\nsystemd-machine-id-setup &> /dev/null\nexec "$@"' > /entry.sh \
    && chmod +x /entry.sh \
    && truncate -c -s0 /etc/issue /etc/motd \

STOPSIGNAL SIGRTMIN+3

ENTRYPOINT ["/entry.sh"]

CMD ["/bin/bash"]
