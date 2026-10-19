# Valid role names for the organization.
data "varnish_roles" "all" {}

# Creating sends an invite; the same resource represents the user once the
# invite is accepted (status flips from "pending" to "member").
resource "varnish_org_user" "alice" {
  email = "alice@example.com"
  role  = "Admin" # a name from data.varnish_roles.all; matched case-insensitively
}

output "alice_status" {
  value = varnish_org_user.alice.status # "pending" or "member"
}
