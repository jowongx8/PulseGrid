resource "aws_ssm_document" "backend_deploy" {
  name            = "pulsegrid-production-deploy-backend"
  document_type   = "Command"
  document_format = "JSON"

  content = jsonencode({
    schemaVersion = "2.2"
    description   = "Deploy one verified digest from the fixed PulseGrid backend repository."
    parameters = {
      ImageDigest = {
        type              = "String"
        description       = "Immutable ECR image digest; repository and recovery controls are host-owned."
        allowedPattern    = "^sha256:[0-9a-f]{64}$"
        interpolationType = "ENV_VAR"
      }
    }
    mainSteps = [{
      action = "aws:runShellScript"
      name   = "deployBackend"
      precondition = {
        StringEquals = ["platformType", "Linux"]
      }
      inputs = {
        timeoutSeconds = "900"
        runCommand = [
          "set -eu",
          "exec /usr/local/sbin/pulsegrid-deploy-backend \"$SSM_ImageDigest\"",
        ]
      }
    }]
  })
}
