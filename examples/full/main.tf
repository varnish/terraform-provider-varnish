# Full reference example for the varnish provider: every resource, data source, and
# settable field with its allowed values. Trim it down to what you actually need.
#
# `settings` is a typed attribute. docs/resources/cdn_service.md lists every field.
# What the fields mean is documented at
# https://www.varnish-cdn.com/docs/configuration.

terraform {
  required_providers {
    varnish = {
      source = "varnish/varnish"
    }
  }
}

# endpoint/access_token/insecure may also come from VARNISH_ENDPOINT /
# VARNISH_ACCESS_TOKEN / VARNISH_INSECURE environment variables.
provider "varnish" {
  # endpoint     = "https://api.varnish-cdn.com" # the default; override for other stacks
  # access_token = var.varnish_access_token # sensitive; prefer the env var
  # insecure     = false                 # true skips TLS verification (test stacks only)
}

# ---------------------------------------------------------------------------
# Certificate (uploaded / DB-stored). Attach to a domain via cert_id.
# ---------------------------------------------------------------------------
variable "certificate_pem" {
  type        = string
  default     = ""
  description = "PEM certificate chain (e.g. file(\"cert.pem\") or a tfvars value)."
}

variable "private_key_pem" {
  type        = string
  default     = ""
  sensitive   = true
  description = "PEM private key (write-only; never returned by the API)."
}

resource "varnish_cdn_certificate" "example" {
  name        = "example-com"
  certificate = var.certificate_pem
  private_key = var.private_key_pem

  # Optional TLS constraints:
  min_version   = "TLSv1.2" # e.g. TLSv1.2 | TLSv1.3
  max_version   = "TLSv1.3"
  cipher_suites = [] # empty = server defaults

  # Computed (read-only): id, common_name, san, not_before, not_after, type
}

