// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/varnish/terraform-provider-varnish/internal/client"
)

func origin(name, host string, def bool) cdnOriginModel {
	return cdnOriginModel{
		Name:             types.StringValue(name),
		Host:             types.StringValue(host),
		Port:             types.Int64Value(0),
		EnableTLS:        types.BoolValue(false),
		HostHeader:       types.StringValue(""),
		IsDefault:        types.BoolValue(def),
		AllowInsecureTLS: types.BoolValue(false),
	}
}

func dom(fqdn, sub, id string, tls bool, cert int64) cdnDomainModel {
	return cdnDomainModel{
		FQDN:      types.StringValue(fqdn),
		Subdomain: types.StringValue(sub),
		TLS:       types.BoolValue(tls),
		CertID:    types.Int64Value(cert),
		ID:        types.StringValue(id),
	}
}

func TestDefaultOriginIndex(t *testing.T) {
	cases := []struct {
		name string
		in   []client.ServiceOrigin
		want int
	}{
		{"empty", nil, 0},
		{"none default", []client.ServiceOrigin{{Name: "a"}, {Name: "b"}}, 0},
		{"second default", []client.ServiceOrigin{{Name: "a"}, {Name: "b", IsDefault: true}}, 1},
		{"first default", []client.ServiceOrigin{{Name: "a", IsDefault: true}, {Name: "b"}}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cdnDefaultOriginIndex(c.in); got != c.want {
				t.Errorf("cdnDefaultOriginIndex = %d, want %d", got, c.want)
			}
		})
	}
}

func TestOriginsChanged(t *testing.T) {
	a := origin("a", "h1", true)
	b := origin("b", "h2", false)
	bMod := origin("b", "h3", false)

	cases := []struct {
		name        string
		state, plan []cdnOriginModel
		want        bool
	}{
		{"identical", []cdnOriginModel{a, b}, []cdnOriginModel{a, b}, false},
		{"len differs", []cdnOriginModel{a}, []cdnOriginModel{a, b}, true},
		{"host differs", []cdnOriginModel{a, b}, []cdnOriginModel{a, bMod}, true},
		{"both empty", nil, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cdnOriginsChanged(c.state, c.plan); got != c.want {
				t.Errorf("cdnOriginsChanged = %v, want %v", got, c.want)
			}
		})
	}
}

func TestOrderedOrigins(t *testing.T) {
	prior := []cdnOriginModel{origin("b", "hb", false), origin("a", "ha", true)}
	server := []client.ServiceOrigin{
		{Name: "a", Host: "ha", IsDefault: true},
		{Name: "b", Host: "hb"},
		{Name: "c", Host: "hc"},
	}

	got := cdnOrderedOrigins(prior, server)
	wantNames := []string{"b", "a", "c"}
	if len(got) != len(wantNames) {
		t.Fatalf("got %d origins, want %d", len(got), len(wantNames))
	}
	for i, n := range wantNames {
		if got[i].Name.ValueString() != n {
			t.Errorf("origin[%d] = %q, want %q", i, got[i].Name.ValueString(), n)
		}
	}
}

func TestOrderedDomains(t *testing.T) {
	prior := []cdnDomainModel{
		dom("b.example.com", "", "2", false, 0),
		dom("a.example.com", "", "1", false, 0),
	}
	server := []client.Domain{
		{ID: 1, FQDN: "a.example.com"},
		{ID: 2, FQDN: "b.example.com"},
		{ID: 3, FQDN: "c.example.com"},
	}

	got := cdnOrderedDomains(prior, server)
	wantFQDN := []string{"b.example.com", "a.example.com", "c.example.com"}
	if len(got) != len(wantFQDN) {
		t.Fatalf("got %d domains, want %d", len(got), len(wantFQDN))
	}
	for i, f := range wantFQDN {
		if got[i].FQDN.ValueString() != f {
			t.Errorf("domain[%d] = %q, want %q", i, got[i].FQDN.ValueString(), f)
		}
	}
}

func TestDiffDomains(t *testing.T) {
	state := []cdnDomainModel{
		dom("keep.example.com", "", "1", false, 0),
		dom("change.example.com", "", "2", false, 0),
		dom("remove.example.com", "", "3", false, 0),
	}
	plan := []cdnDomainModel{
		dom("keep.example.com", "", "", false, 0),
		dom("change.example.com", "", "", true, 0), // tls flipped
		dom("new.example.com", "", "", false, 0),
	}

	got := cdnDiffDomains(state, plan)

	if len(got.toAdd) != 1 || got.toAdd[0].FQDN.ValueString() != "new.example.com" {
		t.Errorf("toAdd = %+v, want [new.example.com]", got.toAdd)
	}
	if len(got.toUpdate) != 1 || got.toUpdate[0].id != 2 {
		t.Errorf("toUpdate = %+v, want [id 2]", got.toUpdate)
	}
	if len(got.toDelete) != 1 || got.toDelete[0] != 3 {
		t.Errorf("toDelete = %+v, want [3]", got.toDelete)
	}
}

