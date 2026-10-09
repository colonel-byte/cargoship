// Copyright 2026 colonel-byte
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

# The root of the inventory: every leaf includes it, and it holds what the whole inventory shares.
#
# It is root.hcl rather than terragrunt.hcl on purpose. Terragrunt warns that a terragrunt.hcl at
# the root of a configuration tree is an anti-pattern and will become an error, because it makes
# the root indistinguishable from a unit -- which is what `skip = true` used to paper over, before
# that argument was removed.
terragrunt_version_constraint = ">= 0.68"
