package aws

import (
	"fmt"
	"log"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/kms"
	"github.com/hashicorp/terraform-plugin-sdk/helper/acctest"
	"github.com/hashicorp/terraform-plugin-sdk/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/terraform"
	"github.com/jen20/awspolicyequivalence"
)

func init() {
	resource.AddTestSweepers("aws_kms_key", &resource.Sweeper{
		Name: "aws_kms_key",
		F:    testSweepKmsKeys,
	})
}

func testSweepKmsKeys(region string) error {
	client, err := sharedClientForRegion(region)
	if err != nil {
		return fmt.Errorf("error getting client: %s", err)
	}
	conn := client.(*AWSClient).kmsconn

	err = conn.ListKeysPages(&kms.ListKeysInput{Limit: aws.Int64(int64(1000))}, func(out *kms.ListKeysOutput, lastPage bool) bool {
		for _, k := range out.Keys {
			kOut, err := conn.DescribeKey(&kms.DescribeKeyInput{
				KeyId: k.KeyId,
			})
			if err != nil {
				log.Printf("Error: Failed to describe key %q: %s", *k.KeyId, err)
				return false
			}
			if *kOut.KeyMetadata.KeyManager == kms.KeyManagerTypeAws {
				// Skip (default) keys which are managed by AWS
				continue
			}
			if *kOut.KeyMetadata.KeyState == kms.KeyStatePendingDeletion {
				// Skip keys which are already scheduled for deletion
				continue
			}

			_, err = conn.ScheduleKeyDeletion(&kms.ScheduleKeyDeletionInput{
				KeyId:               k.KeyId,
				PendingWindowInDays: aws.Int64(int64(7)),
			})
			if err != nil {
				log.Printf("Error: Failed to schedule key %q for deletion: %s", *k.KeyId, err)
				return false
			}
		}
		return !lastPage
	})
	if err != nil {
		if testSweepSkipSweepError(err) {
			log.Printf("[WARN] Skipping KMS Key sweep for %s: %s", region, err)
			return nil
		}
		return fmt.Errorf("Error describing KMS keys: %s", err)
	}

	return nil
}

func TestAccAWSKmsKey_basic(t *testing.T) {
	var keyBefore, keyAfter kms.KeyMetadata
	rName := acctest.RandStringFromCharSet(10, acctest.CharSetAlphaNum)
	resourceName := "aws_kms_key.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckAWSKmsKeyDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccAWSKmsKey(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSKmsKeyExists(resourceName, &keyBefore),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"deletion_window_in_days"},
			},
			{
				Config: testAccAWSKmsKey_removedPolicy(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSKmsKeyExists(resourceName, &keyAfter),
				),
			},
		},
	})
}

func TestAccAWSKmsKey_disappears(t *testing.T) {
	var key kms.KeyMetadata
	rName := acctest.RandStringFromCharSet(10, acctest.CharSetAlphaNum)
	resourceName := "aws_kms_key.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckAWSKmsKeyDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccAWSKmsKey(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSKmsKeyExists(resourceName, &key),
				),
			},
			{
				Config:             testAccAWSKmsKey_other_region(rName),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestAccAWSKmsKey_policy(t *testing.T) {
	var key kms.KeyMetadata
	rName := acctest.RandStringFromCharSet(10, acctest.CharSetAlphaNum)
	resourceName := "aws_kms_key.test"
	expectedPolicyText := `{"Version":"2012-10-17","Id":"kms-tf-1","Statement":[{"Sid":"Enable IAM User Permissions","Effect":"Allow","Principal":{"AWS":"*"},"Action":"kms:*","Resource":"*"}]}`

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckAWSKmsKeyDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccAWSKmsKey(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSKmsKeyExists(resourceName, &key),
					testAccCheckAWSKmsKeyHasPolicy(resourceName, expectedPolicyText),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"deletion_window_in_days"},
			},
		},
	})
}

