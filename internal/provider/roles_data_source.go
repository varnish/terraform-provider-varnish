// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/varnish/terraform-provider-varnish/internal/client"
)

var (
	_ datasource.DataSource              = (*rolesDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*rolesDataSource)(nil)
)

type rolesDataSource struct {
	client *client.Client
}

// NewRolesDataSource is the factory for varnish_roles.
func NewRolesDataSource() datasource.DataSource { return &rolesDataSource{} }

type rolesModel struct {
	Roles []roleModel `tfsdk:"roles"`
}

type roleModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
}

func (d *rolesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_roles"
}

func (d *rolesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The roles available in the access token's organization. Use this to discover valid role names for varnish_org_user.",
		Attributes: map[string]schema.Attribute{
			"roles": schema.ListNestedAttribute{
				Computed:    true,
				Description: "Roles defined in the organization.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":   schema.StringAttribute{Computed: true, Description: "Role ID."},
						"name": schema.StringAttribute{Computed: true, Description: "Role name."},
					},
				},
			},
		},
	}
}

func (d *rolesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *rolesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg rolesModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	orgID, err := d.client.CurrentOrgID(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to resolve organization", err.Error())
		return
	}

	roles, err := d.client.ListOrgRoles(ctx, orgID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list org roles", err.Error())
		return
	}

	cfg.Roles = make([]roleModel, 0, len(roles))
	for _, role := range roles {
		cfg.Roles = append(cfg.Roles, roleModel{
			ID:   types.StringValue(strconv.Itoa(role.ID)),
			Name: types.StringValue(role.Name),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}
