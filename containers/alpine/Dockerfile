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

# The base image is pinned by digest, written literally rather than hoisted into
# an ARG: dependabot's Dockerfile parser only reads literal FROM lines and cannot
# resolve a reference that arrives through a build arg.
FROM alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce

ARG UID=65532
ARG GID=65532
RUN addgroup -g "${GID}" cargoship \
  && adduser -D -u "${UID}" -G cargoship -h /home/cargoship cargoship \
  && mkdir -p \
    /home/cargoship/.cargoship-cache \
    /home/cargoship/.zarf \
    /home/cargoship/.ssh \
    /workspace \
  && chmod 0700 /home/cargoship/.ssh \
  && chown -R "${UID}:${GID}" /home/cargoship /workspace

ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/cargoship /usr/local/bin/cargoship

# base.name carries the tag only, not the digest, so that a dependabot digest
# refresh needs no edit here. Update it by hand when the FROM tag itself moves.
LABEL org.opencontainers.image.title="cargoship" \
  org.opencontainers.image.description="Airgapped Kubernetes distribution packaging and deployment" \
  org.opencontainers.image.source="https://github.com/colonel-byte/cargoship" \
  org.opencontainers.image.licenses="Apache-2.0" \
  org.opencontainers.image.base.name="alpine:3.22"

ENV HOME=/home/cargoship

WORKDIR /workspace

USER 65532:65532

ENTRYPOINT ["/usr/local/bin/cargoship"]
CMD ["--help"]