func TestAccAWSKmsKey_isEnabled(t *testing.T) {
	var key1, key2, key3 kms.KeyMetadata
	rName := acctest.RandStringFromCharSet(10, acctest.CharSetAlphaNum)
	resourceName := "aws_kms_key.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckAWSKmsKeyDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccAWSKmsKey_enabledRotation(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSKmsKeyExists("aws_kms_key.test", &key1),
					resource.TestCheckResourceAttr("aws_kms_key.test", "is_enabled", "true"),
					testAccCheckAWSKmsKeyIsEnabled(&key1, true),
					resource.TestCheckResourceAttr("aws_kms_key.test", "enable_key_rotation", "true"),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"deletion_window_in_days"},
			},
			{
				Config: testAccAWSKmsKey_disabled(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSKmsKeyExists("aws_kms_key.test", &key2),
					resource.TestCheckResourceAttr("aws_kms_key.test", "is_enabled", "false"),
					testAccCheckAWSKmsKeyIsEnabled(&key2, false),
					resource.TestCheckResourceAttr("aws_kms_key.test", "enable_key_rotation", "false"),
				),
			},
			{
				Config: testAccAWSKmsKey_enabled(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSKmsKeyExists("aws_kms_key.test", &key3),
					resource.TestCheckResourceAttr("aws_kms_key.test", "is_enabled", "true"),
					testAccCheckAWSKmsKeyIsEnabled(&key3, true),
					resource.TestCheckResourceAttr("aws_kms_key.test", "enable_key_rotation", "true"),
				),
			},
		},
	})
}

func TestAccAWSKmsKey_tags(t *testing.T) {
	var keyBefore kms.KeyMetadata
	rName := acctest.RandStringFromCharSet(10, acctest.CharSetAlphaNum)
	resourceName := "aws_kms_key.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckAWSKmsKeyDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccAWSKmsKey_tags(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSKmsKeyExists(resourceName, &keyBefore),
					resource.TestCheckResourceAttr(resourceName, "tags.%", "3"),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"deletion_window_in_days"},
			},
		},
	})
}

func testAccCheckAWSKmsKeyHasPolicy(name string, expectedPolicyText string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("Not found: %s", name)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No KMS Key ID is set")
		}

		conn := testAccProvider.Meta().(*AWSClient).kmsconn

		out, err := conn.GetKeyPolicy(&kms.GetKeyPolicyInput{
			KeyId:      aws.String(rs.Primary.ID),
			PolicyName: aws.String("default"),
		})
		if err != nil {
			return err
		}

		actualPolicyText := *out.Policy

		equivalent, err := awspolicy.PoliciesAreEquivalent(actualPolicyText, expectedPolicyText)
		if err != nil {
			return fmt.Errorf("Error testing policy equivalence: %s", err)
		}
		if !equivalent {
			return fmt.Errorf("Non-equivalent policy error:\n\nexpected: %s\n\n     got: %s\n",
				expectedPolicyText, actualPolicyText)
		}

		return nil
	}
}

func testAccCheckAWSKmsKeyDestroy(s *terraform.State) error {
	conn := testAccProvider.Meta().(*AWSClient).kmsconn

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "aws_kms_key" {
			continue
		}

		out, err := conn.DescribeKey(&kms.DescribeKeyInput{
			KeyId: aws.String(rs.Primary.ID),
		})

		if err != nil {
			return err
		}

		if *out.KeyMetadata.KeyState == "PendingDeletion" {
			return nil
		}

		return fmt.Errorf("KMS key still exists:\n%#v", out.KeyMetadata)
	}

	return nil
}

