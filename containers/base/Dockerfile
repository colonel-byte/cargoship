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
FROM --platform=$BUILDPLATFORM docker.io/library/alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 AS skeleton

ARG UID=65532
ARG GID=65532
RUN mkdir -p \
    /skeleton/home/nonroot/.cargoship-cache \
    /skeleton/home/nonroot/.zarf \
    /skeleton/home/nonroot/.ssh \
    /skeleton/workspace \
  && chmod 0700 /skeleton/home/nonroot /skeleton/home/nonroot/.ssh \
  && chown -R "${UID}:${GID}" /skeleton/home/nonroot /skeleton/workspace

FROM registry1.dso.mil/chainguard/cgr.dev/chainguard/static@sha256:f2787f11adca5eadeeb15c5012b59e4b95d3bdbd792a8d243a7a931ea4e70e63

COPY --from=skeleton /skeleton/ /

ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/cargoship /usr/local/bin/cargoship

LABEL org.opencontainers.image.title="cargoship" \
  org.opencontainers.image.description="Airgapped Kubernetes distribution packaging and deployment" \
  org.opencontainers.image.source="https://github.com/colonel-byte/cargoship" \
  org.opencontainers.image.licenses="Apache-2.0" \
  org.opencontainers.image.base.name="registry1.dso.mil/chainguard/cgr.dev/chainguard/static:latest"

ENV HOME=/home/nonroot

WORKDIR /workspace

USER 65532:65532

ENTRYPOINT ["/usr/local/bin/cargoship"]
CMD ["--help"]
