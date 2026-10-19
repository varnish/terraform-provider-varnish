// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/varnish/terraform-provider-varnish/internal/client"
)

// wizardDefaultSettings is the settings document of a new service, every field at its default.
const wizardDefaultSettings = `{
  "wizardVersion": 41,
  "settings": {
    "general.allowMethods": ["GET","HEAD","POST","PUT","PATCH","DELETE","CONNECT","OPTIONS","TRACE"],
    "general.uncacheable": false,
    "general.enableCacheableTTLOverride": true,
    "general.cacheableTTLOverride": "10m",
    "general.enableErrorCodeTTLOverride": true,
    "general.errorCodeTTLOverride": "10m",
    "general.uncacheableTTL": "1m",
    "general.firstByteTimeout": "10s",
    "general.websocket": "disable",
    "general.allowBodyWithGet": false,
    "general.urlDecodeReserved": "enabled",
    "general.maxReqBodySize": "10m",
    "general.chunkedBody.action": "deny",
    "general.sharding": false,
    "general.sharding.hashSource": "url",
    "general.backendRetry": false,
    "general.enableUrlRewrite": false,
    "general.urlRewrite": "/",
    "highAvailability.enableStaleIfError": true,
    "highAvailability.staleIfError.keepStaleDuration": "10m",
    "highAvailability.staleIfError.staleRevive.enable": true,
    "highAvailability.staleIfError.staleRevive.ttl": "1m",
    "highAvailability.staleIfError.staleRevive.grace": "10m",
    "queryParams.removeQueryParameters.action": "sort",
    "headers.onAuthHeader.action": "pass",
    "headers.unsetHeadersForDelivery": [],
    "headers.hashHeaders": [],
    "headers.setBackendHeaders": [],
    "headers.setHeadersForDelivery": [],
    "headers.trueClientIp": false,
    "cookies.onCookie.action": "remove",
    "cookies.onCookie.allowList": [],
    "cookies.onSetCookie.action": "pass",
    "cookies.onSetCookie.allowList": [],
    "cookies.onSetCookie.denyList": [],
    "security.httpsRedirect": true,
    "security.hsts": true,
    "security.botVerifySettings": "enabled",
    "security.enableHotlinkProtection": false,
    "security.hotlinkProtection.friends": [],
    "security.asnFilter.enable": false,
    "security.asnFilter.allowVerifiedBots": true,
    "security.asnFilter.action": "404",
    "security.asnFilter.type": "country_deny_list",
    "security.asnFilter.countryDenyList": [],
    "security.asnFilter.countryAllowList": [],
    "security.asnFilter.asnDenyList": [],
    "security.asnFilter.asnAllowList": [],
    "security.botProtection.vpow": false,
    "security.botProtection.mode": "both",
    "security.botProtection.captchaTtl": "1h",
    "security.waf.enable": false,
    "security.waf.mode": "deny",
    "security.waf.owasp": false,
    "security.waf.atomic": false,
    "security.waf.skipRuleId": [],
    "security.waf.bypass.enable": false,
    "security.waf.bypass.acl": [],
    "security.allowList.enable": false,
    "security.allowList.acl": [],
    "security.denyList.enable": false,
    "security.denyList.action": "403",
    "security.denyList.acl": [],
    "security.ratelimit.enable": false,
    "security.ratelimit.clientRate": 2,
    "security.ratelimit.clientCreditLimit": "1m",
    "security.ratelimit.fetchRate": 1,
    "security.ratelimit.fetchCreditLimit": "1m",
    "optimisation.imageopt.webp": true,
    "optimisation.imageopt.quality": 80,
    "optimisation.imageopt.imagetrans.enable": false,
    "optimisation.imageopt.imagetrans.widthParam": "width",
    "optimisation.imageopt.imagetrans.heightParam": "height",
    "optimisation.imageopt.imagetrans.allowedDimensions": [],
    "optimisation.imageopt.imagetrans.allowedWidths": [],
    "optimisation.imageopt.imagetrans.allowedHeights": []
  },
  "overrides": []
}`

