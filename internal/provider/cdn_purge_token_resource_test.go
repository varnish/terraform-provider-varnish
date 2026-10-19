// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/varnish/terraform-provider-varnish/internal/client"
)

// purgeTokenFixture serves read and list. readStatus sets the single read status.
func purgeTokenFixture(t *testing.T, readStatus int, list []client.PurgeToken) *cdnPurgeTokenResource {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const listPath = "/api/v1/cdn/sessions/purger"
		if r.URL.Path == listPath {
			_ = json.NewEncoder(w).Encode(list)
			return
		}
		if !strings.HasPrefix(r.URL.Path, listPath+"/") {
			http.NotFound(w, r)
			return
		}
		if readStatus != http.StatusOK {
			w.WriteHeader(readStatus)
			return
		}
		_ = json.NewEncoder(w).Encode(client.PurgeToken{ID: 7, Label: "direct-read"})
	}))
	t.Cleanup(srv.Close)
	return &cdnPurgeTokenResource{client: client.New(client.Config{Endpoint: srv.URL, AccessToken: "t"})}
}

func TestReadToken_UsesDirectRead(t *testing.T) {
	r := purgeTokenFixture(t, http.StatusOK, nil)

	got, err := r.readToken(context.Background(), 7)
	if err != nil {
		t.Fatalf("readToken: %v", err)
	}
	if got == nil || got.Label != "direct-read" {
		t.Errorf("token = %+v, want the per-token read result", got)
	}
}

func TestReadToken_GoneReturnsNil(t *testing.T) {
	r := purgeTokenFixture(t, http.StatusNotFound, nil)

	got, err := r.readToken(context.Background(), 7)
	if err != nil {
		t.Fatalf("readToken: %v", err)
	}
	if got != nil {
		t.Errorf("token = %+v, want nil so the resource leaves state", got)
	}
}

// Some roles may list purge tokens but not read one directly.
func TestReadToken_FallsBackToListOn403(t *testing.T) {
	r := purgeTokenFixture(t, http.StatusForbidden, []client.PurgeToken{
		{ID: 6, Label: "other"},
		{ID: 7, Label: "from-list"},
	})

	got, err := r.readToken(context.Background(), 7)
	if err != nil {
		t.Fatalf("readToken: %v", err)
	}
	if got == nil || got.Label != "from-list" {
		t.Errorf("token = %+v, want the list entry", got)
	}
}

// Roles that get 403 on every single read rely on the list: absent from it means gone.
func TestReadToken_ForbiddenAndAbsentIsGone(t *testing.T) {
	r := purgeTokenFixture(t, http.StatusForbidden, []client.PurgeToken{{ID: 6}})

	got, err := r.readToken(context.Background(), 7)
	if err != nil || got != nil {
		t.Fatalf("readToken = %+v, %v, want nil, nil", got, err)
	}
}

// The JWT expiries come back as unix seconds; 0 means the API did not set one.
func TestUnixToTF(t *testing.T) {
	if got := unixToTF(0); !got.IsNull() {
		t.Errorf("unset expiry = %v, want null", got)
	}
	if got := unixToTF(1700000000); got != types.StringValue("2023-11-14T22:13:20Z") {
		t.Errorf("expiry = %v", got)
	}
}

func TestApplyPurgeToken(t *testing.T) {
	var m cdnPurgeTokenModel
	applyPurgeToken(&m, &client.PurgeToken{ID: 7, OrgID: 3, Label: "ci"})

	if m.ID != types.StringValue("7") || m.OrgID != types.StringValue("3") || m.Label != types.StringValue("ci") {
		t.Errorf("model = %+v", m)
	}
	if !m.ExpiresAt.IsNull() {
		t.Errorf("zero expire should stay null, got %v", m.ExpiresAt)
	}
}

func TestApplyPurgeTokenSecrets(t *testing.T) {
	var m cdnPurgeTokenModel
	applyPurgeTokenSecrets(&m, &client.TokenSet{AccessToken: "a", RefreshToken: "r", AccessExpire: 1700000000})

	if m.AccessToken != types.StringValue("a") || m.RefreshToken != types.StringValue("r") {
		t.Errorf("tokens = %v / %v", m.AccessToken, m.RefreshToken)
	}
	if m.AccessTokenExpiresAt.IsNull() {
		t.Error("access token expiry should be set")
	}
	if !m.RefreshTokenExpiresAt.IsNull() {
		t.Errorf("missing refresh expiry should stay null, got %v", m.RefreshTokenExpiresAt)
	}
}
