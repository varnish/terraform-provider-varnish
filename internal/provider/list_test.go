// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/varnish/terraform-provider-varnish/internal/client"
)

const (
	certsPath    = "/api/v1/cdn/certs"
	servicesPath = "/api/v1/cdn/services"
	tokensPath   = "/api/v1/cdn/sessions/purger"
	whoamiPath   = "/api/v1/cdn/auth/whoami"
	org5Path     = "/api/v1/cdn/orgs/5"
	org5Invites  = "/api/v1/cdn/orgs/5/invite"
)

type listable interface {
	resource.ResourceWithIdentity
	list.ListResource
}

// apiClient serves raw JSON bodies by path and 404s the rest.
func apiClient(t *testing.T, routes map[string]string) *client.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return client.New(client.Config{Endpoint: srv.URL, AccessToken: "t"})
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func newListRequest(t *testing.T, r listable, include bool) list.ListRequest {
	t.Helper()
	ctx := context.Background()

	schemaResp := &resource.SchemaResponse{}
	r.Schema(ctx, resource.SchemaRequest{}, schemaResp)
	identResp := &resource.IdentitySchemaResponse{}
	r.IdentitySchema(ctx, resource.IdentitySchemaRequest{}, identResp)
	listResp := &list.ListResourceSchemaResponse{}
	r.ListResourceConfigSchema(ctx, list.ListResourceSchemaRequest{}, listResp)

	cfgType := listResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	vals := make(map[string]tftypes.Value, len(cfgType.AttributeTypes))
	for name, at := range cfgType.AttributeTypes {
		vals[name] = tftypes.NewValue(at, nil)
	}

	return list.ListRequest{
		Config:                 tfsdk.Config{Schema: listResp.Schema, Raw: tftypes.NewValue(cfgType, vals)},
		IncludeResource:        include,
		ResourceSchema:         schemaResp.Schema,
		ResourceIdentitySchema: identResp.IdentitySchema,
	}
}

func runList(t *testing.T, r listable, req list.ListRequest) []list.ListResult {
	t.Helper()
	stream := &list.ListResultsStream{}
	r.List(context.Background(), req, stream)
	results := slices.Collect(stream.Results)
	for _, res := range results {
		if res.Diagnostics.HasError() {
			t.Fatalf("list result error: %v", res.Diagnostics.Errors())
		}
	}
	return results
}

func resultID(t *testing.T, res list.ListResult) string {
	t.Helper()
	var ident idIdentityModel
	if diags := res.Identity.Get(context.Background(), &ident); diags.HasError() {
		t.Fatalf("identity: %v", diags.Errors())
	}
	return ident.ID.ValueString()
}

func TestCertificateList(t *testing.T) {
	c := apiClient(t, map[string]string{
		certsPath: mustJSON(t, []client.Cert{
			{ID: 1, Name: "uploaded", Type: client.CertLocationDB},
			{ID: 2, Name: "issued", Type: client.CertLocationACME},
		}),
		certsPath + "/1": mustJSON(t, client.Cert{
			ID: 1, Name: "uploaded", Certificate: "PEM", MinVersion: "1.2", Type: client.CertLocationDB,
		}),
	})
	r := &cdnCertificateResource{client: c}

	results := runList(t, r, newListRequest(t, r, false))
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1 (ACME skipped)", len(results))
	}
	if got := results[0].DisplayName; got != "uploaded" {
		t.Errorf("display name = %q, want uploaded", got)
	}
	if got := resultID(t, results[0]); got != "1" {
		t.Errorf("identity id = %q, want 1", got)
	}

	results = runList(t, r, newListRequest(t, r, true))
	var m cdnCertificateModel
	if diags := results[0].Resource.Get(context.Background(), &m); diags.HasError() {
		t.Fatalf("resource: %v", diags.Errors())
	}
	if m.Certificate.ValueString() != "PEM" || m.MinVersion.ValueString() != "1.2" {
		t.Errorf("resource = %+v, want the certificate read back", m)
	}
	if !m.PrivateKey.IsNull() {
		t.Error("private_key must stay null, the API never returns it")
	}
}

