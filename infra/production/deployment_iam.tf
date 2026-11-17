data "aws_iam_user" "github_actions" {
  user_name = "pulsegrid-github-actions"
}

resource "aws_iam_role" "github_deployment" {
  name                 = "pulsegrid-production-deployment"
  max_session_duration = 3600

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = "sts:AssumeRole"
      Principal = {
        AWS = "arn:aws:iam::${var.expected_aws_account_id}:root"
      }
      Condition = {
        ArnEquals = {
          "aws:PrincipalArn" = data.aws_iam_user.github_actions.arn
        }
      }
    }]
  })
}

resource "aws_iam_role_policy" "github_deployment" {
  name = "pulsegrid-production-deployment"
  role = aws_iam_role.github_deployment.name

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid      = "ListFrontendBucket"
        Effect   = "Allow"
        Action   = "s3:ListBucket"
        Resource = aws_s3_bucket.frontend.arn
      },
      {
        Sid    = "PublishFrontendObjects"
        Effect = "Allow"
        Action = [
          "s3:GetObject",
          "s3:PutObject",
          "s3:DeleteObject",
        ]
        Resource = "${aws_s3_bucket.frontend.arn}/*"
      },
      {
        Sid      = "AuthenticateToECR"
        Effect   = "Allow"
        Action   = "ecr:GetAuthorizationToken"
        Resource = "*"
      },
      {
        Sid    = "PublishBackendImages"
        Effect = "Allow"
        Action = [
          "ecr:BatchCheckLayerAvailability",
          "ecr:InitiateLayerUpload",
          "ecr:UploadLayerPart",
          "ecr:CompleteLayerUpload",
          "ecr:PutImage",
          "ecr:BatchGetImage",
          "ecr:DescribeImages",
        ]
        Resource = aws_ecr_repository.backend.arn
      },
      {
        Sid    = "InvalidateFrontendCache"
        Effect = "Allow"
        Action = [
          "cloudfront:CreateInvalidation",
          "cloudfront:GetInvalidation",
        ]
        Resource = aws_cloudfront_distribution.production.arn
      },
    ]
  })
}

resource "aws_iam_user_policy" "github_actions_assume_role" {
  name = "pulsegrid-assume-production-deployment"
  user = data.aws_iam_user.github_actions.user_name

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = "sts:AssumeRole"
      Resource = aws_iam_role.github_deployment.arn
    }]
  })
}
