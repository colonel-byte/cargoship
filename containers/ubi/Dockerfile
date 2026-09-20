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

FROM ghcr.io/almalinux/10-minimal:10.2@sha256:ee053be6b73674fdd32ec1dcee1e0b85dc2fa8b2f75976cbf8e72cfda2f718da

ARG TARGETPLATFORM

COPY ${TARGETPLATFORM}/*.rpm /tmp/rpms/
COPY crypto/software@conlon.dev.gpg /etc/pki/rpm-gpg/RPM-GPG-KEY-colonel-byte

RUN rpm --import /etc/pki/rpm-gpg/RPM-GPG-KEY-colonel-byte \
  && rpm --checksig /tmp/rpms/*.rpm \
  && rpm --install --verbose /tmp/rpms/*.rpm \
  && rm -rf /tmp/rpms \
  && cargoship version

ARG UID=65532
ARG GID=65532
# hadolint ignore=DL3040,DL3041
RUN microdnf install -y --nodocs --setopt=install_weak_deps=0 shadow-utils \
  && microdnf clean all \
  && groupadd --gid "${GID}" nonroot \
  && useradd --uid "${UID}" --gid "${GID}" --home-dir /home/nonroot --create-home nonroot \
  && mkdir -p \
    /home/nonroot/.cargoship-cache \
    /home/nonroot/.zarf \
    /home/nonroot/.ssh \
    /workspace \
  && chmod 0700 /home/nonroot /home/nonroot/.ssh \
  && chown -R "${UID}:${GID}" /home/nonroot /workspace \
  && microdnf remove shadow-utils -y \
  && microdnf clean all \
  && rm -rf /var/cache/yum

LABEL org.opencontainers.image.title="cargoship" \
  org.opencontainers.image.description="Airgapped Kubernetes distribution packaging and deployment" \
  org.opencontainers.image.source="https://github.com/colonel-byte/cargoship" \
  org.opencontainers.image.licenses="Apache-2.0" \
  org.opencontainers.image.base.name="ghcr.io/almalinux/10-minimal:10.2"

ENV HOME=/home/nonroot

WORKDIR /workspace

USER 65532:65532

ENTRYPOINT ["/usr/bin/cargoship"]
CMD ["--help"]
