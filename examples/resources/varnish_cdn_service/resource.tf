resource "varnish_cdn_service" "web" {
  domain {
    fqdn = "www.example.com"
    tls  = true
  }

  # An apex domain needs a subdomain. This serves www.example.org, and
  # example.org redirects to it.
  domain {
    fqdn      = "example.org"
    subdomain = "www"
    tls       = true
  }

  # Wildcard domains are supported.
  domain {
    fqdn = "*.example.com"
    tls  = true
  }

  origin {
    name       = "default"
    host       = "origin.example.com"
    port       = 443
    enable_tls = true
    is_default = true
  }

  # Wizard configuration. Declare only the leaves you want to manage: the rest
  # keep tracking the server. What the fields mean is documented at
  # https://www.varnish-cdn.com/docs/configuration.
  settings = {
    general = {
      uncacheable        = false # false = caching enabled
      first_byte_timeout = "30s"
      allow_methods      = ["GET", "HEAD"]
    }

    # Settings applied only to requests matching the given extensions or paths.
    overrides = [
      {
        name       = "cache-images-long"
        extensions = ["jpg", "png", "webp"]
        rules = [
          { string_id = "general.cacheableTTLOverride", value = jsonencode("7d") },
        ]
      },
    ]
  }
}
