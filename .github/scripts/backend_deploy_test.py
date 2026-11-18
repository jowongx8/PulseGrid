"""Execute workflow shell steps with command doubles; never contact AWS or Docker."""

import json
import os
import re
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = ROOT / ".github/workflows/backend-deploy.yml"
SHA = "a" * 40
DIGEST = "sha256:" + "b" * 64
COMMAND = "11111111-2222-3333-4444-555555555555"


def steps(path):
    # Only extract this repository's literal run blocks; actionlint validates YAML.
    result = {}
    for block in re.split(r"^      - name: ", path.read_text(), flags=re.MULTILINE)[1:]:
        name, _, body = block.partition("\n")
        if "\n        run: |\n" in "\n" + body:
            script = body.split("        run: |\n", 1)[1]
            result[name] = "\n".join(line[10:] for line in script.splitlines()) + "\n"
    return result


MOCK = r"""#!/usr/bin/env python3
import json, os, pathlib, sys
name = pathlib.Path(sys.argv[0]).name
args = sys.argv[1:]
p = pathlib.Path(os.environ['CALL_LOG'])
calls = [json.loads(line) for line in p.read_text().splitlines()]
calls.append([name, args, os.environ.get('AWS_MAX_ATTEMPTS')])
p.write_text(''.join(json.dumps(call) + '\n' for call in calls))
if name == 'git':
    if os.environ.get('GIT_FAIL'): sys.exit(1)
    print(os.environ.get('MAIN_SHA', os.environ['GITHUB_SHA']))
elif name == 'aws':
    if args[:2] == ['sts', 'get-caller-identity']:
        print(os.environ['AWS_IDENTITY'])
    elif args[:2] == ['ecr', 'describe-images']:
        if error := os.environ.get('ECR_ERROR'):
            sys.stderr.write('An error occurred (' + error + ') when calling DescribeImages\n')
            sys.exit(1)
        print(os.environ['ECR_DIGEST'])
    elif args[:2] == ['ecr', 'get-login-password']:
        if os.environ.get('LOGIN_FAIL'): sys.exit(1)
        print('local-test-password')
    elif args[:2] == ['ssm', 'send-command']:
        if os.environ.get('SEND_FAIL'): sys.exit(1)
        print(os.environ.get('COMMAND_RESULT', os.environ['COMMAND_ID']))
    elif args[:2] == ['ssm', 'get-command-invocation']:
        n = sum(c[0] == 'aws' and c[1][:2] == args[:2] for c in calls)
        sequence = os.environ.get('POLL_SEQUENCE', 'Success').split(',')
        status = sequence[min(n - 1, len(sequence) - 1)]
        if status in ['InvocationDoesNotExist', 'AccessDeniedException']:
            sys.stderr.write('An error occurred (' + status + ') when calling GetCommandInvocation\n')
            sys.exit(1)
        print(json.dumps({'Status': status, 'ResponseCode': int(os.environ.get('RESPONSE_CODE', '0'))}))
    else: sys.exit('Unexpected AWS operation in test')
elif name == 'docker':
    if os.environ.get('DOCKER_FAIL'): sys.exit(1)
    if args[0] == 'login': sys.stdin.read()
    if args[:2] == ['image', 'inspect']: print(os.environ['IMAGE_IDENTITY'])
elif name == 'curl':
    pathlib.Path(args[args.index('--output') + 1]).write_text(os.environ['API_BODY'])
    print(os.environ.get('HTTP_STATUS', '200'), end='')
    sys.exit(int(os.environ.get('CURL_EXIT', '0')))
elif name == 'timeout':
    os.execvp(args[1], args[1:])
elif name != 'sleep': sys.exit('Unexpected test command')
"""


class BackendDeploymentTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(
            prefix="pulsegrid-backend-workflow-test-"
        )
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.bin = self.directory / "bin"
        self.bin.mkdir()
        for name in ["aws", "docker", "git", "curl", "sleep", "timeout"]:
            p = self.bin / name
            p.write_text(MOCK)
            p.chmod(0o700)
        self.scripts = steps(WORKFLOW)
        self.env = os.environ.copy()
        for key in list(self.env):
            if key.startswith("AWS_"):
                self.env.pop(key)
        self.env.update(
            PATH=str(self.bin) + os.pathsep + self.env["PATH"],
            RUNNER_TEMP=str(self.directory),
            GITHUB_SHA=SHA,
            GITHUB_RUN_ID="123",
            GITHUB_RUN_ATTEMPT="2",
            GITHUB_OUTPUT=str(self.directory / "output"),
            GITHUB_STEP_SUMMARY=str(self.directory / "summary"),
            CALL_LOG=str(self.directory / "calls"),
            AWS_MAX_ATTEMPTS="3",
            AWS_ACCOUNT_ID="852000185801",
            AWS_REGION="ap-southeast-2",
            AWS_DEFAULT_REGION="ap-southeast-2",
            AWS_DEPLOY_ROLE_ARN="arn:aws:iam::852000185801:role/pulsegrid-production-deployment",
            BACKEND_INSTANCE_ID="i-" + "1" * 17,
            BACKEND_SSM_DOCUMENT_VERSION="1",
            ECR_REGISTRY="852000185801.dkr.ecr.ap-southeast-2.amazonaws.com",
            ECR_REPOSITORY="pulsegrid-production-backend",
            ECR_DIGEST=DIGEST,
            SOURCE_URL="https://github.com/example/pulsegrid",
            IMAGE_IDENTITY=f"linux arm64 https://github.com/example/pulsegrid {SHA}",
            IMAGE_EXISTED="true",
            EXISTING_DIGEST=DIGEST,
            PUSHED_DIGEST="",
            IMAGE_DIGEST=DIGEST,
            COMMAND_ID=COMMAND,
            AWS_IDENTITY="852000185801\tarn:aws:sts::852000185801:assumed-role/pulsegrid-production-deployment/pulsegrid-123-2",
            API_BODY=json.dumps(
                {"services": [{"id": "example", "status": {"state": "UNKNOWN"}}]}
            ),
        )

    def run_step(self, name, success=True, script=None, **env):
        for p in ["output", "summary", "calls"]:
            (self.directory / p).write_text("")
        r = subprocess.run(
            [
                "bash",
                "--noprofile",
                "--norc",
                "-eo",
                "pipefail",
                "-c",
                script or self.scripts[name],
            ],
            check=False, env=self.env | env,
            cwd=self.directory,
            capture_output=True,
            text=True,
            timeout=10,
        )
        self.assertEqual(r.returncode == 0, success, r.stdout + r.stderr)
        calls = [
            json.loads(line)
            for line in (self.directory / "calls").read_text().splitlines()
        ]
        return (self.directory / "output").read_text(), calls, r.stdout

    def test_switch_and_workflow_gates(self):
        for value in ["true", "false", "", "TRUE", "true "]:
            with self.subTest(value=value):
                out, calls, _ = self.run_step(
                    "Evaluate backend deployment switch", BACKEND_DEPLOY_ENABLED=value
                )
                self.assertEqual(out, f"enabled={str(value == 'true').lower()}\n")
                self.assertFalse(calls)
        text = WORKFLOW.read_text()
        self.assertIn("needs.validate.result == 'success'", text)
        self.assertIn("needs.validate.outputs.validation_result == 'success'", text)
        self.assertIn("uses: ./.github/workflows/backend-ci.yml", text)
        self.assertIn("cancel-in-progress: false", text)
        self.assertIn("group: pulsegrid-production-backend", text)
        blocks = re.split(r"^      - name: ", text, flags=re.MULTILINE)[1:]
        for block in blocks[1:]:
            self.assertIn("steps.switch.outputs.enabled == 'true'", block)
        early_index = next(
            i for i, b in enumerate(blocks) if b.startswith("Check early freshness")
        )
        for block in blocks[early_index + 1 :]:
            self.assertIn("steps.early.outputs.eligible == 'true'", block)
        submission_index = next(
            i for i, b in enumerate(blocks) if b.startswith("Submit restricted")
        )
        for block in blocks[submission_index:]:
            self.assertIn("steps.final.outputs.eligible == 'true'", block)
        build_index = next(
            i for i, b in enumerate(blocks) if b.startswith("Build and publish")
        )
        for block in blocks[build_index:]:
            # GitHub's implicit success() must stop publication/submission on failure.
            for bypass in [
                "continue-on-error:",
                "always()",
                "failure()",
                "cancelled()",
            ]:
                self.assertNotIn(bypass, block)
        self.assertIn(
            "steps.lookup.outputs.exists == 'false'",
            next(b for b in blocks if b.startswith("Build and publish")),
        )
        self.assertIn("org.opencontainers.image.source=${{ env.SOURCE_URL }}", text)
        self.assertIn("org.opencontainers.image.revision=${{ github.sha }}", text)
        self.assertIn("provenance: mode=min", text)
        self.assertIn("sbom: true", text)
        for forbidden in [
            "RECOVERY_ACKS",
            "ListCommands",
            "list-commands",
            "actions: read",
        ]:
            self.assertNotIn(forbidden, text)
        self.assertIn("ref: ${{ github.sha }}", text)
        self.assertEqual(text.count("github.ref == 'refs/heads/main'"), 2)

    def test_configuration_and_assumed_identity(self):
        self.run_step("Validate production configuration")
        for key in [
            "AWS_ACCOUNT_ID",
            "AWS_REGION",
            "AWS_DEPLOY_ROLE_ARN",
            "BACKEND_INSTANCE_ID",
            "BACKEND_SSM_DOCUMENT_VERSION",
        ]:
            with self.subTest(key=key):
                self.run_step(
                    "Validate production configuration", success=False, **{key: "wrong"}
                )
        self.run_step("Verify assumed role, account and region")
        for env in [
            {"AWS_REGION": "wrong"},
            {"AWS_DEFAULT_REGION": "wrong"},
            {"AWS_IDENTITY": "999\twrong"},
            {"AWS_IDENTITY": "852000185801\twrong"},
        ]:
            self.run_step(
                "Verify assumed role, account and region", success=False, **env
            )

    def test_both_freshness_checks(self):
        for name in ["Check early freshness", "Check final freshness"]:
            self.assertIn(
                f"git -C .backend-main-{'early' if 'early' in name else 'final'}",
                self.scripts[name],
            )
            out, calls, _ = self.run_step(name)
            self.assertEqual(out, "eligible=true\n")
            out, calls, _ = self.run_step(name, MAIN_SHA="c" * 40)
            self.assertEqual(out, "eligible=false\n")
            self.assertFalse(any(c[0] == "aws" for c in calls))
            self.run_step(name, success=False, GIT_FAIL="1")

    def test_ecr_lookup_and_digest_identity(self):
        self.run_step("Log in to the existing ECR registry")
        self.run_step(
            "Log in to the existing ECR registry", success=False, LOGIN_FAIL="1"
        )
        out, _, _ = self.run_step("Look up immutable SHA tag")
        self.assertIn("exists=true", out)
        out, _, _ = self.run_step(
            "Look up immutable SHA tag", ECR_ERROR="ImageNotFoundException"
        )
        self.assertEqual(out, "exists=false\n")
        for env in [
            {"ECR_ERROR": "AccessDeniedException"},
            {"ECR_ERROR": "ThrottlingException"},
            {"ECR_DIGEST": "None"},
        ]:
            self.run_step("Look up immutable SHA tag", success=False, **env)
        name = "Resolve and verify deployable image identity"
        for existing in ["true", "false"]:
            out, calls, _ = self.run_step(
                name, IMAGE_EXISTED=existing, PUSHED_DIGEST=DIGEST
            )
            self.assertEqual(out, f"digest={DIGEST}\n")
            self.assertTrue(
                any(
                    c[0] == "docker"
                    and c[1][:3] == ["pull", "--platform", "linux/arm64"]
                    for c in calls
                )
            )
        for env in [
            {"EXISTING_DIGEST": "sha256:" + "c" * 64},
            {"IMAGE_EXISTED": "false", "PUSHED_DIGEST": ""},
            {"ECR_ERROR": "AccessDeniedException"},
            {"DOCKER_FAIL": "1"},
            {
                "IMAGE_IDENTITY": f"linux amd64 https://github.com/example/pulsegrid {SHA}"
            },
            {"IMAGE_IDENTITY": "linux arm64 wrong-source wrong-revision"},
        ]:
            self.run_step(name, success=False, **env)

    def test_ssm_submission_is_single_attempt(self):
        name = "Submit restricted SSM deployment once"
        out, calls, _ = self.run_step(name)
        self.assertEqual(out, f"command_id={COMMAND}\n")
        self.assertEqual(len(calls), 1)
        self.assertEqual(calls[0][2], "1")
        args = calls[0][1]
        for option, value in [
            ("--document-name", "pulsegrid-production-deploy-backend"),
            ("--document-version", "1"),
            ("--timeout-seconds", "60"),
            ("--parameters", f"ImageDigest={DIGEST}"),
            ("--comment", f"pulsegrid:123:2:{SHA}"),
        ]:
            self.assertEqual(args[args.index(option) + 1], value)
        for env in [{"SEND_FAIL": "1"}, {"COMMAND_RESULT": "None"}]:
            _, calls, output = self.run_step(name, success=False, **env)
            self.assertEqual(len(calls), 1)
            self.assertIn("::error::", output)

    def test_ssm_polling(self):
        name = "Wait for exact SSM deployment result"
        _, calls, _ = self.run_step(
            name,
            POLL_SEQUENCE="InvocationDoesNotExist,Pending,InProgress,Delayed,Success",
        )
        aws_calls = [c for c in calls if c[0] == "aws"]
        self.assertEqual(len(aws_calls), 5)
        self.assertTrue(all(int(c[1][0][:-1]) > 0 for c in calls if c[0] == "timeout"))
        for _, args, retries in aws_calls:
            self.assertEqual(args[args.index("--command-id") + 1], COMMAND)
            self.assertEqual(args[args.index("--plugin-name") + 1], "deployBackend")
            self.assertEqual(
                args[args.index("--instance-id") + 1], self.env["BACKEND_INSTANCE_ID"]
            )
        for status in [
            "Failed",
            "TimedOut",
            "Cancelled",
            "Cancelling",
            "AccessDeniedException",
            "Unexpected",
        ]:
            self.run_step(name, success=False, POLL_SEQUENCE=status)
        self.run_step(name, success=False, RESPONSE_CODE="1")
        # Shorten only the client's time budgets; production values stay in YAML.
        script = (
            self.scripts[name]
            .replace("SECONDS + 1500", "SECONDS + 1")
            .replace("SECONDS + 60", "SECONDS + 1")
        )
        for status in ["Pending", "InvocationDoesNotExist"]:
            self.run_step(name, success=False, script=script, POLL_SEQUENCE=status)

    def test_public_api_contract(self):
        name = "Verify public backend API"
        for state in ["UP", "DOWN", "UNKNOWN"]:
            self.run_step(
                name,
                API_BODY=json.dumps(
                    {"services": [{"id": "x", "status": {"state": state}}]}
                ),
            )
        for env in [
            {"HTTP_STATUS": "404"},
            {"CURL_EXIT": "22"},
            {"API_BODY": "<html>fallback</html>"},
            {"API_BODY": '{"services":[]}'},
            {"API_BODY": '{"services":[{"id":"x","status":{"state":"DEGRADED"}}]}'},
            {"API_BODY": '{"services":[{"name":"x","state":"UP"}]}'},
        ]:
            self.run_step(name, success=False, **env)

    def test_required_ci_gate(self):
        gate = steps(ROOT / ".github/workflows/pr-ci.yml")[
            "Evaluate required validation"
        ]
        base = {
            "DETECTION_RESULT": "success",
            "BACKEND_REQUIRED": "true",
            "BACKEND_RESULT": "success",
            "BACKEND_VALIDATION_RESULT": "success",
            "FRONTEND_REQUIRED": "false",
            "FRONTEND_RESULT": "skipped",
            "FRONTEND_VALIDATION_RESULT": "",
            "DEPLOYMENT_REQUIRED": "true",
            "DEPLOYMENT_RESULT": "success",
            "DEPLOYMENT_VALIDATION_RESULT": "success",
        }
        self.run_step("", script=gate, **base)
        for env in [
            {"BACKEND_RESULT": "failure"},
            {"BACKEND_RESULT": "skipped"},
            {"BACKEND_VALIDATION_RESULT": ""},
            {"DEPLOYMENT_VALIDATION_RESULT": "failure"},
        ]:
            self.run_step("", script=gate, success=False, **(base | env))


if __name__ == "__main__":
    unittest.main()
