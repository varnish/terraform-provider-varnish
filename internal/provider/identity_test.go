// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"maps"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/varnish/terraform-provider-varnish/internal/client"
)

type identified interface {
	resource.ResourceWithIdentity
	resource.ResourceWithImportState
}

func nullState(t *testing.T, r resource.Resource) tfsdk.State {
	t.Helper()
	ctx := context.Background()
	resp := &resource.SchemaResponse{}
	r.Schema(ctx, resource.SchemaRequest{}, resp)
	return tfsdk.State{Schema: resp.Schema, Raw: tftypes.NewValue(resp.Schema.Type().TerraformType(ctx), nil)}
}

func nullIdentity(t *testing.T, r resource.ResourceWithIdentity) *tfsdk.ResourceIdentity {
	t.Helper()
	ctx := context.Background()
	resp := &resource.IdentitySchemaResponse{}
	r.IdentitySchema(ctx, resource.IdentitySchemaRequest{}, resp)
	return &tfsdk.ResourceIdentity{
		Schema: resp.IdentitySchema,
		Raw:    tftypes.NewValue(resp.IdentitySchema.Type().TerraformType(ctx), nil),
	}
}

// stateWith returns a state with only the given string attributes set.
func stateWith(t *testing.T, r resource.Resource, attrs map[string]string) tfsdk.State {
	t.Helper()
	s := nullState(t, r)
	for name, v := range attrs {
		if diags := s.SetAttribute(context.Background(), path.Root(name), v); diags.HasError() {
			t.Fatalf("set %s: %v", name, diags.Errors())
		}
	}
	return s
}

func identityID(t *testing.T, ident *tfsdk.ResourceIdentity) string {
	t.Helper()
	var m idIdentityModel
	if diags := ident.Get(context.Background(), &m); diags.HasError() {
		t.Fatalf("identity: %v", diags.Errors())
	}
	return m.ID.ValueString()
}

func TestImportByID(t *testing.T) {
	for name, r := range map[string]identified{
		"certificate": NewCDNCertificateResource().(identified),
		"purge token": NewCDNPurgeTokenResource().(identified),
		"service":     NewCDNServiceResource().(identified),
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			ident := nullIdentity(t, r)

			byID := &resource.ImportStateResponse{State: nullState(t, r), Identity: nullIdentity(t, r)}
			r.ImportState(ctx, resource.ImportStateRequest{ID: "42", Identity: ident}, byID)

			given := nullIdentity(t, r)
			if diags := given.Set(ctx, idIdentityModel{ID: types.StringValue("42")}); diags.HasError() {
				t.Fatalf("identity: %v", diags.Errors())
			}
			byIdentity := &resource.ImportStateResponse{State: nullState(t, r), Identity: nullIdentity(t, r)}
			r.ImportState(ctx, resource.ImportStateRequest{Identity: given}, byIdentity)

			for how, resp := range map[string]*resource.ImportStateResponse{"id": byID, "identity": byIdentity} {
				if resp.Diagnostics.HasError() {
					t.Fatalf("import by %s: %v", how, resp.Diagnostics.Errors())
				}
				var id types.String
				resp.State.GetAttribute(ctx, path.Root("id"), &id)
				if id.ValueString() != "42" {
					t.Errorf("import by %s: state id = %q, want 42", how, id.ValueString())
				}
			}
		})
	}
}

func TestOrgUserImport(t *testing.T) {
	ctx := context.Background()
	r := &orgUserResource{}

	given := nullIdentity(t, r)
	if diags := given.Set(ctx, orgUserIdentityModel{
		OrgID: types.StringValue("5"),
		Email: types.StringValue("u@example.com"),
	}); diags.HasError() {
		t.Fatalf("identity: %v", diags.Errors())
	}

	for how, req := range map[string]resource.ImportStateRequest{
		"id":       {ID: "5:u@example.com", Identity: nullIdentity(t, r)},
		"identity": {Identity: given},
	} {
		t.Run(how, func(t *testing.T) {
			resp := &resource.ImportStateResponse{State: nullState(t, r), Identity: nullIdentity(t, r)}
			r.ImportState(ctx, req, resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("import: %v", resp.Diagnostics.Errors())
			}

			var m orgUserModel
			for name, dst := range map[string]*types.String{"id": &m.ID, "org_id": &m.OrgID, "email": &m.Email} {
				resp.State.GetAttribute(ctx, path.Root(name), dst)
			}
			if m.ID.ValueString() != "5:u@example.com" || m.OrgID.ValueString() != "5" || m.Email.ValueString() != "u@example.com" {
				t.Errorf("state = %+v", m)
			}
		})
	}
}

