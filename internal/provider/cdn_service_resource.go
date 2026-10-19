// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/varnish/terraform-provider-varnish/internal/client"
)

var (
	_ resource.Resource                   = (*cdnServiceResource)(nil)
	_ resource.ResourceWithConfigure      = (*cdnServiceResource)(nil)
	_ resource.ResourceWithImportState    = (*cdnServiceResource)(nil)
	_ resource.ResourceWithValidateConfig = (*cdnServiceResource)(nil)
	_ resource.ResourceWithIdentity       = (*cdnServiceResource)(nil)
	_ list.ListResourceWithConfigure      = (*cdnServiceResource)(nil)
)

type cdnServiceResource struct {
	client *client.Client
}

// NewCDNServiceResource is the factory for varnish_cdn_service.
func NewCDNServiceResource() resource.Resource { return &cdnServiceResource{} }

// NewCDNServiceListResource is the factory for the varnish_cdn_service list resource.
func NewCDNServiceListResource() list.ListResource { return &cdnServiceResource{} }

type cdnServiceModel struct {
	ID                     types.String     `tfsdk:"id"`
	Name                   types.String     `tfsdk:"name"`
	WizardAnswerTemplateID types.Int64      `tfsdk:"wizard_answer_template_id"`
	Deployed               types.Bool       `tfsdk:"deployed"`
	OriginSecret           types.String     `tfsdk:"origin_secret"`
	Status                 types.String     `tfsdk:"status"`
	CreatedAt              types.String     `tfsdk:"created_at"`
	UpdatedAt              types.String     `tfsdk:"updated_at"`
	Settings               types.Object     `tfsdk:"settings"`
	Origins                []cdnOriginModel `tfsdk:"origin"`
	Domains                []cdnDomainModel `tfsdk:"domain"`
}

type cdnOriginModel struct {
	Name             types.String `tfsdk:"name"`
	Host             types.String `tfsdk:"host"`
	Port             types.Int64  `tfsdk:"port"`
	EnableTLS        types.Bool   `tfsdk:"enable_tls"`
	HostHeader       types.String `tfsdk:"host_header"`
	IsDefault        types.Bool   `tfsdk:"is_default"`
	AllowInsecureTLS types.Bool   `tfsdk:"allow_insecure_tls"`
}

type cdnDomainModel struct {
	FQDN       types.String `tfsdk:"fqdn"`
	Subdomain  types.String `tfsdk:"subdomain"`
	TLS        types.Bool   `tfsdk:"tls"`
	CertID     types.Int64  `tfsdk:"cert_id"`
	ID         types.String `tfsdk:"id"`
	Status     types.String `tfsdk:"status"`
	TXTRecord  types.String `tfsdk:"txt_record"`
	VerifiedAt types.String `tfsdk:"verified_at"`
	IsCDN      types.Bool   `tfsdk:"is_cdn"`
}

func (r *cdnServiceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cdn_service"
}

