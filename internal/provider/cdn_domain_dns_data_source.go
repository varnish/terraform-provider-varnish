// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/varnish/terraform-provider-varnish/internal/client"
)

var (
	_ datasource.DataSource              = (*cdnDomainDNSDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*cdnDomainDNSDataSource)(nil)
)

type cdnDomainDNSDataSource struct {
	client *client.Client
}

// NewCDNDomainDNSDataSource is the factory for varnish_cdn_domain_dns.
func NewCDNDomainDNSDataSource() datasource.DataSource { return &cdnDomainDNSDataSource{} }

type cdnDomainDNSModel struct {
	ServiceID         types.String `tfsdk:"service_id"`
	FQDN              types.String `tfsdk:"fqdn"`
	DomainID          types.String `tfsdk:"domain_id"`
	Subdomain         types.String `tfsdk:"subdomain"`
	IsApex            types.Bool   `tfsdk:"is_apex"`
	ValidCNAME        types.Bool   `tfsdk:"valid_cname"`
	ValidACMECNAME    types.Bool   `tfsdk:"valid_acme_cname"`
	ValidTXT          types.Bool   `tfsdk:"valid_txt"`
	ValidA            types.Bool   `tfsdk:"valid_a"`
	CDNRouted         types.Bool   `tfsdk:"cdn_routed"`
	Verified          types.Bool   `tfsdk:"verified"`
	ARecords          types.List   `tfsdk:"a_records"`
	TXTRecords        types.List   `tfsdk:"txt_records"`
	CNAME             types.String `tfsdk:"cname"`
	ExpectedCNAME     types.String `tfsdk:"expected_cname"`
	ExpectedARecords  types.List   `tfsdk:"expected_a_records"`
	ExpectedTXT       types.String `tfsdk:"expected_txt"`
	ExpectedACMECNAME types.String `tfsdk:"expected_acme_cname"`
}

func (d *cdnDomainDNSDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cdn_domain_dns"
}

func (d *cdnDomainDNSDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Live DNS verification settings for a single service domain.",
		Attributes: map[string]schema.Attribute{
			"service_id": schema.StringAttribute{
				Required:    true,
				Description: "Service identifier.",
			},
			"fqdn": schema.StringAttribute{
				Required:    true,
				Description: "Domain FQDN to look up.",
			},
			"domain_id": schema.StringAttribute{
				Computed:    true,
				Description: "Server-assigned domain identifier.",
			},
			"subdomain": schema.StringAttribute{
				Computed:    true,
				Description: "Subdomain portion, if any.",
			},
			"is_apex": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the domain is an apex domain.",
			},
			"valid_cname": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the CNAME record is valid.",
			},
			"valid_acme_cname": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the ACME CNAME record is valid.",
			},
			"valid_txt": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the TXT record is valid.",
			},
			"valid_a": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the A record is valid.",
			},
			"cdn_routed": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the domain is effectively routed to the CDN.",
			},
			"verified": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the domain is verified.",
			},
			"a_records": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "A records found in DNS.",
			},
			"txt_records": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "TXT records found in DNS.",
			},
			"cname": schema.StringAttribute{
				Computed:    true,
				Description: "CNAME found in DNS.",
			},
			"expected_cname": schema.StringAttribute{
				Computed:    true,
				Description: "Expected CNAME (the generated CDN domain).",
			},
			"expected_a_records": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Expected A/AAAA records.",
			},
			"expected_txt": schema.StringAttribute{
				Computed:    true,
				Description: "Expected TXT record (verification token).",
			},
			"expected_acme_cname": schema.StringAttribute{
				Computed:    true,
				Description: "Expected ACME CNAME.",
			},
		},
	}
}

func (d *cdnDomainDNSDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data",
			fmt.Sprintf("expected *client.Client, got %T", req.ProviderData))
		return
	}
	d.client = c
}

func (d *cdnDomainDNSDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg cdnDomainDNSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := strconv.Atoi(cfg.ServiceID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid service_id", err.Error())
		return
	}

	settings, err := d.client.VerifyDNS(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Failed to verify DNS", err.Error())
		return
	}

	match := cdnFindDNSSettings(settings, cfg.FQDN.ValueString())
	if match == nil {
		resp.Diagnostics.AddError("Domain not found",
			fmt.Sprintf("no DNS settings for fqdn %q on service %d", cfg.FQDN.ValueString(), id))
		return
	}

	state, diags := cdnDNSSettingsToModel(ctx, *match, cfg)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func cdnDNSSettingsToModel(ctx context.Context, s client.DNSSettings, cfg cdnDomainDNSModel) (cdnDomainDNSModel, diag.Diagnostics) {
	var diags diag.Diagnostics
	aRecords, d := stringListToTF(ctx, s.ARecords)
	diags.Append(d...)
	txtRecords, d := stringListToTF(ctx, s.TXTRecords)
	diags.Append(d...)
	expectedA, d := stringListToTF(ctx, s.ExpectedARecords)
	diags.Append(d...)

	return cdnDomainDNSModel{
		ServiceID:         cfg.ServiceID,
		FQDN:              cfg.FQDN,
		DomainID:          types.StringValue(strconv.Itoa(s.DomainID)),
		Subdomain:         types.StringValue(s.Subdomain),
		IsApex:            types.BoolValue(s.IsApex),
		ValidCNAME:        types.BoolValue(s.ValidCNAME),
		ValidACMECNAME:    types.BoolValue(s.Acme.ValidCNAME),
		ValidTXT:          types.BoolValue(s.ValidTXT),
		ValidA:            types.BoolValue(s.ValidA),
		CDNRouted:         types.BoolValue(s.CDNRouted),
		Verified:          types.BoolValue(s.Verified),
		ARecords:          aRecords,
		TXTRecords:        txtRecords,
		CNAME:             types.StringValue(s.CNAME),
		ExpectedCNAME:     types.StringValue(s.ExpectedCNAME),
		ExpectedARecords:  expectedA,
		ExpectedTXT:       types.StringValue(s.ExpectedTXT),
		ExpectedACMECNAME: types.StringValue(s.Acme.ExpectedCNAME),
	}, diags
}

// cdnFindDNSSettings returns the entry matching fqdn.
func cdnFindDNSSettings(settings []client.DNSSettings, fqdn string) *client.DNSSettings {
	for i := range settings {
		s := &settings[i]
		if s.Domain == fqdn {
			return s
		}
		if s.Subdomain != "" && s.Subdomain+"."+s.Domain == fqdn {
			return s
		}
	}
	return nil
}
