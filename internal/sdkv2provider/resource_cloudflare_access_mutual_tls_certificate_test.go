package sdkv2provider

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/cloudflare/cloudflare-go"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func init() {
	resource.AddTestSweepers("cloudflare_access_mutual_tls_certificate", &resource.Sweeper{
		Name: "cloudflare_access_mutual_tls_certificate",
		F:    testSweepCloudflareAccessMutualTLSCertificate,
	})
}

func testSweepCloudflareAccessMutualTLSCertificate(r string) error {
	ctx := context.Background()

	client, clientErr := sharedClient()
	if clientErr != nil {
		tflog.Error(ctx, fmt.Sprintf("Failed to create Cloudflare client: %s", clientErr))
	}

	accountID := os.Getenv("CLOUDFLARE_ACCOUNT_ID")

	accountCerts, err := client.AccessMutualTLSCertificates(context.Background(), accountID)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Failed to fetch Cloudflare Access Mutual TLS certificates: %s", err))
	}

	for _, cert := range accountCerts {
		err := client.DeleteAccessMutualTLSCertificate(context.Background(), accountID, cert.ID)

		if err != nil {
			tflog.Error(ctx, fmt.Sprintf("Failed to delete Cloudflare Access Mutual TLS certificate (%s) in account ID: %s", cert.ID, accountID))
		}
	}

	zoneID := os.Getenv("CLOUDFLARE_ZONE_ID")
	zoneCerts, err := client.ZoneAccessMutualTLSCertificates(context.Background(), zoneID)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Failed to fetch Cloudflare Access Mutual TLS certificates: %s", err))
	}

	for _, cert := range zoneCerts {
		err := client.DeleteZoneAccessMutualTLSCertificate(context.Background(), zoneID, cert.ID)

		if err != nil {
			tflog.Error(ctx, fmt.Sprintf("Failed to delete Cloudflare Access Mutual TLS certificate (%s) in zone ID: %s", cert.ID, zoneID))
		}
	}

	return nil
}

func TestAccCloudflareAccessMutualTLSBasic(t *testing.T) {
	// Temporarily unset CLOUDFLARE_API_TOKEN if it is set as the Access
	// service does not yet support the API tokens and it results in
	// misleading state error messages.
	if os.Getenv("CLOUDFLARE_API_TOKEN") != "" {
		t.Setenv("CLOUDFLARE_API_TOKEN", "")
	}

	rnd := generateRandomResourceName()
	name := fmt.Sprintf("cloudflare_access_mutual_tls_certificate.%s", rnd)
	cert := os.Getenv("CLOUDFLARE_MUTUAL_TLS_CERTIFICATE")
	domain := os.Getenv("CLOUDFLARE_DOMAIN")

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testAccPreCheckAccount(t)
		},
		ProviderFactories: providerFactories,
		CheckDestroy:      testAccCheckCloudflareAccessMutualTLSCertificateDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccessMutualTLSCertificateConfigBasic(rnd, AccessIdentifier{Type: AccountType, Value: accountID}, cert, domain),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(name, "account_id", accountID),
					resource.TestCheckResourceAttr(name, "name", rnd),
					resource.TestCheckResourceAttrSet(name, "certificate"),
					resource.TestCheckResourceAttr(name, "associated_hostnames.0", domain),
				),
			},
			{
				Config: testAccessMutualTLSCertificateUpdated(rnd, AccessIdentifier{Type: AccountType, Value: accountID}, cert),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(name, "account_id", accountID),
					resource.TestCheckResourceAttr(name, "name", rnd),
					resource.TestCheckResourceAttrSet(name, "certificate"),
					resource.TestCheckResourceAttr(name, "associated_hostnames.#", "0"),
				),
			},
		},
	})
}

func TestAccCloudflareAccessMutualTLSBasicWithZoneID(t *testing.T) {
	// Temporarily unset CLOUDFLARE_API_TOKEN if it is set as the Access
	// service does not yet support the API tokens and it results in
	// misleading state error messages.
	if os.Getenv("CLOUDFLARE_API_TOKEN") != "" {
		t.Setenv("CLOUDFLARE_API_TOKEN", "")
	}

	rnd := generateRandomResourceName()
	name := fmt.Sprintf("cloudflare_access_mutual_tls_certificate.%s", rnd)
	cert := os.Getenv("CLOUDFLARE_MUTUAL_TLS_CERTIFICATE")
	domain := os.Getenv("CLOUDFLARE_DOMAIN")

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
		},
		ProviderFactories: providerFactories,
		CheckDestroy:      testAccCheckCloudflareAccessMutualTLSCertificateDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccessMutualTLSCertificateConfigBasic(rnd, AccessIdentifier{Type: ZoneType, Value: zoneID}, cert, domain),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(name, "zone_id", zoneID),
					resource.TestCheckResourceAttr(name, "name", rnd),
					resource.TestCheckResourceAttrSet(name, "certificate"),
					resource.TestCheckResourceAttr(name, "associated_hostnames.0", domain),
				),
			},
			{
				Config: testAccessMutualTLSCertificateUpdated(rnd, AccessIdentifier{Type: ZoneType, Value: zoneID}, cert),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(name, "zone_id", zoneID),
					resource.TestCheckResourceAttr(name, "name", rnd),
					resource.TestCheckResourceAttrSet(name, "certificate"),
					resource.TestCheckResourceAttr(name, "associated_hostnames.#", "0"),
				),
			},
		},
	})
}

func testAccCheckCloudflareAccessMutualTLSCertificateDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*cloudflare.API)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "cloudflare_access_mutual_tls_certificate" {
			continue
		}

		if rs.Primary.Attributes["zone_id"] != "" {
			_, err := client.AccessMutualTLSCertificate(context.Background(), rs.Primary.Attributes["zone_id"], rs.Primary.ID)
			if err == nil {
				return fmt.Errorf("AccessMutualTLSCertificate still exists")
			}
		}

		if rs.Primary.Attributes["account_id"] != "" {
			_, err := client.AccessMutualTLSCertificate(context.Background(), rs.Primary.Attributes["account_id"], rs.Primary.ID)
			if err == nil {
				return fmt.Errorf("AccessMutualTLSCertificate still exists")
			}
		}
	}

	return nil
}

func testAccessMutualTLSCertificateConfigBasic(rnd string, identifier AccessIdentifier, cert, domain string) string {
	return fmt.Sprintf(`
resource "cloudflare_access_mutual_tls_certificate" "%[1]s" {
	name                 = "%[1]s"
	%[2]s_id             = "%[3]s"
	associated_hostnames = ["%[5]s"]
	certificate          = "%[4]s"
}
`, rnd, identifier.Type, identifier.Value, cert, domain)
}

func testAccessMutualTLSCertificateUpdated(rnd string, identifier AccessIdentifier, cert string) string {
	return fmt.Sprintf(`
resource "cloudflare_access_mutual_tls_certificate" "%[1]s" {
	name                 = "%[1]s"
	%[2]s_id             = "%[3]s"
	associated_hostnames = []
	certificate          = "%[4]s"
}
`, rnd, identifier.Type, identifier.Value, cert)
}
