data "varnish_cdn_domain_dns" "api" {
  service_id = varnish_cdn_service.web.id
  fqdn       = "api.example.com"
}

# Wire the expected records into your DNS provider, e.g.:
output "expected_cname" {
  value = data.varnish_cdn_domain_dns.api.expected_cname
}

output "expected_txt" {
  value = data.varnish_cdn_domain_dns.api.expected_txt
}
