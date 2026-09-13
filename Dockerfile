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

ARG BASE_IMAGE=alpine:3.22

FROM --platform=$BUILDPLATFORM ${BASE_IMAGE} AS rootfs

ARG UID=65532
ARG GID=65532
RUN addgroup -g "${GID}" cargoship \
  && adduser -D -u "${UID}" -G cargoship -h /home/cargoship cargoship \
  && mkdir -p \
    /home/cargoship/.cargoship-cache \
    /home/cargoship/.zarf \
    /home/cargoship/.ssh \
    /workspace \
    /tmp \
  && chmod 0700 /home/cargoship/.ssh \
  && chmod 1777 /tmp \
  && chown -R "${UID}:${GID}" /home/cargoship /workspace

FROM ${BASE_IMAGE}

ARG BASE_IMAGE

COPY --from=rootfs /etc/passwd /etc/group /etc/
COPY --from=rootfs --chown=65532:65532 /home/cargoship /home/cargoship
COPY --from=rootfs --chown=65532:65532 /workspace /workspace

ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/cargoship /usr/local/bin/cargoship

LABEL org.opencontainers.image.title="cargoship" \
  org.opencontainers.image.description="Airgapped Kubernetes distribution packaging and deployment" \
  org.opencontainers.image.source="https://github.com/colonel-byte/cargoship" \
  org.opencontainers.image.licenses="Apache-2.0" \
  org.opencontainers.image.base.name="${BASE_IMAGE}"

ENV HOME=/home/cargoship

WORKDIR /workspace

USER 65532:65532

ENTRYPOINT ["/usr/local/bin/cargoship"]
CMD ["--help"]
