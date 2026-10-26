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
FROM --platform=$BUILDPLATFORM docker.io/library/alpine:3.24@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a AS skeleton

ARG UID=65532
ARG GID=65532
RUN mkdir -p \
    /skeleton/home/nonroot/.cargoship-cache \
    /skeleton/home/nonroot/.zarf \
    /skeleton/home/nonroot/.ssh \
    /skeleton/workspace \
  && chmod 0700 /skeleton/home/nonroot /skeleton/home/nonroot/.ssh \
  && chown -R "${UID}:${GID}" /skeleton/home/nonroot /skeleton/workspace

FROM cgr.dev/chainguard/static:latest@sha256:9e2306b457ff71ce370043dc68c7b52bb1e6622c1c6c6714edbed57496f3116e

COPY --from=skeleton /skeleton/ /

ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/cargoship /usr/local/bin/cargoship

ENV HOME=/home/nonroot

WORKDIR /workspace

USER 65532:65532

ENTRYPOINT ["/usr/local/bin/cargoship"]
CMD ["--help"]