func TestOrgUserImport_Invalid(t *testing.T) {
	ctx := context.Background()
	r := &orgUserResource{}

	identity := func(orgID, email types.String) *tfsdk.ResourceIdentity {
		given := nullIdentity(t, r)
		if diags := given.Set(ctx, orgUserIdentityModel{OrgID: orgID, Email: email}); diags.HasError() {
			t.Fatalf("identity: %v", diags.Errors())
		}
		return given
	}

	for name, req := range map[string]resource.ImportStateRequest{
		"id without email":         {ID: "5:", Identity: nullIdentity(t, r)},
		"id with non-numeric org":  {ID: "acme:u@example.com", Identity: nullIdentity(t, r)},
		"identity without email":   {Identity: identity(types.StringValue("5"), types.StringNull())},
		"identity non-numeric org": {Identity: identity(types.StringValue("acme"), types.StringValue("u@example.com"))},
	} {
		t.Run(name, func(t *testing.T) {
			resp := &resource.ImportStateResponse{State: nullState(t, r), Identity: nullIdentity(t, r)}
			r.ImportState(ctx, req, resp)
			if !resp.Diagnostics.HasError() {
				t.Error("import must be rejected")
			}
		})
	}
}

// Read sets the identity even when the object is gone, since prior state may have none.
func TestReadSetsIdentity(t *testing.T) {
	c := apiClient(t, map[string]string{
		certsPath + "/3": mustJSON(t, client.Cert{ID: 3, Name: "c", Type: client.CertLocationDB}),
	})

	for name, tc := range map[string]struct {
		r        identified
		id       string
		wantGone bool
	}{
		"certificate found": {&cdnCertificateResource{client: c}, "3", false},
		"certificate gone":  {&cdnCertificateResource{client: c}, "4", true},
		"purge token gone":  {&cdnPurgeTokenResource{client: c}, "4", true},
		"service gone":      {&cdnServiceResource{client: c}, "4", true},
	} {
		t.Run(name, func(t *testing.T) {
			resp := readResource(t, tc.r, map[string]string{"id": tc.id})
			if gone := resp.State.Raw.IsNull(); gone != tc.wantGone {
				t.Errorf("removed = %v, want %v", gone, tc.wantGone)
			}
			if got := identityID(t, resp.Identity); got != tc.id {
				t.Errorf("identity id = %q, want %s", got, tc.id)
			}
		})
	}
}

// readResource reads a state with the given attributes and fails on any error.
func readResource(t *testing.T, r identified, attrs map[string]string) *resource.ReadResponse {
	t.Helper()
	state := stateWith(t, r, attrs)
	resp := &resource.ReadResponse{State: state, Identity: nullIdentity(t, r)}
	r.Read(context.Background(), resource.ReadRequest{State: state, Identity: nullIdentity(t, r)}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("read: %v", resp.Diagnostics.Errors())
	}
	return resp
}

func orgUserIdentity(t *testing.T, ident *tfsdk.ResourceIdentity) orgUserIdentityModel {
	t.Helper()
	var m orgUserIdentityModel
	if diags := ident.Get(context.Background(), &m); diags.HasError() {
		t.Fatalf("identity: %v", diags.Errors())
	}
	return m
}

