variable "expected_aws_account_id" {
  description = "Independently verified AWS account ID in which bootstrap may provision resources."
  type        = string

  validation {
    condition     = can(regex("^[0-9]{12}$", var.expected_aws_account_id))
    error_message = "expected_aws_account_id must be a 12-digit AWS account ID."
  }
}
