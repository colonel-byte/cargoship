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

# A stand-in for zarf, for the zarf_package_info tests.
#
# zarf_package_info runs `zarf package list --output-format json` and decodes what comes back, so a
# zarf that prints a recorded document exercises the whole of the module except the cluster. Every
# argument is ignored on purpose: what the module renders is held by
# internal/zarfmod/infoplugins_test.go, and what these tests prove is what it does with the answer.
#
# CARGOSHIP_E2E_ZARF_PACKAGE_LIST names the document to serve. Nothing here is contacted.
set -eu
cat "${CARGOSHIP_E2E_ZARF_PACKAGE_LIST}"