func defaultSettings(t *testing.T) *client.CDNWizardSettings {
	t.Helper()
	var env client.CDNWizardSettings
	if err := json.Unmarshal([]byte(wizardDefaultSettings), &env); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return &env
}

// nullOf returns the null value of a type.
func nullOf(typ attr.Type) attr.Value { return typ.ValueType(context.Background()) }

// settingsObjectWith builds a settings object with only one leaf set.
func settingsObjectWith(t *testing.T, wire string, v attr.Value) types.Object {
	t.Helper()
	values := make(map[string]attr.Value, len(cdnSettingsTree)+1)
	for _, f := range cdnSettingsTree {
		values[f.name] = fieldWith(f, wire, v)
	}
	values[cdnOverridesAttrName] = types.ListNull(cdnOverrideType)
	obj, diags := types.ObjectValue(cdnSettingsObjectType().AttrTypes, values)
	if diags.HasError() {
		t.Fatalf("build settings object: %v", diags.Errors())
	}
	return obj
}

func fieldWith(f cdnSettingsField, wire string, v attr.Value) attr.Value {
	if f.children == nil {
		if f.wire == wire {
			return v
		}
		return nullOf(f.attrType())
	}
	values := make(map[string]attr.Value, len(f.children))
	for _, c := range f.children {
		values[c.name] = fieldWith(c, wire, v)
	}
	obj, _ := types.ObjectValue(f.attrType().(types.ObjectType).AttrTypes, values)
	return obj
}

// objectAt walks a settings object down an HCL path.
func objectAt(t *testing.T, obj types.Object, path ...string) attr.Value {
	t.Helper()
	var cur attr.Value = obj
	for _, name := range path {
		o, ok := cur.(types.Object)
		if !ok {
			t.Fatalf("%v is not an object at %q", path, name)
		}
		cur = o.Attributes()[name]
		if cur == nil {
			t.Fatalf("attribute %q not found in %v", name, path)
		}
	}
	return cur
}

// A settings document must round-trip through the typed attribute unchanged.
func TestSettingsRoundTrip(t *testing.T) {
	ctx := context.Background()
	env := defaultSettings(t)

	obj, diags := cdnSettingsFromAPI(ctx, env)
	if diags.HasError() {
		t.Fatalf("cdnSettingsFromAPI: %v", diags.Errors())
	}
	if obj.IsNull() || obj.IsUnknown() {
		t.Fatal("settings object must be fully known")
	}

	overlay, diags := cdnSettingsOverlay(ctx, obj)
	if diags.HasError() {
		t.Fatalf("cdnSettingsOverlay: %v", diags.Errors())
	}
	if len(overlay.settings) != len(env.Settings) {
		t.Fatalf("round-trip produced %d settings, want %d", len(overlay.settings), len(env.Settings))
	}
	for k, want := range env.Settings {
		got, ok := overlay.settings[k]
		if !ok {
			t.Errorf("setting %s lost in round-trip", k)
			continue
		}
		if !jsonEqual(got, want) {
			t.Errorf("setting %s = %v, want %v", k, got, want)
		}
	}
	if !overlay.overridesManaged || len(overlay.overrides) != 0 {
		t.Errorf("overrides = %v (managed %v), want an empty managed list", overlay.overrides, overlay.overridesManaged)
	}

	// merging the document onto itself must be a no-op, or every apply redeploys
	if !jsonEqual(*env, cdnMergeSettings(*env, overlay)) {
		t.Error("merging a round-tripped document changed it")
	}
}

func TestSettingsFromAPI_EmptyListsStayEmpty(t *testing.T) {
	ctx := context.Background()
	obj, diags := cdnSettingsFromAPI(ctx, defaultSettings(t))
	if diags.HasError() {
		t.Fatalf("cdnSettingsFromAPI: %v", diags.Errors())
	}

	acl := objectAt(t, obj, "security", "allow_list", "acl").(types.List)
	if acl.IsNull() || acl.IsUnknown() || len(acl.Elements()) != 0 {
		t.Fatalf("acl = %v, want an empty list", acl)
	}

	overlay, diags := cdnSettingsOverlay(ctx, obj)
	if diags.HasError() {
		t.Fatalf("cdnSettingsOverlay: %v", diags.Errors())
	}
	if !jsonEqual(overlay.settings["security.allowList.acl"], []string{}) {
		t.Errorf("acl encoded as %v, want []", overlay.settings["security.allowList.acl"])
	}
}

