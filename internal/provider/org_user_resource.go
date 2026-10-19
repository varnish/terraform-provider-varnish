// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/varnish/terraform-provider-varnish/internal/client"
)

var (
	_ resource.Resource                = (*orgUserResource)(nil)
	_ resource.ResourceWithConfigure   = (*orgUserResource)(nil)
	_ resource.ResourceWithImportState = (*orgUserResource)(nil)
	_ resource.ResourceWithIdentity    = (*orgUserResource)(nil)
	_ list.ListResourceWithConfigure   = (*orgUserResource)(nil)
)

const (
	statusPending = "pending"
	statusMember  = "member"
)

type orgUserResource struct {
	client *client.Client
}

// NewOrgUserResource is the factory for varnish_org_user.
func NewOrgUserResource() resource.Resource { return &orgUserResource{} }

// NewOrgUserListResource is the factory for the varnish_org_user list resource.
func NewOrgUserListResource() list.ListResource { return &orgUserResource{} }

// orgUserModel is one org user: a pending invite, then the member once accepted.
// The id is "<orgID>:<email>".
type orgUserModel struct {
	ID        types.String `tfsdk:"id"`
	OrgID     types.String `tfsdk:"org_id"`
	Email     types.String `tfsdk:"email"`
	Role      types.String `tfsdk:"role"`
	Status    types.String `tfsdk:"status"`
	AccountID types.String `tfsdk:"account_id"`
}

type orgUserIdentityModel struct {
	OrgID types.String `tfsdk:"org_id"`
	Email types.String `tfsdk:"email"`
}

func (r *orgUserResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_org_user"
}

func (r *orgUserResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An organization user, managed uniformly whether the invite is still pending or already accepted (a member). " +
			"Creating sends an invite; once accepted the same resource represents the member. " +
			"Removing/updating accepted members may require an org-admin-scoped access token.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Composite identifier org_id:email, e.g. 2031304261:alice@example.com. Stable across pending and member states.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"org_id": schema.StringAttribute{
				Computed:      true,
				Description:   "Organization ID the user belongs to: the organization the access token is scoped to.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"email": schema.StringAttribute{
				Required:      true,
				Description:   "Email address of the invited/member user.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"role": schema.StringAttribute{
				Required:    true,
				Description: "Org role name as defined in the organization (e.g. \"Admin\", \"Finance\", \"Developer\"; see the varnish_roles data source). Matched case-insensitively. Updated in place.",
			},
			"status": schema.StringAttribute{
				Computed:    true,
				Description: "Current state: \"pending\" (invite not yet accepted) or \"member\" (accepted).",
			},
			"account_id": schema.StringAttribute{
				Computed:    true,
				Description: "Account ID when the user is an accepted member; null while pending.",
			},
		},
	}
}

func (r *orgUserResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"org_id": identityschema.StringAttribute{
				RequiredForImport: true,
				Description:       "Organization ID the user belongs to.",
			},
			"email": identityschema.StringAttribute{
				RequiredForImport: true,
				Description:       "Email address of the user.",
			},
		},
	}
}

func (r *orgUserResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *orgUserResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan orgUserModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	orgID, err := r.client.CurrentOrgID(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to resolve org_id", err.Error())
		return
	}
	email := plan.Email.ValueString()

	if err := r.client.InviteToOrg(ctx, orgID, email, plan.Role.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to invite org user", err.Error())
		return
	}

	state, err := r.lookup(ctx, orgID, email, plan.Role.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read org user", err.Error())
		return
	}
	if state == nil {
		resp.Diagnostics.AddError("Invite not found after create",
			"the invite was sent but neither a member nor a pending invite was found on read-back")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	resp.Diagnostics.Append(setOrgUserIdentity(ctx, resp.Identity, *state)...)
}

func (r *orgUserResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state orgUserModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	orgID, err := strconv.Atoi(state.OrgID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid org_id", err.Error())
		return
	}
	resp.Diagnostics.Append(setOrgUserIdentity(ctx, resp.Identity, state)...)

	newState, err := r.lookup(ctx, orgID, state.Email.ValueString(), state.Role.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read org user", err.Error())
		return
	}
	if newState == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, newState)...)
}

