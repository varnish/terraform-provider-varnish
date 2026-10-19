// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/list"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/varnish/terraform-provider-varnish/internal/client"
)

var (
	_ resource.Resource                = (*cdnPurgeTokenResource)(nil)
	_ resource.ResourceWithConfigure   = (*cdnPurgeTokenResource)(nil)
	_ resource.ResourceWithImportState = (*cdnPurgeTokenResource)(nil)
	_ resource.ResourceWithIdentity    = (*cdnPurgeTokenResource)(nil)
	_ list.ListResourceWithConfigure   = (*cdnPurgeTokenResource)(nil)
)

type cdnPurgeTokenResource struct {
	client *client.Client
}

// purgeTokenCreateMu serializes create. The new token is found by diffing the
// token list, which breaks if Terraform creates two tokens in parallel.
var purgeTokenCreateMu sync.Mutex

// NewCDNPurgeTokenResource is the factory for varnish_cdn_purge_token.
func NewCDNPurgeTokenResource() resource.Resource { return &cdnPurgeTokenResource{} }

// NewCDNPurgeTokenListResource is the factory for the varnish_cdn_purge_token list resource.
func NewCDNPurgeTokenListResource() list.ListResource { return &cdnPurgeTokenResource{} }

// cdnPurgeTokenModel is a purge token. The API returns the JWTs only at create,
// so they are kept in state.
type cdnPurgeTokenModel struct {
	ID            types.String `tfsdk:"id"`
	Label         types.String `tfsdk:"label"`
	Expire        types.String `tfsdk:"expire"`
	BasicUsername types.String `tfsdk:"basic_username"`
	BasicSecret   types.String `tfsdk:"basic_secret"`

	OrgID                 types.String `tfsdk:"org_id"`
	ExpiresAt             types.String `tfsdk:"expires_at"`
	AccessToken           types.String `tfsdk:"access_token"`
	RefreshToken          types.String `tfsdk:"refresh_token"`
	AccessTokenExpiresAt  types.String `tfsdk:"access_token_expires_at"`
	RefreshTokenExpiresAt types.String `tfsdk:"refresh_token_expires_at"`
}

func (r *cdnPurgeTokenResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cdn_purge_token"
}

func (r *cdnPurgeTokenResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	// tokens are immutable: inputs force replacement, computed values stay as in state
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}

	resp.Schema = schema.Schema{
		Description: "A purge token: a long-lived credential that may only invalidate cached content. " +
			"The token itself is returned once, when it is created, and is stored in the Terraform state. " +
			"Changing any argument issues a new token and revokes the old one.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Token identifier.",
				PlanModifiers: keep,
			},
			"label": schema.StringAttribute{
				Required:      true,
				Description:   "Name shown for the token in the UI and API.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"expire": schema.StringAttribute{
				Optional: true,
				Description: "Lifetime as a Go duration, for example \"8760h\" for a year. " +
					"Defaults to the organization's access token TTL. " +
					"The API never returns it, so an imported token has no expire in state: setting it in config then " +
					"replaces that token on the next apply.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"basic_username": schema.StringAttribute{
				Optional: true,
				Description: "Username for HTTP basic auth against the purge endpoint, as an alternative to the bearer token. " +
					"Must be set together with basic_secret.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"basic_secret": schema.StringAttribute{
				Optional:      true,
				Sensitive:     true,
				Description:   "Password for basic_username. Must be set together with basic_username.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"org_id": schema.StringAttribute{
				Computed:      true,
				Description:   "Organization the token belongs to.",
				PlanModifiers: keep,
			},
			"expires_at": schema.StringAttribute{
				Computed:    true,
				Description: "When the token stops working, RFC3339.",
			},
			"access_token": schema.StringAttribute{
				Computed:      true,
				Sensitive:     true,
				Description:   "Bearer token used to purge. Null on an imported token: the API never returns it again.",
				PlanModifiers: keep,
			},
			"refresh_token": schema.StringAttribute{
				Computed:      true,
				Sensitive:     true,
				Description:   "Refresh token issued alongside the access token. Null on an imported token.",
				PlanModifiers: keep,
			},
			"access_token_expires_at": schema.StringAttribute{
				Computed:      true,
				Description:   "Expiry of access_token, RFC3339. Null on an imported token.",
				PlanModifiers: keep,
			},
			"refresh_token_expires_at": schema.StringAttribute{
				Computed:      true,
				Description:   "Expiry of refresh_token, RFC3339. Null on an imported token.",
				PlanModifiers: keep,
			},
		},
	}
}

func (r *cdnPurgeTokenResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = idIdentitySchema()
}

