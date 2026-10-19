// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/varnish/terraform-provider-varnish/internal/client"
)

func TestRequiresReplaceAnswerTemplate(t *testing.T) {
	tests := []struct {
		name  string
		state types.Int64
		want  bool
	}{
		// old services report no template id
		{"null state adopts", types.Int64Null(), false},
		{"non-null state replaces", types.Int64Value(1), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := &int64planmodifier.RequiresReplaceIfFuncResponse{}
			cdnRequiresReplaceAnswerTemplate(context.Background(), planmodifier.Int64Request{StateValue: tc.state}, resp)
			if resp.RequiresReplace != tc.want {
				t.Errorf("RequiresReplace = %v, want %v", resp.RequiresReplace, tc.want)
			}
		})
	}
}

// The settings schema and its attr types are built separately and must agree.
func TestServiceSchema(t *testing.T) {
	ctx := context.Background()
	resp := &resource.SchemaResponse{}
	NewCDNServiceResource().Schema(ctx, resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema: %v", resp.Diagnostics.Errors())
	}
	if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Fatalf("invalid schema: %v", diags.Errors())
	}

	settings, diags := resp.Schema.AttributeAtPath(ctx, path.Root("settings"))
	if diags.HasError() {
		t.Fatalf("settings attribute: %v", diags.Errors())
	}
	if !settings.GetType().Equal(cdnSettingsObjectType()) {
		t.Errorf("settings type = %v, want %v", settings.GetType(), cdnSettingsObjectType())
	}
}

// newSettingsServer serves the default settings and records requests and the last write.
func newSettingsServer(t *testing.T, methods *[]string, written *client.CDNWizardSettings) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*methods = append(*methods, r.Method)
		if r.Method == http.MethodPut {
			if err := json.NewDecoder(r.Body).Decode(written); err != nil {
				t.Errorf("decode written settings: %v", err)
			}
		}
		_, _ = w.Write([]byte(wizardDefaultSettings))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A failed settings read is a warning on refresh and an error on import.