func (r *orgUserResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state orgUserModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	orgID, err := strconv.Atoi(state.OrgID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid org_id", err.Error())
		return
	}
	email := state.Email.ValueString()
	newRole := plan.Role.ValueString()
	resp.Diagnostics.Append(setOrgUserIdentity(ctx, resp.Identity, state)...)

	switch state.Status.ValueString() {
	case statusMember:
		if err := r.updateMemberRole(ctx, orgID, state, newRole); err != nil {
			resp.Diagnostics.AddError("Failed to update member role", err.Error())
			return
		}
	default: // pending: re-send the invite with the new role
		if err := r.client.InviteToOrg(ctx, orgID, email, newRole); err != nil {
			resp.Diagnostics.AddError("Failed to update pending invite role", err.Error())
			return
		}
	}

	newState, err := r.lookup(ctx, orgID, email, newRole)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read org user", err.Error())
		return
	}
	if newState == nil {
		resp.Diagnostics.AddError("Org user is gone",
			fmt.Sprintf("%s is no longer a member or invited to org %d. Run terraform apply again to invite them.", email, orgID))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, newState)...)
}

func (r *orgUserResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state orgUserModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	orgID, err := strconv.Atoi(state.OrgID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid org_id", err.Error())
		return
	}

	if state.Status.ValueString() == statusMember {
		accountID, err := strconv.Atoi(state.AccountID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Invalid account_id", err.Error())
			return
		}
		if err := r.client.UnassignAccount(ctx, orgID, accountID); err != nil && !client.IsNotFound(err) {
			resp.Diagnostics.AddError("Failed to remove member", err.Error())
		}
		return
	}

	if err := r.client.UninviteFromOrg(ctx, orgID, state.Email.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to revoke invite", err.Error())
	}
}

// ImportState takes "<org_id>:<email>" or the identity.
func (r *orgUserResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	var orgID, email string
	if req.ID != "" {
		var ok bool
		orgID, email, ok = strings.Cut(req.ID, ":")
		if !ok || orgID == "" || email == "" {
			resp.Diagnostics.AddError("Invalid import ID",
				fmt.Sprintf("expected \"<org_id>:<email>\", got %q", req.ID))
			return
		}
	} else {
		var ident orgUserIdentityModel
		resp.Diagnostics.Append(req.Identity.Get(ctx, &ident)...)
		if resp.Diagnostics.HasError() {
			return
		}
		orgID, email = ident.OrgID.ValueString(), ident.Email.ValueString()
		if orgID == "" || email == "" {
			resp.Diagnostics.AddError("Invalid import identity", "both org_id and email must be set")
			return
		}
	}
	if _, err := strconv.Atoi(orgID); err != nil {
		resp.Diagnostics.AddError("Invalid org_id", fmt.Sprintf("org_id must be numeric, got %q", orgID))
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), orgID+":"+email)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("org_id"), orgID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("email"), email)...)
}

func (r *orgUserResource) ListResourceConfigSchema(_ context.Context, _ list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		Description: "Lists the members and pending invites of the access token's organization, one entry per email. A member wins over an invite.",
	}
}

func (r *orgUserResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	orgID, err := r.client.CurrentOrgID(ctx)
	if err != nil {
		listError(stream, "Failed to resolve org_id", err.Error())
		return
	}

	users, err := r.users(ctx, orgID)
	if err != nil {
		listError(stream, "Failed to list org users", err.Error())
		return
	}

	stream.Results = listResults(ctx, req, users, func(u orgUserModel, result *list.ListResult) bool {
		result.DisplayName = u.Email.ValueString()
		result.Diagnostics.Append(setOrgUserIdentity(ctx, result.Identity, u)...)
		if req.IncludeResource && !result.Diagnostics.HasError() {
			result.Diagnostics.Append(result.Resource.Set(ctx, u)...)
		}
		return true
	})
}

// users returns the members, then the invites of emails that are not members.
func (r *orgUserResource) users(ctx context.Context, orgID int) ([]orgUserModel, error) {
	org, err := r.client.GetOrg(ctx, orgID)
	if err != nil {
		return nil, err
	}
	invites, err := r.client.ListOrgInvites(ctx, orgID)
	if err != nil && !client.IsNotFound(err) {
		return nil, err
	}

	seen := make(map[string]bool, len(org.Accounts)+len(invites))
	out := make([]orgUserModel, 0, len(org.Accounts)+len(invites))
	for _, acc := range org.Accounts {
		key := strings.ToLower(acc.Email)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, orgMember(orgID, acc.Email, "", acc))
	}
	for _, inv := range invites {
		key := strings.ToLower(inv.Email)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, orgInvitee(orgID, inv.Email, "", inv))
	}
	return out, nil
}

