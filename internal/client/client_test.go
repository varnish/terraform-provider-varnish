// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// newTestClient returns a Client pointed at srv with a fixed token.
func newTestClient(srv *httptest.Server) *Client {
	return New(Config{Endpoint: srv.URL, AccessToken: "test-token"})
}

func TestGetService_RequestShape(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		_ = json.NewEncoder(w).Encode(Service{ID: 7, Name: "svc"})
	}))
	defer srv.Close()

	svc, err := newTestClient(srv).GetService(context.Background(), 7)
	if err != nil {
		t.Fatalf("GetService: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/api/v1/cdn/services/7" {
		t.Errorf("path = %q, want /api/v1/cdn/services/7", gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("auth = %q, want Bearer test-token", gotAuth)
	}
	if gotAccept != "application/json" {
		t.Errorf("accept = %q", gotAccept)
	}
	if svc.ID != 7 || svc.Name != "svc" {
		t.Errorf("decoded = %+v", svc)
	}
}

func TestSetOrigins_SendsFullArrayViaPut(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody []ServiceOrigin
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	origins := []ServiceOrigin{
		{Name: "default", Host: "a", Port: 80, IsDefault: true},
		{Name: "second", Host: "b", Port: 443, EnableTLS: true},
	}
	if err := newTestClient(srv).SetOrigins(context.Background(), 3, origins); err != nil {
		t.Fatalf("SetOrigins: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if gotPath != "/api/v1/cdn/services/3/origins" {
		t.Errorf("path = %q", gotPath)
	}
	if len(gotBody) != 2 || gotBody[1].Name != "second" {
		t.Errorf("body = %+v, want full 2-origin array", gotBody)
	}
}

func TestDo_NotFoundMapsToAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"nope"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetService(context.Background(), 99)
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsNotFound(err) {
		t.Errorf("IsNotFound = false, want true for err %v", err)
	}
}

func TestCreateCert_PostsToCerts(t *testing.T) {
	var gotMethod, gotPath, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		_ = json.NewEncoder(w).Encode(Cert{ID: 1, Name: "c"})
	}))
	defer srv.Close()

	cert, err := newTestClient(srv).CreateCert(context.Background(), Cert{Name: "c"})
	if err != nil {
		t.Fatalf("CreateCert: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/cdn/certs" {
		t.Errorf("got %s %s", gotMethod, gotPath)
	}
	if gotContentType != "application/json" {
		t.Errorf("content-type = %q", gotContentType)
	}
	if cert.ID != 1 {
		t.Errorf("cert = %+v", cert)
	}
}

func TestWizardSettings_RoundTrip(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody CDNWizardSettings
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		if r.Method == http.MethodPut {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			_ = json.NewEncoder(w).Encode(Service{ID: 7})
			return
		}
		_, _ = w.Write([]byte(`{"wizardVersion":41,"settings":{"general.uncacheable":false},` +
			`"overrides":[{"name":"imgs","paths":[],"extensions":["jpg"],"originName":"default",` +
			`"rules":[{"stringId":"general.uncacheable","value":true}]}]}`))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	got, err := c.GetWizardSettings(context.Background(), 7)
	if err != nil {
		t.Fatalf("GetWizardSettings: %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/v1/cdn/services/7/wizard/settings" {
		t.Errorf("got %s %s", gotMethod, gotPath)
	}
	if got.WizardVersion != 41 || got.Settings["general.uncacheable"] != false {
		t.Errorf("settings = %+v", got)
	}
	if len(got.Overrides) != 1 || got.Overrides[0].OriginName != "default" ||
		string(got.Overrides[0].Rules[0].Value) != "true" {
		t.Errorf("overrides = %+v", got.Overrides)
	}

	if err := c.PutWizardSettings(context.Background(), 7, got); err != nil {
		t.Fatalf("PutWizardSettings: %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/v1/cdn/services/7/wizard/settings" {
		t.Errorf("got %s %s", gotMethod, gotPath)
	}
	if !reflect.DeepEqual(&gotBody, got) {
		t.Errorf("wrote %+v, want %+v", gotBody, got)
	}
}

// A redirect must not be followed: replaying the request would hand the bearer
// token to whatever host the redirect names.
func TestDoOnce_DoesNotFollowRedirects(t *testing.T) {
	var target *httptest.Server
	target = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the redirect target was called with %q", r.Header.Get("Authorization"))
	}))
	defer target.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/api/v1/cdn/services/7", http.StatusFound)
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetService(context.Background(), 7)
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.StatusCode != http.StatusFound {
		t.Fatalf("err = %v, want a 302 APIError", err)
	}
}

// An oversized error body must not be carried around whole.
func TestDoOnce_TruncatesErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write(bytes.Repeat([]byte("x"), maxErrorBodyBytes*4))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetService(context.Background(), 7)
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("err = %v, want an APIError", err)
	}
	if len(apiErr.Body) > maxErrorBodyBytes+len("... (truncated)") {
		t.Errorf("error body kept %d bytes", len(apiErr.Body))
	}
}

// The API rejects an absent overrides key, so a nil slice must go out as [].
func TestPutWizardSettings_AlwaysSendsOverrides(t *testing.T) {
	var gotBody map[string]json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
	}))
	defer srv.Close()

	in := &CDNWizardSettings{WizardVersion: 41, Settings: map[string]any{}}
	if err := newTestClient(srv).PutWizardSettings(context.Background(), 7, in); err != nil {
		t.Fatalf("PutWizardSettings: %v", err)
	}
	if string(gotBody["overrides"]) != "[]" {
		t.Errorf("overrides = %s, want []", gotBody["overrides"])
	}
	if in.Overrides != nil {
		t.Error("the caller's document was modified")
	}
}

// A wrapped API error must still be recognised.
func TestIsNotFoundWrapped(t *testing.T) {
	err := fmt.Errorf("read service: %w", &APIError{StatusCode: http.StatusNotFound})
	if !IsNotFound(err) {
		t.Error("IsNotFound missed a wrapped 404")
	}
	if IsForbidden(err) {
		t.Error("IsForbidden matched a 404")
	}
}
