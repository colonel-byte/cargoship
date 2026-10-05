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

# syntax=docker/dockerfile:1

# The zarf binary the colonel_byte.zarf module files drive. They render a zarf command line and run
# the installed zarf; without one in the image, every task in that collection fails on a binary it
# cannot find.
#
# It comes from the agent image rather than from a release archive because that image is already a
# signed, multi-architecture artifact carrying the whole CLI at /zarf -- zarf's own agent runs
# `zarf internal agent` out of it -- so there is no tarball to fetch, verify and unpack at build
# time, and the architecture follows the image the way every other FROM here does. It is pinned by
# the digest of the image index, not of one platform's manifest, so the arm64 build resolves too.
#
# It costs about 200MB in the image, which is zarf's own size: it vendors Helm, the Kubernetes
# clients, Kustomize and cosign. There is no smaller artifact that is still the whole CLI.
FROM ghcr.io/zarf-dev/zarf/agent:v0.87.0@sha256:63c400ea3a859ca1cac21c23e1a50240fc128bd00fac6fac189fe4b4348c484d AS zarf

FROM ghcr.io/almalinux/10-base:10.2@sha256:ed618ca107e9fd47ee2a64d1d33119ef356cfaf65a338daaa3fad19b80ae5da4

ARG TARGETPLATFORM
ARG ANSIBLE_CORE_VERSION="2.16.16-2.el10_2.1"
ARG BASH_COMPLETION_VERSION="2.11-16.el10"

# hadolint ignore=DL3040
RUN dnf install -y --nodocs --setopt=install_weak_deps=0 ansible-core-${ANSIBLE_CORE_VERSION} bash-completion-${BASH_COMPLETION_VERSION} \
  && dnf clean all \
  && rm -rf /var/cache/yum

ARG UID=65532
ARG GID=65532
RUN groupadd --gid "${GID}" nonroot \
  && useradd --uid "${UID}" --gid "${GID}" --home-dir /home/nonroot --create-home nonroot \
  && mkdir -p \
    /home/nonroot/.cargoship-cache \
    /home/nonroot/.zarf \
    /home/nonroot/.zarf-cache \
    /home/nonroot/.ssh \
    /workspace \
  && chmod 0700 /home/nonroot /home/nonroot/.ssh \
  && chown -R "${UID}:${GID}" /home/nonroot /workspace

# 0755 explicitly: the binary is 0700 in the agent image, where it only ever runs as that image's
# own user, and a mode nobody else can execute is not what belongs on a management node's PATH.
COPY --from=zarf --chmod=0755 /zarf /usr/local/bin/zarf

COPY ${TARGETPLATFORM}/*.rpm /tmp/rpms/
COPY crypto/software@conlon.dev.gpg /etc/pki/rpm-gpg/RPM-GPG-KEY-colonel-byte

RUN rpm --import /etc/pki/rpm-gpg/RPM-GPG-KEY-colonel-byte \
  && rpm --checksig /tmp/rpms/*.rpm \
  && rpm --install --verbose /tmp/rpms/*.rpm \
  && rm -rf /tmp/rpms \
  && cargoship version \
  && zarf version

# Both collections the package installs are on ansible-core's default collections path, and both
# of their module files answer. This is the only place in the pipeline where that is true at once:
# the collection, the binaries it dispatches to, and an ansible-core to find them are all here.
#
# The zarf_init and zarf_package_deploy checks are the ZEP-0072 proof's contract, and each one fails the image build rather
# than a playbook. Invoked under a zarf_ name that is not an action it answers as, it must refuse
# with a non-zero status rather than emit JSON nobody asked for -- package_remove is such a name,
# because these binaries answer as init and package_deploy and nothing else; given parameters it
# cannot read, a module
# must still answer with one JSON object and exit 0, because a non-zero status makes Ansible report
# MODULE FAILURE instead of the message the module wrote; and in check mode it must report the task
# as skipped, since zarf init has no dry run. The last one reads its parameters on stdin, which is
# the path this collection's action plugin uses.
# The checks below pipe the module's output into a parser, and a wrapper that crashed mid-pipe
# must fail the build rather than be covered by the parser's exit status.
SHELL ["/bin/bash", "-o", "pipefail", "-c"]

RUN ansible-galaxy collection list >/tmp/collections 2>/dev/null \
  && grep -q 'colonel_byte.cargoship' /tmp/collections \
  && grep -q 'colonel_byte.zarf' /tmp/collections \
  && rm -f /tmp/collections \
  && test -L /usr/share/ansible/collections/ansible_collections/colonel_byte/zarf/plugins/modules/zarf_init \
  && ln -s /usr/bin/zarf_init /tmp/zarf_package_remove \
  && ! /tmp/zarf_package_remove >/dev/null 2>&1 \
  && rm -f /tmp/zarf_package_remove \
  && ZARF_ANSIBLE_MODULE=init zarf_init /dev/null \
    | python3 -c 'import json,sys; r=json.load(sys.stdin); sys.exit(0 if r.get("failed") else 1)' \
  && printf '%s' '{"init_package":"/tmp","_ansible_check_mode":true}' \
    | ZARF_ANSIBLE_MODULE=init zarf_init \
    | python3 -c 'import json,sys; r=json.load(sys.stdin); sys.exit(0 if r.get("skipped") else 1)' \
  && test -L /usr/share/ansible/collections/ansible_collections/colonel_byte/zarf/plugins/modules/zarf_package_deploy \
  && ZARF_ANSIBLE_MODULE=package_deploy zarf_package_deploy /dev/null \
    | python3 -c 'import json,sys; r=json.load(sys.stdin); sys.exit(0 if r.get("failed") else 1)' \
  && printf '%s' '{"package":"/tmp/pkg.tar.zst","_ansible_check_mode":true}' \
    | ZARF_ANSIBLE_MODULE=package_deploy zarf_package_deploy \
    | python3 -c 'import json,sys; r=json.load(sys.stdin); sys.exit(0 if r.get("skipped") else 1)' \
  && printf '%s' '{"package":"/tmp/pkg.tar.zst","kubeconfig":"/tmp/none"}' \
    | ZARF_ANSIBLE_MODULE=package_deploy zarf_package_deploy \
    | python3 -c 'import json,sys; r=json.load(sys.stdin); d=r.get("zarf",{}); sys.exit(0 if r.get("failed") and d.get("exitCode") else 1)'

ENV HOME=/home/nonroot

WORKDIR /workspace

USER 65532:65532

CMD ["ansible-playbook", "--help"]