func (r *cdnServiceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A CDN service: a set of origins and domains deployed to the CDN.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Server-assigned service identifier.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Computed:    true,
				Description: "Service name (derived server-side from the first domain).",
			},
			"wizard_answer_template_id": schema.Int64Attribute{
				Optional: true,
				Computed: true,
				Description: "Wizard answer template id used at creation time. Defaults to the latest available template when omitted. " +
					"Available ids come from the varnish_cdn_wizard_templates data source (answerTemplates[].id). " +
					"Changing it recreates the service, except when state holds no value yet (services created before the " +
					"API recorded the template id report none): setting it then only adopts it into state.",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplaceIf(cdnRequiresReplaceAnswerTemplate,
						cdnAnswerTemplateReplaceDesc, cdnAnswerTemplateReplaceDesc),
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"deployed": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether the service is deployed. Services auto-deploy on create.",
			},
			"origin_secret": schema.StringAttribute{
				Computed:    true,
				Sensitive:   true,
				Description: "Shared secret the CDN sends to origins.",
			},
			"status": schema.StringAttribute{
				Computed:    true,
				Description: "Service status (deployed/undeployed/unknown).",
			},
			"created_at": schema.StringAttribute{
				Computed:    true,
				Description: "Creation timestamp (RFC3339).",
			},
			"updated_at": schema.StringAttribute{
				Computed:    true,
				Description: "Last update timestamp (RFC3339).",
			},
			"settings": cdnSettingsAttribute(),
		},
		Blocks: map[string]schema.Block{
			"origin": schema.ListNestedBlock{
				Description: "Origin servers for the service. Exactly one should be marked is_default.",
				Validators:  []validator.List{listvalidator.SizeAtLeast(1)},
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Required:    true,
							Description: "Origin name.",
						},
						"host": schema.StringAttribute{
							Required:    true,
							Description: "Origin hostname or IP.",
						},
						"port": schema.Int64Attribute{
							Required:    true,
							Description: "Origin port (e.g. 80 or 443).",
						},
						"enable_tls": schema.BoolAttribute{
							Optional:    true,
							Computed:    true,
							Default:     booldefault.StaticBool(false),
							Description: "Connect to the origin over TLS.",
						},
						"host_header": schema.StringAttribute{
							Optional:    true,
							Computed:    true,
							Default:     stringdefault.StaticString(""),
							Description: "Host header to send to the origin.",
						},
						"is_default": schema.BoolAttribute{
							Optional:    true,
							Computed:    true,
							Default:     booldefault.StaticBool(false),
							Description: "Whether this is the default origin.",
						},
						"allow_insecure_tls": schema.BoolAttribute{
							Optional:    true,
							Computed:    true,
							Default:     booldefault.StaticBool(false),
							Description: "Skip origin TLS certificate verification.",
						},
					},
				},
			},
			"domain": schema.ListNestedBlock{
				Description: "Domains served by this service.",
				Validators:  []validator.List{listvalidator.SizeAtLeast(1)},
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"fqdn": schema.StringAttribute{
							Required:    true,
							Description: "Fully-qualified domain name. Wildcards (e.g. \"*.example.com\") are supported.",
						},
						"subdomain": schema.StringAttribute{
							Optional: true,
							Computed: true,
							Default:  stringdefault.StaticString(""),
							Description: "Required when fqdn is an apex domain such as example.com, and not allowed otherwise. " +
								"The service is served on subdomain.fqdn, e.g. www.example.com, and the apex redirects to it. " +
								"Accounts without apex redirect support reject it. There, set fqdn to the host name to serve instead.",
						},
						"tls": schema.BoolAttribute{
							Optional:    true,
							Computed:    true,
							Default:     booldefault.StaticBool(false),
							Description: "Whether TLS is enabled for the domain.",
						},
						"cert_id": schema.Int64Attribute{
							Optional: true,
							Computed: true,
							Description: "Certificate id attached to the domain. Leave unset to use the CDN's automatic (ACME) certificate. " +
								"Removing cert_id from an existing domain does not detach the certificate, it keeps the one already attached. " +
								"Set cert_id to another certificate instead.",
						},
						"id": schema.StringAttribute{
							Computed:    true,
							Description: "Server-assigned domain identifier.",
						},
						"status": schema.StringAttribute{
							Computed:    true,
							Description: "Domain verification status.",
						},
						"txt_record": schema.StringAttribute{
							Computed:    true,
							Description: "TXT record value for domain verification.",
						},
						"verified_at": schema.StringAttribute{
							Computed:    true,
							Description: "Verification timestamp (RFC3339).",
						},
						"is_cdn": schema.BoolAttribute{
							Computed:    true,
							Description: "Whether this is the generated CDN domain.",
						},
					},
				},
			},
		},
	}
}

const cdnAnswerTemplateReplaceDesc = "Requires replace unless the prior state has no answer template id (post-import adoption)."

// cdnRequiresReplaceAnswerTemplate forces replacement when the answer template
// changes, unless state has none. Imported services may lack it.
func cdnRequiresReplaceAnswerTemplate(_ context.Context, req planmodifier.Int64Request, resp *int64planmodifier.RequiresReplaceIfFuncResponse) {
	resp.RequiresReplace = !req.StateValue.IsNull()
}

func (r *cdnServiceResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = idIdentitySchema()
}

