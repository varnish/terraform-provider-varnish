// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/varnish/terraform-provider-varnish/internal/client"
)

// orgUserFixture serves the org members and invite endpoints.
func orgUserFixture(t *testing.T, accounts []client.Account, invites []client.OrgInvite) *orgUserResource {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/cdn/orgs/5":
			_ = json.NewEncoder(w).Encode(client.Org{ID: 5, Accounts: accounts})
		case "/api/v1/cdn/orgs/5/invite":
			_ = json.NewEncoder(w).Encode(invites)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &orgUserResource{client: client.New(client.Config{Endpoint: srv.URL, AccessToken: "t"})}
}

func TestLookup_PrefersMemberOverPending(t *testing.T) {
	// Same email appears as both an accepted member and a stale pending invite.
	r := orgUserFixture(t,
		[]client.Account{{ID: 9, Email: "u@example.com", Role: client.Role{ID: 2, Name: "admin"}}},
		[]client.OrgInvite{{Email: "u@example.com", Role: "user"}},
	)

	got, err := r.lookup(context.Background(), 5, "u@example.com", "user")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got == nil {
		t.Fatal("lookup returned nil, want member")
	}
	if got.Status.ValueString() != statusMember {
		t.Errorf("status = %q, want member", got.Status.ValueString())
	}
	if got.AccountID.ValueString() != "9" {
		t.Errorf("account_id = %q, want 9", got.AccountID.ValueString())
	}
	if got.Role.ValueString() != "admin" {
		t.Errorf("role = %q, want admin (from member, not invite)", got.Role.ValueString())
	}
}

func TestLookup_PendingWhenNotMember(t *testing.T) {
	r := orgUserFixture(t,
		[]client.Account{{ID: 1, Email: "other@example.com"}},
		[]client.OrgInvite{{Email: "p@example.com", Role: "user"}},
	)

	got, err := r.lookup(context.Background(), 5, "p@example.com", "")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got == nil {
		t.Fatal("lookup returned nil, want pending")
	}
	if got.Status.ValueString() != statusPending {
		t.Errorf("status = %q, want pending", got.Status.ValueString())
	}
	if !got.AccountID.IsNull() {
		t.Errorf("account_id = %q, want null while pending", got.AccountID.ValueString())
	}
	if got.Role.ValueString() != "user" {
		t.Errorf("role = %q, want user", got.Role.ValueString())
	}
}

func TestLookup_GoneReturnsNil(t *testing.T) {
	r := orgUserFixture(t, nil, nil)
	got, err := r.lookup(context.Background(), 5, "missing@example.com", "user")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got != nil {
		t.Errorf("lookup = %+v, want nil (user gone)", got)
	}
}
