variable "expected_aws_account_id" {
  description = "Independently verified AWS account ID in which production may provision resources."
  type        = string

  validation {
    condition     = can(regex("^[0-9]{12}$", var.expected_aws_account_id))
    error_message = "expected_aws_account_id must be a 12-digit AWS account ID."
  }
}

variable "backend_origin_hostname" {
  description = "Registered lowercase DuckDNS hostname pointing to the existing backend Elastic IP, without a scheme, path, port, or trailing dot."
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\\.duckdns\\.org$", var.backend_origin_hostname))
    error_message = "backend_origin_hostname must be a bare lowercase hostname such as <registered-name>.duckdns.org, with a valid 1-63 character DNS label."
  }
}

variable "backend_origin_secret" {
  description = "Private CloudFront-to-Caddy origin header value, supplied through ignored local configuration."
  type        = string
  sensitive   = true

  validation {
    condition     = can(regex("^[0-9a-f]{64}$", var.backend_origin_secret))
    error_message = "backend_origin_secret must contain exactly 64 lowercase hexadecimal characters."
  }
}
