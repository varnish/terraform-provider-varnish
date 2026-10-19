// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/varnish/terraform-provider-varnish/internal/client"
)

// cdnSettingsOverlayDoc is the part of a settings document the configuration sets.
type cdnSettingsOverlayDoc struct {
	settings         map[string]any
	overrides        []client.CDNWizardOverride
	overridesManaged bool
}

// cdnNameValueRow is one row of a name/value settings table.
type cdnNameValueRow struct {
	Name  types.String `tfsdk:"name"`
	Value types.String `tfsdk:"value"`
}

type cdnOverrideModel struct {
	Name       types.String `tfsdk:"name"`
	Paths      types.List   `tfsdk:"paths"`
	Extensions types.List   `tfsdk:"extensions"`
	OriginName types.String `tfsdk:"origin_name"`
	Rules      types.Set    `tfsdk:"rules"`
}

type cdnOverrideRuleModel struct {
	StringID types.String         `tfsdk:"string_id"`
	Value    jsontypes.Normalized `tfsdk:"value"`
}

// cdnSettingsOverlay flattens the configured settings into wizard keys. Null and
// unknown leaves are not managed and are skipped.
func cdnSettingsOverlay(ctx context.Context, obj types.Object) (cdnSettingsOverlayDoc, diag.Diagnostics) {
	var diags diag.Diagnostics
	out := cdnSettingsOverlayDoc{settings: map[string]any{}}
	if obj.IsNull() || obj.IsUnknown() {
		return out, diags
	}

	attrs := obj.Attributes()
	for _, f := range cdnSettingsTree {
		cdnOverlayField(ctx, f, attrs[f.name], out.settings, &diags)
	}

	if ov, ok := attrs[cdnOverridesAttrName].(types.List); ok && !ov.IsNull() && !ov.IsUnknown() {
		out.overrides = cdnOverridesToAPI(ctx, ov, &diags)
		out.overridesManaged = true
	}
	return out, diags
}

func cdnOverlayField(ctx context.Context, f cdnSettingsField, v attr.Value, out map[string]any, diags *diag.Diagnostics) {
	if v == nil || v.IsNull() || v.IsUnknown() {
		return
	}

	if f.children != nil {
		obj, ok := v.(types.Object)
		if !ok {
			diags.AddError("Invalid wizard settings", fmt.Sprintf("settings section %q is not an object", f.name))
			return
		}
		attrs := obj.Attributes()
		for _, c := range f.children {
			cdnOverlayField(ctx, c, attrs[c.name], out, diags)
		}
		return
	}

	val, d := cdnLeafToAPI(ctx, f, v)
	diags.Append(d...)
	if !d.HasError() {
		out[f.wire] = val
	}
}

func cdnLeafToAPI(ctx context.Context, f cdnSettingsField, v attr.Value) (any, diag.Diagnostics) {
	var diags diag.Diagnostics
	switch t := v.(type) {
	case types.Bool:
		return t.ValueBool(), diags
	case types.String:
		return t.ValueString(), diags
	case types.Int64:
		return t.ValueInt64(), diags
	case types.Float64:
		return t.ValueFloat64(), diags
	case types.List:
		if f.kind == kindNameValueRows {
			var rows []cdnNameValueRow
			diags.Append(t.ElementsAs(ctx, &rows, false)...)
			out := make([]map[string]any, 0, len(rows))
			for _, r := range rows {
				out = append(out, map[string]any{"name": r.Name.ValueString(), "value": r.Value.ValueString()})
			}
			return out, diags
		}
		return stringsFrom(ctx, t, &diags), diags
	}
	diags.AddError("Invalid wizard settings", fmt.Sprintf("setting %s has unexpected type %T", f.wire, v))
	return nil, diags
}

// stringsFrom returns an empty, not nil, slice for a null or unknown list so documents compare equal.
func stringsFrom(ctx context.Context, l types.List, diags *diag.Diagnostics) []string {
	out := []string{}
	if l.IsNull() || l.IsUnknown() {
		return out
	}
	diags.Append(l.ElementsAs(ctx, &out, false)...)
	if out == nil {
		out = []string{}
	}
	return out
}

func cdnOverridesToAPI(ctx context.Context, l types.List, diags *diag.Diagnostics) []client.CDNWizardOverride {
	var models []cdnOverrideModel
	diags.Append(l.ElementsAs(ctx, &models, false)...)

	out := make([]client.CDNWizardOverride, 0, len(models))
	for _, m := range models {
		var rules []cdnOverrideRuleModel
		if !m.Rules.IsNull() && !m.Rules.IsUnknown() {
			diags.Append(m.Rules.ElementsAs(ctx, &rules, false)...)
		}
		o := client.CDNWizardOverride{
			Name:       m.Name.ValueString(),
			Paths:      stringsFrom(ctx, m.Paths, diags),
			Extensions: stringsFrom(ctx, m.Extensions, diags),
			OriginName: m.OriginName.ValueString(),
			Rules:      make([]client.CDNWizardRule, 0, len(rules)),
		}
		for _, r := range rules {
			o.Rules = append(o.Rules, client.CDNWizardRule{
				StringId: r.StringID.ValueString(),
				Value:    json.RawMessage(r.Value.ValueString()),
			})
		}
		out = append(out, o)
	}
	return out
}

