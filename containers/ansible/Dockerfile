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

FROM docker.io/alpine/ansible:2.21.0@sha256:1a8ac3fd28c217210a9530b385f57d4b8afbe13956e5f0043063d98f5bd71bd5

ARG TARGETPLATFORM

COPY ${TARGETPLATFORM}/*.apk /tmp/apks/
COPY crypto/software@conlon.dev.rsa.pub /etc/apk/keys/software@conlon.dev.rsa.pub

# hadolint ignore=DL3018
RUN apk verify /tmp/apks/*.apk \
  && apk add --no-cache /tmp/apks/*.apk \
  && rm -rf /tmp/apks \
  && cargoship version \
  && test -x /usr/share/ansible/collections/ansible_collections/colonel_byte/cargoship/plugins/modules/cargoship_apply

ARG UID=65532
ARG GID=65532
RUN addgroup -g "${GID}" nonroot \
  && adduser -u "${UID}" -G nonroot -h /home/nonroot -D nonroot \
  && mkdir -p \
    /home/nonroot/.cargoship-cache \
    /home/nonroot/.zarf \
    /home/nonroot/.ssh \
    /workspace \
  && chmod 0700 /home/nonroot /home/nonroot/.ssh \
  && chown -R "${UID}:${GID}" /home/nonroot /workspace

LABEL org.opencontainers.image.title="cargoship-ansible" \
  org.opencontainers.image.description="Airgapped Kubernetes distribution packaging and deployment, with the Ansible collection" \
  org.opencontainers.image.source="https://github.com/colonel-byte/cargoship" \
  org.opencontainers.image.licenses="Apache-2.0" \
  org.opencontainers.image.base.name="docker.io/alpine/ansible:2.21.0"

ENV HOME=/home/nonroot

WORKDIR /workspace

USER 65532:65532

CMD ["ansible-playbook", "--help"]
