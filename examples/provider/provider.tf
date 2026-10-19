terraform {
  required_providers {
    varnish = {
      source = "varnish/varnish"
    }
  }
}

# Endpoint and token may also be supplied via the VARNISH_ENDPOINT and
# VARNISH_ACCESS_TOKEN environment variables.
provider "varnish" {
  # endpoint   = "https://api.varnish-cdn.com" # the default; override for other stacks
  access_token = var.varnish_access_token
  # insecure   = true  # only for local/test stacks with self-signed certs
}

variable "varnish_access_token" {
  type      = string
  sensitive = true
}