func TestSettingsOverlay_SkipsUnmanagedLeaves(t *testing.T) {
	ctx := context.Background()

	obj := settingsObjectWith(t, "general.uncacheable", types.BoolValue(true))
	overlay, diags := cdnSettingsOverlay(ctx, obj)
	if diags.HasError() {
		t.Fatalf("cdnSettingsOverlay: %v", diags.Errors())
	}
	if len(overlay.settings) != 1 || overlay.settings["general.uncacheable"] != true {
		t.Errorf("overlay = %v, want only general.uncacheable", overlay.settings)
	}
	if overlay.overridesManaged {
		t.Error("a null overrides list must not be managed")
	}

	// unknown leaves are skipped like null ones
	obj = settingsObjectWith(t, "general.uncacheable", types.BoolUnknown())
	overlay, diags = cdnSettingsOverlay(ctx, obj)
	if diags.HasError() {
		t.Fatalf("cdnSettingsOverlay unknown: %v", diags.Errors())
	}
	if len(overlay.settings) != 0 {
		t.Errorf("overlay = %v, want no settings", overlay.settings)
	}

	// A null or unknown settings object manages nothing at all.
	for _, o := range []types.Object{types.ObjectNull(cdnSettingsObjectType().AttrTypes), types.ObjectUnknown(cdnSettingsObjectType().AttrTypes)} {
		overlay, diags = cdnSettingsOverlay(ctx, o)
		if diags.HasError() {
			t.Fatalf("cdnSettingsOverlay: %v", diags.Errors())
		}
		if len(overlay.settings) != 0 || overlay.overridesManaged {
			t.Errorf("overlay = %+v, want empty", overlay)
		}
	}
}

// HCL names are derived from string ids, including acronyms and the sharding gate.
func TestSettingsNameMapping(t *testing.T) {
	paths := map[string][]string{
		"general.cacheableTTLOverride":                    {"general", "cacheable_ttl_override"},
		"general.uncacheableTTL":                          {"general", "uncacheable_ttl"},
		"general.sharding":                                {"general", "sharding", "enable"},
		"general.sharding.hashSource":                     {"general", "sharding", "hash_source"},
		"headers.trueClientIp":                            {"headers", "true_client_ip"},
		"security.waf.skipRuleId":                         {"security", "waf", "skip_rule_id"},
		"highAvailability.staleIfError.staleRevive.ttl":   {"high_availability", "stale_if_error", "stale_revive", "ttl"},
		"optimisation.imageopt.imagetrans.allowedWidths":  {"optimisation", "imageopt", "imagetrans", "allowed_widths"},
		"queryParams.removeQueryParameters.action":        {"query_params", "remove_query_parameters", "action"},
		"security.asnFilter.countryDenyList":              {"security", "asn_filter", "country_deny_list"},
		"optimisation.imageopt.imagetrans.allowedHeights": {"optimisation", "imageopt", "imagetrans", "allowed_heights"},
	}
	for wire, path := range paths {
		if got := hclPath(t, wire); !slices.Equal(got, path) {
			t.Errorf("%s maps to %v, want %v", wire, got, path)
		}
	}
}

// hclPath returns the schema path of a string id, failing when it is absent.
func hclPath(t *testing.T, wire string) []string {
	t.Helper()
	var walk func(prefix []string, f cdnSettingsField) []string
	walk = func(prefix []string, f cdnSettingsField) []string {
		path := append(append([]string{}, prefix...), f.name)
		if f.children == nil {
			if f.wire == wire {
				return path
			}
			return nil
		}
		for _, c := range f.children {
			if got := walk(path, c); got != nil {
				return got
			}
		}
		return nil
	}
	for _, f := range cdnSettingsTree {
		if got := walk(nil, f); got != nil {
			return got
		}
	}
	t.Fatalf("string id %s is not in the schema", wire)
	return nil
}

