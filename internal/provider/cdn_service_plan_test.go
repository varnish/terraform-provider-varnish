// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/varnish/terraform-provider-varnish/internal/client"
)

const (
	wireWafMode     = "security.waf.mode"
	wireUncacheable = "general.uncacheable"
	verifiedTime    = "2026-01-02T03:04:05Z"
)

// planDom is a planned domain whose computed fields are unknown.
func planDom(fqdn string, tls bool) cdnDomainModel {
	return cdnDomainModel{
		FQDN:       types.StringValue(fqdn),
		Subdomain:  types.StringValue(""),
		TLS:        types.BoolValue(tls),
		CertID:     types.Int64Unknown(),
		ID:         types.StringUnknown(),
		Status:     types.StringUnknown(),
		TXTRecord:  types.StringUnknown(),
		VerifiedAt: types.StringUnknown(),
		IsCDN:      types.BoolUnknown(),
	}
}

// stateDom is a verified domain as read from the API.
func stateDom(fqdn, id string, cert int64) cdnDomainModel {
	d := dom(fqdn, "", id, false, cert)
	d.Status = types.StringValue(client.VerificationStatusVerified.String())
	d.TXTRecord = types.StringValue("txt-" + id)
	d.VerifiedAt = types.StringValue(verifiedTime)
	d.IsCDN = types.BoolValue(false)
	return d
}

// cfgDom is a configured domain without cert_id.
func cfgDom(fqdn string) cdnDomainModel {
	return cdnDomainModel{
		FQDN:       types.StringValue(fqdn),
		Subdomain:  types.StringValue(""),
		TLS:        types.BoolValue(false),
		CertID:     types.Int64Null(),
		ID:         types.StringNull(),
		Status:     types.StringNull(),
		TXTRecord:  types.StringNull(),
		VerifiedAt: types.StringNull(),
		IsCDN:      types.BoolNull(),
	}
}

// Matched domains take state values by key, so a reorder or an insert does not shift them.
func TestPlanDomains_MatchByKey(t *testing.T) {
	state := []cdnDomainModel{stateDom("a.example.com", "1", 5), stateDom("b.example.com", "2", 0)}
	plan := []cdnDomainModel{planDom("b.example.com", false), planDom("new.example.com", false), planDom("a.example.com", false)}
	config := []cdnDomainModel{cfgDom("b.example.com"), cfgDom("new.example.com"), cfgDom("a.example.com")}

	got := cdnPlanDomains(config, plan, state)

	if got[0].ID.ValueString() != "2" || got[2].ID.ValueString() != "1" {
		t.Errorf("ids = %v, %v, want 2 and 1", got[0].ID, got[2].ID)
	}
	if got[2].CertID.ValueInt64() != 5 || got[2].TXTRecord.ValueString() != "txt-1" {
		t.Errorf("a.example.com = %+v, want cert 5 and txt-1", got[2])
	}
	if got[2].Status.ValueString() != "verified" || got[2].VerifiedAt.ValueString() != verifiedTime {
		t.Errorf("a.example.com verification = %v, %v", got[2].Status, got[2].VerifiedAt)
	}
	if got[2].IsCDN.IsUnknown() {
		t.Error("is_cdn of a matched domain is unknown")
	}
	n := got[1]
	if !n.ID.IsUnknown() || !n.Status.IsUnknown() || !n.TXTRecord.IsUnknown() || !n.CertID.IsUnknown() {
		t.Errorf("a new domain must stay unknown: %+v", n)
	}
}

// A tls change may restart verification, so status and verified_at stay unknown.
func TestPlanDomains_TLSChangeKeepsVerificationUnknown(t *testing.T) {
	state := []cdnDomainModel{stateDom("a.example.com", "1", 0)}
	plan := []cdnDomainModel{planDom("a.example.com", true)}
	config := []cdnDomainModel{cfgDom("a.example.com")}

	got := cdnPlanDomains(config, plan, state)[0]
	if got.ID.ValueString() != "1" {
		t.Errorf("id = %v, want 1", got.ID)
	}
	if !got.Status.IsUnknown() || !got.VerifiedAt.IsUnknown() {
		t.Errorf("status %v and verified_at %v must stay unknown", got.Status, got.VerifiedAt)
	}
}