func TestDiffDomains_SameFQDNDifferentSubdomain(t *testing.T) {
	// same fqdn, different subdomain: two separate domains
	state := []cdnDomainModel{
		dom("example.com", "www", "1", false, 0),
		dom("example.com", "api", "2", true, 0),
	}
	plan := []cdnDomainModel{
		dom("example.com", "www", "", true, 0), // tls flipped on the www entry
		dom("example.com", "api", "", true, 0), // unchanged
	}

	got := cdnDiffDomains(state, plan)

	if len(got.toAdd) != 0 {
		t.Errorf("toAdd = %+v, want none", got.toAdd)
	}
	if len(got.toDelete) != 0 {
		t.Errorf("toDelete = %+v, want none", got.toDelete)
	}
	if len(got.toUpdate) != 1 || got.toUpdate[0].id != 1 {
		t.Errorf("toUpdate = %+v, want [id 1]", got.toUpdate)
	}
}

func TestDomainFieldsChanged(t *testing.T) {
	base := dom("a.example.com", "sub", "1", false, 5)

	cases := []struct {
		name string
		plan cdnDomainModel
		want bool
	}{
		{"identical", dom("a.example.com", "sub", "1", false, 5), false},
		{"tls changed", dom("a.example.com", "sub", "1", true, 5), true},
		{"cert changed", dom("a.example.com", "sub", "1", false, 9), true},
		{"subdomain changed", dom("a.example.com", "other", "1", false, 5), true},
		{"unknown tls ignored", cdnDomainModel{
			FQDN:      types.StringValue("a.example.com"),
			Subdomain: types.StringValue("sub"),
			TLS:       types.BoolUnknown(),
			CertID:    types.Int64Value(5),
		}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cdnDomainFieldsChanged(base, c.plan); got != c.want {
				t.Errorf("cdnDomainFieldsChanged = %v, want %v", got, c.want)
			}
		})
	}
}

func TestFindDNSSettings(t *testing.T) {
	settings := []client.DNSSettings{
		{DomainID: 1, Domain: "apex.example.com"},
		{DomainID: 2, Domain: "example.com", Subdomain: "www"},
	}

	cases := []struct {
		name   string
		fqdn   string
		wantID int // -1 means nil
	}{
		{"exact domain", "apex.example.com", 1},
		{"subdomain composite", "www.example.com", 2},
		{"no match", "missing.example.com", -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := cdnFindDNSSettings(settings, c.fqdn)
			if c.wantID == -1 {
				if got != nil {
					t.Errorf("got %+v, want nil", got)
				}
				return
			}
			if got == nil || got.DomainID != c.wantID {
				t.Errorf("got %+v, want DomainID %d", got, c.wantID)
			}
		})
	}
}

func TestDomainCertID(t *testing.T) {
	cases := []struct {
		name string
		in   client.Domain
		want types.Int64
	}{
		{"managed cert", client.Domain{CertID: 5, CertType: client.CertLocationDB}, types.Int64Value(5)},
		// The CDN's own certificate is not a managed input.
		{"acme cert", client.Domain{CertID: 5, CertType: client.CertLocationACME}, types.Int64Null()},
		{"no cert", client.Domain{}, types.Int64Null()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cdnDomainCertID(c.in); !got.Equal(c.want) {
				t.Errorf("cdnDomainCertID = %v, want %v", got, c.want)
			}
		})
	}
}

// An unresolved cert_id must not detach the domain's certificate when an
// unrelated field changes.
func TestDiffDomains_UnknownCertKeepsState(t *testing.T) {
	state := []cdnDomainModel{dom("a.example.com", "", "1", false, 5)}
	plan := []cdnDomainModel{{
		FQDN:      types.StringValue("a.example.com"),
		Subdomain: types.StringValue(""),
		TLS:       types.BoolValue(true),
		CertID:    types.Int64Unknown(),
	}}

	got := cdnDiffDomains(state, plan)
	if len(got.toUpdate) != 1 {
		t.Fatalf("toUpdate = %v, want one domain", got.toUpdate)
	}
	if !got.toUpdate[0].plan.CertID.Equal(types.Int64Value(5)) {
		t.Errorf("cert_id = %v, want the one in state", got.toUpdate[0].plan.CertID)
	}
}