// Keys from an older or newer wizard version only warn.
func TestSettingsFromAPI_KeySkew(t *testing.T) {
	ctx := context.Background()

	env := defaultSettings(t)
	delete(env.Settings, "general.uncacheable")

	obj, diags := cdnSettingsFromAPI(ctx, env)
	if diags.HasError() || obj.IsNull() {
		t.Fatalf("missing key: want a settings object and no error, got %v", diags)
	}
	if len(diags.Warnings()) != 1 || !strings.Contains(diags.Warnings()[0].Detail(), "general.uncacheable") {
		t.Fatalf("missing key: want one warning naming it, got %v", diags)
	}
	general := obj.Attributes()["general"].(types.Object)
	if !general.Attributes()["uncacheable"].IsNull() {
		t.Error("missing key: the leaf must be null")
	}

	env = defaultSettings(t)
	env.Settings["general.newSetting"] = true

	obj, diags = cdnSettingsFromAPI(ctx, env)
	if diags.HasError() || obj.IsNull() {
		t.Fatalf("unknown key: want a settings object and no error, got %v", diags)
	}
	if len(diags.Warnings()) != 1 || !strings.Contains(diags.Warnings()[0].Detail(), "general.newSetting") {
		t.Fatalf("unknown key: want one warning naming it, got %v", diags)
	}

	merged := cdnMergeSettings(*env, cdnSettingsOverlayDoc{settings: map[string]any{"general.uncacheable": true}})
	if merged.Settings["general.newSetting"] != true {
		t.Error("a write must keep the unknown key's server value")
	}
}

// Name/value rows are empty in the defaults fixture, so test them separately.
func TestSettingsNameValueRowsRoundTrip(t *testing.T) {
	ctx := context.Background()

	env := defaultSettings(t)
	rows := []any{
		map[string]any{"name": "X-Forwarded-Host", "value": "example.com"},
		map[string]any{"name": "X-Debug", "value": ""},
	}
	env.Settings["headers.setBackendHeaders"] = rows

	obj, diags := cdnSettingsFromAPI(ctx, env)
	if diags.HasError() {
		t.Fatalf("cdnSettingsFromAPI: %v", diags.Errors())
	}

	got := objectAt(t, obj, "headers", "set_backend_headers").(types.List)
	if len(got.Elements()) != len(rows) {
		t.Fatalf("set_backend_headers = %v, want %d rows", got, len(rows))
	}

	overlay, diags := cdnSettingsOverlay(ctx, obj)
	if diags.HasError() {
		t.Fatalf("cdnSettingsOverlay: %v", diags.Errors())
	}
	if !jsonEqual(overlay.settings["headers.setBackendHeaders"], rows) {
		t.Errorf("rows round-trip = %v, want %v", overlay.settings["headers.setBackendHeaders"], rows)
	}
}

// A row the schema cannot represent must be named, not silently dropped.
func TestSettingsFromAPI_MalformedRows(t *testing.T) {
	ctx := context.Background()

	for name, row := range map[string]any{
		"extra key":     map[string]any{"name": "X-A", "value": "1", "bogus": "2"},
		"missing key":   map[string]any{"name": "X-A"},
		"wrong type":    map[string]any{"name": "X-A", "value": 1},
		"not an object": "X-A",
	} {
		t.Run(name, func(t *testing.T) {
			env := defaultSettings(t)
			env.Settings["headers.setBackendHeaders"] = []any{row}

			_, diags := cdnSettingsFromAPI(ctx, env)
			if !diags.HasError() {
				t.Fatal("expected an error for a malformed row")
			}
			if detail := diags.Errors()[0].Detail(); !strings.Contains(detail, "headers.setBackendHeaders") {
				t.Errorf("error does not name the setting: %s", detail)
			}
		})
	}
}

