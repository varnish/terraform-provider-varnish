// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/varnish/terraform-provider-varnish/internal/client"
)

// testAccProtoV6ProviderFactories wires the in-process provider for acceptance tests.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"varnish": providerserver.NewProtocol6WithError(New("test")()),
}

// A plain http endpoint sends the access token in the clear, so it is only
// allowed together with the insecure opt-out.
func TestCheckEndpoint(t *testing.T) {
	cases := map[string]struct {
		endpoint string
		insecure bool
		wantErr  bool
	}{
		"https":             {"https://api.example.com", false, false},
		"http":              {"http://api.example.com", false, true},
		"http insecure":     {"http://api.example.com", true, false},
		"no scheme":         {"api.example.com", false, true},
		"not a url":         {"://", false, true},
		"scheme only":       {"https://", false, true},
		"https with a path": {"https://api.example.com/api", false, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var diags diag.Diagnostics
			checkEndpoint(tc.endpoint, tc.insecure, &diags)
			if diags.HasError() != tc.wantErr {
				t.Errorf("checkEndpoint(%q, %v) diagnostics = %v, want error: %v", tc.endpoint, tc.insecure, diags, tc.wantErr)
			}
		})
	}
}

// testAccPreCheck checks the environment needed for acceptance tests.
func testAccPreCheck(t *testing.T) {
	if os.Getenv("VARNISH_ENDPOINT") == "" {
		t.Fatal("VARNISH_ENDPOINT must be set for acceptance tests")
	}
	if os.Getenv("VARNISH_ACCESS_TOKEN") == "" {
		t.Fatal("VARNISH_ACCESS_TOKEN must be set for acceptance tests")
	}
}

// testAccClient builds a client from the provider environment variables.
func testAccClient() *client.Client {
	return client.New(client.Config{
		Endpoint:    os.Getenv("VARNISH_ENDPOINT"),
		AccessToken: os.Getenv("VARNISH_ACCESS_TOKEN"),
		Insecure:    os.Getenv("VARNISH_INSECURE") == "true",
	})
}

// getWizardSettingFromAPI returns one live wizard setting by string id.
func getWizardSettingFromAPI(serviceID int, stringID string) (any, error) {
	settings, err := testAccClient().GetWizardSettings(context.Background(), serviceID)
	if err != nil {
		return nil, err
	}
	v, ok := settings.Settings[stringID]
	if !ok {
		return nil, fmt.Errorf("setting %q not found in the service's wizard version", stringID)
	}
	return v, nil
}

// An unknown endpoint must fail, not fall back to the default endpoint.
func TestConfigureUnknownValues(t *testing.T) {
	t.Setenv("VARNISH_ENDPOINT", "")
	ctx := context.Background()
	p := New("test")()
	schemaResp := &provider.SchemaResponse{}
	p.Schema(ctx, provider.SchemaRequest{}, schemaResp)
	objType := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)

	config := func(endpoint any) tfsdk.Config {
		return tfsdk.Config{Schema: schemaResp.Schema, Raw: tftypes.NewValue(objType, map[string]tftypes.Value{
			"endpoint":     tftypes.NewValue(tftypes.String, endpoint),
			"access_token": tftypes.NewValue(tftypes.String, "token"),
			"insecure":     tftypes.NewValue(tftypes.Bool, nil),
		})}
	}

	unknown := &provider.ConfigureResponse{}
	p.Configure(ctx, provider.ConfigureRequest{Config: config(tftypes.UnknownValue)}, unknown)
	if !unknown.Diagnostics.HasError() || unknown.ResourceData != nil {
		t.Errorf("unknown endpoint: want an error and no client, got %v", unknown.Diagnostics)
	}

	unset := &provider.ConfigureResponse{}
	p.Configure(ctx, provider.ConfigureRequest{Config: config(nil)}, unset)
	c, ok := unset.ResourceData.(*client.Client)
	if unset.Diagnostics.HasError() || !ok || c.EndpointURL() != client.DefaultEndpoint {
		t.Errorf("unset endpoint: want the default endpoint, got %v", unset.Diagnostics)
	}
}
