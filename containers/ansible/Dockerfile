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

ENV HOME=/home/nonroot

WORKDIR /workspace

USER 65532:65532

CMD ["ansible-playbook", "--help"]