// A configured cert_id change may restart verification too.
func TestPlanDomains_CertChangeKeepsVerificationUnknown(t *testing.T) {
	state := []cdnDomainModel{stateDom("a.example.com", "1", 5)}
	plan := []cdnDomainModel{planDom("a.example.com", false)}
	plan[0].CertID = types.Int64Value(6)
	config := []cdnDomainModel{cfgDom("a.example.com")}
	config[0].CertID = types.Int64Value(6)

	got := cdnPlanDomains(config, plan, state)[0]
	if got.CertID.ValueInt64() != 6 || got.ID.ValueString() != "1" {
		t.Errorf("cert_id %v, id %v; want 6 and 1", got.CertID, got.ID)
	}
	if !got.Status.IsUnknown() || !got.VerifiedAt.IsUnknown() {
		t.Errorf("status %v and verified_at %v must stay unknown", got.Status, got.VerifiedAt)
	}
}

// An unknown configured cert_id comes from another resource and must stay unknown.
func TestPlanDomains_UnknownConfigCertStaysUnknown(t *testing.T) {
	state := []cdnDomainModel{stateDom("a.example.com", "1", 5)}
	plan := []cdnDomainModel{planDom("a.example.com", false)}
	config := []cdnDomainModel{planDom("a.example.com", false)}

	got := cdnPlanDomains(config, plan, state)[0]
	if !got.CertID.IsUnknown() || !got.Status.IsUnknown() {
		t.Errorf("cert_id %v and status %v must stay unknown", got.CertID, got.Status)
	}
	if got.ID.ValueString() != "1" {
		t.Errorf("id = %v, want 1", got.ID)
	}
}

// hclSettings is a settings config as HCL builds it: sections that do not hold wire are null.
func hclSettings(t *testing.T, wire string, v attr.Value) types.Object {
	t.Helper()
	values := make(map[string]attr.Value, len(cdnSettingsTree)+1)
	for _, f := range cdnSettingsTree {
		values[f.name] = sparseField(f, wire, v)
	}
	values[cdnOverridesAttrName] = types.ListNull(cdnOverrideType)
	obj, diags := types.ObjectValue(cdnSettingsObjectType().AttrTypes, values)
	if diags.HasError() {
		t.Fatalf("build settings object: %v", diags.Errors())
	}
	return obj
}

func sparseField(f cdnSettingsField, wire string, v attr.Value) attr.Value {
	if f.children == nil {
		if f.wire == wire {
			return v
		}
		return nullOf(f.attrType())
	}
	values := make(map[string]attr.Value, len(f.children))
	set := false
	for _, c := range f.children {
		values[c.name] = sparseField(c, wire, v)
		set = set || !values[c.name].IsNull()
	}
	if !set {
		return nullOf(f.attrType())
	}
	obj, _ := types.ObjectValue(f.attrType().(types.ObjectType).AttrTypes, values)
	return obj
}

// settingsWith returns the default settings with the given wire values replaced.
func settingsWith(t *testing.T, values map[string]any) types.Object {
	t.Helper()
	env := defaultSettings(t)
	for k, v := range values {
		env.Settings[k] = v
	}
	obj, diags := cdnSettingsFromAPI(context.Background(), env)
	if diags.HasError() {
		t.Fatalf("cdnSettingsFromAPI: %v", diags.Errors())
	}
	return obj
}

// An unset leaf keeps its planned value, a configured leaf takes the applied one.
func TestSettingsKeepPlanned(t *testing.T) {
	ctx := context.Background()
	config := hclSettings(t, wireWafMode, types.StringValue("deny"))
	plan := settingsWith(t, map[string]any{wireWafMode: "deny"})
	applied := settingsWith(t, map[string]any{wireWafMode: "deny", wireUncacheable: true})

	keep := func(config, plan types.Object) attr.Value {
		t.Helper()
		var diags diag.Diagnostics
		got := cdnSettingsKeepPlanned(ctx, config, plan, applied, &diags)
		if diags.HasError() {
			t.Fatalf("cdnSettingsKeepPlanned: %v", diags.Errors())
		}
		return got
	}

	if got := keep(config, plan); !got.Equal(plan) {
		t.Errorf("settings = %v, want the plan", got)
	}

	// a configured leaf keeps the API value, e.g. after normalization
	config = settingsObjectWith(t, wireUncacheable, types.BoolValue(true))
	got, _ := keep(config, plan).(types.Object)
	if v := objectAt(t, got, "general", "uncacheable"); !v.Equal(types.BoolValue(true)) {
		t.Errorf("configured uncacheable = %v, want the applied true", v)
	}

	// no settings in config keeps the whole planned object
	if got := keep(types.ObjectNull(cdnSettingsObjectType().AttrTypes), plan); !got.Equal(plan) {
		t.Error("settings without config must keep the plan")
	}

	// an unknown plan (create) takes the applied value
	if got := keep(config, types.ObjectUnknown(cdnSettingsObjectType().AttrTypes)); !got.Equal(applied) {
		t.Error("an unknown plan must take the applied settings")
	}
}