func setOrgUserIdentity(ctx context.Context, ident *tfsdk.ResourceIdentity, m orgUserModel) diag.Diagnostics {
	return ident.Set(ctx, orgUserIdentityModel{OrgID: m.OrgID, Email: m.Email})
}

// updateMemberRole sets a member's role by name.
func (r *orgUserResource) updateMemberRole(ctx context.Context, orgID int, state orgUserModel, roleName string) error {
	accountID, err := strconv.Atoi(state.AccountID.ValueString())
	if err != nil {
		return fmt.Errorf("invalid account_id: %w", err)
	}
	roleID, err := r.resolveRoleID(ctx, orgID, roleName)
	if err != nil {
		return err
	}

	org, err := r.client.GetOrg(ctx, orgID)
	if err != nil {
		return err
	}
	acct, ok := findAccount(org.Accounts, state.Email.ValueString())
	if !ok {
		return fmt.Errorf("member with email %q no longer present in org %d", state.Email.ValueString(), orgID)
	}
	return r.client.UpdateAccountRole(ctx, accountID, roleID, acct)
}

func (r *orgUserResource) resolveRoleID(ctx context.Context, orgID int, name string) (int, error) {
	roles, err := r.client.ListOrgRoles(ctx, orgID)
	if err != nil {
		return 0, err
	}
	// invites use "admin", roles use "Admin"
	for _, role := range roles {
		if strings.EqualFold(role.Name, name) {
			return role.ID, nil
		}
	}
	return 0, fmt.Errorf("role %q not found in org %d", name, orgID)
}

// lookup returns the user's state, preferring a member over an invite, or nil
// when gone. priorRole is used when the API reports no role name.
func (r *orgUserResource) lookup(ctx context.Context, orgID int, email, priorRole string) (*orgUserModel, error) {
	org, err := r.client.GetOrg(ctx, orgID)
	if err != nil {
		if !client.IsNotFound(err) {
			return nil, err
		}
	} else if acct, ok := findAccount(org.Accounts, email); ok {
		m := orgMember(orgID, email, priorRole, acct)
		return &m, nil
	}

	invites, err := r.client.ListOrgInvites(ctx, orgID)
	if err != nil {
		if client.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	for _, inv := range invites {
		if strings.EqualFold(inv.Email, email) {
			m := orgInvitee(orgID, email, priorRole, inv)
			return &m, nil
		}
	}
	return nil, nil
}

// orgMember maps an accepted member. priorRole is used when the API reports no role name.
func orgMember(orgID int, email, priorRole string, acct client.Account) orgUserModel {
	return orgUserModel{
		ID:        types.StringValue(compositeID(orgID, email)),
		OrgID:     types.StringValue(strconv.Itoa(orgID)),
		Email:     types.StringValue(email),
		Role:      types.StringValue(keepRole(acct.Role.Name, priorRole)),
		Status:    types.StringValue(statusMember),
		AccountID: types.StringValue(strconv.Itoa(acct.ID)),
	}
}

// orgInvitee maps a pending invite. priorRole is used when the API reports no role name.
func orgInvitee(orgID int, email, priorRole string, inv client.OrgInvite) orgUserModel {
	return orgUserModel{
		ID:        types.StringValue(compositeID(orgID, email)),
		OrgID:     types.StringValue(strconv.Itoa(orgID)),
		Email:     types.StringValue(email),
		Role:      types.StringValue(keepRole(inv.Role, priorRole)),
		Status:    types.StringValue(statusPending),
		AccountID: types.StringNull(),
	}
}

// keepRole keeps the configured case ("admin" vs "Admin") to avoid a diff.
func keepRole(role, priorRole string) string {
	if priorRole != "" && (role == "" || strings.EqualFold(role, priorRole)) {
		return priorRole
	}
	return role
}

func findAccount(accounts []client.Account, email string) (client.Account, bool) {
	for _, acc := range accounts {
		if strings.EqualFold(acc.Email, email) {
			return acc, true
		}
	}
	return client.Account{}, false
}

func compositeID(orgID int, email string) string { return strconv.Itoa(orgID) + ":" + email }
