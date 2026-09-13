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

# The base image is pinned by digest, and repeated on each FROM rather than
# hoisted into an ARG: dependabot's Dockerfile parser only reads literal FROM
# lines and cannot resolve a reference that arrives through a build arg. Keep
# every FROM in this file on the same digest.
FROM registry.access.redhat.com/ubi9/ubi-minimal:9.8@sha256:d235f607e1d6d833f031db107dc42206e4dd4d5aa9142c43d3771fb7f9bea76a

ARG TARGETPLATFORM

COPY ${TARGETPLATFORM}/*.rpm /tmp/rpms/

RUN rpm --install --verbose /tmp/rpms/*.rpm \
  && rm -rf /tmp/rpms \
  && cargoship version

ARG UID=65532
ARG GID=65532
RUN groupadd --gid "${GID}" cargoship \
  && useradd --uid "${UID}" --gid "${GID}" --home-dir /home/cargoship --create-home cargoship \
  && mkdir -p \
    /home/cargoship/.cargoship-cache \
    /home/cargoship/.zarf \
    /home/cargoship/.ssh \
    /workspace \
  && chmod 0700 /home/cargoship/.ssh \
  && chown -R "${UID}:${GID}" /home/cargoship /workspace

# base.name carries the tag only, not the digest, so that a dependabot digest
# refresh needs no edit here. Update it by hand when the FROM tag itself moves.
LABEL org.opencontainers.image.title="cargoship" \
  org.opencontainers.image.description="Airgapped Kubernetes distribution packaging and deployment" \
  org.opencontainers.image.source="https://github.com/colonel-byte/cargoship" \
  org.opencontainers.image.licenses="Apache-2.0" \
  org.opencontainers.image.base.name="registry.access.redhat.com/ubi9/ubi-minimal:9.8"

ENV HOME=/home/cargoship

WORKDIR /workspace

USER 65532:65532

ENTRYPOINT ["/usr/bin/cargoship"]
CMD ["--help"]
