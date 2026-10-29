// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/varnish/terraform-provider-varnish/internal/client"
)

func serviceConfig(fqdn, host string) string {
	return fmt.Sprintf(`
resource "varnish_cdn_service" "test" {
  domain {
    fqdn = %[1]q
    tls  = true
  }
  origin {
    name       = "default"
    host       = %[2]q
    port       = 80
    is_default = true
  }
}
`, fqdn, host)
}

func serviceConfigWithSettings(fqdn, host string) string {
	return fmt.Sprintf(`
resource "varnish_cdn_service" "test" {
  domain {
    fqdn = %[1]q
    tls  = true
  }
  origin {
    name       = "default"
    host       = %[2]q
    port       = 80
    is_default = true
  }
  settings = {
    general = {
      uncacheable = true
    }
  }
}
`, fqdn, host)
}

// allOptionsServiceConfig sets every field and every wizard setting at its
// default, to prove the document round-trips without drift.
func allOptionsServiceConfig(fqdn, host string) string {
	return fmt.Sprintf(`
resource "varnish_cdn_service" "test" {
  deployed = true

  domain {
    fqdn      = %[1]q
    subdomain = ""
    tls       = true
  }

  origin {
    name               = "default"
    host               = %[2]q
    port               = 80
    enable_tls         = false
    allow_insecure_tls = false
    host_header        = ""
    is_default         = true
  }

  settings = {
    general = {
      allow_methods                  = ["GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "CONNECT", "OPTIONS", "TRACE"]
      uncacheable                    = false
      enable_cacheable_ttl_override  = true
      cacheable_ttl_override         = "10m"
      enable_error_code_ttl_override = true
      error_code_ttl_override        = "10m"
      uncacheable_ttl                = "1m"
      first_byte_timeout             = "10s"
      websocket                      = "disable"
      allow_body_with_get            = false
      url_decode_reserved            = "enabled"
      max_req_body_size              = "10m"
      chunked_body                   = { action = "deny" }
      sharding                       = { enable = false, hash_source = "url" }
      backend_retry                  = false
      enable_url_rewrite             = false
      url_rewrite                    = "/"
    }

    high_availability = {
      enable_stale_if_error = true
      stale_if_error = {
        keep_stale_duration = "10m"
        stale_revive        = { enable = true, ttl = "1m", grace = "10m" }
      }
    }

    query_params = { remove_query_parameters = { action = "sort" } }

    headers = {
      on_auth_header             = { action = "pass" }
      unset_headers_for_delivery = []
      hash_headers               = []
      set_backend_headers        = []
      set_headers_for_delivery   = []
      true_client_ip             = false
    }

    cookies = {
      on_cookie     = { action = "remove", allow_list = [], allow_wordpress_cookies = false, allow_drupal_cookies = false }
      on_set_cookie = { action = "pass", allow_list = [], deny_list = [] }
    }

    security = {
      https_redirect            = true
      hsts                      = true
      bot_verify_settings       = "enabled"
      enable_hotlink_protection = false
      hotlink_protection        = { friends = [] }
      asn_filter = {
        enable              = false
        allow_verified_bots = true
        action              = "404"
        type                = "country_deny_list"
        country_deny_list   = []
        country_allow_list  = []
        asn_deny_list       = []
        asn_allow_list      = []
      }
      bot_protection = { vpow = false, mode = "both", captcha_ttl = "1h" }
      waf = {
        enable       = false
        mode         = "deny"
        owasp        = false
        atomic       = false
        skip_rule_id = []
        bypass       = { enable = false, acl = [] }
      }
      allow_list = { enable = false, acl = [] }
      deny_list  = { enable = false, action = "403", acl = [] }
      ratelimit = {
        enable              = false
        client_rate         = 2
        client_credit_limit = "1m"
        fetch_rate          = 1
        fetch_credit_limit  = "1m"
      }
    }

    optimisation = {
      imageopt = {
        webp    = true
        quality = 80
        imagetrans = {
          enable             = false
          width_param        = "width"
          height_param       = "height"
          allowed_dimensions = []
          allowed_widths     = []
          allowed_heights    = []
        }
      }
    }

    overrides = []
  }
}
`, fqdn, host)
}

