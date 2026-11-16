resource "aws_security_group" "backend" {
  name        = "pulsegrid-production-backend"
  description = "Session Manager administration with no inbound access"
  vpc_id      = aws_vpc.production.id
  ingress     = []

  tags = {
    Name = "pulsegrid-production-backend"
  }
}

resource "aws_vpc_security_group_egress_rule" "backend_ipv4" {
  security_group_id = aws_security_group.backend.id
  description       = "Allow outbound IPv4 traffic"
  ip_protocol       = "-1"
  cidr_ipv4         = "0.0.0.0/0"
}

resource "aws_instance" "backend" {
  ami                    = "ami-071733a41b3cc3de4"
  instance_type          = "t4g.small"
  subnet_id              = aws_subnet.public.id
  vpc_security_group_ids = [aws_security_group.backend.id]
  iam_instance_profile   = aws_iam_instance_profile.backend.name
  monitoring             = false

  credit_specification {
    cpu_credits = "standard"
  }

  metadata_options {
    http_endpoint               = "enabled"
    http_tokens                 = "required"
    http_put_response_hop_limit = 1
  }

  root_block_device {
    volume_type           = "gp3"
    volume_size           = 16
    encrypted             = true
    delete_on_termination = true
  }

  tags = {
    Name = "pulsegrid-production-backend"
  }

  depends_on = [
    aws_iam_role_policy_attachment.backend_ssm,
    aws_vpc_security_group_egress_rule.backend_ipv4,
  ]
}

resource "aws_eip" "backend" {
  domain = "vpc"

  tags = {
    Name = "pulsegrid-production-backend-ip"
  }
}

resource "aws_eip_association" "backend" {
  allocation_id = aws_eip.backend.id
  instance_id   = aws_instance.backend.id

  depends_on = [
    aws_route.internet_ipv4,
    aws_route_table_association.public,
  ]
}