func testAccCheckAWSKmsKeyExists(name string, key *kms.KeyMetadata) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("Not found: %s", name)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No KMS Key ID is set")
		}

		conn := testAccProvider.Meta().(*AWSClient).kmsconn

		o, err := retryOnAwsCode("NotFoundException", func() (interface{}, error) {
			return conn.DescribeKey(&kms.DescribeKeyInput{
				KeyId: aws.String(rs.Primary.ID),
			})
		})
		if err != nil {
			return err
		}
		out := o.(*kms.DescribeKeyOutput)

		*key = *out.KeyMetadata

		return nil
	}
}

func testAccCheckAWSKmsKeyIsEnabled(key *kms.KeyMetadata, isEnabled bool) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		if *key.Enabled != isEnabled {
			return fmt.Errorf("Expected key %q to have is_enabled=%t, given %t",
				*key.Arn, isEnabled, *key.Enabled)
		}

		return nil
	}
}

func testAccAWSKmsKey(rName string) string {
	return fmt.Sprintf(`
resource "aws_kms_key" "test" {
  description             = "Terraform acc test %s"
  deletion_window_in_days = 7

  policy = <<POLICY
{
  "Version": "2012-10-17",
  "Id": "kms-tf-1",
  "Statement": [
    {
      "Sid": "Enable IAM User Permissions",
      "Effect": "Allow",
      "Principal": {
        "AWS": "*"
      },
      "Action": "kms:*",
      "Resource": "*"
    }
  ]
}
POLICY

  tags = {
    Name = "tf-acc-test-kms-key-%s"
  }
}
`, rName, rName)
}

func testAccAWSKmsKey_other_region(rName string) string {
	return fmt.Sprintf(`
provider "aws" {
  region = "us-east-1"
}

resource "aws_kms_key" "test" {
  description             = "Terraform acc test %s"
  deletion_window_in_days = 7

  policy = <<POLICY
{
  "Version": "2012-10-17",
  "Id": "kms-tf-1",
  "Statement": [
    {
      "Sid": "Enable IAM User Permissions",
      "Effect": "Allow",
      "Principal": {
        "AWS": "*"
      },
      "Action": "kms:*",
      "Resource": "*"
    }
  ]
}
POLICY

  tags = {
    Name = "tf-acc-test-kms-key-%s"
  }
}
`, rName, rName)
}

func testAccAWSKmsKey_removedPolicy(rName string) string {
	return fmt.Sprintf(`
resource "aws_kms_key" "test" {
  description             = "Terraform acc test %s"
  deletion_window_in_days = 7

  tags = {
    Name = "tf-acc-test-kms-key-%s"
  }
}
`, rName, rName)
}

func testAccAWSKmsKey_enabledRotation(rName string) string {
	return fmt.Sprintf(`
resource "aws_kms_key" "test" {
  description             = "Terraform acc test is_enabled %s"
  deletion_window_in_days = 7
  enable_key_rotation     = true

  tags = {
    Name = "tf-acc-test-kms-key-%s"
  }
}
`, rName, rName)
}

func testAccAWSKmsKey_disabled(rName string) string {
	return fmt.Sprintf(`
resource "aws_kms_key" "test" {
  description             = "Terraform acc test is_enabled %s"
  deletion_window_in_days = 7
  enable_key_rotation     = false
  is_enabled              = false

  tags = {
    Name = "tf-acc-test-kms-key-%s"
  }
}
`, rName, rName)
}

func testAccAWSKmsKey_enabled(rName string) string {
	return fmt.Sprintf(`
resource "aws_kms_key" "test" {
  description             = "Terraform acc test is_enabled %s"
  deletion_window_in_days = 7
  enable_key_rotation     = true
  is_enabled              = true

  tags = {
    Name = "tf-acc-test-kms-key-%s"
  }
}
`, rName, rName)
}

func testAccAWSKmsKey_tags(rName string) string {
	return fmt.Sprintf(`
resource "aws_kms_key" "test" {
  description = "Terraform acc test %s"

  tags = {
    Name        = "tf-acc-test-kms-key-%s"
    Key1        = "Value One"
    Description = "Very interesting"
  }
}
`, rName, rName)
}