func (r *cdnPurgeTokenResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *cdnPurgeTokenResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan cdnPurgeTokenModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	user, secret := plan.BasicUsername.ValueString(), plan.BasicSecret.ValueString()
	if (user == "") != (secret == "") {
		resp.Diagnostics.AddError("Incomplete basic auth credentials",
			"basic_username and basic_secret must be set together, or both left out.")
		return
	}

	purgeTokenCreateMu.Lock()
	defer purgeTokenCreateMu.Unlock()

	// create returns no id, so snapshot the ids to find the new token after
	before, err := r.client.PurgeTokenIDs(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list purge tokens", err.Error())
		return
	}

	label := plan.Label.ValueString()
	tokens, err := r.client.CreatePurgeToken(ctx, client.NewPurgeToken{
		Label:         label,
		Expire:        plan.Expire.ValueString(),
		BasicUsername: user,
		BasicSecret:   secret,
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create purge token", err.Error())
		return
	}

	created, err := r.client.FindNewPurgeToken(ctx, before, label)
	if err != nil {
		// the token exists but has no known id, so it would leak
		resp.Diagnostics.AddError("Failed to identify the created purge token",
			err.Error()+"\n\nThe token was issued. Remove it in the UI before retrying.")
		return
	}

	state := plan
	applyPurgeToken(&state, created)
	applyPurgeTokenSecrets(&state, tokens)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	resp.Diagnostics.Append(setIDIdentity(ctx, resp.Identity, state.ID)...)
}

func (r *cdnPurgeTokenResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state cdnPurgeTokenModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := strconv.Atoi(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid id", err.Error())
		return
	}
	resp.Diagnostics.Append(setIDIdentity(ctx, resp.Identity, state.ID)...)

	token, err := r.readToken(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read purge token", err.Error())
		return
	}
	if token == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	applyPurgeToken(&state, token)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update is never called since every argument forces replacement.
func (r *cdnPurgeTokenResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Purge tokens cannot be updated",
		"A purge token is issued once and is immutable. Changing an argument replaces it; this call should not happen.")
}

func (r *cdnPurgeTokenResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state cdnPurgeTokenModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := strconv.Atoi(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid id", err.Error())
		return
	}

	if err := r.client.DeletePurgeToken(ctx, id); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete purge token", err.Error())
	}
}

// ImportState takes the token id. The JWTs stay null, the API returns them only at create.
func (r *cdnPurgeTokenResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importID(ctx, req, resp)
}

func (r *cdnPurgeTokenResource) ListResourceConfigSchema(_ context.Context, _ list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		Description: "Lists the purge tokens. The token values are returned only at create, so they stay null, as on import.",
	}
}

func (r *cdnPurgeTokenResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	tokens, err := r.client.ListPurgeTokens(ctx)
	if err != nil {
		listError(stream, "Failed to list purge tokens", err.Error())
		return
	}

	stream.Results = listResults(ctx, req, tokens, func(t client.PurgeToken, result *list.ListResult) bool {
		var m cdnPurgeTokenModel
		applyPurgeToken(&m, &t)
		result.DisplayName = t.Label
		result.Diagnostics.Append(setIDIdentity(ctx, result.Identity, m.ID)...)
		if req.IncludeResource && !result.Diagnostics.HasError() {
			result.Diagnostics.Append(result.Resource.Set(ctx, m)...)
		}
		return true
	})
}

// readToken returns the token, or nil when it is gone. Some roles get 403 on the
// single read but may list, so a 403 falls back to the list.
func (r *cdnPurgeTokenResource) readToken(ctx context.Context, id int) (*client.PurgeToken, error) {
	token, err := r.client.GetPurgeToken(ctx, id)
	if err == nil {
		return token, nil
	}
	if client.IsNotFound(err) {
		return nil, nil
	}
	if !client.IsForbidden(err) {
		return nil, err
	}

	tokens, listErr := r.client.ListPurgeTokens(ctx)
	if listErr != nil {
		return nil, fmt.Errorf("%w (and the token list was not readable either: %v)", err, listErr)
	}
	for i := range tokens {
		if tokens[i].ID == id {
			return &tokens[i], nil
		}
	}
	// some roles get 403 on every single read, so the org's list decides: absent means gone
	return nil, nil
}

// applyPurgeToken writes the server-side fields of a stored token into state.
func applyPurgeToken(m *cdnPurgeTokenModel, t *client.PurgeToken) {
	m.ID = types.StringValue(strconv.Itoa(t.ID))
	m.Label = types.StringValue(t.Label)
	m.OrgID = types.StringValue(strconv.Itoa(t.OrgID))
	m.ExpiresAt = timePtrToTF(&t.Expire)
}

// applyPurgeTokenSecrets stores the JWT pair returned by create.
func applyPurgeTokenSecrets(m *cdnPurgeTokenModel, s *client.TokenSet) {
	m.AccessToken = types.StringValue(s.AccessToken)
	m.RefreshToken = types.StringValue(s.RefreshToken)
	m.AccessTokenExpiresAt = unixToTF(s.AccessExpire)
	m.RefreshTokenExpiresAt = unixToTF(s.RefreshExpire)
}

// unixToTF renders a unix timestamp as RFC3339, or null when unset.
func unixToTF(sec int64) types.String {
	if sec == 0 {
		return types.StringNull()
	}
	return types.StringValue(time.Unix(sec, 0).UTC().Format(time.RFC3339))
}
