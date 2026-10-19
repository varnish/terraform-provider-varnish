# terraform-provider-varnish

A Terraform and OpenTofu provider for the Varnish Software SaaS products. Today it covers
Varnish CDN: services, their origins and domains, TLS certificates, and the
wizard (caching/WAF/routing). Product resources carry a product infix, e.g.
`varnish_cdn_*`. Org-level resources are shared across products.

## Terraform and OpenTofu

The same provider binary works with both Terraform (1.0+) and OpenTofu (1.6+), since
both speak plugin protocol 6. The examples below use `terraform`; with OpenTofu, run
`tofu` instead.

The `source = "varnish/varnish"` short form expands to the registry of the CLI in
use: `registry.terraform.io/varnish/varnish` for Terraform, and
`registry.opentofu.org/varnish/varnish` for OpenTofu. Config written with the short
form therefore works unchanged with either tool.

## Resources and data sources

| Name | Kind | Description |
|------|------|-------------|
| `varnish_org_user` | resource | Organization user, invited and then member |
| `varnish_cdn_service` | resource | CDN service with `origin` and `domain` blocks and wizard `settings` |
| `varnish_cdn_certificate` | resource | TLS certificate |
| `varnish_cdn_purge_token` | resource | Long-lived token that can only invalidate cached content |
| `varnish_roles` | data source | Role names for `varnish_org_user` |
| `varnish_cdn_domain_dns` | data source | Expected DNS records and verification status |
| `varnish_cdn_wizard_templates` | data source | Available wizard templates |

Full documentation is in [docs/](docs/).

## Authentication

The provider uses a long-lived API access token, created in the web UI or with
`POST /api/v1/cdn/sessions/api`. It is sent as a bearer token and never refreshed.

Configure via provider attributes or environment variables:

| Attribute      | Env var                | Description |
|----------------|------------------------|-------------|
| `endpoint`     | `VARNISH_ENDPOINT`     | API base URL, default `https://api.varnish-cdn.com` |
| `access_token` | `VARNISH_ACCESS_TOKEN` | API access token |
| `insecure`     | `VARNISH_INSECURE`     | Skip TLS verification and allow http. For test setups only |

## Example

```hcl
terraform {
  required_providers {
    varnish = { source = "varnish/varnish" }
  }
}

provider "varnish" {}

resource "varnish_cdn_service" "web" {
  domain {
    fqdn = "www.example.com"
    tls  = true
  }

  origin {
    name       = "default"
    host       = "origin.example.com"
    port       = 443
    enable_tls = true
    is_default = true
  }
}
```

More in [examples/](examples/).

## Bulk import

Terraform 1.14 or newer can find and import existing objects in bulk. Every resource
type has a list resource with the same name. Write a `.tfquery.hcl` file with
`list` blocks:

```hcl
list "varnish_cdn_service" "all" {
  provider = varnish
}

list "varnish_org_user" "all" {
  provider = varnish
}
```

Then generate config with import blocks for everything found, review it, and apply:

```sh
terraform query -generate-config-out=generated.tf
terraform plan
terraform apply
```

Notes:

- ACME certificates are skipped. The CDN issues them, they are not managed inputs.
- Purge token values cannot be imported. The API never returns them.
- The API never returns a certificate's PEM or private key either. Set
  `certificate` and `private_key` in the generated config, e.g. with `file()`, or
  remove the certificate if you do not want to manage it.
- `varnish_org_user` lists members and pending invites of the token's organization,
  including yourself. Remove your own user from the generated config, or a later
  destroy removes you from the organization.

OpenTofu does not support `list` blocks or `query` yet
([opentofu/opentofu#3787](https://github.com/opentofu/opentofu/issues/3787)).
OpenTofu users import with `import` blocks, by `id`, or by `identity` on OpenTofu 1.12+.

## Development and Local Installation

```sh
make build      # build the provider
make test       # unit tests
make testacc    # acceptance tests, needs VARNISH_ENDPOINT and VARNISH_ACCESS_TOKEN
make docs       # regenerate docs/
make lint       # go vet and gofmt check
make install    # install locally and print a dev_overrides block
```

To use a local build, add the printed block to `~/.terraformrc`, or `~/.tofurc` for
OpenTofu. Then skip `terraform init` and run `plan`/`apply` directly:

```hcl
provider_installation {
  dev_overrides { "varnish/varnish" = "/home/you/go/bin" }
  direct {}
}
```

### Debugging

`terraform-provider-varnish -debug` starts the provider for a debugger such as delve
and prints a `TF_REATTACH_PROVIDERS` value keyed by
`registry.terraform.io/varnish/varnish`. OpenTofu expands `varnish/varnish` to
`registry.opentofu.org/varnish/varnish`, so to attach OpenTofu, either change the
hostname in the printed key before exporting it, or use the fully qualified
`source = "registry.terraform.io/varnish/varnish"` in the test config.

### Acceptance tests

The acceptance tests create real resources. Create an API token with an org admin
access token, then point the provider at the API:

```sh
export VARNISH_ENDPOINT=http://localhost:8002
export VARNISH_INSECURE=true
export VARNISH_ACCESS_TOKEN=$(curl -s -X POST -H "Authorization: Bearer $ORG_TOKEN" \
  "$VARNISH_ENDPOINT/api/v1/cdn/sessions/api" \
  -d '{"Expire":"8760h","Label":"terraform"}' | jq -r .accessToken)

make testacc
```

### Adding a product

- `internal/client/<product>.go` and `<product>_paths.go` for API calls and paths.
- `internal/provider/<product>_*.go` for resources, named `varnish_<product>_<name>`.
- Register them in `provider.go`.
- `examples/resources/varnish_<product>_<name>/`. The directory name must match the
  type name for `make docs`.

Org-level resources (`varnish_org_user`, `varnish_roles`) have no product prefix.

## Releasing

1. Tag the commit with `vX.Y.Z`. The `v` is required.
2. Create and publish a GitHub release for the tag.

Publishing runs `.github/workflows/release.yml`, which builds, signs and uploads the
release artifacts.

The same GitHub release artifacts serve both registries. The Terraform Registry
publishes a new release via its webhook. The OpenTofu Registry needs a one-time
listing: add the provider and the signing key's public half through the
[opentofu/registry](https://github.com/opentofu/registry) repository. After that,
it picks up new releases automatically.

## License

Copyright (c) Varnish Software AB. Licensed under the [Mozilla Public License 2.0](LICENSE).
