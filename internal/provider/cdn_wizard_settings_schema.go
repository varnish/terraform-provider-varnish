// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Typed schema for the wizard settings document. The API document is flat and
// keyed by wizard string id. This schema is the nested HCL view of it. The HCL
// name of a leaf is the last string id segment in snake_case.
//
// Support is checked by key set, not by version number, since the version a
// service reports is a database id that differs per installation.
//
// Mirrors testdata/wizard.json. TestSettingsSchemaCoversWizardVersion checks it.

type cdnSettingsKind int

const (
	kindBool cdnSettingsKind = iota
	kindString
	kindInt
	kindFloat
	kindStringList
	// kindCsvList is stored as one comma separated string, so entries cannot hold a comma
	kindCsvList
	kindNameValueRows
)

// cdnSettingsField is a group (children) or a leaf (wizard string id and kind).
type cdnSettingsField struct {
	name      string
	wire      string
	kind      cdnSettingsKind
	sensitive bool
	options   []string // select values, in wizard order (kindString only)
	legacy    []string // older wizard spellings of options, still valid on old services
	dflt      string   // wizard-shipped default, shown in the description
	children  []cdnSettingsField
}

func grp(name string, children ...cdnSettingsField) cdnSettingsField {
	return cdnSettingsField{name: name, children: children}
}

// lf builds a leaf named after the last segment of its string id.
func lf(kind cdnSettingsKind, wire string) cdnSettingsField {
	name := wire
	if i := strings.LastIndex(wire, "."); i >= 0 {
		name = wire[i+1:]
	}
	return cdnSettingsField{name: camelToSnake(name), wire: wire, kind: kind}
}

// lfn builds a leaf with an explicit name, for ids that prefix other ids (general.sharding).
func lfn(kind cdnSettingsKind, name, wire string) cdnSettingsField {
	return cdnSettingsField{name: name, wire: wire, kind: kind}
}

// lsel builds a select leaf limited to options, with the default in its description.
func lsel(wire, dflt string, options ...string) cdnSettingsField {
	f := lf(kindString, wire)
	f.options = options
	f.dflt = dflt
	return f
}

// withLegacy also accepts values a renamed option has on older wizard versions.
func (f cdnSettingsField) withLegacy(values ...string) cdnSettingsField {
	f.legacy = values
	return f
}

// lfsec builds a sensitive leaf.
func lfsec(kind cdnSettingsKind, wire string) cdnSettingsField {
	f := lf(kind, wire)
	f.sensitive = true
	return f
}