// cdnSettingsFromAPI turns a settings document into a settings object. The keys
// must match the schema exactly, else the wizard version is not supported.
func cdnSettingsFromAPI(ctx context.Context, env *client.CDNWizardSettings) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	objType := cdnSettingsObjectType()
	if env == nil {
		diags.AddError("Missing wizard settings", "the API returned no wizard settings document")
		return types.ObjectNull(objType.AttrTypes), diags
	}

	diags.Append(cdnCheckSettingsKeys(env.Settings)...)
	if diags.HasError() {
		return types.ObjectNull(objType.AttrTypes), diags
	}

	values := make(map[string]attr.Value, len(cdnSettingsTree)+1)
	for _, f := range cdnSettingsTree {
		values[f.name] = cdnFieldFromAPI(ctx, f, env.Settings, &diags)
	}
	values[cdnOverridesAttrName] = cdnOverridesFromAPI(env.Overrides, &diags)
	if diags.HasError() {
		return types.ObjectNull(objType.AttrTypes), diags
	}

	obj, d := types.ObjectValue(objType.AttrTypes, values)
	diags.Append(d...)
	return obj, diags
}

// cdnCheckSettingsKeys compares the document's keys with the schema's leaves.
func cdnCheckSettingsKeys(settings map[string]any) diag.Diagnostics {
	var diags diag.Diagnostics

	known := map[string]bool{}
	for _, w := range cdnSettingsWireKeys() {
		known[w] = true
	}

	var missing, unknown []string
	for w := range known {
		if _, ok := settings[w]; !ok {
			missing = append(missing, w)
		}
	}
	for k := range settings {
		if !known[k] {
			unknown = append(unknown, k)
		}
	}
	// unknown keys come from a newer wizard: writes keep their server value
	if len(unknown) > 0 {
		sort.Strings(unknown)
		diags.AddWarning("Unknown wizard settings",
			fmt.Sprintf("the service has settings this provider does not know: %s. "+
				"They keep their current values and are not managed. Upgrade the provider to manage them.", keyList(unknown)))
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		diags.AddWarning("Older wizard version",
			fmt.Sprintf("the service's wizard version does not have these settings: %s. "+
				"They stay null and cannot be set until the service's wizard version is upgraded.", keyList(missing)))
	}
	return diags
}

// maxListedKeys caps how many key names an error lists.
const maxListedKeys = 20

func keyList(keys []string) string {
	if len(keys) <= maxListedKeys {
		return strings.Join(keys, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(keys[:maxListedKeys], ", "), len(keys)-maxListedKeys)
}

// cdnSettingsWireKeys returns every wizard string id the schema covers.
func cdnSettingsWireKeys() []string {
	var out []string
	var walk func(f cdnSettingsField)
	walk = func(f cdnSettingsField) {
		if f.children == nil {
			out = append(out, f.wire)
			return
		}
		for _, c := range f.children {
			walk(c)
		}
	}
	for _, f := range cdnSettingsTree {
		walk(f)
	}
	return out
}

func cdnFieldFromAPI(ctx context.Context, f cdnSettingsField, settings map[string]any, diags *diag.Diagnostics) attr.Value {
	if f.children != nil {
		values := make(map[string]attr.Value, len(f.children))
		for _, c := range f.children {
			values[c.name] = cdnFieldFromAPI(ctx, c, settings, diags)
		}
		obj, d := types.ObjectValue(f.attrType().(types.ObjectType).AttrTypes, values)
		diags.Append(d...)
		return obj
	}

	raw, ok := settings[f.wire]
	if !ok {
		// an older wizard version: the service has no such setting
		return f.attrType().ValueType(ctx)
	}

	v, err := cdnLeafFromAPI(f, raw)
	if err != nil {
		diags.AddError("Invalid wizard settings", fmt.Sprintf("setting %s: %v", f.wire, err))
		// null of the right type, so the enclosing object still builds
		return f.attrType().ValueType(ctx)
	}
	return v
}

func cdnLeafFromAPI(f cdnSettingsField, raw any) (attr.Value, error) {
	switch f.kind {
	case kindBool:
		b, ok := raw.(bool)
		if !ok {
			return nil, fmt.Errorf("expected a boolean, got %T", raw)
		}
		return types.BoolValue(b), nil
	case kindInt:
		n, ok := raw.(float64)
		if !ok {
			return nil, fmt.Errorf("expected a number, got %T", raw)
		}
		return types.Int64Value(int64(n)), nil
	case kindFloat:
		n, ok := raw.(float64)
		if !ok {
			return nil, fmt.Errorf("expected a number, got %T", raw)
		}
		return types.Float64Value(n), nil
	case kindStringList, kindCsvList:
		items, err := jsonList(raw)
		if err != nil {
			return nil, err
		}
		elems := make([]attr.Value, 0, len(items))
		for _, item := range items {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("expected a list of strings, got %T", raw)
			}
			elems = append(elems, types.StringValue(s))
		}
		l, d := types.ListValue(types.StringType, elems)
		return l, diagsError(d)
	case kindNameValueRows:
		items, err := jsonList(raw)
		if err != nil {
			return nil, err
		}
		elems := make([]attr.Value, 0, len(items))
		for _, item := range items {
			row, ok := item.(map[string]any)
			if !ok || len(row) != len(cdnNameValueType.AttrTypes) {
				return nil, fmt.Errorf("expected rows of {name, value}, got %T", raw)
			}
			name, nameOK := row["name"].(string)
			value, valueOK := row["value"].(string)
			if !nameOK || !valueOK {
				return nil, fmt.Errorf("expected rows of {name, value}, got %T", raw)
			}
			obj, d := types.ObjectValue(cdnNameValueType.AttrTypes, map[string]attr.Value{
				"name":  types.StringValue(name),
				"value": types.StringValue(value),
			})
			if err := diagsError(d); err != nil {
				return nil, err
			}
			elems = append(elems, obj)
		}
		l, d := types.ListValue(cdnNameValueType, elems)
		return l, diagsError(d)
	}

	s, ok := raw.(string)
	if !ok {
		return nil, fmt.Errorf("expected a string, got %T", raw)
	}
	return types.StringValue(s), nil
}

