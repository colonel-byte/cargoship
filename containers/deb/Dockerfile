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

FROM docker.io/library/debian:13-slim@sha256:a99cfc517144bc59b1978475ec53b46ecabec7e43635402ee5b77cc54cd1b20a

ARG TARGETPLATFORM

COPY ${TARGETPLATFORM}/*.deb /tmp/debs/

RUN dpkg --install /tmp/debs/*.deb \
  && rm -rf /tmp/debs \
  && cargoship version \
  && test -x /usr/share/ansible/collections/ansible_collections/colonel_byte/cargoship/plugins/modules/cargoship_apply

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
  org.opencontainers.image.description="Airgapped Kubernetes distribution packaging and deployment" \
  org.opencontainers.image.source="https://github.com/colonel-byte/cargoship" \
  org.opencontainers.image.licenses="Apache-2.0" \
  org.opencontainers.image.base.name="docker.io/library/debian:13-slim"

ENV HOME=/home/nonroot

WORKDIR /workspace

USER 65532:65532

ENTRYPOINT ["/usr/bin/cargoship"]
CMD ["--help"]
