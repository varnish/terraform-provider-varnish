// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"

	"github.com/varnish/terraform-provider-varnish/internal/client"
)

var (
	_ datasource.DataSource              = (*cdnWizardTemplatesDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*cdnWizardTemplatesDataSource)(nil)
)

type cdnWizardTemplatesDataSource struct {
	client *client.Client
}

// NewCDNWizardTemplatesDataSource is the factory for varnish_cdn_wizard_templates.
func NewCDNWizardTemplatesDataSource() datasource.DataSource { return &cdnWizardTemplatesDataSource{} }

type cdnWizardTemplatesModel struct {
	JSON jsontypes.Normalized `tfsdk:"json"`
}

func (d *cdnWizardTemplatesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cdn_wizard_templates"
}

func (d *cdnWizardTemplatesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The wizard templates available to the organization. Use jsondecode() on the json attribute to pick " +
			"an answer template id for varnish_cdn_service.wizard_answer_template_id.",
		Attributes: map[string]schema.Attribute{
			"json": schema.StringAttribute{
				Computed:   true,
				CustomType: jsontypes.NormalizedType{},
				Description: "Raw JSON array of available wizard templates, shaped " +
					"[{\"id\":<wizard id>,\"name\":\"...\",\"answerTemplates\":[{\"id\":<answer template id>,\"name\":\"...\"}]}]. " +
					"It is answerTemplates[].id (not the wizard id) that varnish_cdn_service.wizard_answer_template_id takes.",
			},
		},
	}
}

func (d *cdnWizardTemplatesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *cdnWizardTemplatesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	out, err := d.client.ListWizardTemplates(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list wizard templates", err.Error())
		return
	}
	state := cdnWizardTemplatesModel{JSON: jsontypes.NewNormalizedValue(string(out))}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