// cdnSettingsTree is the section list of the settings attribute, overrides aside.
var cdnSettingsTree = []cdnSettingsField{
	grp("general",
		lf(kindStringList, "general.allowMethods"),
		lf(kindBool, "general.uncacheable"),
		lf(kindBool, "general.enableCacheableTTLOverride"),
		lf(kindString, "general.cacheableTTLOverride"),
		lf(kindBool, "general.enableErrorCodeTTLOverride"),
		lf(kindString, "general.errorCodeTTLOverride"),
		lf(kindString, "general.uncacheableTTL"),
		lf(kindString, "general.firstByteTimeout"),
		lsel("general.websocket", "disable", "disable", "enable", "exclusive"),
		lf(kindBool, "general.allowBodyWithGet"),
		lsel("general.urlDecodeReserved", "enabled", "enabled", "skip"),
		lf(kindString, "general.maxReqBodySize"),
		grp("chunked_body",
			lsel("general.chunkedBody.action", "deny", "deny", "cache", "allow"),
		),
		grp("sharding",
			lfn(kindBool, "enable", "general.sharding"),
			lsel("general.sharding.hashSource", "url", "url", "client_ip"),
		),
		lf(kindBool, "general.backendRetry"),
		lf(kindBool, "general.enableUrlRewrite"),
		lf(kindString, "general.urlRewrite"),
	),
	grp("high_availability",
		lf(kindBool, "highAvailability.enableStaleIfError"),
		grp("stale_if_error",
			lf(kindString, "highAvailability.staleIfError.keepStaleDuration"),
			grp("stale_revive",
				lf(kindBool, "highAvailability.staleIfError.staleRevive.enable"),
				lf(kindString, "highAvailability.staleIfError.staleRevive.ttl"),
				lf(kindString, "highAvailability.staleIfError.staleRevive.grace"),
			),
		),
	),
	grp("query_params",
		grp("remove_query_parameters",
			lsel("queryParams.removeQueryParameters.action", "sort", "redirect", "continue", "sort", "off"),
		),
	),
	grp("headers",
		grp("on_auth_header",
			lsel("headers.onAuthHeader.action", "pass", "remove", "pass"),
		),
		lf(kindStringList, "headers.unsetHeadersForDelivery"),
		lf(kindStringList, "headers.hashHeaders"),
		// headers sent to the origin, typically carrying origin authentication
		lfsec(kindNameValueRows, "headers.setBackendHeaders"),
		lf(kindNameValueRows, "headers.setHeadersForDelivery"),
		lf(kindBool, "headers.trueClientIp"),
	),
	grp("cookies",
		grp("on_cookie",
			lsel("cookies.onCookie.action", "remove", "pass", "remove", "allow_list").withLegacy("allowList"),
			lf(kindStringList, "cookies.onCookie.allowList"),
			lf(kindBool, "cookies.onCookie.allowWordpressCookies"),
			lf(kindBool, "cookies.onCookie.allowDrupalCookies"),
		),
		grp("on_set_cookie",
			lsel("cookies.onSetCookie.action", "pass", "pass", "remove", "allow_list", "deny_list").withLegacy("allowList", "denyList"),
			lf(kindStringList, "cookies.onSetCookie.allowList"),
			lf(kindStringList, "cookies.onSetCookie.denyList"),
		),
	),
	grp("security",
		lf(kindBool, "security.httpsRedirect"),
		lf(kindBool, "security.hsts"),
		lsel("security.botVerifySettings", "enabled", "enabled", "disabled"),
		lf(kindBool, "security.enableHotlinkProtection"),
		grp("hotlink_protection",
			lf(kindCsvList, "security.hotlinkProtection.friends"),
		),
		grp("asn_filter",
			lf(kindBool, "security.asnFilter.enable"),
			lf(kindBool, "security.asnFilter.allowVerifiedBots"),
			lsel("security.asnFilter.action", "404", "429", "404", "403"),
			lsel("security.asnFilter.type", "country_deny_list", "country_deny_list", "country_allow_list", "asn_deny_list", "asn_allow_list"),
			lf(kindStringList, "security.asnFilter.countryDenyList"),
			lf(kindStringList, "security.asnFilter.countryAllowList"),
			lf(kindCsvList, "security.asnFilter.asnDenyList"),
			lf(kindCsvList, "security.asnFilter.asnAllowList"),
		),
		grp("bot_protection",
			lf(kindBool, "security.botProtection.vpow"),
			lsel("security.botProtection.mode", "both", "captcha", "pow", "both"),
			lf(kindString, "security.botProtection.captchaTtl"),
		),
		grp("waf",
			lf(kindBool, "security.waf.enable"),
			lsel("security.waf.mode", "deny", "detect", "deny"),
			lf(kindBool, "security.waf.owasp"),
			lf(kindBool, "security.waf.atomic"),
			lf(kindCsvList, "security.waf.skipRuleId"),
			grp("bypass",
				lf(kindBool, "security.waf.bypass.enable"),
				lf(kindCsvList, "security.waf.bypass.acl"),
			),
		),
		grp("allow_list",
			lf(kindBool, "security.allowList.enable"),
			lf(kindCsvList, "security.allowList.acl"),
		),
		grp("deny_list",
			lf(kindBool, "security.denyList.enable"),
			lsel("security.denyList.action", "403", "400", "403", "404"),
			lf(kindCsvList, "security.denyList.acl"),
		),
		grp("ratelimit",
			lf(kindBool, "security.ratelimit.enable"),
			lf(kindFloat, "security.ratelimit.clientRate"),
			lf(kindString, "security.ratelimit.clientCreditLimit"),
			lf(kindFloat, "security.ratelimit.fetchRate"),
			lf(kindString, "security.ratelimit.fetchCreditLimit"),
		),
	),
	grp("optimisation",
		grp("imageopt",
			lf(kindBool, "optimisation.imageopt.webp"),
			lf(kindInt, "optimisation.imageopt.quality"),
			grp("imagetrans",
				lf(kindBool, "optimisation.imageopt.imagetrans.enable"),
				lf(kindString, "optimisation.imageopt.imagetrans.widthParam"),
				lf(kindString, "optimisation.imageopt.imagetrans.heightParam"),
				lf(kindCsvList, "optimisation.imageopt.imagetrans.allowedDimensions"),
				lf(kindCsvList, "optimisation.imageopt.imagetrans.allowedWidths"),
				lf(kindCsvList, "optimisation.imageopt.imagetrans.allowedHeights"),
			),
		),
	),
}

