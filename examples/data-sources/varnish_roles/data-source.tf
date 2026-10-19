# Role names valid for varnish_org_user.role, for the token's organization.
data "varnish_roles" "all" {}

output "available_roles" {
  value = data.varnish_roles.all.roles
}

# Pick one by name instead of hardcoding it:
locals {
  admin_role = one([for r in data.varnish_roles.all.roles : r.name if lower(r.name) == "admin"])
}
