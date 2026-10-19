// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

// Package provider implements the Varnish Terraform provider on top of the
// Varnish REST API. It is built on terraform-plugin-framework.
package provider

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/varnish/terraform-provider-varnish/internal/client"
)

var (
	_ provider.Provider                  = (*varnishProvider)(nil)
	_ provider.ProviderWithListResources = (*varnishProvider)(nil)
)

type varnishProvider struct {
	version string
}

type varnishProviderModel struct {
	Endpoint    types.String `tfsdk:"endpoint"`
	AccessToken types.String `tfsdk:"access_token"`
	Insecure    types.Bool   `tfsdk:"insecure"`
}

// New returns a provider factory for the given version.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &varnishProvider{version: version}
	}
}

func (p *varnishProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "varnish"
	resp.Version = p.version
}

func (p *varnishProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage Varnish resources via the Varnish REST API. Currently Varnish CDN (varnish_cdn_*) plus the org-level resources.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Optional:    true,
				Description: "Base URL of the API gateway. Defaults to https://api.varnish-cdn.com; may be overridden here or via VARNISH_ENDPOINT.",
			},
			"access_token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Long-lived API access token. May be set via VARNISH_ACCESS_TOKEN.",
			},
			"insecure": schema.BoolAttribute{
				Optional:    true,
				Description: "Disable TLS certificate verification, and allow a plain http endpoint (test stacks only). May be set via VARNISH_INSECURE.",
			},
		},
	}
}

func (p *varnishProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg varnishProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// an unknown value would fall back to the default endpoint and send the token there
	for name, v := range map[string]attr.Value{"endpoint": cfg.Endpoint, "access_token": cfg.AccessToken, "insecure": cfg.Insecure} {
		if v.IsUnknown() {
			resp.Diagnostics.AddAttributeError(path.Root(name), "Unknown provider value",
				fmt.Sprintf("%s is not known until apply. Set it to a value known at plan time, "+
					"or apply the resources it depends on first, e.g. with -target.", name))
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}

	endpoint := cmp.Or(cfg.Endpoint.ValueString(), os.Getenv("VARNISH_ENDPOINT"), client.DefaultEndpoint)
	token := cmp.Or(cfg.AccessToken.ValueString(), os.Getenv("VARNISH_ACCESS_TOKEN"))
	insecure := cfg.Insecure.ValueBool() || os.Getenv("VARNISH_INSECURE") == "true"

	if token == "" {
		resp.Diagnostics.AddAttributeError(path.Root("access_token"), "Missing access token",
			"Set the provider 'access_token' attribute or the VARNISH_ACCESS_TOKEN environment variable.")
	}
	checkEndpoint(endpoint, insecure, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if insecure {
		resp.Diagnostics.AddWarning("TLS certificate verification disabled",
			"insecure = true: the provider accepts any certificate the endpoint presents, so the access token and "+
				"the configuration it carries are exposed to anyone who can intercept the connection. Use it on test stacks only.")
	}

	c := client.New(client.Config{Endpoint: endpoint, AccessToken: token, Insecure: insecure})
	resp.ResourceData = c
	resp.DataSourceData = c
	resp.ListResourceData = c
}

// One resource and data source pair per product, plus org-level.

func (p *varnishProvider) Resources(_ context.Context) []func() resource.Resource {
	return append(cdnResources(), orgResources()...)
}

func (p *varnishProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return append(cdnDataSources(), orgDataSources()...)
}

func (p *varnishProvider) ListResources(_ context.Context) []func() list.ListResource {
	return append(cdnListResources(), orgListResources()...)
}

func cdnResources() []func() resource.Resource {
	return []func() resource.Resource{
		NewCDNCertificateResource,
		NewCDNPurgeTokenResource,
		NewCDNServiceResource,
	}
}

func cdnDataSources() []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewCDNDomainDNSDataSource,
		NewCDNWizardTemplatesDataSource,
	}
}

func cdnListResources() []func() list.ListResource {
	return []func() list.ListResource{
		NewCDNCertificateListResource,
		NewCDNPurgeTokenListResource,
		NewCDNServiceListResource,
	}
}

func orgResources() []func() resource.Resource {
	return []func() resource.Resource{
		NewOrgUserResource,
	}
}

func orgListResources() []func() list.ListResource {
	return []func() list.ListResource{
		NewOrgUserListResource,
	}
}

func orgDataSources() []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewRolesDataSource,
	}
}

// checkEndpoint rejects plain http unless insecure is set, so the token is not sent in clear.
func checkEndpoint(endpoint string, insecure bool, diags *diag.Diagnostics) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		diags.AddAttributeError(path.Root("endpoint"), "Invalid endpoint",
			fmt.Sprintf("%q is not a valid base URL, expected something like https://api.varnish-cdn.com.", endpoint))
		return
	}
	if u.Scheme != "https" && !insecure {
		diags.AddAttributeError(path.Root("endpoint"), "Endpoint is not https",
			fmt.Sprintf("%q uses %q, which would send the access token unencrypted. Use https, or set insecure = true "+
				"(VARNISH_INSECURE) for a test stack.", endpoint, u.Scheme))
	}
}
