// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.ResourceWithModifyPlan = (*cdnServiceResource)(nil)

const (
	cdnStatusAttrName   = "status"
	cdnDeployedAttrName = "deployed"
	cdnDomainAttrName   = "domain"
	cdnSettingsAttrName = "settings"
)

// ModifyPlan keeps computed values an update does not change, so the plan shows only real changes.
func (r *cdnServiceResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}

	var state cdnServiceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	planned, ok := cdnDomainList(ctx, req.Plan, &resp.Diagnostics)
	configured, cok := cdnDomainList(ctx, req.Config, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if ok && cok {
		planned = cdnPlanDomains(configured, planned, state.Domains)
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root(cdnDomainAttrName), planned)...)
	}

	var status types.String
	var deployed types.Bool
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root(cdnStatusAttrName), &status)...)
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root(cdnDeployedAttrName), &deployed)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if status.IsUnknown() && !deployed.IsUnknown() && deployed.Equal(state.Deployed) {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root(cdnStatusAttrName), state.Status)...)
	}
}

// attrGetter is a tfsdk.Plan or tfsdk.Config.
type attrGetter interface {
	GetAttribute(context.Context, path.Path, any) diag.Diagnostics
}

// cdnDomainList reads the domain blocks. It returns false while the list or an element is unknown.
func cdnDomainList(ctx context.Context, data attrGetter, diags *diag.Diagnostics) ([]cdnDomainModel, bool) {
	var list types.List
	diags.Append(data.GetAttribute(ctx, path.Root(cdnDomainAttrName), &list)...)
	if diags.HasError() || list.IsNull() || list.IsUnknown() {
		return nil, false
	}
	for _, e := range list.Elements() {
		if e.IsUnknown() {
			return nil, false
		}
	}

	var out []cdnDomainModel
	diags.Append(list.ElementsAs(ctx, &out, false)...)
	return out, !diags.HasError()
}

// cdnPlanDomains fills unknown computed fields of planned domains from the state
// domain with the same key. New domains stay unknown. config is index aligned with plan.
func cdnPlanDomains(config, plan, state []cdnDomainModel) []cdnDomainModel {
	byKey := make(map[string]cdnDomainModel, len(state))
	for _, s := range state {
		byKey[cdnDomainKey(s.FQDN.ValueString(), s.Subdomain.ValueString())] = s
	}

	out := make([]cdnDomainModel, len(plan))
	for i, p := range plan {
		out[i] = p
		if p.FQDN.IsUnknown() || p.Subdomain.IsUnknown() {
			continue
		}
		s, ok := byKey[cdnDomainKey(p.FQDN.ValueString(), p.Subdomain.ValueString())]
		if !ok {
			continue
		}

		p.ID = knownOr(p.ID, s.ID)
		p.TXTRecord = knownOr(p.TXTRecord, s.TXTRecord)
		p.IsCDN = knownOr(p.IsCDN, s.IsCDN)
		if p.CertID.IsUnknown() && i < len(config) && config[i].CertID.IsNull() {
			p.CertID = s.CertID
		}

		// a tls or certificate change may restart verification
		if !p.TLS.IsUnknown() && !p.CertID.IsUnknown() && !cdnDomainFieldsChanged(s, p) {
			p.Status = knownOr(p.Status, s.Status)
			p.VerifiedAt = knownOr(p.VerifiedAt, s.VerifiedAt)
		}
		out[i] = p
	}
	return out
}

func knownOr[T attr.Value](v, fallback T) T {
	if v.IsUnknown() {
		return fallback
	}
	return v
}

// unsetOr keeps the planned value of an attribute the config leaves unset. A
// configured value takes the API value, so a value the API ignores shows as drift.
func unsetOr[T attr.Value](config, plan, applied T) T {
	if config.IsNull() {
		return knownOr(plan, applied)
	}
	return applied
}

// cdnKeepPlanned writes known planned values of computed attributes into the
// applied state. Terraform rejects an apply result that differs from a known
// planned value, and the API may have changed in between. The next refresh syncs.
func cdnKeepPlanned(config, plan cdnServiceModel, applied *cdnServiceModel) {
	applied.Name = knownOr(plan.Name, applied.Name)
	applied.Status = knownOr(plan.Status, applied.Status)
	applied.OriginSecret = knownOr(plan.OriginSecret, applied.OriginSecret)
	applied.CreatedAt = knownOr(plan.CreatedAt, applied.CreatedAt)
	applied.Deployed = unsetOr(config.Deployed, plan.Deployed, applied.Deployed)

	planByKey := cdnDomainsByKey(plan.Domains)
	configByKey := cdnDomainsByKey(config.Domains)
	for i, d := range applied.Domains {
		k := cdnDomainKey(d.FQDN.ValueString(), d.Subdomain.ValueString())
		p, ok := planByKey[k]
		if !ok {
			continue
		}
		d.ID = knownOr(p.ID, d.ID)
		d.Status = knownOr(p.Status, d.Status)
		d.TXTRecord = knownOr(p.TXTRecord, d.TXTRecord)
		d.VerifiedAt = knownOr(p.VerifiedAt, d.VerifiedAt)
		d.IsCDN = knownOr(p.IsCDN, d.IsCDN)
		if c, ok := configByKey[k]; ok {
			d.CertID = unsetOr(c.CertID, p.CertID, d.CertID)
		}
		applied.Domains[i] = d
	}
}

func cdnDomainsByKey(domains []cdnDomainModel) map[string]cdnDomainModel {
	out := make(map[string]cdnDomainModel, len(domains))
	for _, d := range domains {
		out[cdnDomainKey(d.FQDN.ValueString(), d.Subdomain.ValueString())] = d
	}
	return out
}

// cdnSettingsKeepPlanned returns the applied settings, except where the
// configuration leaves a value unset and the plan knows it. An unset section
// keeps its planned value whole.
func cdnSettingsKeepPlanned(ctx context.Context, config, plan, applied attr.Value, diags *diag.Diagnostics) attr.Value {
	if config == nil || plan == nil || applied == nil {
		return applied
	}
	if config.IsNull() && !plan.IsUnknown() {
		return plan
	}

	c, cok := config.(types.Object)
	p, pok := plan.(types.Object)
	a, aok := applied.(types.Object)
	if !cok || !pok || !aok || c.IsUnknown() || p.IsUnknown() || a.IsNull() || a.IsUnknown() {
		return applied
	}

	attrs := make(map[string]attr.Value, len(a.Attributes()))
	for k, av := range a.Attributes() {
		attrs[k] = cdnSettingsKeepPlanned(ctx, c.Attributes()[k], p.Attributes()[k], av, diags)
	}
	obj, d := types.ObjectValue(a.AttributeTypes(ctx), attrs)
	diags.Append(d...)
	return obj
}

// cdnConfigSettings reads the settings the configuration sets.
func cdnConfigSettings(ctx context.Context, config tfsdk.Config, diags *diag.Diagnostics) types.Object {
	var settings types.Object
	diags.Append(config.GetAttribute(ctx, path.Root(cdnSettingsAttrName), &settings)...)
	return settings
}
