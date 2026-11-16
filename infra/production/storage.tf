resource "aws_ebs_volume" "sqlite" {
  availability_zone = aws_subnet.public.availability_zone
  size              = 10
  type              = "gp3"
  encrypted         = true

  tags = {
    Name = "pulsegrid-production-sqlite"
  }

  lifecycle {
    prevent_destroy = true
  }
}

resource "aws_volume_attachment" "sqlite" {
  device_name  = "/dev/sdf"
  volume_id    = aws_ebs_volume.sqlite.id
  instance_id  = aws_instance.backend.id
  force_detach = false
}
