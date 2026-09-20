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
FROM --platform=$BUILDPLATFORM docker.io/library/alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS skeleton

ARG UID=65532
ARG GID=65532
RUN mkdir -p \
    /skeleton/home/nonroot/.cargoship-cache \
    /skeleton/home/nonroot/.zarf \
    /skeleton/home/nonroot/.ssh \
    /skeleton/workspace \
  && chmod 0700 /skeleton/home/nonroot /skeleton/home/nonroot/.ssh \
  && chown -R "${UID}:${GID}" /skeleton/home/nonroot /skeleton/workspace

FROM registry1.dso.mil/chainguard/cgr.dev/chainguard/static@sha256:90b7f22bcf781377e97cea715ef8e6d6f334eb23f3f8a83053b6155f5e33e66f

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
