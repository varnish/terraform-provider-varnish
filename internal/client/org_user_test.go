// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInviteToOrg_PathAndQuery(t *testing.T) {
	var gotMethod, gotPath, gotEmail, gotRole string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotEmail = r.URL.Query().Get("email")
		gotRole = r.URL.Query().Get("role")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := newTestClient(srv).InviteToOrg(context.Background(), 5, "a+b@example.com", "admin"); err != nil {
		t.Fatalf("InviteToOrg: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/cdn/orgs/5/invite" {
		t.Errorf("path = %q", gotPath)
	}
	if gotEmail != "a+b@example.com" {
		t.Errorf("email = %q, want url-decoded a+b@example.com", gotEmail)
	}
	if gotRole != "admin" {
		t.Errorf("role = %q", gotRole)
	}
}

func TestListOrgInvites_Decodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/cdn/orgs/5/invite" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]OrgInvite{{Email: "p@example.com", Role: "user"}})
	}))
	defer srv.Close()

	invites, err := newTestClient(srv).ListOrgInvites(context.Background(), 5)
	if err != nil {
		t.Fatalf("ListOrgInvites: %v", err)
	}
	if len(invites) != 1 || invites[0].Email != "p@example.com" || invites[0].Role != "user" {
		t.Errorf("invites = %+v", invites)
	}
}

func TestUninviteFromOrg_DeleteWithEmail(t *testing.T) {
	var gotMethod, gotPath, gotEmail string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotEmail = r.URL.Query().Get("email")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := newTestClient(srv).UninviteFromOrg(context.Background(), 5, "p@example.com"); err != nil {
		t.Fatalf("UninviteFromOrg: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	if gotPath != "/api/v1/cdn/orgs/5/invite" {
		t.Errorf("path = %q", gotPath)
	}
	if gotEmail != "p@example.com" {
		t.Errorf("email = %q", gotEmail)
	}
}

func TestUnassignAccount_BodyShape(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := newTestClient(srv).UnassignAccount(context.Background(), 5, 42); err != nil {
		t.Fatalf("UnassignAccount: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if gotPath != "/api/v1/cdn/orgs/5/unassign" {
		t.Errorf("path = %q", gotPath)
	}
	// the API reads only "id"
	if string(gotBody["id"]) != "42" {
		t.Errorf("body id = %s, want 42 (body=%v)", gotBody["id"], gotBody)
	}
}

func TestListOrgRoles_Decodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/cdn/orgs/5/roles" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]Role{{ID: 1, Name: "user"}, {ID: 2, Name: "admin"}})
	}))
	defer srv.Close()

	roles, err := newTestClient(srv).ListOrgRoles(context.Background(), 5)
	if err != nil {
		t.Fatalf("ListOrgRoles: %v", err)
	}
	if len(roles) != 2 || roles[1].Name != "admin" || roles[1].ID != 2 {
		t.Errorf("roles = %+v", roles)
	}
}

func TestUpdateAccountRole_BodyShape(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody Account
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Carry the member's current name/email; the role is sent as a relation.
	acct := Account{Name: "alice", Email: "alice@example.com", Password: "secret"}
	if err := newTestClient(srv).UpdateAccountRole(context.Background(), 42, 2, acct); err != nil {
		t.Fatalf("UpdateAccountRole: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if gotPath != "/api/v1/cdn/accounts/42" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody.ID != 42 {
		t.Errorf("body id = %d, want 42", gotBody.ID)
	}
	if gotBody.Role.ID != 2 {
		t.Errorf("body role.id = %d, want 2", gotBody.Role.ID)
	}
	if gotBody.Name != "alice" || gotBody.Email != "alice@example.com" {
		t.Errorf("name/email not carried: %+v", gotBody)
	}
	if gotBody.Password != "" {
		t.Errorf("password = %q, want cleared to preserve existing password", gotBody.Password)
	}
}

func TestGetOrg_DecodesAccounts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/cdn/orgs/5" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(Org{
			ID:       5,
			Accounts: []Account{{ID: 9, Email: "m@example.com", Role: Role{ID: 2, Name: "admin"}}},
		})
	}))
	defer srv.Close()

	org, err := newTestClient(srv).GetOrg(context.Background(), 5)
	if err != nil {
		t.Fatalf("GetOrg: %v", err)
	}
	if len(org.Accounts) != 1 || org.Accounts[0].Email != "m@example.com" || org.Accounts[0].Role.Name != "admin" {
		t.Errorf("org = %+v", org)
	}
}
