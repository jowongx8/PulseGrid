output "state_bucket_name" {
  description = "S3 bucket for the future production Terraform backend."
  value       = aws_s3_bucket.terraform_state.id
}