func TestAccService_basic(t *testing.T) {
	fqdn := acctest.RandomWithPrefix("tf-acc") + ".example.com"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckServiceDestroyed,
		Steps: []resource.TestStep{
			{
				Config: serviceConfig(fqdn, "nginx-1.example.com"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("varnish_cdn_service.test", "id"),
					resource.TestCheckResourceAttr("varnish_cdn_service.test", "domain.0.fqdn", fqdn),
					resource.TestCheckResourceAttr("varnish_cdn_service.test", "origin.0.name", "default"),
					resource.TestCheckResourceAttr("varnish_cdn_service.test", "origin.0.port", "80"),
					resource.TestCheckResourceAttrSet("varnish_cdn_service.test", "origin_secret"),
					resource.TestCheckResourceAttrSet("varnish_cdn_service.test", "status"),
					testAccCheckServiceExists("varnish_cdn_service.test"),
				),
			},
			{
				ResourceName:      "varnish_cdn_service.test",
				ImportState:       true,
				ImportStateVerify: true,
				// verification runs async and can change between reads
				ImportStateVerifyIgnore: []string{
					"domain.0.verified_at",
					"domain.0.status",
				},
			},
		},
	})
}

func TestAccService_updateOriginAndSettings(t *testing.T) {
	fqdn := acctest.RandomWithPrefix("tf-acc") + ".example.com"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckServiceDestroyed,
		Steps: []resource.TestStep{
			{
				Config: serviceConfig(fqdn, "nginx-1.example.com"),
				Check:  resource.TestCheckResourceAttr("varnish_cdn_service.test", "origin.0.host", "nginx-1.example.com"),
			},
			{
				Config: serviceConfigWithSettings(fqdn, "nginx-2.example.com"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("varnish_cdn_service.test", "origin.0.host", "nginx-2.example.com"),
					testAccCheckWizardSetting("varnish_cdn_service.test", "general.uncacheable", true),
				),
			},
		},
	})
}

func TestAccService_allOptionsIdempotent(t *testing.T) {
	fqdn := acctest.RandomWithPrefix("tf-acc") + ".example.com"
	cfg := allOptionsServiceConfig(fqdn, "nginx-1.example.com")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckServiceDestroyed,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check:  testAccCheckServiceExists("varnish_cdn_service.test"),
			},
			{
				Config:   cfg,
				PlanOnly: true,
			},
		},
	})
}

// testAccCheckServiceExists confirms the service exists in the API.
func testAccCheckServiceExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		id, err := serviceIDFromState(s, resourceName)
		if err != nil {
			return err
		}
		if _, err := testAccClient().GetService(context.Background(), id); err != nil {
			return fmt.Errorf("service %d not found via the API: %w", id, err)
		}
		return nil
	}
}

// testAccCheckWizardSetting confirms a wizard field has the expected value in the API.
func testAccCheckWizardSetting(resourceName, dottedPath string, want any) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		id, err := serviceIDFromState(s, resourceName)
		if err != nil {
			return err
		}
		got, err := getWizardSettingFromAPI(id, dottedPath)
		if err != nil {
			return err
		}
		if !jsonEqual(got, want) {
			return fmt.Errorf("wizard %s = %v, want %v", dottedPath, got, want)
		}
		return nil
	}
}

// testAccCheckServiceDestroyed verifies every service in prior state is gone.
func testAccCheckServiceDestroyed(s *terraform.State) error {
	for name, rs := range s.RootModule().Resources {
		if rs.Type != "varnish_cdn_service" {
			continue
		}
		id, err := strconv.Atoi(rs.Primary.ID)
		if err != nil {
			return fmt.Errorf("%s: invalid id %q", name, rs.Primary.ID)
		}
		_, err = testAccClient().GetService(context.Background(), id)
		if err == nil {
			return fmt.Errorf("service %d still exists after destroy", id)
		}
		if !client.IsNotFound(err) {
			return fmt.Errorf("unexpected error checking destroyed service %d: %w", id, err)
		}
	}
	return nil
}

func serviceIDFromState(s *terraform.State, resourceName string) (int, error) {
	rs, ok := s.RootModule().Resources[resourceName]
	if !ok {
		return 0, fmt.Errorf("resource %s not found in state", resourceName)
	}
	return strconv.Atoi(rs.Primary.ID)
}