func cdnOverridesFromAPI(overrides []client.CDNWizardOverride, diags *diag.Diagnostics) types.List {
	elems := make([]attr.Value, 0, len(overrides))
	for _, o := range overrides {
		rules := make([]attr.Value, 0, len(o.Rules))
		for _, r := range o.Rules {
			// invalid JSON in state would break every later plan
			if !json.Valid(r.Value) {
				diags.AddError("Invalid wizard settings",
					fmt.Sprintf("override %q rule %s: the API returned a value that is not valid JSON: %s",
						o.Name, r.StringId, string(r.Value)))
				continue
			}
			rule, d := types.ObjectValue(cdnOverrideRuleType.AttrTypes, map[string]attr.Value{
				"string_id": types.StringValue(r.StringId),
				"value":     jsontypes.NewNormalizedValue(string(r.Value)),
			})
			diags.Append(d...)
			rules = append(rules, rule)
		}
		ruleSet, d := types.SetValue(cdnOverrideRuleType, rules)
		diags.Append(d...)

		paths, d := stringListValue(o.Paths)
		diags.Append(d...)
		extensions, d := stringListValue(o.Extensions)
		diags.Append(d...)

		obj, d := types.ObjectValue(cdnOverrideType.AttrTypes, map[string]attr.Value{
			"name":        types.StringValue(o.Name),
			"paths":       paths,
			"extensions":  extensions,
			"origin_name": types.StringValue(o.OriginName),
			"rules":       ruleSet,
		})
		diags.Append(d...)
		elems = append(elems, obj)
	}

	l, d := types.ListValue(cdnOverrideType, elems)
	diags.Append(d...)
	return l
}

func stringListValue(in []string) (types.List, diag.Diagnostics) {
	elems := make([]attr.Value, 0, len(in))
	for _, s := range in {
		elems = append(elems, types.StringValue(s))
	}
	return types.ListValue(types.StringType, elems)
}

// jsonList accepts a JSON array, treating null as empty.
func jsonList(raw any) ([]any, error) {
	if raw == nil {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("expected a list, got %T", raw)
	}
	return items, nil
}

func diagsError(d diag.Diagnostics) error {
	if !d.HasError() {
		return nil
	}
	return errors.New(d.Errors()[0].Detail())
}

// cdnMergeSettings applies the overlay onto a settings document.
func cdnMergeSettings(live client.CDNWizardSettings, o cdnSettingsOverlayDoc) client.CDNWizardSettings {
	merged := live
	merged.Settings = make(map[string]any, len(live.Settings))
	for k, v := range live.Settings {
		merged.Settings[k] = v
	}
	for k, v := range o.settings {
		merged.Settings[k] = v
	}
	if o.overridesManaged {
		merged.Overrides = o.overrides
	}
	return merged
}

// jsonEqual reports whether two values encode to the same JSON.
func jsonEqual(a, b any) bool {
	ma, err1 := json.Marshal(a)
	mb, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return string(ma) == string(mb)
}
