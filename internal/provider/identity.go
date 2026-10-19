// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const identityIDAttr = "id"

// idIdentityModel is the identity of a resource known by its numeric id.
type idIdentityModel struct {
	ID types.String `tfsdk:"id"`
}

func idIdentitySchema() identityschema.Schema {
	return identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			identityIDAttr: identityschema.StringAttribute{
				RequiredForImport: true,
				Description:       "Server-assigned identifier, the same value as the id attribute.",
			},
		},
	}
}

func setIDIdentity(ctx context.Context, ident *tfsdk.ResourceIdentity, id types.String) diag.Diagnostics {
	return ident.Set(ctx, idIdentityModel{ID: id})
}

// importID imports by the import ID string or by the id identity attribute.
func importID(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idPath := path.Root(identityIDAttr)
	resource.ImportStatePassthroughWithIdentity(ctx, idPath, idPath, req, resp)
}