const cdnSettingsDescription = "Wizard (caching/security/routing) configuration. Every leaf is optional and computed: " +
	"leaves you do not set keep tracking the server, so unmanaged parts of the wizard never show as drift. " +
	"Removing a leaf you previously set does not revert it, it only stops managing it. " +
	"An omitted field tracks the service's current value, which starts from its wizard answer template default; " +
	"terraform plan and import show the effective values. " +
	"What the fields mean is documented at https://www.varnish-cdn.com/docs/configuration."

const cdnOverridesDescription = "Custom rules: settings applied only to requests matching the given paths, extensions or origin. " +
	"The list is declared in full and is applied in order."

// cdnSettingsAttribute builds the typed settings attribute of varnish_cdn_service.
func cdnSettingsAttribute() schema.SingleNestedAttribute {
	attrs := make(map[string]schema.Attribute, len(cdnSettingsTree)+1)
	for _, f := range cdnSettingsTree {
		attrs[f.name] = f.attribute()
	}
	attrs[cdnOverridesAttrName] = cdnOverridesAttribute()
	return schema.SingleNestedAttribute{
		Optional:    true,
		Computed:    true,
		Description: cdnSettingsDescription,
		Attributes:  attrs,
	}
}

const cdnOverridesAttrName = "overrides"

func legacyNote(legacy []string) string {
	if len(legacy) == 0 {
		return ""
	}
	return " (services on an older wizard version use " + strings.Join(legacy, ", ") + " instead)"
}

// noComma rejects a comma in a csv-backed list entry.
var noComma = stringvalidator.RegexMatches(regexp.MustCompile(`^[^,]*$`),
	"must not contain a comma: the wizard stores this setting as a comma separated list")

func (f cdnSettingsField) attribute() schema.Attribute {
	if f.children != nil {
		attrs := make(map[string]schema.Attribute, len(f.children))
		for _, c := range f.children {
			attrs[c.name] = c.attribute()
		}
		return schema.SingleNestedAttribute{Optional: true, Computed: true, Attributes: attrs}
	}

	desc := "Wizard setting " + f.wire + "."
	switch f.kind {
	case kindBool:
		return schema.BoolAttribute{Optional: true, Computed: true, Description: desc}
	case kindInt:
		return schema.Int64Attribute{Optional: true, Computed: true, Description: desc}
	case kindFloat:
		return schema.Float64Attribute{Optional: true, Computed: true,
			Description: desc + " It is stored as a 32 bit float, so it keeps about 7 significant digits."}
	case kindStringList:
		return schema.ListAttribute{Optional: true, Computed: true, ElementType: types.StringType, Description: desc}
	case kindCsvList:
		return schema.ListAttribute{
			Optional:    true,
			Computed:    true,
			ElementType: types.StringType,
			Validators:  []validator.List{listvalidator.ValueStringsAre(noComma)},
			Description: desc + " Entries must not contain a comma.",
		}
	case kindNameValueRows:
		if f.sensitive {
			desc += " Row values are treated as secrets and hidden from plan output."
		}
		return schema.ListNestedAttribute{
			Optional:    true,
			Computed:    true,
			Description: desc,
			NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
				"name":  schema.StringAttribute{Required: true},
				"value": schema.StringAttribute{Required: true, Sensitive: f.sensitive},
			}},
		}
	}
	if f.options != nil {
		return schema.StringAttribute{
			Optional:   true,
			Computed:   true,
			Validators: []validator.String{stringvalidator.OneOf(append(slices.Clone(f.options), f.legacy...)...)},
			Description: desc + " Valid values: " + strings.Join(f.options, ", ") + legacyNote(f.legacy) +
				". Wizard default: " + f.dflt + " (omit to track the service value).",
		}
	}
	return schema.StringAttribute{Optional: true, Computed: true, Description: desc}
}