// A comma in a csv-backed list entry must fail at plan time.
func TestSettingsCsvListRejectsComma(t *testing.T) {
	ctx := context.Background()

	req := validator.ListRequest{
		Path:        path.Root("settings").AtName("security").AtName("allow_list").AtName("acl"),
		ConfigValue: types.ListValueMust(types.StringType, []attr.Value{types.StringValue("10.0.0.0/8,10.1.0.0/16")}),
	}
	acl := leafAttribute(t, "security.allowList.acl").(schema.ListAttribute)
	if len(acl.Validators) == 0 {
		t.Fatal("security.allowList.acl has no validators")
	}

	resp := &validator.ListResponse{}
	for _, v := range acl.Validators {
		v.ValidateList(ctx, req, resp)
	}
	if !resp.Diagnostics.HasError() {
		t.Error("a comma in a csv-backed list entry must be rejected")
	}

	// an entry without a comma stays valid
	req.ConfigValue = types.ListValueMust(types.StringType, []attr.Value{types.StringValue("10.0.0.0/8")})
	resp = &validator.ListResponse{}
	for _, v := range acl.Validators {
		v.ValidateList(ctx, req, resp)
	}
	if resp.Diagnostics.HasError() {
		t.Errorf("a comma free entry was rejected: %v", resp.Diagnostics.Errors())
	}

	// array-backed lists take arbitrary entries, so they carry no such validator
	if hashHeaders := leafAttribute(t, "headers.hashHeaders").(schema.ListAttribute); len(hashHeaders.Validators) != 0 {
		t.Error("headers.hashHeaders is array-backed and must not reject commas")
	}
}

// leafAttribute builds the schema attribute of one wizard string id.
func leafAttribute(t *testing.T, wire string) schema.Attribute {
	t.Helper()
	var walk func(f cdnSettingsField) schema.Attribute
	walk = func(f cdnSettingsField) schema.Attribute {
		if f.children == nil {
			if f.wire == wire {
				return f.attribute()
			}
			return nil
		}
		for _, c := range f.children {
			if got := walk(c); got != nil {
				return got
			}
		}
		return nil
	}
	for _, f := range cdnSettingsTree {
		if got := walk(f); got != nil {
			return got
		}
	}
	t.Fatalf("string id %s is not in the schema", wire)
	return nil
}

func TestSettingsOverridesRoundTrip(t *testing.T) {
	ctx := context.Background()

	env := defaultSettings(t)
	env.Overrides = []client.CDNWizardOverride{{
		Name:       "images",
		Paths:      []string{},
		Extensions: []string{"jpg", "png"},
		OriginName: "default",
		Rules: []client.CDNWizardRule{
			{StringId: "general.uncacheable", Value: json.RawMessage(`true`)},
			{StringId: "general.cacheableTTLOverride", Value: json.RawMessage(`"1h"`)},
			{StringId: "general.allowMethods", Value: json.RawMessage(`["GET"]`)},
		},
	}}

	obj, diags := cdnSettingsFromAPI(ctx, env)
	if diags.HasError() {
		t.Fatalf("cdnSettingsFromAPI: %v", diags.Errors())
	}
	overlay, diags := cdnSettingsOverlay(ctx, obj)
	if diags.HasError() {
		t.Fatalf("cdnSettingsOverlay: %v", diags.Errors())
	}
	if !jsonEqual(overlay.overrides, env.Overrides) {
		t.Errorf("overrides round-trip = %v, want %v", overlay.overrides, env.Overrides)
	}
}

// Override rules read back in another order must give the same state.
func TestSettingsOverrideRuleOrderIrrelevant(t *testing.T) {
	ctx := context.Background()

	rules := []client.CDNWizardRule{
		{StringId: "general.cacheableTTLOverride", Value: json.RawMessage(`"1h"`)},
		{StringId: "general.uncacheable", Value: json.RawMessage(`false`)},
		{StringId: "general.allowMethods", Value: json.RawMessage(`["GET"]`)},
	}
	reversed := slices.Clone(rules)
	slices.Reverse(reversed)

	written := defaultSettings(t)
	written.Overrides = []client.CDNWizardOverride{{Name: "images", Paths: []string{}, Extensions: []string{}, Rules: rules}}
	readBack := defaultSettings(t)
	readBack.Overrides = []client.CDNWizardOverride{{Name: "images", Paths: []string{}, Extensions: []string{}, Rules: reversed}}

	planned, diags := cdnSettingsFromAPI(ctx, written)
	if diags.HasError() {
		t.Fatalf("cdnSettingsFromAPI: %v", diags.Errors())
	}
	applied, diags := cdnSettingsFromAPI(ctx, readBack)
	if diags.HasError() {
		t.Fatalf("cdnSettingsFromAPI reversed: %v", diags.Errors())
	}
	if !planned.Equal(applied) {
		t.Errorf("rule order changed the state:\n%v\n%v", planned, applied)
	}
}

