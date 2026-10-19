// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreatePurgeToken_RequestShape(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(TokenSet{AccessToken: "a", RefreshToken: "r", AccessExpire: 42})
	}))
	defer srv.Close()

	set, err := newTestClient(srv).CreatePurgeToken(context.Background(), NewPurgeToken{Label: "ci", Expire: "24h"})
	if err != nil {
		t.Fatalf("CreatePurgeToken: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/api/v1/cdn/sessions/purger" {
		t.Errorf("path = %s", gotPath)
	}
	// the API uses the Go field names as json keys
	if gotBody["Label"] != "ci" || gotBody["Expire"] != "24h" {
		t.Errorf("body = %v", gotBody)
	}
	// the API rejects a username without a secret, so omit empty basic auth
	if _, ok := gotBody["BasicUsername"]; ok {
		t.Errorf("empty BasicUsername was sent: %v", gotBody)
	}
	if set.AccessToken != "a" || set.RefreshToken != "r" || set.AccessExpire != 42 {
		t.Errorf("token set = %+v", set)
	}
}

func TestPurgeTokenPaths(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewEncoder(w).Encode(PurgeToken{ID: 3, Label: "ci"})
	}))
	defer srv.Close()
	c := newTestClient(srv)

	if _, err := c.GetPurgeToken(context.Background(), 3); err != nil {
		t.Fatalf("GetPurgeToken: %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/v1/cdn/sessions/purger/3" {
		t.Errorf("read = %s %s", gotMethod, gotPath)
	}

	if err := c.DeletePurgeToken(context.Background(), 3); err != nil {
		t.Fatalf("DeletePurgeToken: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/v1/cdn/sessions/purger/3" {
		t.Errorf("delete = %s %s", gotMethod, gotPath)
	}
}

// Tokens sharing the new token's label must not be picked.
func TestFindNewPurgeToken(t *testing.T) {
	list := []PurgeToken{
		{ID: 1, Label: "ci"},
		{ID: 2, Label: "other"},
		{ID: 3, Label: "ci"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(list)
	}))
	defer srv.Close()
	c := newTestClient(srv)

	got, err := c.FindNewPurgeToken(context.Background(), map[int]bool{1: true, 2: true}, "ci")
	if err != nil {
		t.Fatalf("FindNewPurgeToken: %v", err)
	}
	if got.ID != 3 {
		t.Errorf("id = %d, want 3", got.ID)
	}

	if _, err := c.FindNewPurgeToken(context.Background(), map[int]bool{1: true, 2: true, 3: true}, "ci"); err == nil {
		t.Error("want an error when nothing new appeared")
	}

	_, err = c.FindNewPurgeToken(context.Background(), nil, "ci")
	if err == nil || !strings.Contains(err.Error(), "cannot tell which one") {
		t.Errorf("want an ambiguity error, got %v", err)
	}
}

func TestIsForbidden(t *testing.T) {
	if !IsForbidden(&APIError{StatusCode: http.StatusForbidden}) {
		t.Error("403 should be forbidden")
	}
	if IsForbidden(&APIError{StatusCode: http.StatusNotFound}) {
		t.Error("404 is not forbidden")
	}
	if IsForbidden(context.Canceled) {
		t.Error("a non-API error is not forbidden")
	}
}
