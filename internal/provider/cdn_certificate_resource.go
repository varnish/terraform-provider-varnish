// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/varnish/terraform-provider-varnish/internal/client"
)

var (
	_ resource.Resource                = (*cdnCertificateResource)(nil)
	_ resource.ResourceWithConfigure   = (*cdnCertificateResource)(nil)
	_ resource.ResourceWithImportState = (*cdnCertificateResource)(nil)
	_ resource.ResourceWithIdentity    = (*cdnCertificateResource)(nil)
	_ list.ListResourceWithConfigure   = (*cdnCertificateResource)(nil)
)

type cdnCertificateResource struct {
	client *client.Client
}

// NewCDNCertificateResource is the factory for varnish_cdn_certificate.
func NewCDNCertificateResource() resource.Resource { return &cdnCertificateResource{} }

// NewCDNCertificateListResource is the factory for the varnish_cdn_certificate list resource.
func NewCDNCertificateListResource() list.ListResource { return &cdnCertificateResource{} }

type cdnCertificateModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Certificate  types.String `tfsdk:"certificate"`
	PrivateKey   types.String `tfsdk:"private_key"`
	CipherSuites types.List   `tfsdk:"cipher_suites"`
	MinVersion   types.String `tfsdk:"min_version"`
	MaxVersion   types.String `tfsdk:"max_version"`
	CommonName   types.String `tfsdk:"common_name"`
	SAN          types.List   `tfsdk:"san"`
	NotBefore    types.String `tfsdk:"not_before"`
	NotAfter     types.String `tfsdk:"not_after"`
	Type         types.String `tfsdk:"type"`
}

func (r *cdnCertificateResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cdn_certificate"
}

func (r *cdnCertificateResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A TLS certificate stored in the CDN for use by service domains.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Server-assigned certificate identifier.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Human-readable certificate name.",
			},
			"certificate": schema.StringAttribute{
				Required:    true,
				Description: "PEM-encoded certificate chain.",
			},
			"private_key": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "PEM-encoded private key. Never returned by the API, but stored in Terraform state, so protect the state.",
			},
			"cipher_suites": schema.ListAttribute{
				Optional:      true,
				Computed:      true,
				ElementType:   types.StringType,
				Description:   "Allowed TLS cipher suites. Server defaults apply when omitted.",
				PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
			"min_version": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "Minimum TLS version.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"max_version": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "Maximum TLS version.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"common_name": schema.StringAttribute{
				Computed:    true,
				Description: "Certificate common name (from the certificate).",
			},
			"san": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Subject alternative names (from the certificate).",
			},
			"not_before": schema.StringAttribute{
				Computed:    true,
				Description: "Certificate validity start (RFC3339).",
			},
			"not_after": schema.StringAttribute{
				Computed:    true,
				Description: "Certificate validity end (RFC3339).",
			},
			"type": schema.StringAttribute{
				Computed:    true,
				Description: "Where the certificate is stored: `Database` for uploaded certificates, `ACME` for ones issued by the CDN.",
			},
		},
	}
}

func (r *cdnCertificateResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = idIdentitySchema()
}

func (r *cdnCertificateResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data",
			fmt.Sprintf("expected *client.Client, got %T", req.ProviderData))
		return
	}
	r.client = c
}

func (r *cdnCertificateResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan cdnCertificateModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in, diags := cdnCertModelToAPI(ctx, plan, 0)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.client.CreateCert(ctx, in)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create certificate", err.Error())
		return
	}

	state, diags := cdnCertAPIToModel(ctx, out, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	resp.Diagnostics.Append(setIDIdentity(ctx, resp.Identity, state.ID)...)
}

func (r *cdnCertificateResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state cdnCertificateModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := strconv.Atoi(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid certificate ID", err.Error())
		return
	}
	resp.Diagnostics.Append(setIDIdentity(ctx, resp.Identity, state.ID)...)

	out, err := r.client.GetCert(ctx, id)
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read certificate", err.Error())
		return
	}

	// the API never returns the private key
	newState, diags := cdnCertAPIToModel(ctx, out, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, newState)...)
}