// wizardJSONPath is the wizard definition the schema mirrors. See testdata/README.md.
const wizardJSONPath = "testdata/wizard.json"

// The schema must describe exactly the fields of the wizard definition.
func TestSettingsSchemaCoversWizardVersion(t *testing.T) {
	raw, err := os.ReadFile(wizardJSONPath)
	if err != nil {
		t.Fatalf("read wizard version: %v", err)
	}

	var doc struct {
		WizardVersion struct {
			Steps []struct {
				Fields []wizardJSONField `json:"fields"`
			} `json:"steps"`
		} `json:"wizardVersion"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", wizardJSONPath, err)
	}

	var wizardIDs []string
	wizardTypes := map[string]string{}
	wizardOptions := map[string][]string{}
	var collect func(f wizardJSONField)
	collect = func(f wizardJSONField) {
		wizardIDs = append(wizardIDs, f.StringID)
		wizardTypes[f.StringID] = f.Type
		if f.Type == "select" {
			opts := make([]string, len(f.ValueOptions))
			for i, o := range f.ValueOptions {
				opts[i] = o.Value
			}
			wizardOptions[f.StringID] = opts
		}
		for _, c := range f.ConditionalFields {
			collect(c)
		}
	}
	for _, step := range doc.WizardVersion.Steps {
		for _, f := range step.Fields {
			collect(f)
		}
	}
	if len(wizardIDs) == 0 {
		t.Fatalf("no fields found in %s", wizardJSONPath)
	}

	schemaIDs := cdnSettingsWireKeys()
	sort.Strings(wizardIDs)
	sort.Strings(schemaIDs)
	if !slices.Equal(wizardIDs, schemaIDs) {
		t.Errorf("schema and wizard version differ:\nwizard: %v\nschema: %v", wizardIDs, schemaIDs)
	}

	// a changed field type must fail here, not at apply
	kinds := schemaKinds()
	for id, wizardType := range wizardTypes {
		want, ok := kindForWizardType[wizardType]
		if !ok {
			t.Errorf("%s has wizard type %q, which no schema kind covers", id, wizardType)
			continue
		}
		if got, ok := kinds[id]; ok && got != want {
			t.Errorf("%s is %q in the wizard but kind %v in the schema, want %v", id, wizardType, got, want)
		}
	}

	// select options must match the wizard, in order
	options := schemaOptions()
	for id, wizardType := range wizardTypes {
		if wizardType == "select" {
			if !slices.Equal(options[id], wizardOptions[id]) {
				t.Errorf("%s options are %v in the schema, want %v", id, options[id], wizardOptions[id])
			}
		} else if len(options[id]) != 0 {
			t.Errorf("%s is %q in the wizard but carries select options %v in the schema", id, wizardType, options[id])
		}
	}

	// The fixture the other tests use must be the same document the server sends.
	fixture := defaultSettings(t)
	var fixtureIDs []string
	for k := range fixture.Settings {
		fixtureIDs = append(fixtureIDs, k)
	}
	sort.Strings(fixtureIDs)
	if !slices.Equal(fixtureIDs, wizardIDs) {
		t.Errorf("fixture does not match the wizard version:\nfixture: %v\nwizard: %v", fixtureIDs, wizardIDs)
	}
}

type wizardJSONField struct {
	StringID     string `json:"stringId"`
	Type         string `json:"type"`
	ValueOptions []struct {
		Value string `json:"value"`
	} `json:"valueOptions"`
	ConditionalFields []wizardJSONField `json:"conditionalFields"`
}

// kindForWizardType is the schema kind each wizard field type must map to.
var kindForWizardType = map[string]cdnSettingsKind{
	"boolean":        kindBool,
	"text":           kindString,
	"select":         kindString,
	"integer":        kindInt,
	"float":          kindFloat,
	"array":          kindStringList,
	"multi-select":   kindStringList,
	"country-select": kindStringList,
	"csv":            kindCsvList,
	"multi-array":    kindNameValueRows,
}

// schemaKinds indexes the schema's leaf kinds by string id.
func schemaKinds() map[string]cdnSettingsKind {
	out := map[string]cdnSettingsKind{}
	var walk func(f cdnSettingsField)
	walk = func(f cdnSettingsField) {
		if f.children == nil {
			out[f.wire] = f.kind
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

// schemaOptions indexes the schema's select option lists by string id.
func schemaOptions() map[string][]string {
	out := map[string][]string{}
	var walk func(f cdnSettingsField)
	walk = func(f cdnSettingsField) {
		if f.children == nil {
			out[f.wire] = f.options
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

func TestCamelToSnake(t *testing.T) {
	cases := map[string]string{
		"uncacheable":          "uncacheable",
		"cacheableTTLOverride": "cacheable_ttl_override",
		"uncacheableTTL":       "uncacheable_ttl",
		"trueClientIp":         "true_client_ip",
		"hashSource":           "hash_source",
	}
	for in, want := range cases {
		if got := camelToSnake(in); got != want {
			t.Errorf("camelToSnake(%q) = %q, want %q", in, got, want)
		}
	}
}

// Invalid JSON must be refused, or every later plan fails.
func TestOverridesFromAPI_RejectsInvalidJSONValue(t *testing.T) {
	ctx := context.Background()

	env := defaultSettings(t)
	env.Overrides = []client.CDNWizardOverride{{
		Name:       "images",
		Extensions: []string{"jpg"},
		Rules:      []client.CDNWizardRule{{StringId: "general.uncacheable", Value: json.RawMessage(`not json`)}},
	}}

	_, diags := cdnSettingsFromAPI(ctx, env)
	if !diags.HasError() {
		t.Fatal("expected an error for a value that is not valid JSON")
	}
	if detail := diags.Errors()[0].Detail(); !strings.Contains(detail, "images") || !strings.Contains(detail, "general.uncacheable") {
		t.Errorf("error does not name the rule: %s", detail)
	}
}

// Backend headers carry origin credentials; delivery headers do not.
func TestSettingsBackendHeaderValuesAreSensitive(t *testing.T) {
	headers := cdnSettingsAttribute().Attributes["headers"].(schema.SingleNestedAttribute)
	rowValue := func(name string) schema.Attribute {
		return headers.Attributes[name].(schema.ListNestedAttribute).NestedObject.Attributes["value"]
	}
	if !rowValue("set_backend_headers").IsSensitive() {
		t.Error("set_backend_headers row values must be sensitive")
	}
	if rowValue("set_headers_for_delivery").IsSensitive() {
		t.Error("set_headers_for_delivery row values must not be sensitive")
	}
}

func TestKeyListCap(t *testing.T) {
	keys := make([]string, maxListedKeys+5)
	for i := range keys {
		keys[i] = "k"
	}
	got := keyList(keys)
	if strings.Count(got, "k") != maxListedKeys || !strings.Contains(got, "and 5 more") {
		t.Errorf("keyList = %q", got)
	}
	if keyList(keys[:3]) != "k, k, k" {
		t.Errorf("a short list must be listed in full: %q", keyList(keys[:3]))
	}
}

// Renamed select values keep working for services on an older wizard version.
func TestSelectAcceptsLegacyValues(t *testing.T) {
	attr := leafAttribute(t, "cookies.onSetCookie.action").(schema.StringAttribute)
	for value, valid := range map[string]bool{"allow_list": true, "allowList": true, "denyList": true, "bogus": false} {
		resp := &validator.StringResponse{}
		for _, v := range attr.Validators {
			v.ValidateString(context.Background(), validator.StringRequest{ConfigValue: types.StringValue(value)}, resp)
		}
		if got := !resp.Diagnostics.HasError(); got != valid {
			t.Errorf("%q valid = %v, want %v", value, got, valid)
		}
	}
	if !strings.Contains(attr.Description, "allowList") {
		t.Errorf("description does not mention the old spelling: %s", attr.Description)
	}
}