func TestCertificateList_Error(t *testing.T) {
	r := &cdnCertificateResource{client: apiClient(t, nil)}

	stream := &list.ListResultsStream{}
	r.List(context.Background(), newListRequest(t, r, false), stream)
	results := slices.Collect(stream.Results)
	if len(results) != 1 || !results[0].Diagnostics.HasError() {
		t.Fatalf("results = %+v, want a single error", results)
	}
}

func TestPurgeTokenList(t *testing.T) {
	expire := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	c := apiClient(t, map[string]string{
		tokensPath: mustJSON(t, []client.PurgeToken{
			{ID: 7, OrgID: 5, Label: "ci", Expire: expire},
			{ID: 8, OrgID: 5, Label: "ops", Expire: expire},
		}),
	})
	r := &cdnPurgeTokenResource{client: c}

	results := runList(t, r, newListRequest(t, r, true))
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if results[1].DisplayName != "ops" || resultID(t, results[1]) != "8" {
		t.Errorf("second result = %q / %q, want ops / 8", results[1].DisplayName, resultID(t, results[1]))
	}

	var m cdnPurgeTokenModel
	if diags := results[0].Resource.Get(context.Background(), &m); diags.HasError() {
		t.Fatalf("resource: %v", diags.Errors())
	}
	if m.Label.ValueString() != "ci" || m.OrgID.ValueString() != "5" || m.ExpiresAt.ValueString() != "2030-01-02T03:04:05Z" {
		t.Errorf("resource = %+v", m)
	}
	if !m.AccessToken.IsNull() || !m.RefreshToken.IsNull() {
		t.Error("token values must stay null, the API returns them only at create")
	}

	req := newListRequest(t, r, false)
	req.Limit = 1
	if got := len(runList(t, r, req)); got != 1 {
		t.Errorf("results with limit 1 = %d, want 1", got)
	}
}

func TestServiceList(t *testing.T) {
	c := apiClient(t, map[string]string{
		servicesPath: mustJSON(t, []client.Service{{ID: 7, Name: "www.example.com"}}),
		servicesPath + "/7": mustJSON(t, client.Service{
			ID:      7,
			Name:    "www.example.com",
			Origins: []client.ServiceOrigin{{Name: "o", Host: "origin.example.com", Port: 443, IsDefault: true}},
			Domains: []client.Domain{{ID: 3, FQDN: "www.example.com"}},
		}),
		servicesPath + "/7/wizard/settings": wizardDefaultSettings,
	})
	r := &cdnServiceResource{client: c}

	results := runList(t, r, newListRequest(t, r, false))
	if len(results) != 1 || results[0].DisplayName != "www.example.com" || resultID(t, results[0]) != "7" {
		t.Fatalf("results = %+v, want service 7", results)
	}
	if !results[0].Resource.Raw.IsNull() {
		t.Error("resource must not be filled unless requested")
	}

	results = runList(t, r, newListRequest(t, r, true))
	var m cdnServiceModel
	if diags := results[0].Resource.Get(context.Background(), &m); diags.HasError() {
		t.Fatalf("resource: %v", diags.Errors())
	}
	if m.ID.ValueString() != "7" || len(m.Origins) != 1 || len(m.Domains) != 1 {
		t.Errorf("resource = %+v, want service 7 with its origin and domain", m)
	}
	if m.Settings.IsNull() {
		t.Error("settings must be read for the generated config")
	}
}