func TestOrgUserReadSetsIdentity(t *testing.T) {
	r := orgUserFixture(t, []client.Account{{ID: 9, Email: "u@example.com", Role: client.Role{Name: "Admin"}}}, nil)

	for name, tc := range map[string]struct {
		email    string
		wantGone bool
	}{
		"found": {"u@example.com", false},
		"gone":  {"x@example.com", true},
	} {
		t.Run(name, func(t *testing.T) {
			resp := readResource(t, r, map[string]string{"id": "5:" + tc.email, "org_id": "5", "email": tc.email})
			if gone := resp.State.Raw.IsNull(); gone != tc.wantGone {
				t.Errorf("removed = %v, want %v", gone, tc.wantGone)
			}
			if got := orgUserIdentity(t, resp.Identity); got.OrgID.ValueString() != "5" || got.Email.ValueString() != tc.email {
				t.Errorf("identity = %+v", got)
			}
		})
	}
}

// planOf builds a plan from a model of the resource.
func planOf(t *testing.T, r resource.Resource, m any) tfsdk.Plan {
	t.Helper()
	s := nullState(t, r)
	plan := tfsdk.Plan{Schema: s.Schema, Raw: s.Raw}
	if diags := plan.Set(context.Background(), m); diags.HasError() {
		t.Fatalf("plan: %v", diags.Errors())
	}
	return plan
}

func TestOrgUserCreateUpdateSetIdentity(t *testing.T) {
	ctx := context.Background()
	c := apiClient(t, map[string]string{
		whoamiPath:  mustJSON(t, client.WhoAmI{Org: client.Org{ID: 5}}),
		org5Path:    mustJSON(t, client.Org{ID: 5}),
		org5Invites: mustJSON(t, []client.OrgInvite{{Email: "p@example.com", Role: "user"}}),
	})
	r := &orgUserResource{client: c}

	plan := planOf(t, r, orgUserModel{
		OrgID: types.StringValue("5"),
		Email: types.StringValue("p@example.com"),
		Role:  types.StringValue("user"),
	})
	created := &resource.CreateResponse{State: nullState(t, r), Identity: nullIdentity(t, r)}
	r.Create(ctx, resource.CreateRequest{Plan: plan, Identity: nullIdentity(t, r)}, created)
	if created.Diagnostics.HasError() {
		t.Fatalf("create: %v", created.Diagnostics.Errors())
	}
	if got := orgUserIdentity(t, created.Identity); got.OrgID.ValueString() != "5" || got.Email.ValueString() != "p@example.com" {
		t.Errorf("create identity = %+v", got)
	}

	state := created.State
	updated := &resource.UpdateResponse{State: state, Identity: nullIdentity(t, r)}
	r.Update(ctx, resource.UpdateRequest{Plan: tfsdk.Plan(state), State: state, Identity: nullIdentity(t, r)}, updated)
	if updated.Diagnostics.HasError() {
		t.Fatalf("update: %v", updated.Diagnostics.Errors())
	}
	if got := orgUserIdentity(t, updated.Identity); got.OrgID.ValueString() != "5" || got.Email.ValueString() != "p@example.com" {
		t.Errorf("update identity = %+v", got)
	}
}

func TestServiceCreateUpdateSetIdentity(t *testing.T) {
	ctx := context.Background()
	svc := client.Service{
		ID:      7,
		Name:    "www.example.com",
		Origins: []client.ServiceOrigin{{Name: "o", Host: "origin.example.com", Port: 443, IsDefault: true}},
		Domains: []client.Domain{{ID: 3, FQDN: "www.example.com"}},
	}
	c := apiClient(t, map[string]string{
		"/api/v1/cdn/wizards":               `[{"id":1,"answerTemplates":[{"id":3}]}]`,
		servicesPath:                        mustJSON(t, client.Service{ID: 7}),
		servicesPath + "/7":                 mustJSON(t, svc),
		servicesPath + "/7/wizard/settings": wizardDefaultSettings,
	})
	r := &cdnServiceResource{client: c}

	plan := planOf(t, r, cdnServiceModel{
		Settings: types.ObjectNull(cdnSettingsObjectType().AttrTypes),
		Origins: []cdnOriginModel{{
			Name: types.StringValue("o"), Host: types.StringValue("origin.example.com"), Port: types.Int64Value(443),
			EnableTLS: types.BoolValue(false), HostHeader: types.StringValue(""), IsDefault: types.BoolValue(true),
			AllowInsecureTLS: types.BoolValue(false),
		}},
		Domains: []cdnDomainModel{{
			FQDN: types.StringValue("www.example.com"), Subdomain: types.StringValue(""), TLS: types.BoolValue(false),
		}},
	})
	created := &resource.CreateResponse{State: nullState(t, r), Identity: nullIdentity(t, r)}
	r.Create(ctx, resource.CreateRequest{Plan: plan, Config: tfsdk.Config(plan), Identity: nullIdentity(t, r)}, created)
	if created.Diagnostics.HasError() {
		t.Fatalf("create: %v", created.Diagnostics.Errors())
	}
	if got := identityID(t, created.Identity); got != "7" {
		t.Errorf("create identity id = %q, want 7", got)
	}

	state := created.State
	updated := &resource.UpdateResponse{State: state, Identity: nullIdentity(t, r)}
	r.Update(ctx, resource.UpdateRequest{Plan: tfsdk.Plan(state), Config: tfsdk.Config(state), State: state, Identity: nullIdentity(t, r)}, updated)
	if updated.Diagnostics.HasError() {
		t.Fatalf("update: %v", updated.Diagnostics.Errors())
	}
	if got := identityID(t, updated.Identity); got != "7" {
		t.Errorf("update identity id = %q, want 7", got)
	}
}