// liveService serves one service and a settings document that PUT replaces.
func liveService(t *testing.T, svc client.Service, live *client.CDNWizardSettings, puts *int) *client.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case servicesPath, servicesPath + "/7":
			_, _ = w.Write([]byte(mustJSON(t, svc)))
		case servicesPath + "/7/wizard/settings":
			if r.Method == http.MethodPut {
				*puts++
				if err := json.NewDecoder(r.Body).Decode(live); err != nil {
					t.Errorf("decode settings: %v", err)
				}
				return
			}
			_, _ = w.Write([]byte(mustJSON(t, live)))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return client.New(client.Config{Endpoint: srv.URL, AccessToken: "t"})
}

func testService() client.Service {
	return client.Service{
		ID:           7,
		Name:         "www.example.com",
		Status:       client.ServiceStatusDeployed,
		OriginSecret: "secret",
		Origins:      []client.ServiceOrigin{{Name: "o", Host: "origin.example.com", Port: 443, IsDefault: true}},
		Domains:      []client.Domain{{ID: 3, FQDN: "www.example.com", Status: client.VerificationStatusVerified, TXTRecord: "txt"}},
	}
}

// serviceState is the state of testService with the given settings.
func serviceState(settings types.Object) cdnServiceModel {
	svc := testService()
	m, _ := cdnServiceAPIToModel(&svc, cdnServiceModel{
		WizardAnswerTemplateID: types.Int64Value(3),
		Settings:               settings,
	})
	return m
}

// serviceConfigOf is the configuration of testService that sets only the given settings.
func serviceConfigOf(settings types.Object) cdnServiceModel {
	s := serviceState(settings)
	return cdnServiceModel{
		ID:                     types.StringNull(),
		Name:                   types.StringNull(),
		WizardAnswerTemplateID: types.Int64Null(),
		Deployed:               types.BoolNull(),
		OriginSecret:           types.StringNull(),
		Status:                 types.StringNull(),
		CreatedAt:              types.StringNull(),
		UpdatedAt:              types.StringNull(),
		Settings:               settings,
		Origins:                s.Origins,
		Domains:                []cdnDomainModel{cfgDom("www.example.com")},
	}
}

// Update sends only configured settings, so a UI change to an unset leaf
// survives, and state keeps the planned value of that leaf.
func TestUpdate_AppliesConfigNotPlan(t *testing.T) {
	ctx := context.Background()
	live := defaultSettings(t)
	live.Settings[wireWafMode] = "detect"
	live.Settings[wireUncacheable] = true // changed in the UI after the plan
	puts := 0
	r := &cdnServiceResource{client: liveService(t, testService(), live, &puts)}

	state := serviceState(settingsWith(t, map[string]any{wireWafMode: "detect"}))
	plan := state
	plan.Settings = settingsWith(t, map[string]any{wireWafMode: "deny"})
	config := serviceConfigOf(hclSettings(t, wireWafMode, types.StringValue("deny")))

	resp := &resource.UpdateResponse{State: nullState(t, r), Identity: nullIdentity(t, r)}
	r.Update(ctx, resource.UpdateRequest{
		Plan:     planOf(t, r, plan),
		Config:   tfsdk.Config(planOf(t, r, config)),
		State:    tfsdk.State(planOf(t, r, state)),
		Identity: nullIdentity(t, r),
	}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("update: %v", resp.Diagnostics.Errors())
	}

	if puts != 1 || live.Settings[wireWafMode] != "deny" || live.Settings[wireUncacheable] != true {
		t.Errorf("puts = %d, live mode = %v, live uncacheable = %v; want 1, deny, true",
			puts, live.Settings[wireWafMode], live.Settings[wireUncacheable])
	}

	var got cdnServiceModel
	if diags := resp.State.Get(ctx, &got); diags.HasError() {
		t.Fatalf("state: %v", diags.Errors())
	}
	if !got.Settings.Equal(plan.Settings) {
		t.Error("state settings differ from the plan, Terraform would reject the apply")
	}
}