# ---------------------------------------------------------------------------
# Service: domains + origins + the wizard configuration.
# ---------------------------------------------------------------------------
resource "varnish_cdn_service" "web" {
  # Optional; defaults to the latest available answer template. Changing it
  # forces replacement.
  # wizard_answer_template_id = 1

  deployed = true # false keeps the service undeployed

  # Computed (read-only): id, name, origin_secret, status, created_at, updated_at

  # ---- Domains (repeatable block) -------------------------------------
  domain {
    fqdn      = "www.example.com"                  # required
    subdomain = ""                                 # only for apex domains, e.g. "www" with fqdn = "example.com"
    tls       = true                               # enable TLS for the domain
    cert_id   = varnish_cdn_certificate.example.id # attach an uploaded cert; omit for the CDN's automatic cert
    # Computed (read-only): id, status, txt_record, verified_at, is_cdn
  }
  domain {
    fqdn = "api.example.com"
    tls  = true
  }
  # Wildcard domains (e.g. "*.example.com") are supported.
  domain {
    fqdn = "*.example.com"
    tls  = true
  }

  # ---- Origins (repeatable block; host:port must be unique) -----------
  origin {
    name               = "default"            # required, unique per service
    host               = "origin.example.com" # required
    port               = 443                  # required (use 443 with enable_tls)
    enable_tls         = true                 # connect to the origin over TLS
    allow_insecure_tls = false                # accept self-signed/invalid origin certs
    host_header        = ""                   # Host header to the origin ("" = default)
    is_default         = true                 # exactly one origin must be the default
  }
  origin {
    name       = "static"
    host       = "static.example.com"
    port       = 80
    is_default = false
  }

  # ---- Wizard configuration -------------------------------------------
  # Set only the leaves you want to manage: the rest keep tracking the server
  # and never show up as drift. Removing a leaf you set stops managing it, it
  # does not revert it.
  settings = {
    general = {
      uncacheable                    = false # true disables caching entirely
      enable_cacheable_ttl_override  = true
      cacheable_ttl_override         = "10m" # duration; overrides non-zero origin TTLs
      enable_error_code_ttl_override = true
      error_code_ttl_override        = "10m" # duration
      uncacheable_ttl                = "1m"  # duration
      first_byte_timeout             = "10s" # duration
      max_req_body_size              = "10m" # size
      backend_retry                  = false
      enable_url_rewrite             = false
      url_rewrite                    = "/"
      allow_body_with_get            = false
      url_decode_reserved            = "enabled"                               # enabled | skip
      websocket                      = "disable"                               # disable | enable | exclusive
      chunked_body                   = { action = "deny" }                     # deny | cache | allow
      sharding                       = { enable = false, hash_source = "url" } # url | client_ip
      allow_methods                  = ["GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "CONNECT", "OPTIONS", "TRACE"]
    }

    high_availability = {
      enable_stale_if_error = true
      stale_if_error = {
        keep_stale_duration = "10m"
        stale_revive        = { enable = true, ttl = "1m", grace = "10m" }
      }
    }

    query_params = {
      remove_query_parameters = { action = "sort" } # redirect | continue | sort | off
    }

    cookies = {
      on_cookie     = { action = "remove", allow_list = [] }               # pass | remove | allow_list
      on_set_cookie = { action = "pass", allow_list = [], deny_list = [] } # pass | remove | allow_list | deny_list
    }

    headers = {
      on_auth_header             = { action = "pass" } # remove | pass
      true_client_ip             = false
      hash_headers               = []
      unset_headers_for_delivery = []
      set_headers_for_delivery   = []
      # rows of name/value pairs
      set_backend_headers = [
        # { name = "X-Forwarded-Host", value = "www.example.com" },
      ]
    }

    optimisation = {
      imageopt = { webp = true, quality = 80 } # quality 1-100
    }

    security = {
      https_redirect      = true
      hsts                = true
      bot_verify_settings = "enabled"                                           # enabled | disabled
      bot_protection      = { vpow = false, mode = "both", captcha_ttl = "1h" } # mode: captcha | pow | both
      allow_list          = { enable = false, acl = [] }
      deny_list           = { enable = false, action = "403", acl = [] } # 400 | 403 | 404
      waf = {
        enable = false
        mode   = "deny" # detect | deny
        owasp  = false
        atomic = false
      }
    }

    # Custom rules: settings applied only to matching requests. A rule group
    # takes either paths or extensions, never both, and each rule value is JSON
    # encoded exactly like the setting it overrides.
    overrides = [
      {
        name       = "cache-images-long"
        extensions = ["jpg", "png", "webp"]
        rules = [
          { string_id = "general.cacheableTTLOverride", value = jsonencode("7d") },
          { string_id = "general.uncacheable", value = jsonencode(false) },
        ]
      },
    ]
  }
}

# ---------------------------------------------------------------------------
# Data source: expected DNS records + verification status for a domain.
# ---------------------------------------------------------------------------
data "varnish_cdn_domain_dns" "www" {
  service_id = varnish_cdn_service.web.id
  fqdn       = "www.example.com"
}

# ---------------------------------------------------------------------------
# Data source: wizard templates available to the org (raw JSON).
# ---------------------------------------------------------------------------
data "varnish_cdn_wizard_templates" "all" {}

# ---------------------------------------------------------------------------
# Org users (invites), in the access token's organization.
# ---------------------------------------------------------------------------
data "varnish_roles" "all" {} # discover valid role names

resource "varnish_org_user" "alice" {
  email = "alice@example.com"
  role  = "Admin" # an org role name from data.varnish_roles.all (e.g. Admin, Finance, Viewer, Developer)
  # Computed: id ("<org>:<email>"), org_id, status ("pending"|"member"), account_id
}

# ---------------------------------------------------------------------------
# Outputs
# ---------------------------------------------------------------------------
output "service_id" {
  value = varnish_cdn_service.web.id
}

output "expected_dns" {
  value = {
    cname     = data.varnish_cdn_domain_dns.www.expected_cname
    txt       = data.varnish_cdn_domain_dns.www.expected_txt
    a_records = data.varnish_cdn_domain_dns.www.expected_a_records
    verified  = data.varnish_cdn_domain_dns.www.verified
  }
}

output "available_roles" {
  value = data.varnish_roles.all.roles
}
