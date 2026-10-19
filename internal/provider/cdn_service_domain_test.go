// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestCDNCheckDomain(t *testing.T) {
	tests := []struct {
		fqdn, subdomain string
		warn, problem   bool
	}{
		{"example.com", "www", false, false},
		{"example.co.uk", "www", false, false},
		{"Example.COM", "www", false, false},
		{"example.com", "", true, false},
		{"example.co.uk", "", true, false},
		{"example.com", "not_valid", false, true},
		{"shop.example.com", "", false, false},
		{"shop.example.com", "www", false, true},
		{"*.example.com", "", false, false},
		{"*.example.com", "www", false, true},
	}
	for _, tc := range tests {
		warn, problem := cdnCheckDomain(tc.fqdn, tc.subdomain)
		if (warn != "") != tc.warn || (problem != "") != tc.problem {
			t.Errorf("cdnCheckDomain(%q, %q) = (%q, %q), want warning %v, problem %v",
				tc.fqdn, tc.subdomain, warn, problem, tc.warn, tc.problem)
		}
	}
}

// domainConfig builds a config with one domain block. A nil subdomain is left unset.
func domainConfig(t *testing.T, fqdn string, subdomain any) tfsdk.Config {
	t.Helper()
	ctx := context.Background()
	resp := &resource.SchemaResponse{}
	NewCDNServiceResource().Schema(ctx, resource.SchemaRequest{}, resp)

	objType := resp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	vals := make(map[string]tftypes.Value, len(objType.AttributeTypes))
	for name, at := range objType.AttributeTypes {
		vals[name] = tftypes.NewValue(at, nil)
	}

	list := objType.AttributeTypes["domain"].(tftypes.List)
	elem := list.ElementType.(tftypes.Object)
	dom := make(map[string]tftypes.Value, len(elem.AttributeTypes))
	for n, at := range elem.AttributeTypes {
		dom[n] = tftypes.NewValue(at, nil)
	}
	dom["fqdn"] = tftypes.NewValue(tftypes.String, fqdn)
	dom["subdomain"] = tftypes.NewValue(tftypes.String, subdomain)
	vals["domain"] = tftypes.NewValue(list, []tftypes.Value{tftypes.NewValue(elem, dom)})

	return tfsdk.Config{Schema: resp.Schema, Raw: tftypes.NewValue(objType, vals)}
}

func TestServiceValidateConfigDomain(t *testing.T) {
	tests := []struct {
		name          string
		fqdn          string
		subdomain     any
		warn, wantErr bool
	}{
		{"apex with subdomain", "example.com", "www", false, false},
		{"apex, subdomain unset", "example.com", nil, true, false},
		{"host with subdomain", "shop.example.com", "www", false, true},
		{"unknown subdomain", "example.com", tftypes.UnknownValue, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := &resource.ValidateConfigResponse{}
			NewCDNServiceResource().(*cdnServiceResource).ValidateConfig(context.Background(),
				resource.ValidateConfigRequest{Config: domainConfig(t, tc.fqdn, tc.subdomain)}, resp)
			if got := resp.Diagnostics.WarningsCount() > 0; got != tc.warn {
				t.Errorf("warning = %v, want %v: %v", got, tc.warn, resp.Diagnostics)
			}
			if got := resp.Diagnostics.HasError(); got != tc.wantErr {
				t.Errorf("error = %v, want %v: %v", got, tc.wantErr, resp.Diagnostics)
			}
		})
	}
}