// Create also sends only configured settings, never plan values.
func TestCreate_AppliesConfigNotPlan(t *testing.T) {
	ctx := context.Background()
	live := defaultSettings(t)
	live.Settings[wireUncacheable] = true
	puts := 0
	r := &cdnServiceResource{client: liveService(t, testService(), live, &puts)}

	plan := serviceState(settingsWith(t, map[string]any{wireWafMode: "detect"}))
	config := serviceConfigOf(hclSettings(t, wireWafMode, types.StringValue("detect")))

	resp := &resource.CreateResponse{State: nullState(t, r), Identity: nullIdentity(t, r)}
	r.Create(ctx, resource.CreateRequest{
		Plan:     planOf(t, r, plan),
		Config:   tfsdk.Config(planOf(t, r, config)),
		Identity: nullIdentity(t, r),
	}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("create: %v", resp.Diagnostics.Errors())
	}

	if puts != 1 || live.Settings[wireWafMode] != "detect" || live.Settings[wireUncacheable] != true {
		t.Errorf("puts = %d, live mode = %v, live uncacheable = %v; want 1, detect, true",
			puts, live.Settings[wireWafMode], live.Settings[wireUncacheable])
	}
}

// planService runs the framework's plan for an update of state to config.
func planService(t *testing.T, state, proposed, config cdnServiceModel) cdnServiceModel {
	t.Helper()
	ctx := context.Background()
	r := &cdnServiceResource{}
	typ := nullState(t, r).Schema.Type().TerraformType(ctx)

	dv := func(m cdnServiceModel) *tfprotov6.DynamicValue {
		v, err := tfprotov6.NewDynamicValue(typ, planOf(t, r, m).Raw)
		if err != nil {
			t.Fatalf("dynamic value: %v", err)
		}
		return &v
	}

	server, err := testAccProtoV6ProviderFactories["varnish"]()
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	resp, err := server.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName:         "varnish_cdn_service",
		PriorState:       dv(state),
		ProposedNewState: dv(proposed),
		Config:           dv(config),
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	for _, d := range resp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("plan: %s: %s", d.Summary, d.Detail)
		}
	}
	if len(resp.RequiresReplace) != 0 {
		t.Errorf("requires replace = %v, want none", resp.RequiresReplace)
	}

	raw, err := resp.PlannedState.Unmarshal(typ)
	if err != nil {
		t.Fatalf("unmarshal plan: %v", err)
	}
	var got cdnServiceModel
	planned := tfsdk.State{Schema: nullState(t, r).Schema, Raw: raw}
	if diags := planned.Get(ctx, &got); diags.HasError() {
		t.Fatalf("decode plan: %v", diags.Errors())
	}
	return got
}

// Changing one setting plans only that change: everything else stays known.
func TestPlan_OneSettingChangeKeepsTheRestKnown(t *testing.T) {
	state := serviceState(settingsWith(t, map[string]any{wireWafMode: "detect"}))
	proposed := state
	proposed.Settings = settingsWith(t, map[string]any{wireWafMode: "deny"})
	config := serviceConfigOf(hclSettings(t, wireWafMode, types.StringValue("deny")))

	got := planService(t, state, proposed, config)

	if !got.Settings.Equal(proposed.Settings) {
		t.Errorf("planned settings differ from state plus the change")
	}
	if !got.UpdatedAt.IsUnknown() {
		t.Errorf("updated_at = %v, want unknown", got.UpdatedAt)
	}
	for name, v := range map[string]attr.Value{
		"name": got.Name, "status": got.Status, "deployed": got.Deployed, "origin_secret": got.OriginSecret,
		"created_at": got.CreatedAt, "domain.id": got.Domains[0].ID, "domain.status": got.Domains[0].Status,
		"domain.verified_at": got.Domains[0].VerifiedAt, "domain.cert_id": got.Domains[0].CertID,
	} {
		if v.IsUnknown() {
			t.Errorf("%s is unknown", name)
		}
	}
}

