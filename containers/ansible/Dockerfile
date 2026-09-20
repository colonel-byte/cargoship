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

# A management node in a container: ansible-core plus cargoship, so the collection
# under ansible/colonel_byte/cargoship can be run without staging both onto a VM by
# hand. The base already carries ansible, bash, openssh, sshpass and rsync, which is
# the whole of what the playbooks need beyond cargoship itself.
#
# The base image is pinned by digest, written literally rather than hoisted into an
# ARG: dependabot's Dockerfile parser only reads literal FROM lines and cannot
# resolve a reference that arrives through a build arg. The digest is the
# manifest-list (index) digest, not a per-platform one, so a cross-platform build
# can still find its platform.
FROM docker.io/alpine/ansible:2.21.0@sha256:1a8ac3fd28c217210a9530b385f57d4b8afbe13956e5f0043063d98f5bd71bd5

ARG TARGETPLATFORM

COPY ${TARGETPLATFORM}/*.apk /tmp/apks/

# --allow-untrusted: the apk is signed, but with the release RSA key whose public half
# lives in crypto/ -- and the build context GoReleaser assembles holds artifacts,
# not repository files, so that key cannot be COPY'd in to verify against. Provenance
# for this apk comes from the pipeline that produced the context, not from apk. An
# operator installing the same package on a real host should trust the key instead:
# see the Package Signing section of docs/dev/goreleaser.md.
#
# The version check is deliberately more than `cargoship version`. The package also
# installs the collection tree under /usr/share/ansible/collections -- already on
# ansible-core's default collections path -- and symlinks each module file there to
# the binary. A missing link is invisible until a playbook fails to resolve a task,
# so one link is resolved and exec-tested at build time.
# hadolint ignore=DL3018
RUN apk add --no-cache --allow-untrusted /tmp/apks/*.apk \
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

# base.name carries the tag only, not the digest, so that a dependabot digest
# refresh needs no edit here. Update it by hand when the FROM tag itself moves.
LABEL org.opencontainers.image.title="cargoship-ansible" \
  org.opencontainers.image.description="Airgapped Kubernetes distribution packaging and deployment, with the Ansible collection" \
  org.opencontainers.image.source="https://github.com/colonel-byte/cargoship" \
  org.opencontainers.image.licenses="Apache-2.0" \
  org.opencontainers.image.base.name="docker.io/alpine/ansible:2.21.0"

ENV HOME=/home/nonroot

WORKDIR /workspace

USER 65532:65532

# No ENTRYPOINT, matching the base. The other two images exist to run cargoship and
# name it as their entrypoint; this one exists to run playbooks against a fleet, and
# a run legitimately reaches for ansible-playbook, ansible-inventory, ansible-galaxy
# or cargoship itself. Fixing one of those as the entrypoint would make the rest
# reachable only with --entrypoint.
CMD ["ansible-playbook", "--help"]