func (r *cdnServiceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// ValidateConfig checks that exactly one origin is the default, as the API requires.
func (r *cdnServiceResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	r.validateDomains(ctx, req, resp)

	// read only the origin block: with unknown variables a dynamic block is an
	// unknown list that []cdnOriginModel cannot hold
	var list types.List
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("origin"), &list)...)
	if resp.Diagnostics.HasError() || list.IsNull() || list.IsUnknown() {
		return
	}
	for _, e := range list.Elements() {
		if e.IsUnknown() {
			return // origin not yet known; let apply-time validation handle it
		}
	}

	var origins []cdnOriginModel
	resp.Diagnostics.Append(list.ElementsAs(ctx, &origins, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	defaults := 0
	for _, o := range origins {
		if o.IsDefault.IsUnknown() {
			return // value not yet known; let apply-time validation handle it
		}
		if o.IsDefault.ValueBool() {
			defaults++
		}
	}
	if len(origins) > 0 && defaults != 1 {
		resp.Diagnostics.AddAttributeError(
			path.Root("origin"),
			"Exactly one default origin required",
			fmt.Sprintf("a service must have exactly one origin with is_default = true; found %d", defaults),
		)
	}
}

func (r *cdnServiceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan cdnServiceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if len(plan.Domains) == 0 {
		resp.Diagnostics.AddError("Invalid service", "at least one domain is required")
		return
	}
	if len(plan.Origins) == 0 {
		resp.Diagnostics.AddError("Invalid service", "at least one origin is required")
		return
	}

	// use the configured answer template, or the latest
	templateID := int(plan.WizardAnswerTemplateID.ValueInt64())
	if plan.WizardAnswerTemplateID.IsNull() || plan.WizardAnswerTemplateID.IsUnknown() {
		latest, err := r.client.LatestAnswerTemplateID(ctx)
		if err != nil {
			resp.Diagnostics.AddError("Failed to resolve wizard answer template", err.Error())
			return
		}
		templateID = latest
	}
	plan.WizardAnswerTemplateID = types.Int64Value(int64(templateID))

	allOrigins := cdnOriginsToAPI(plan.Origins)
	wizard := client.ServiceWizard{
		WizardAnswerTemplateID: templateID,
		Domains:                cdnDomainsToAPI(plan.Domains),
		// create takes one origin, the rest are set after
		Origins: []client.ServiceOrigin{allOrigins[cdnDefaultOriginIndex(allOrigins)]},
	}

	svc, err := r.client.CreateService(ctx, wizard)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create service", err.Error())
		return
	}

	// Terraform drops the resource when Create fails, so delete the service if a
	// later step fails. Otherwise it is orphaned and the domain stays taken.
	rollback := func() {
		if delErr := r.client.DeleteService(ctx, svc.ID); delErr != nil && !client.IsNotFound(delErr) {
			resp.Diagnostics.AddWarning("Rollback failed",
				fmt.Sprintf("could not delete partially-created service %d after a failed create; "+
					"delete it manually to avoid a duplicate-domain conflict: %v", svc.ID, delErr))
		}
	}
	fail := func(summary string, cause error) {
		resp.Diagnostics.AddError(summary, cause.Error())
		rollback()
	}

	if len(allOrigins) > 1 {
		if err := r.client.SetOrigins(ctx, svc.ID, allOrigins); err != nil {
			fail("Failed to set service origins", err)
			return
		}
	}

	settings, sdiags := r.applySettings(ctx, svc.ID, plan.Settings)
	resp.Diagnostics.Append(sdiags...)
	if sdiags.HasError() {
		rollback()
		return
	}

	// the service deploys on create, so undeploy when deployed = false
	if !plan.Deployed.IsNull() && !plan.Deployed.IsUnknown() && !plan.Deployed.ValueBool() {
		if err := r.client.Undeploy(ctx, svc.ID); err != nil {
			fail("Failed to undeploy service", err)
			return
		}
	}

	full, err := r.client.GetService(ctx, svc.ID)
	if err != nil {
		fail("Failed to read created service", err)
		return
	}

	state, diags := cdnServiceAPIToModel(full, plan)
	resp.Diagnostics.Append(diags...)
	settingsObj, sdiags := cdnSettingsFromAPI(ctx, settings)
	resp.Diagnostics.Append(sdiags...)
	if resp.Diagnostics.HasError() {
		rollback()
		return
	}
	state.Settings = settingsObj
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	resp.Diagnostics.Append(setIDIdentity(ctx, resp.Identity, state.ID)...)
}

func (r *cdnServiceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state cdnServiceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := strconv.Atoi(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid service ID", err.Error())
		return
	}
	resp.Diagnostics.Append(setIDIdentity(ctx, resp.Identity, state.ID)...)

	newState, diags := r.read(ctx, id, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if newState == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, *newState)...)
}

