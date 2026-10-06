#!/bin/sh
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

# A stand-in for zarf, for the zarf_state_info redaction test.
#
# zarf_state_info runs `zarf tools kubectl get secret zarf-state -o jsonpath={.data.state}` and
# decodes what comes back, so a zarf that prints the base64 of a recorded state exercises the whole
# of the module except the cluster. Every argument is ignored on purpose: what the module renders is
# held by internal/zarfmod/infoplugins_test.go, and what this proves is what it does with the answer.
#
# CARGOSHIP_E2E_ZARF_STATE names the state document to serve. Nothing here is contacted.
set -eu
base64 -w0 "${CARGOSHIP_E2E_ZARF_STATE}"