// The framework rejects a list resource without a matching resource that has identity.
func TestProviderSchemas(t *testing.T) {
	ctx := context.Background()
	server, err := testAccProtoV6ProviderFactories["varnish"]()
	if err != nil {
		t.Fatalf("server: %v", err)
	}

	want := []string{"varnish_cdn_certificate", "varnish_cdn_purge_token", "varnish_cdn_service", "varnish_org_user"}

	schemas, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil || len(schemas.Diagnostics) > 0 {
		t.Fatalf("provider schema: %v %v", err, schemas.Diagnostics)
	}
	if got := slices.Sorted(maps.Keys(schemas.ListResourceSchemas)); !slices.Equal(got, want) {
		t.Errorf("list resources = %v, want %v", got, want)
	}

	idents, err := server.GetResourceIdentitySchemas(ctx, &tfprotov6.GetResourceIdentitySchemasRequest{})
	if err != nil || len(idents.Diagnostics) > 0 {
		t.Fatalf("identity schemas: %v %v", err, idents.Diagnostics)
	}
	if got := slices.Sorted(maps.Keys(idents.IdentitySchemas)); !slices.Equal(got, want) {
		t.Errorf("identity schemas = %v, want %v", got, want)
	}
}

// A user that is gone during Update is an error, not silently dropped state.
func TestOrgUserUpdateGone(t *testing.T) {
	c := apiClient(t, map[string]string{
		org5Path:    mustJSON(t, client.Org{ID: 5}),
		org5Invites: mustJSON(t, []client.OrgInvite{}),
	})
	r := &orgUserResource{client: c}

	state := tfsdk.State(planOf(t, r, orgUserModel{
		ID:    types.StringValue("5:p@example.com"),
		OrgID: types.StringValue("5"),
		Email: types.StringValue("p@example.com"),
		Role:  types.StringValue("user"),
	}))
	resp := &resource.UpdateResponse{State: state, Identity: nullIdentity(t, r)}
	r.Update(context.Background(), resource.UpdateRequest{Plan: tfsdk.Plan(state), State: state, Identity: nullIdentity(t, r)}, resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("want an error when the user is gone")
	}
}

// Updating a certificate must keep TLS settings the config does not set.
func TestCertKeepsTLSSettings(t *testing.T) {
	resp := &resource.SchemaResponse{}
	NewCDNCertificateResource().Schema(context.Background(), resource.SchemaRequest{}, resp)

	if mods := resp.Schema.Attributes["cipher_suites"].(schema.ListAttribute).PlanModifiers; len(mods) != 1 {
		t.Errorf("cipher_suites plan modifiers = %d, want UseStateForUnknown", len(mods))
	}
	for _, name := range []string{"min_version", "max_version"} {
		if mods := resp.Schema.Attributes[name].(schema.StringAttribute).PlanModifiers; len(mods) != 1 {
			t.Errorf("%s plan modifiers = %d, want UseStateForUnknown", name, len(mods))
		}
	}
}