// read returns the service with its settings, or nil when it is gone.
func (r *cdnServiceResource) read(ctx context.Context, id int, prior cdnServiceModel) (*cdnServiceModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	svc, err := r.client.GetService(ctx, id)
	if err != nil {
		if !client.IsNotFound(err) {
			diags.AddError("Failed to read service", err.Error())
		}
		return nil, diags
	}

	m, d := cdnServiceAPIToModel(svc, prior)
	diags.Append(d...)
	if diags.HasError() {
		return nil, diags
	}

	// always read settings so an import gets the full configuration
	imported := prior.Settings.IsNull()
	diags.Append(r.refreshSettings(ctx, id, &m, imported)...)
	if diags.HasError() {
		return nil, diags
	}
	return &m, diags
}

// refreshSettings reads the wizard settings into state. A failure is an error on
// import. On refresh it is a warning and prior settings are kept, since destroy
// refreshes first and would otherwise be blocked.
func (r *cdnServiceResource) refreshSettings(ctx context.Context, id int, state *cdnServiceModel, fatal bool) diag.Diagnostics {
	var diags diag.Diagnostics

	report := func(summary, detail string) {
		if fatal {
			diags.AddError(summary, detail+" The service's wizard settings could not be imported.")
			return
		}
		diags.AddWarning(summary, detail+" Keeping the wizard settings recorded in state. "+
			"Run terraform destroy -refresh=false if you need to remove the resource without reading them.")
	}

	settings, err := r.client.GetWizardSettings(ctx, id)
	if err != nil {
		report("Failed to read wizard settings", fmt.Sprintf("service %d: %v.", id, err))
		return diags
	}

	settingsObj, sdiags := cdnSettingsFromAPI(ctx, settings)
	if sdiags.HasError() {
		diags.Append(sdiags.Warnings()...)
		for _, e := range sdiags.Errors() {
			report(e.Summary(), e.Detail())
		}
		return diags
	}

	diags.Append(sdiags...)
	state.Settings = settingsObj
	return diags
}

func (r *cdnServiceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state cdnServiceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := strconv.Atoi(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid service ID", err.Error())
		return
	}

	if cdnOriginsChanged(state.Origins, plan.Origins) {
		if err := r.client.SetOrigins(ctx, id, cdnOriginsToAPI(plan.Origins)); err != nil {
			resp.Diagnostics.AddError("Failed to update origins", err.Error())
			return
		}
	}

	plan2 := cdnDiffDomains(state.Domains, plan.Domains)
	for _, d := range plan2.toAdd {
		if _, err := r.client.AddDomains(ctx, id, []client.Domain{cdnDomainToAPI(d, 0)}); err != nil {
			resp.Diagnostics.AddError("Failed to add domain", err.Error())
			return
		}
	}
	for _, upd := range plan2.toUpdate {
		d := cdnDomainToAPI(upd.plan, upd.id)
		if _, err := r.client.UpdateDomain(ctx, id, d); err != nil {
			resp.Diagnostics.AddError("Failed to update domain", err.Error())
			return
		}
	}
	for _, did := range plan2.toDelete {
		if err := r.client.DeleteDomain(ctx, id, did); err != nil && !client.IsNotFound(err) {
			resp.Diagnostics.AddError("Failed to delete domain", err.Error())
			return
		}
	}

	if !plan.Deployed.IsUnknown() && !plan.Deployed.IsNull() {
		want := plan.Deployed.ValueBool()
		have := state.Deployed.ValueBool()
		if want != have {
			if want {
				err = r.client.Deploy(ctx, id)
			} else {
				err = r.client.Undeploy(ctx, id)
			}
			if err != nil {
				resp.Diagnostics.AddError("Failed to change deploy state", err.Error())
				return
			}
		}
	}

	var settings *client.CDNWizardSettings
	if !plan.Settings.Equal(state.Settings) {
		var sdiags diag.Diagnostics
		settings, sdiags = r.applySettings(ctx, id, plan.Settings)
		resp.Diagnostics.Append(sdiags...)
		if sdiags.HasError() {
			return
		}
	}

	svc, err := r.client.GetService(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read updated service", err.Error())
		return
	}

	newState, diags := cdnServiceAPIToModel(svc, plan)
	resp.Diagnostics.Append(diags...)
	if settings != nil {
		settingsObj, sdiags := cdnSettingsFromAPI(ctx, settings)
		resp.Diagnostics.Append(sdiags...)
		newState.Settings = settingsObj
	}
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, newState)...)
	resp.Diagnostics.Append(setIDIdentity(ctx, resp.Identity, newState.ID)...)
}

func (r *cdnServiceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state cdnServiceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := strconv.Atoi(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid service ID", err.Error())
		return
	}

	if err := r.client.DeleteService(ctx, id); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete service", err.Error())
	}
}

