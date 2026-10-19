# A purge token can only invalidate cached content, nothing else.
resource "varnish_cdn_purge_token" "ci" {
  label  = "ci-pipeline"
  expire = "8760h" # one year
}

# Optional: also allow HTTP basic auth against the purge endpoint.
resource "varnish_cdn_purge_token" "legacy" {
  label          = "legacy-cms"
  basic_username = "cms"
  basic_secret   = var.cms_purge_secret
}

output "ci_purge_token" {
  value     = varnish_cdn_purge_token.ci.access_token
  sensitive = true
}