func TestRefreshSettings_FailureIsFatalOnlyOnImport(t *testing.T) {
	ctx := context.Background()

	for name, body := range map[string]string{
		"unreachable settings": "",
		"invalid value":        strings.Replace(wizardDefaultSettings, `"general.uncacheable": false`, `"general.uncacheable": "yes"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if body == "" {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				_, _ = w.Write([]byte(body))
			}))
			t.Cleanup(srv.Close)
			r := &cdnServiceResource{client: client.New(client.Config{Endpoint: srv.URL, AccessToken: "test-token"})}

			prior, diags := cdnSettingsFromAPI(ctx, defaultSettings(t))
			if diags.HasError() {
				t.Fatalf("cdnSettingsFromAPI: %v", diags.Errors())
			}

			state := cdnServiceModel{Settings: prior}
			diags = r.refreshSettings(ctx, 7, &state, false)
			if diags.HasError() {
				t.Errorf("a refresh failure must be a warning: %v", diags.Errors())
			}
			hatch := false
			for _, w := range diags.Warnings() {
				hatch = hatch || strings.Contains(w.Detail(), "-refresh=false")
			}
			if !hatch {
				t.Errorf("no warning points at the destroy escape hatch: %v", diags.Warnings())
			}
			if !state.Settings.Equal(prior) {
				t.Error("the settings in state were not kept")
			}

			imported := cdnServiceModel{Settings: types.ObjectNull(cdnSettingsObjectType().AttrTypes)}
			diags = r.refreshSettings(ctx, 7, &imported, true)
			if !diags.HasError() {
				t.Fatal("an import failure must be an error")
			}
			if detail := diags.Errors()[0].Detail(); !strings.Contains(detail, "could not be imported") {
				t.Errorf("error does not say the import failed: %s", detail)
			}
		})
	}
}

func TestApplySettings_SkipsWriteWhenUnchanged(t *testing.T) {
	var methods []string
	srv := newSettingsServer(t, &methods, &client.CDNWizardSettings{})
	r := &cdnServiceResource{client: client.New(client.Config{Endpoint: srv.URL, AccessToken: "test-token"})}

	// no managed settings, or unchanged values, must not write
	for _, obj := range []types.Object{
		types.ObjectNull(cdnSettingsObjectType().AttrTypes),
		settingsObjectWith(t, "general.uncacheable", types.BoolValue(false)),
	} {
		methods = nil
		if _, diags := r.applySettings(context.Background(), 7, obj); diags.HasError() {
			t.Fatalf("applySettings: %v", diags.Errors())
		}
		if want := []string{http.MethodGet}; !slices.Equal(methods, want) {
			t.Errorf("methods = %v, want %v", methods, want)
		}
	}
}

// A setting the service's wizard version does not have must fail before any write.
func TestApplySettings_RejectsMissingSetting(t *testing.T) {
	var methods []string
	body := strings.Replace(wizardDefaultSettings, `    "general.urlRewrite": "/",`+"\n", "", 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	r := &cdnServiceResource{client: client.New(client.Config{Endpoint: srv.URL, AccessToken: "test-token"})}

	obj := settingsObjectWith(t, "general.urlRewrite", types.StringValue("/new"))
	_, diags := r.applySettings(context.Background(), 7, obj)
	if !diags.HasError() || !strings.Contains(diags.Errors()[0].Detail(), "general.urlRewrite") {
		t.Fatalf("want an error naming general.urlRewrite, got %v", diags)
	}
	if want := []string{http.MethodGet}; !slices.Equal(methods, want) {
		t.Errorf("methods = %v, want %v", methods, want)
	}
}

func TestApplySettings_WritesFullDocument(t *testing.T) {
	var methods []string
	var written client.CDNWizardSettings
	srv := newSettingsServer(t, &methods, &written)
	r := &cdnServiceResource{client: client.New(client.Config{Endpoint: srv.URL, AccessToken: "test-token"})}

	obj := settingsObjectWith(t, "general.uncacheable", types.BoolValue(true))
	if _, diags := r.applySettings(context.Background(), 7, obj); diags.HasError() {
		t.Fatalf("applySettings: %v", diags.Errors())
	}

	if want := []string{http.MethodGet, http.MethodPut, http.MethodGet}; !slices.Equal(methods, want) {
		t.Fatalf("methods = %v, want %v", methods, want)
	}
	if written.Settings["general.uncacheable"] != true {
		t.Errorf("written general.uncacheable = %v, want true", written.Settings["general.uncacheable"])
	}
	if len(written.Settings) != len(defaultSettings(t).Settings) {
		t.Errorf("wrote %d settings, want the full document", len(written.Settings))
	}
	if written.WizardVersion != defaultSettings(t).WizardVersion {
		t.Errorf("wizard version = %d, want the one read back", written.WizardVersion)
	}
}

// originConfig builds a config with only the origin block set.
func originConfig(t *testing.T, origins func(tftypes.List) tftypes.Value) tfsdk.Config {
	t.Helper()
	ctx := context.Background()
	resp := &resource.SchemaResponse{}
	NewCDNServiceResource().Schema(ctx, resource.SchemaRequest{}, resp)

	objType := resp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	vals := make(map[string]tftypes.Value, len(objType.AttributeTypes))
	for name, at := range objType.AttributeTypes {
		vals[name] = tftypes.NewValue(at, nil)
	}
	vals["origin"] = origins(objType.AttributeTypes["origin"].(tftypes.List))
	return tfsdk.Config{Schema: resp.Schema, Raw: tftypes.NewValue(objType, vals)}
}

// originValue builds an origin block with the fields validation reads.
func originValue(t *testing.T, list tftypes.List, name string, isDefault bool) tftypes.Value {
	t.Helper()
	elem := list.ElementType.(tftypes.Object)
	vals := make(map[string]tftypes.Value, len(elem.AttributeTypes))
	for n, at := range elem.AttributeTypes {
		vals[n] = tftypes.NewValue(at, nil)
	}
	vals["name"] = tftypes.NewValue(tftypes.String, name)
	vals["is_default"] = tftypes.NewValue(tftypes.Bool, isDefault)
	return tftypes.NewValue(elem, vals)
}

// An unknown origin list, from a dynamic block, must be skipped.
func TestServiceValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		origins func(tftypes.List) tftypes.Value
		wantErr bool
	}{
		{"unknown list", func(l tftypes.List) tftypes.Value {
			return tftypes.NewValue(l, tftypes.UnknownValue)
		}, false},
		{"unknown element", func(l tftypes.List) tftypes.Value {
			return tftypes.NewValue(l, []tftypes.Value{tftypes.NewValue(l.ElementType, tftypes.UnknownValue)})
		}, false},
		{"no origins", func(l tftypes.List) tftypes.Value {
			return tftypes.NewValue(l, []tftypes.Value{})
		}, false},
		{"one default", func(l tftypes.List) tftypes.Value {
			return tftypes.NewValue(l, []tftypes.Value{
				originValue(t, l, "a", true), originValue(t, l, "b", false),
			})
		}, false},
		{"no default", func(l tftypes.List) tftypes.Value {
			return tftypes.NewValue(l, []tftypes.Value{originValue(t, l, "a", false)})
		}, true},
		{"two defaults", func(l tftypes.List) tftypes.Value {
			return tftypes.NewValue(l, []tftypes.Value{
				originValue(t, l, "a", true), originValue(t, l, "b", true),
			})
		}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := &resource.ValidateConfigResponse{}
			NewCDNServiceResource().(*cdnServiceResource).ValidateConfig(context.Background(),
				resource.ValidateConfigRequest{Config: originConfig(t, tc.origins)}, resp)
			if got := resp.Diagnostics.HasError(); got != tc.wantErr {
				t.Errorf("error = %v, want %v: %v", got, tc.wantErr, resp.Diagnostics)
			}
		})
	}
}
