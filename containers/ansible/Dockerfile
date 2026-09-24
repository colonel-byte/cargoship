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

FROM ghcr.io/almalinux/10-base:10.2@sha256:f110fee4d46a239ed55f72f1f927cb89b2dfe1c2a8728208806eba5b73798762

ARG TARGETPLATFORM
ARG ANSIBLE_CORE_VERSION="2.16.16-2.el10_2.1"
ARG BASH_COMPLETION_VERSION="2.11-16.el10"

COPY ${TARGETPLATFORM}/*.rpm /tmp/rpms/
COPY crypto/software@conlon.dev.gpg /etc/pki/rpm-gpg/RPM-GPG-KEY-colonel-byte
COPY ansible/colonel_byte/cargoship/requirements.yml /etc/cargoship/ansible/requirements.yml

# hadolint ignore=DL3040
RUN rpm --import /etc/pki/rpm-gpg/RPM-GPG-KEY-colonel-byte \
  && rpm --checksig /tmp/rpms/*.rpm \
  && rpm --install --verbose /tmp/rpms/*.rpm \
  && rm -rf /tmp/rpms \
  && cargoship version \
  && dnf install -y --nodocs --setopt=install_weak_deps=0 \
    ansible-core-${ANSIBLE_CORE_VERSION} \
    bash-completion-${BASH_COMPLETION_VERSION} \
  && dnf clean all \
  && rm -rf /var/cache/yum \
  && ansible-galaxy collection install --requirements-file /etc/cargoship/ansible/requirements.yml --collections-path /usr/share/ansible/collections/ansible_collections

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

LABEL org.opencontainers.image.title="cargoship" \
  org.opencontainers.image.description="Airgapped Kubernetes distribution packaging and deployment, with the Ansible collection" \
  org.opencontainers.image.source="https://github.com/colonel-byte/cargoship" \
  org.opencontainers.image.licenses="Apache-2.0" \
  org.opencontainers.image.base.name="ghcr.io/almalinux/10-base:10.2"

ENV HOME=/home/nonroot

WORKDIR /workspace

USER 65532:65532

CMD ["ansible-playbook", "--help"]
