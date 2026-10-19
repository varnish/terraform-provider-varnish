// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/varnish/terraform-provider-varnish/internal/client"
)

// writeSelfSignedCert writes a self-signed PEM cert and key for cn into dir.
func writeSelfSignedCert(t *testing.T, dir, cn string) (certPath, keyPath string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     []string{cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("createcert: %v", err)
	}

	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certPath, keyPath
}

func TestAccCertificate_basic(t *testing.T) {
	dir := t.TempDir()
	name := acctest.RandomWithPrefix("tf-acc-cert")
	certPath, keyPath := writeSelfSignedCert(t, dir, "tf-acc.example.com")

	config := fmt.Sprintf(`
resource "varnish_cdn_certificate" "test" {
  name        = %[1]q
  certificate = file(%[2]q)
  private_key = file(%[3]q)
}
`, name, certPath, keyPath)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCertificateDestroyed,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("varnish_cdn_certificate.test", "id"),
					resource.TestCheckResourceAttr("varnish_cdn_certificate.test", "name", name),
					resource.TestCheckResourceAttrSet("varnish_cdn_certificate.test", "common_name"),
					testAccCheckCertificateExists("varnish_cdn_certificate.test"),
				),
			},
			{
				ResourceName:      "varnish_cdn_certificate.test",
				ImportState:       true,
				ImportStateVerify: true,
				// private_key and certificate bodies are not returned by the API on read.
				ImportStateVerifyIgnore: []string{"private_key", "certificate"},
			},
		},
	})
}

func testAccCheckCertificateExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("resource %s not found", resourceName)
		}
		id, err := strconv.Atoi(rs.Primary.ID)
		if err != nil {
			return fmt.Errorf("invalid id %q", rs.Primary.ID)
		}
		if _, err := testAccClient().GetCert(context.Background(), id); err != nil {
			return fmt.Errorf("certificate %d not found via the API: %w", id, err)
		}
		return nil
	}
}

func testAccCheckCertificateDestroyed(s *terraform.State) error {
	for name, rs := range s.RootModule().Resources {
		if rs.Type != "varnish_cdn_certificate" {
			continue
		}
		id, err := strconv.Atoi(rs.Primary.ID)
		if err != nil {
			return fmt.Errorf("%s: invalid id %q", name, rs.Primary.ID)
		}
		_, err = testAccClient().GetCert(context.Background(), id)
		if err == nil {
			return fmt.Errorf("certificate %d still exists after destroy", id)
		}
		if !client.IsNotFound(err) {
			return fmt.Errorf("unexpected error checking destroyed certificate %d: %w", id, err)
		}
	}
	return nil
}
