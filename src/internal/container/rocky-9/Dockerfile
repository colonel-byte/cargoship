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

FROM docker.io/rockylinux/rockylinux:9-ubi

ENV container docker

RUN dnf -y install sudo systemd hostname procps-ng net-tools iproute iputils openssh-server && dnf clean all

RUN systemctl set-default multi-user.target
RUN systemctl mask \
    dev-hugepages.mount \
    sys-fs-fuse-connections.mount \
    systemd-update-utmp.service \
    systemd-tmpfiles-setup.service \
    console-getty.service

RUN >/etc/machine-id

EXPOSE 22

# https://www.freedesktop.org/wiki/Software/systemd/ContainerInterface/
STOPSIGNAL SIGRTMIN+3

CMD ["/bin/bash"]