// Adding a domain keeps the name, and undeploying changes status.
func TestPlan_NameAndStatusRules(t *testing.T) {
	settings := settingsWith(t, nil)
	state := serviceState(settings)
	config := serviceConfigOf(types.ObjectNull(cdnSettingsObjectType().AttrTypes))
	config.Deployed = types.BoolValue(false)
	config.Domains = append(config.Domains, cfgDom("new.example.com"))

	proposed := state
	proposed.Deployed = types.BoolValue(false)
	proposed.Domains = append(append([]cdnDomainModel{}, state.Domains...), cfgDom("new.example.com"))

	got := planService(t, state, proposed, config)

	if !got.Name.Equal(state.Name) || !got.Status.IsUnknown() {
		t.Errorf("name %v, status %v; want %v and unknown", got.Name, got.Status, state.Name)
	}
	if got.Domains[0].ID.ValueString() != "3" || !got.Domains[1].ID.IsUnknown() {
		t.Errorf("domain ids = %v, %v; want 3 and unknown", got.Domains[0].ID, got.Domains[1].ID)
	}
	if !got.Settings.Equal(settings) {
		t.Error("unset settings must keep their state values")
	}
}

// Removing a domain keeps the name, even the domain it was set from.
func TestPlan_DomainRemovalKeepsName(t *testing.T) {
	state := serviceState(settingsWith(t, nil))
	state.Domains = append([]cdnDomainModel{stateDom("first.example.com", "1", 0)}, state.Domains...)
	state.Domains[0].CertID = types.Int64Null()
	config := serviceConfigOf(types.ObjectNull(cdnSettingsObjectType().AttrTypes))

	proposed := state
	proposed.Domains = state.Domains[1:]

	got := planService(t, state, proposed, config)

	if !got.Name.Equal(state.Name) {
		t.Errorf("name = %v, want %v", got.Name, state.Name)
	}
	if len(got.Domains) != 1 || got.Domains[0].ID.ValueString() != "3" {
		t.Errorf("domains = %+v, want www.example.com with id 3", got.Domains)
	}
}

// ModifyPlan does nothing on create or destroy.
func TestModifyPlan_CreateDestroy(t *testing.T) {
	ctx := context.Background()
	r := &cdnServiceResource{}
	state := serviceState(settingsWith(t, nil))

	unknownPlan := state
	unknownPlan.Name = types.StringUnknown()
	unknownPlan.Domains = []cdnDomainModel{planDom("www.example.com", false)}

	for name, req := range map[string]resource.ModifyPlanRequest{
		"create":  {State: nullState(t, r), Plan: planOf(t, r, unknownPlan), Config: tfsdk.Config(planOf(t, r, unknownPlan))},
		"destroy": {State: tfsdk.State(planOf(t, r, state)), Plan: tfsdk.Plan(nullState(t, r)), Config: tfsdk.Config(nullState(t, r))},
	} {
		t.Run(name, func(t *testing.T) {
			resp := &resource.ModifyPlanResponse{Plan: req.Plan}
			r.ModifyPlan(ctx, req, resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("ModifyPlan: %v", resp.Diagnostics.Errors())
			}
			if !resp.Plan.Raw.Equal(req.Plan.Raw) {
				t.Error("the plan was changed")
			}
		})
	}
}