func (r *cdnCertificateResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state cdnCertificateModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := strconv.Atoi(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid certificate ID", err.Error())
		return
	}

	in, diags := cdnCertModelToAPI(ctx, plan, id)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.client.UpdateCert(ctx, in)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update certificate", err.Error())
		return
	}

	newState, diags := cdnCertAPIToModel(ctx, out, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, newState)...)
	resp.Diagnostics.Append(setIDIdentity(ctx, resp.Identity, newState.ID)...)
}

func (r *cdnCertificateResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state cdnCertificateModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := strconv.Atoi(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid certificate ID", err.Error())
		return
	}

	if err := r.client.DeleteCert(ctx, id); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete certificate", err.Error())
	}
}

func (r *cdnCertificateResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importID(ctx, req, resp)
}

func (r *cdnCertificateResource) ListResourceConfigSchema(_ context.Context, _ list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		Description: "Lists the uploaded certificates. ACME certificates are issued by the CDN and are skipped.",
	}
}

func (r *cdnCertificateResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	all, err := r.client.ListCerts(ctx)
	if err != nil {
		listError(stream, "Failed to list certificates", err.Error())
		return
	}

	certs := make([]client.Cert, 0, len(all))
	for _, c := range all {
		if c.Type != client.CertLocationACME {
			certs = append(certs, c)
		}
	}

	stream.Results = listResults(ctx, req, certs, func(c client.Cert, result *list.ListResult) bool {
		id := types.StringValue(strconv.Itoa(c.ID))
		result.DisplayName = c.Name
		result.Diagnostics.Append(setIDIdentity(ctx, result.Identity, id)...)
		if !req.IncludeResource || result.Diagnostics.HasError() {
			return true
		}

		out, err := r.client.GetCert(ctx, c.ID)
		if client.IsNotFound(err) {
			return false
		}
		if err != nil {
			result.Diagnostics.AddError("Failed to read certificate", err.Error())
			return true
		}
		m, diags := cdnCertAPIToModel(ctx, out, cdnCertificateModel{})
		result.Diagnostics.Append(diags...)
		if !result.Diagnostics.HasError() {
			result.Diagnostics.Append(result.Resource.Set(ctx, m)...)
		}
		return true
	})
}

// cdnCertModelToAPI builds a Cert from the model. id 0 means create.
func cdnCertModelToAPI(ctx context.Context, m cdnCertificateModel, id int) (client.Cert, diag.Diagnostics) {
	ciphers, diags := tfListToStrings(ctx, m.CipherSuites)
	return client.Cert{
		ID:           id,
		Name:         m.Name.ValueString(),
		Certificate:  m.Certificate.ValueString(),
		PrivateKey:   m.PrivateKey.ValueString(),
		CipherSuites: ciphers,
		MinVersion:   m.MinVersion.ValueString(),
		MaxVersion:   m.MaxVersion.ValueString(),
	}, diags
}

// cdnCertAPIToModel maps a Cert into the model. The private key and certificate
// body are kept from prior when the API omits them.
func cdnCertAPIToModel(ctx context.Context, c *client.Cert, prior cdnCertificateModel) (cdnCertificateModel, diag.Diagnostics) {
	var diags diag.Diagnostics
	ciphers, d := stringListToTF(ctx, c.CipherSuites)
	diags.Append(d...)
	san, d := stringListToTF(ctx, c.SAN)
	diags.Append(d...)

	key := prior.PrivateKey
	if c.PrivateKey != "" {
		key = types.StringValue(c.PrivateKey)
	}
	cert := prior.Certificate
	if c.Certificate != "" {
		cert = types.StringValue(c.Certificate)
	}

	return cdnCertificateModel{
		ID:           types.StringValue(strconv.Itoa(c.ID)),
		Name:         types.StringValue(c.Name),
		Certificate:  cert,
		PrivateKey:   key,
		CipherSuites: ciphers,
		MinVersion:   types.StringValue(c.MinVersion),
		MaxVersion:   types.StringValue(c.MaxVersion),
		CommonName:   types.StringValue(c.CommonName),
		SAN:          san,
		NotBefore:    timePtrToTF(c.NotBefore),
		NotAfter:     timePtrToTF(c.NotAfter),
		Type:         types.StringValue(c.Type.String()),
	}, diags
}