func (r *cdnServiceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importID(ctx, req, resp)
}

func (r *cdnServiceResource) ListResourceConfigSchema(_ context.Context, _ list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		Description: "Lists the CDN services, including their wizard settings when the resource is included.",
	}
}

func (r *cdnServiceResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	svcs, err := r.client.ListServices(ctx)
	if err != nil {
		listError(stream, "Failed to list services", err.Error())
		return
	}

	stream.Results = listResults(ctx, req, svcs, func(svc client.Service, result *list.ListResult) bool {
		result.DisplayName = svc.Name
		result.Diagnostics.Append(setIDIdentity(ctx, result.Identity, types.StringValue(strconv.Itoa(svc.ID)))...)
		if !req.IncludeResource || result.Diagnostics.HasError() {
			return true
		}

		prior := cdnServiceModel{Settings: types.ObjectNull(cdnSettingsObjectType().AttrTypes)}
		m, diags := r.read(ctx, svc.ID, prior)
		result.Diagnostics.Append(diags...)
		if result.Diagnostics.HasError() {
			return true
		}
		if m == nil {
			return false
		}
		result.Diagnostics.Append(result.Resource.Set(ctx, *m)...)
		return true
	})
}

// cdnServiceAPIToModel maps a Service into the model, keeping the prior order of
// origins and domains. Settings are copied from prior.
func cdnServiceAPIToModel(svc *client.Service, prior cdnServiceModel) (cdnServiceModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	m := cdnServiceModel{
		ID:                     types.StringValue(strconv.Itoa(svc.ID)),
		Name:                   types.StringValue(svc.Name),
		WizardAnswerTemplateID: cdnAnswerTemplateID(svc, prior.WizardAnswerTemplateID),
		Deployed:               types.BoolValue(svc.Status == client.ServiceStatusDeployed),
		OriginSecret:           types.StringValue(svc.OriginSecret),
		Status:                 types.StringValue(svc.Status.String()),
		CreatedAt:              types.StringValue(timeToRFC3339(svc.CreatedAt)),
		UpdatedAt:              types.StringValue(timeToRFC3339(svc.UpdatedAt)),
		Settings:               prior.Settings,
		Origins:                cdnOrderedOrigins(prior.Origins, svc.Origins),
		Domains:                cdnOrderedDomains(prior.Domains, svc.Domains),
	}
	return m, diags
}

// cdnAnswerTemplateID returns the service's answer template, or the prior value
// for old services where the API has none.
func cdnAnswerTemplateID(svc *client.Service, prior types.Int64) types.Int64 {
	if svc.WizardAnswerTemplateID != 0 {
		return types.Int64Value(int64(svc.WizardAnswerTemplateID))
	}
	return prior
}

// applySettings merges the configured settings onto the current document and
// writes it. Nothing is written when unchanged, to avoid a redeploy.
func (r *cdnServiceResource) applySettings(ctx context.Context, id int, planSettings types.Object) (*client.CDNWizardSettings, diag.Diagnostics) {
	var diags diag.Diagnostics

	live, err := r.client.GetWizardSettings(ctx, id)
	if err != nil {
		diags.AddError("Failed to read wizard settings", fmt.Sprintf("service %d: %v", id, err))
		return nil, diags
	}

	overlay, d := cdnSettingsOverlay(ctx, planSettings)
	diags.Append(d...)
	if diags.HasError() {
		return nil, diags
	}

	var missing []string
	for k := range overlay.settings {
		if _, ok := live.Settings[k]; !ok {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		diags.AddError("Setting not in wizard version",
			fmt.Sprintf("service %d: its wizard version does not have %s. Upgrade the service's wizard version, "+
				"or remove these settings from the configuration.", id, strings.Join(missing, ", ")))
		return nil, diags
	}

	merged := cdnMergeSettings(*live, overlay)
	if jsonEqual(*live, merged) {
		return live, diags
	}
	if err := r.client.PutWizardSettings(ctx, id, &merged); err != nil {
		diags.AddError("Failed to apply wizard settings", fmt.Sprintf("service %d: %v", id, err))
		return nil, diags
	}

	// the write returns the service, so re-read the settings
	out, err := r.client.GetWizardSettings(ctx, id)
	if err != nil {
		diags.AddError("Failed to read wizard settings", fmt.Sprintf("service %d: %v", id, err))
		return nil, diags
	}
	return out, diags
}