// Update writes known planned values over the API ones, which may have moved since the plan.
func TestKeepPlanned(t *testing.T) {
	plan := serviceState(types.ObjectNull(cdnSettingsObjectType().AttrTypes))
	plan.UpdatedAt = types.StringUnknown()
	plan.Domains = append(plan.Domains, planDom("new.example.com", false))

	applied := plan
	applied.Name = types.StringValue("renamed")
	applied.UpdatedAt = types.StringValue(verifiedTime)
	applied.Domains = []cdnDomainModel{stateDom("new.example.com", "9", 0), stateDom("www.example.com", "3", 0)}
	applied.Domains[1].Status = types.StringValue("pending")

	config := serviceConfigOf(types.ObjectNull(cdnSettingsObjectType().AttrTypes))
	config.Domains = append(config.Domains, cfgDom("new.example.com"))
	cdnKeepPlanned(config, plan, &applied)

	if applied.Name.ValueString() != "www.example.com" || applied.UpdatedAt.ValueString() != verifiedTime {
		t.Errorf("name %v, updated_at %v", applied.Name, applied.UpdatedAt)
	}
	if applied.Domains[1].Status.ValueString() != "verified" {
		t.Errorf("www status = %v, want the planned verified", applied.Domains[1].Status)
	}
	if applied.Domains[0].ID.ValueString() != "9" || applied.Domains[0].CertID.ValueInt64() != 0 {
		t.Errorf("new domain id %v, cert_id %v; want the applied 9 and 0", applied.Domains[0].ID, applied.Domains[0].CertID)
	}
}

// A configured value the API did not take must show as drift, so state gets the API value.
func TestKeepPlanned_ConfiguredTakesApplied(t *testing.T) {
	plan := serviceState(types.ObjectNull(cdnSettingsObjectType().AttrTypes))
	plan.Deployed = types.BoolValue(false)
	plan.Domains[0].CertID = types.Int64Value(5)

	config := serviceConfigOf(types.ObjectNull(cdnSettingsObjectType().AttrTypes))
	config.Deployed = plan.Deployed
	config.Domains[0].CertID = plan.Domains[0].CertID

	applied := serviceState(types.ObjectNull(cdnSettingsObjectType().AttrTypes))
	cdnKeepPlanned(config, plan, &applied)

	if !applied.Deployed.ValueBool() || !applied.Domains[0].CertID.IsNull() {
		t.Errorf("deployed %v, cert_id %v; want the applied true and null", applied.Deployed, applied.Domains[0].CertID)
	}

	// unset in config, the planned value is kept
	config.Deployed = types.BoolNull()
	config.Domains[0].CertID = types.Int64Null()
	cdnKeepPlanned(config, plan, &applied)
	if applied.Deployed.ValueBool() || applied.Domains[0].CertID.ValueInt64() != 5 {
		t.Errorf("deployed %v, cert_id %v; want the planned false and 5", applied.Deployed, applied.Domains[0].CertID)
	}
}

// An override attribute removed from config takes its default, not the state value.
func TestPlan_OverrideDefaultsWinOverState(t *testing.T) {
	var diags diag.Diagnostics
	rule := client.CDNWizardRule{StringId: wireUncacheable, Value: json.RawMessage("true")}
	stateOverrides := cdnOverridesFromAPI([]client.CDNWizardOverride{
		{Name: "r", Paths: []string{"/a"}, Extensions: []string{}, Rules: []client.CDNWizardRule{rule}},
	}, &diags)
	if diags.HasError() {
		t.Fatalf("overrides: %v", diags.Errors())
	}

	withOverrides := func(obj types.Object, overrides types.List) types.Object {
		attrs := obj.Attributes()
		attrs[cdnOverridesAttrName] = overrides
		return types.ObjectValueMust(obj.AttributeTypes(context.Background()), attrs)
	}
	ruleObj := stateOverrides.Elements()[0].(types.Object).Attributes()["rules"]
	cfgOverride := types.ObjectValueMust(cdnOverrideType.AttrTypes, map[string]attr.Value{
		"name":        types.StringValue("r"),
		"paths":       types.ListNull(types.StringType),
		"extensions":  types.ListNull(types.StringType),
		"origin_name": types.StringNull(),
		"rules":       ruleObj,
	})
	cfgOverrides := types.ListValueMust(cdnOverrideType, []attr.Value{cfgOverride})

	state := serviceState(withOverrides(settingsWith(t, nil), stateOverrides))
	proposed := state
	proposed.Settings = withOverrides(settingsWith(t, nil), cfgOverrides)
	config := serviceConfigOf(withOverrides(hclSettings(t, "", nil), cfgOverrides))

	got := planService(t, state, proposed, config)

	paths := objectAt(t, got.Settings, cdnOverridesAttrName).(types.List).Elements()[0].(types.Object).Attributes()["paths"]
	if !paths.Equal(emptyStringList) {
		t.Errorf("paths = %v, want the empty default", paths)
	}
}
