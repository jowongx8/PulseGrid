provider "aws" {
  region              = "ap-southeast-2"
  allowed_account_ids = [var.expected_aws_account_id]

  default_tags {
    tags = {
      Project   = "PulseGrid"
      ManagedBy = "Terraform"
    }
  }
}
