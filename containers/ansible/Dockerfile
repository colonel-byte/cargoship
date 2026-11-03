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

FROM ghcr.io/almalinux/10-base:10.2@sha256:f72ef25730edaa7bde18a0bc7f8010ec7d1d8d6cef7fa47ecdd280aef65fa785

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
    /home/nonroot/.ssh \
    /workspace \
  && chmod 0700 /home/nonroot /home/nonroot/.ssh \
  && chown -R "${UID}:${GID}" /home/nonroot /workspace

COPY ${TARGETPLATFORM}/*.rpm /tmp/rpms/
COPY crypto/software@conlon.dev.gpg /etc/pki/rpm-gpg/RPM-GPG-KEY-colonel-byte

RUN rpm --import /etc/pki/rpm-gpg/RPM-GPG-KEY-colonel-byte \
  && rpm --checksig /tmp/rpms/*.rpm \
  && rpm --install --verbose /tmp/rpms/*.rpm \
  && rm -rf /tmp/rpms \
  && cargoship version

# The collection, installed the way a management node that did not install
# ansible-collection-colonel-byte-cargoship does it: ansible-galaxy from a tarball, then
# hack/ansible-link-modules.sh for the module files, which are symlinks to the binary installed
# above. The collection package is noarch and so cannot reach this build context -- see the
# comment on the ansible-collection nfpms entry in .goreleaser.yaml -- which makes this image a
# check of the Galaxy install path rather than of the package. /usr/share/ansible/collections is
# on ansible-core's default collections path, so nothing further is configured.
COPY ansible/colonel_byte/cargoship /tmp/collection/
COPY hack/ansible-link-modules.sh /tmp/ansible-link-modules.sh

RUN ansible-galaxy collection build --output-path /tmp /tmp/collection \
  && ansible-galaxy collection install --no-deps \
    --collections-path /usr/share/ansible/collections /tmp/colonel_byte-cargoship-*.tar.gz \
  && /tmp/ansible-link-modules.sh \
    /usr/share/ansible/collections/ansible_collections/colonel_byte/cargoship \
  && rm -rf /tmp/collection /tmp/ansible-link-modules.sh /tmp/colonel_byte-cargoship-*.tar.gz \
  && ansible-galaxy collection list >/tmp/collections 2>/dev/null \
  && grep -q 'colonel_byte.cargoship' /tmp/collections \
  && rm -f /tmp/collections

ENV HOME=/home/nonroot

WORKDIR /workspace

USER 65532:65532

CMD ["ansible-playbook", "--help"]
