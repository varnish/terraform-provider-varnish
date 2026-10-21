## 0.1.1 (2026-10-21)

SECURITY:

* Built with Go 1.27.3, which includes security fixes to `crypto/x509`.

## 0.1.0 (2026-10-19)

Initial release, with support for Varnish CDN.

FEATURES:

* **New resource:** `varnish_cdn_service`, a CDN service with its origins, domains and
  wizard settings.
* **New resource:** `varnish_cdn_certificate`, an uploaded TLS certificate.
* **New resource:** `varnish_cdn_purge_token`, a long-lived token that can only
  invalidate cached content.
* **New resource:** `varnish_org_user`, an organization user, from invite to member.
* **New data source:** `varnish_cdn_domain_dns`, the DNS records a domain needs and
  its verification status.
* **New data source:** `varnish_cdn_wizard_templates`, the available wizard templates.
* **New data source:** `varnish_roles`, the role names for `varnish_org_user`.
* **New list resources** for all four resources, for bulk import with
  `terraform query` (Terraform 1.14 or newer).
* Import by ID, or by identity on Terraform 1.12+ and OpenTofu 1.12+.
* Wizard settings follow wizard version 25. Services on older wizard versions keep
  working, and settings their version does not have stay unmanaged.