func TestOrgUserList(t *testing.T) {
	c := apiClient(t, map[string]string{
		whoamiPath: mustJSON(t, client.WhoAmI{Org: client.Org{ID: 5}}),
		org5Path: mustJSON(t, client.Org{ID: 5, Accounts: []client.Account{
			{ID: 9, Email: "u@example.com", Role: client.Role{Name: "Admin"}},
		}}),
		org5Invites: mustJSON(t, []client.OrgInvite{
			{Email: "U@example.com", Role: "user"},
			{Email: "p@example.com", Role: "user"},
		}),
	})
	r := &orgUserResource{client: c}

	results := runList(t, r, newListRequest(t, r, true))
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2 (member wins over its invite)", len(results))
	}

	want := []struct{ email, status string }{
		{"u@example.com", statusMember},
		{"p@example.com", statusPending},
	}
	for i, w := range want {
		res := results[i]
		if res.DisplayName != w.email {
			t.Errorf("result %d display name = %q, want %q", i, res.DisplayName, w.email)
		}

		var ident orgUserIdentityModel
		if diags := res.Identity.Get(context.Background(), &ident); diags.HasError() {
			t.Fatalf("identity: %v", diags.Errors())
		}
		if ident.OrgID.ValueString() != "5" || ident.Email.ValueString() != w.email {
			t.Errorf("result %d identity = %+v", i, ident)
		}

		var m orgUserModel
		if diags := res.Resource.Get(context.Background(), &m); diags.HasError() {
			t.Fatalf("resource: %v", diags.Errors())
		}
		if m.Status.ValueString() != w.status || m.ID.ValueString() != "5:"+w.email {
			t.Errorf("result %d resource = %+v, want status %s", i, m, w.status)
		}
	}
}

// Objects deleted between the list call and the per-item read are skipped.
func TestList_SkipsDeleted(t *testing.T) {
	c := apiClient(t, map[string]string{
		certsPath: mustJSON(t, []client.Cert{
			{ID: 1, Name: "kept", Type: client.CertLocationDB},
			{ID: 2, Name: "deleted", Type: client.CertLocationDB},
		}),
		certsPath + "/1": mustJSON(t, client.Cert{ID: 1, Name: "kept", Type: client.CertLocationDB}),
		servicesPath:     mustJSON(t, []client.Service{{ID: 7, Name: "kept"}, {ID: 8, Name: "deleted"}}),
		servicesPath + "/7": mustJSON(t, client.Service{
			ID: 7, Name: "kept",
			Origins: []client.ServiceOrigin{{Name: "o", Host: "origin.example.com", Port: 443, IsDefault: true}},
			Domains: []client.Domain{{ID: 3, FQDN: "www.example.com"}},
		}),
		servicesPath + "/7/wizard/settings": wizardDefaultSettings,
	})

	for name, r := range map[string]listable{
		"certificate": &cdnCertificateResource{client: c},
		"service":     &cdnServiceResource{client: c},
	} {
		t.Run(name, func(t *testing.T) {
			results := runList(t, r, newListRequest(t, r, true))
			if len(results) != 1 || results[0].DisplayName != "kept" {
				t.Fatalf("results = %+v, want only the kept object", results)
			}
			if len(results[0].Diagnostics) > 0 {
				t.Errorf("diagnostics = %v, want none", results[0].Diagnostics)
			}
		})
	}
}

// The limit counts users after the member-over-invite dedupe.
func TestOrgUserList_DedupeWithLimit(t *testing.T) {
	c := apiClient(t, map[string]string{
		whoamiPath: mustJSON(t, client.WhoAmI{Org: client.Org{ID: 5}}),
		org5Path:   mustJSON(t, client.Org{ID: 5, Accounts: []client.Account{{ID: 9, Email: "u@example.com"}}}),
		org5Invites: mustJSON(t, []client.OrgInvite{
			{Email: "U@example.com"},
			{Email: "p@example.com"},
		}),
	})
	r := &orgUserResource{client: c}

	for limit, want := range map[int64][]string{
		1: {"u@example.com"},
		2: {"u@example.com", "p@example.com"},
	} {
		req := newListRequest(t, r, false)
		req.Limit = limit
		var got []string
		for _, res := range runList(t, r, req) {
			got = append(got, res.DisplayName)
		}
		if !slices.Equal(got, want) {
			t.Errorf("limit %d: results = %v, want %v", limit, got, want)
		}
	}
}
