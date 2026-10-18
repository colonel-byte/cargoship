# The root of the inventory: every leaf includes it, and it holds what the whole inventory shares.
#
# It is root.hcl rather than terragrunt.hcl on purpose. Terragrunt warns that a terragrunt.hcl at
# the root of a configuration tree is an anti-pattern and will become an error, because it makes
# the root indistinguishable from a unit -- which is what `skip = true` used to paper over, before
# that argument was removed.
terragrunt_version_constraint = ">= 0.68"