// emptyStringList is the default for an override's paths and extensions. Without
// it, Terraform fills a null computed attribute from the element that used to
// be at that list index, so reordering overrides would mix their values.
var emptyStringList = types.ListValueMust(types.StringType, []attr.Value{})

func cdnOverridesAttribute() schema.ListNestedAttribute {
	return schema.ListNestedAttribute{
		Optional:    true,
		Computed:    true,
		Description: cdnOverridesDescription,
		NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Rule name.",
			},
			"paths": schema.ListAttribute{
				Optional:    true,
				Computed:    true,
				Default:     listdefault.StaticValue(emptyStringList),
				ElementType: types.StringType,
				Description: "Request paths the rule applies to.",
			},
			"extensions": schema.ListAttribute{
				Optional:    true,
				Computed:    true,
				Default:     listdefault.StaticValue(emptyStringList),
				ElementType: types.StringType,
				Description: "File extensions the rule applies to.",
			},
			"origin_name": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Description: "Origin the rule applies to.",
			},
			// a set, since the API returns rules in its own order
			"rules": schema.SetNestedAttribute{
				Required:    true,
				Description: "Settings overridden by this rule. The order is not significant.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"string_id": schema.StringAttribute{
						Required:    true,
						Description: "Wizard string id of the overridden setting, e.g. general.uncacheable.",
					},
					"value": schema.StringAttribute{
						Required:    true,
						Sensitive:   true,
						CustomType:  jsontypes.NormalizedType{},
						Description: "Value as JSON (use jsonencode), encoded like the setting itself.",
					},
				}},
			},
		}},
	}
}

var cdnNameValueType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"name":  types.StringType,
	"value": types.StringType,
}}

var cdnOverrideRuleType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"string_id": types.StringType,
	"value":     jsontypes.NormalizedType{},
}}

var cdnOverrideType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"name":        types.StringType,
	"paths":       types.ListType{ElemType: types.StringType},
	"extensions":  types.ListType{ElemType: types.StringType},
	"origin_name": types.StringType,
	"rules":       types.SetType{ElemType: cdnOverrideRuleType},
}}

func cdnSettingsObjectType() types.ObjectType {
	at := make(map[string]attr.Type, len(cdnSettingsTree)+1)
	for _, f := range cdnSettingsTree {
		at[f.name] = f.attrType()
	}
	at[cdnOverridesAttrName] = types.ListType{ElemType: cdnOverrideType}
	return types.ObjectType{AttrTypes: at}
}

func (f cdnSettingsField) attrType() attr.Type {
	if f.children != nil {
		at := make(map[string]attr.Type, len(f.children))
		for _, c := range f.children {
			at[c.name] = c.attrType()
		}
		return types.ObjectType{AttrTypes: at}
	}
	switch f.kind {
	case kindBool:
		return types.BoolType
	case kindInt:
		return types.Int64Type
	case kindFloat:
		return types.Float64Type
	case kindStringList, kindCsvList:
		return types.ListType{ElemType: types.StringType}
	case kindNameValueRows:
		return types.ListType{ElemType: cdnNameValueType}
	}
	return types.StringType
}

// camelToSnake renders a lowerCamelCase wizard string id segment as an HCL name,
// keeping acronym runs together (cacheableTTLOverride -> cacheable_ttl_override).
func camelToSnake(s string) string {
	rs := []rune(s)
	var b strings.Builder
	for i, r := range rs {
		if unicode.IsUpper(r) {
			prevLower := i > 0 && !unicode.IsUpper(rs[i-1])
			nextLower := i+1 < len(rs) && unicode.IsLower(rs[i+1])
			if i > 0 && (prevLower || nextLower) {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
