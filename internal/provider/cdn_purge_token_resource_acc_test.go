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

func TestAccPurgeToken_basic(t *testing.T) {
	label := acctest.RandomWithPrefix("tf-acc-purger")
	relabel := acctest.RandomWithPrefix("tf-acc-purger")

	config := func(l string) string {
		return fmt.Sprintf(`
resource "varnish_cdn_purge_token" "test" {
  label  = %[1]q
  expire = "24h"
}
`, l)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckPurgeTokenDestroyed,
		Steps: []resource.TestStep{
			{
				Config: config(label),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("varnish_cdn_purge_token.test", "id"),
					resource.TestCheckResourceAttr("varnish_cdn_purge_token.test", "label", label),
					resource.TestCheckResourceAttrSet("varnish_cdn_purge_token.test", "access_token"),
					resource.TestCheckResourceAttrSet("varnish_cdn_purge_token.test", "expires_at"),
					testAccCheckPurgeTokenExists("varnish_cdn_purge_token.test"),
				),
			},
			{
				// A new label is a new token: the old one is revoked.
				Config: config(relabel),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("varnish_cdn_purge_token.test", "label", relabel),
					testAccCheckPurgeTokenExists("varnish_cdn_purge_token.test"),
				),
			},
			{
				ResourceName:      "varnish_cdn_purge_token.test",
				ImportState:       true,
				ImportStateVerify: true,
				// an imported token has no JWTs
				ImportStateVerifyIgnore: []string{
					"access_token", "refresh_token",
					"access_token_expires_at", "refresh_token_expires_at",
					"expire",
				},
			},
		},
	})
}

func testAccCheckPurgeTokenExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("resource %s not found", resourceName)
		}
		id, err := strconv.Atoi(rs.Primary.ID)
		if err != nil {
			return fmt.Errorf("invalid id %q", rs.Primary.ID)
		}
		token, err := testAccPurgeTokenRead(id)
		if err != nil {
			return fmt.Errorf("purge token %d: %w", id, err)
		}
		if token == nil {
			return fmt.Errorf("purge token %d not found via the API", id)
		}
		return nil
	}
}

func testAccCheckPurgeTokenDestroyed(s *terraform.State) error {
	for name, rs := range s.RootModule().Resources {
		if rs.Type != "varnish_cdn_purge_token" {
			continue
		}
		id, err := strconv.Atoi(rs.Primary.ID)
		if err != nil {
			return fmt.Errorf("%s: invalid id %q", name, rs.Primary.ID)
		}
		token, err := testAccPurgeTokenRead(id)
		if err != nil {
			return fmt.Errorf("unexpected error checking destroyed purge token %d: %w", id, err)
		}
		if token != nil {
			return fmt.Errorf("purge token %d still exists after destroy", id)
		}
	}
	return nil
}

// testAccPurgeTokenRead reads like the resource does, so roles that get 403 on a single read work too.
func testAccPurgeTokenRead(id int) (*client.PurgeToken, error) {
	return (&cdnPurgeTokenResource{client: testAccClient()}).readToken(context.Background(), id)
}
