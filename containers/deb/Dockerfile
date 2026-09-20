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

# The Debian-family counterpart to containers/ubi/Dockerfile: a dpkg-registered
# install rather than a loose binary, for environments that want one. It is also
# the only thing in the pipeline that installs the .deb, which is the point --
# a package nothing installs is a package nobody knows is broken.
#
# The directory is named for the package format, not the base, because the format
# is the invariant: this image exists to exercise the .deb, and swapping Debian for
# Ubuntu would not change that. See the note on directory naming in
# docs/dev/goreleaser.md.
#
# The base image is pinned by digest, written literally rather than hoisted into an
# ARG: dependabot's Dockerfile parser only reads literal FROM lines and cannot
# resolve a reference that arrives through a build arg. The digest is the
# manifest-list (index) digest, not a per-platform one, so a cross-platform build
# can still find its platform.
FROM docker.io/library/debian:13-slim@sha256:a99cfc517144bc59b1978475ec53b46ecabec7e43635402ee5b77cc54cd1b20a

ARG TARGETPLATFORM

COPY ${TARGETPLATFORM}/*.deb /tmp/debs/

# dpkg -i, not apt-get install. The package declares no dependencies, so nothing
# needs resolving and the build stays hermetic -- no repo access at image build
# time, which is the same reasoning as the UBI image's `rpm -i`. Switch to apt-get
# if nfpms.*.depends ever gains an entry.
#
# The version check is deliberately more than `cargoship version`. The package also
# installs the Ansible collection tree under /usr/share/ansible/collections and
# symlinks each module file there to the binary; a missing link is invisible until
# a playbook fails to resolve a task, so one link is resolved and exec-tested here.
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

# base.name carries the tag only, not the digest, so that a dependabot digest
# refresh needs no edit here. Update it by hand when the FROM tag itself moves.
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
