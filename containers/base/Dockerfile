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

# Skeleton stage. The runtime base below ships no shell and no package manager, so
# the final image cannot mkdir/chown anything for itself; this stage lays out the
# directory tree the CLI expects and hands it over pre-owned. It only creates empty
# directories -- nothing architecture-specific -- so it is pinned to $BUILDPLATFORM
# and never pays for emulation on a cross build.
#
# The base image is pinned by digest, written literally rather than hoisted into
# an ARG: dependabot's Dockerfile parser only reads literal FROM lines and cannot
# resolve a reference that arrives through a build arg.
FROM --platform=$BUILDPLATFORM alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce AS skeleton

ARG UID=65532
ARG GID=65532
RUN mkdir -p \
    /skeleton/home/nonroot/.cargoship-cache \
    /skeleton/home/nonroot/.zarf \
    /skeleton/home/nonroot/.ssh \
    /skeleton/workspace \
  && chmod 0700 /skeleton/home/nonroot /skeleton/home/nonroot/.ssh \
  && chown -R "${UID}:${GID}" /skeleton/home/nonroot /skeleton/workspace

# Same digest-pinning rule as above. chainguard/static already provides the
# `nonroot` account at uid/gid 65532 along with its 0700 /home/nonroot, a CA bundle
# with SSL_CERT_FILE pointing at it, and a 1777 /tmp, so none of that is repeated
# here -- the skeleton above only adds the subdirectories the CLI expects, at the
# same ownership and mode the base already uses.
FROM cgr.dev/chainguard/static:latest@sha256:bf639cba19ba56329e6907ac26a7afcdde57a80b6aa66d5100da6883196e6b82

# The whole skeleton tree is copied at its root on purpose. A COPY whose
# destination directory does not yet exist creates that directory root-owned and
# only carries ownership on the entries beneath it, which would leave /workspace
# unwritable by the runtime user; merging the tree into / keeps every directory's
# own uid/gid/mode.
COPY --from=skeleton /skeleton/ /

ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/cargoship /usr/local/bin/cargoship

# base.name carries the tag only, not the digest, so that a dependabot digest
# refresh needs no edit here. Update it by hand when the FROM tag itself moves.
LABEL org.opencontainers.image.title="cargoship" \
  org.opencontainers.image.description="Airgapped Kubernetes distribution packaging and deployment" \
  org.opencontainers.image.source="https://github.com/colonel-byte/cargoship" \
  org.opencontainers.image.licenses="Apache-2.0" \
  org.opencontainers.image.base.name="cgr.dev/chainguard/static:latest"

ENV HOME=/home/nonroot

WORKDIR /workspace

USER 65532:65532

ENTRYPOINT ["/usr/local/bin/cargoship"]
CMD ["--help"]
