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
FROM registry.access.redhat.com/ubi9/ubi-minimal:9.8@sha256:984df0a2b8d9011d419b0ad260b25f6b74b7ce69ab0595b75522e30f8849f27f

ARG TARGETPLATFORM

COPY ${TARGETPLATFORM}/*.rpm /tmp/rpms/

RUN rpm --install --verbose /tmp/rpms/*.rpm \
  && rm -rf /tmp/rpms \
  && cargoship version

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

# base.name carries the tag only, not the digest, so that a dependabot digest
# refresh needs no edit here. Update it by hand when the FROM tag itself moves.
LABEL org.opencontainers.image.title="cargoship" \
  org.opencontainers.image.description="Airgapped Kubernetes distribution packaging and deployment" \
  org.opencontainers.image.source="https://github.com/colonel-byte/cargoship" \
  org.opencontainers.image.licenses="Apache-2.0" \
  org.opencontainers.image.base.name="registry.access.redhat.com/ubi9/ubi-minimal:9.8"

ENV HOME=/home/nonroot

WORKDIR /workspace

USER 65532:65532

ENTRYPOINT ["/usr/bin/cargoship"]
CMD ["--help"]
