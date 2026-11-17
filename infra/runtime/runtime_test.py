"""Host control-flow tests use temporary files and command doubles, never AWS/Docker."""

import json
import subprocess
import tempfile
import unittest
from pathlib import Path

RUNTIME = Path(__file__).resolve().parent
DIGEST = "sha256:" + "a" * 64
PREVIOUS = "sha256:" + "b" * 64


class RuntimeTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(prefix="pulsegrid-runtime-test-")
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        for name in ("state", "run", "data"):
            (self.root / name).mkdir(mode=0o700)
        self.prefix = f"""
set -euo pipefail
source '{RUNTIME}/check-backend-start.sh'
source '{RUNTIME}/verify-backend.sh'
source '{RUNTIME}/deploy-backend.sh'
PG_REAL_CONTAINER=$(declare -f pg_container)
PG_REAL_PRIVATE=$(declare -f pg_private)
PG_REAL_PROCESS_START=$(declare -f pg_process_start)
PG_STATE='{self.root}/state'
PG_RUN='{self.root}/run'
PG_DATA='{self.root}/data'
PG_UUID=9fcff7d8-57f2-4ce4-b667-eebda409636b
PG_VOLUME=vol-0123456789abcdef0
PG_REPOSITORY=123456789012.dkr.ecr.ap-southeast-2.amazonaws.com/pulsegrid-production-backend
PG_CONTAINER=pulsegrid-production-backend
PG_REGION=ap-southeast-2
DIGEST={DIGEST}
PREVIOUS={PREVIOUS}
INVOCATION_ID=0123456789abcdef0123456789abcdef
pg_private() {{ [[ ! -L $1 && -f $1 ]]; }}
pg_boot_id() {{ echo boot-one; }}
pg_process_start() {{ echo 123; }}
pg_storage() {{ echo storage >> "$PG_RUN/calls"; }}
pg_container() {{ echo 'container-one 172.18.0.2'; }}
curl() {{ return 0; }}
sleep() {{ :; }}
docker() {{
    if [[ $* == *State.Running* ]]; then echo false;
    elif [[ $* == *State.Status* ]]; then echo running;
    else return 1; fi
}}
pg_pull() {{ echo pull >> "$PG_RUN/calls"; }}
flock() {{ python3 -c 'import fcntl; fcntl.flock(9, fcntl.LOCK_EX | fcntl.LOCK_NB)' 9>&9; }}
systemctl() {{
    echo "$*" >> "$PG_RUN/calls"
    case "$1" in
        stop) rm -f "$PG_RUN/active" ;;
        is-active) [[ -f $PG_RUN/active ]] ;;
        reset-failed)
            shift
            local include_inactive=0 pattern unit matched=0
            if [[ ${{1:-}} == --all ]]; then include_inactive=1; shift; fi
            [[ $# == 1 ]] || return 90
            pattern=$1
            [[ $pattern == *.service ]] || pattern+=.service
            if [[ $pattern == *'['* || $pattern == *'*'* || $pattern == *'?'* ]]; then
                [[ ${{SYSTEMCTL_LIST_EXIT:-0}} == 0 ]] || return "$SYSTEMCTL_LIST_EXIT"
                for unit in ${{SYSTEMCTL_UNITS:-pulsegrid-backend.service}}; do
                    if [[ $unit == pulsegrid-backend.service ]]; then
                        [[ ${{SYSTEMCTL_UNIT_LOADED:-1}} == 1 ]] || continue
                        [[ $include_inactive == 1 || ${{SYSTEMCTL_UNIT_STATE:-inactive}} != inactive ]] || continue
                    fi
                    [[ $unit == $pattern ]] || continue
                    echo "reset $unit" >> "$PG_RUN/calls"
                    matched=1
                done
                [[ $matched == 1 ]] || return 0
            else
                [[ ${{SYSTEMCTL_UNIT_LOADED:-1}} == 1 ]] || return 1
                echo "reset $pattern" >> "$PG_RUN/calls"
            fi
            [[ ${{SYSTEMCTL_RESET_EXIT:-0}} == 0 ]] || return "$SYSTEMCTL_RESET_EXIT"
            SYSTEMCTL_LIMIT_HIT=0
            SYSTEMCTL_UNIT_STATE=inactive ;;
        start)
            [[ ${{SYSTEMCTL_START_EXIT:-0}} == 0 ]] || return "$SYSTEMCTL_START_EXIT"
            [[ ${{SYSTEMCTL_LIMIT_HIT:-0}} == 0 ]] || return 1
            SYSTEMCTL_UNIT_LOADED=1
            if pg_admit && verify_backend; then touch "$PG_RUN/active";
            else rm -f "$PG_RUN/active"; return 1; fi ;;
        *) return 1 ;;
    esac
}}
select_image() {{ pg_atomic text "$PG_STATE/selected.env" "PULSEGRID_BACKEND_IMAGE=$PG_REPOSITORY@$1"; }}
permit() {{ pg_atomic json "$PG_RUN/permit.json" candidate "$DIGEST" deployment deployment-one boot boot-one pid "$$" started 123; }}
hold() {{ pg_atomic json "$PG_STATE/hold.json" candidate "$DIGEST" previous "$PREVIOUS" deployment deployment-one; }}
verified() {{ pg_atomic json "$PG_STATE/verified.json" digest "$1" previous '' container_id old; }}
deny() {{ if "$@"; then echo 'unexpected success' >&2; exit 80; fi; }}
"""

    def shell(self, body):
        result = subprocess.run(
            ["/bin/bash", "-c", self.prefix + body], check=False, capture_output=True, text=True
        )
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        return result

    def test_first_deployment_without_verified_image(self):
        self.shell('deploy_backend "$DIGEST"; [[ ! -e $PG_STATE/hold.json ]]; [[ -f $PG_RUN/active ]]')
        record = json.loads((self.root / "state/verified.json").read_text())
        self.assertEqual(record["digest"], DIGEST)
        self.assertEqual(record["previous"], "")

    def test_first_deployment_with_unit_absent_from_systemd_memory(self):
        self.shell('SYSTEMCTL_UNIT_LOADED=0; deploy_backend "$DIGEST"; [[ -f $PG_RUN/active && ! -e $PG_STATE/hold.json ]]')
        calls = (self.root / "run/calls").read_text().splitlines()
        self.assertFalse(any(call.startswith("reset ") for call in calls))
        self.assertIn("start pulsegrid-backend.service", calls)

    def test_failed_unit_and_exhausted_restart_limit_are_reset(self):
        self.shell('SYSTEMCTL_UNIT_STATE=failed; SYSTEMCTL_LIMIT_HIT=1; deploy_backend "$DIGEST"; [[ -f $PG_RUN/active && ! -e $PG_STATE/hold.json ]]')
        calls = (self.root / "run/calls").read_text().splitlines()
        self.assertLess(calls.index("reset pulsegrid-backend.service"), calls.index("start pulsegrid-backend.service"))

    def test_loaded_inactive_unit_rate_limit_is_reset(self):
        self.shell('SYSTEMCTL_UNIT_STATE=inactive; SYSTEMCTL_LIMIT_HIT=1; deploy_backend "$DIGEST"; [[ -f $PG_RUN/active && ! -e $PG_STATE/hold.json ]]')
        self.assertIn("reset pulsegrid-backend.service", (self.root / "run/calls").read_text().splitlines())

    def test_reset_targets_only_the_backend_unit(self):
        self.shell('SYSTEMCTL_UNITS="pulsegrid-backend.service pulsegrid-caddy.service pulsegrid-backend.service-other.service"; deploy_backend "$DIGEST"')
        resets = [call for call in (self.root / "run/calls").read_text().splitlines() if call.startswith("reset ")]
        self.assertEqual(resets, ["reset pulsegrid-backend.service"])

    def test_reset_failure_aborts_before_start_and_preserves_hold(self):
        self.shell('SYSTEMCTL_RESET_EXIT=1; deny deploy_backend "$DIGEST"; (trap deploy_cleanup EXIT; exit 1) && exit 81; [[ -f $PG_STATE/hold.json && -f $PG_STATE/selected.env && ! -e $PG_RUN/permit.json && ! -e $PG_STATE/verified.json ]]; deny pg_admit')
        self.assertNotIn("start pulsegrid-backend.service", (self.root / "run/calls").read_text().splitlines())

    def test_manager_query_failure_is_not_treated_as_an_unloaded_unit(self):
        self.shell('SYSTEMCTL_LIST_EXIT=1; deny deploy_backend "$DIGEST"; (trap deploy_cleanup EXIT; exit 1) && exit 81; [[ -f $PG_STATE/hold.json && -f $PG_STATE/selected.env && ! -e $PG_RUN/permit.json && ! -e $PG_STATE/verified.json ]]; deny pg_admit')
        self.assertNotIn("start pulsegrid-backend.service", (self.root / "run/calls").read_text().splitlines())

    def test_missing_unit_start_failure_retains_first_deployment_records(self):
        self.shell('SYSTEMCTL_UNIT_LOADED=0; SYSTEMCTL_START_EXIT=4; deny deploy_backend "$DIGEST"; (trap deploy_cleanup EXIT; exit 1) && exit 81; [[ -f $PG_STATE/hold.json && -f $PG_STATE/selected.env && ! -e $PG_RUN/permit.json && ! -e $PG_STATE/verified.json ]]; deny pg_admit')
        self.assertIn("start pulsegrid-backend.service", (self.root / "run/calls").read_text().splitlines())

    def test_start_failure_after_reset_preserves_the_deployment_hold(self):
        self.shell('SYSTEMCTL_START_EXIT=1; deny deploy_backend "$DIGEST"; (trap deploy_cleanup EXIT; exit 1) && exit 81; [[ -f $PG_STATE/hold.json && ! -e $PG_RUN/permit.json && ! -e $PG_STATE/verified.json ]]; deny pg_admit')
        calls = (self.root / "run/calls").read_text().splitlines()
        self.assertLess(calls.index("reset pulsegrid-backend.service"), calls.index("start pulsegrid-backend.service"))

    def test_successful_upgrade_preserves_previous_digest(self):
        self.shell('verified "$PREVIOUS"; select_image "$PREVIOUS"; deploy_backend "$DIGEST"; [[ $(pg_field "$PG_STATE/verified.json" previous) == "$PREVIOUS" ]]')

    def test_same_digest_redeploy_keeps_distinct_previous_digest(self):
        self.shell('pg_atomic json "$PG_STATE/verified.json" digest "$DIGEST" previous "$PREVIOUS" container_id old; select_image "$DIGEST"; deploy_backend "$DIGEST"; [[ $(pg_field "$PG_STATE/verified.json" previous) == "$PREVIOUS" ]]')

    def test_unselected_or_unverified_image_is_denied(self):
        self.shell('deny pg_admit; select_image "$DIGEST"; deny pg_admit')

    def test_wrong_repository_is_denied(self):
        self.shell('pg_atomic text "$PG_STATE/selected.env" "PULSEGRID_BACKEND_IMAGE=evil.example/backend@$DIGEST"; deny pg_admit')

    def test_digest_contract(self):
        for value in ("latest", "sha-" + "a" * 40, DIGEST.upper(), DIGEST + ";id", "repository@" + DIGEST, DIGEST + "\n", " " + DIGEST):
            with self.subTest(value=value):
                self.shell(f"deny deploy_backend '{value}'; [[ ! -e $PG_STATE/hold.json ]]")
        self.shell('deny deploy_backend; deny deploy_backend "$DIGEST" extra')

    def test_permit_is_consumed_once(self):
        self.shell('select_image "$DIGEST"; hold; permit; pg_admit; [[ ! -e $PG_RUN/permit.json ]]; deny pg_admit')

    def test_permit_is_consumed_even_when_storage_fails(self):
        self.shell('select_image "$DIGEST"; hold; permit; pg_storage() { return 1; }; deny pg_admit; [[ ! -e $PG_RUN/permit.json ]]; deny pg_admit')

    def test_reboot_and_dead_deployer_deny_permit(self):
        self.shell('select_image "$DIGEST"; hold; permit; pg_boot_id() { echo boot-two; }; deny pg_admit')
        self.shell('select_image "$DIGEST"; hold; permit; pg_process_start() { return 1; }; deny pg_admit')

    def test_wrong_systemd_invocation_cannot_verify(self):
        self.shell('select_image "$DIGEST"; hold; permit; pg_admit; INVOCATION_ID=other; deny verify_backend; [[ -f $PG_STATE/hold.json ]]')

    def test_verified_start_cannot_clear_new_same_digest_hold(self):
        self.shell('select_image "$DIGEST"; verified "$DIGEST"; pg_admit; hold; deny pg_commit container-one; [[ -f $PG_STATE/hold.json ]]')

    def test_stale_candidate_verifier_cannot_clear_another_hold(self):
        self.shell('select_image "$DIGEST"; hold; permit; pg_admit; pg_atomic json "$PG_STATE/hold.json" candidate "$DIGEST" previous "$PREVIOUS" deployment deployment-two; deny pg_commit container-one; [[ $(pg_field "$PG_STATE/hold.json" deployment) == deployment-two ]]')

    def test_permit_from_another_transaction_is_denied(self):
        self.shell('select_image "$DIGEST"; hold; permit; pg_atomic json "$PG_STATE/hold.json" candidate "$DIGEST" previous "$PREVIOUS" deployment deployment-two; deny pg_admit; [[ ! -e $PG_RUN/admitted.json ]]')

    def test_failed_hold_removal_reinstates_live_hold(self):
        self.shell('select_image "$DIGEST"; hold; permit; pg_admit; pg_clear_hold() { pg_atomic remove "$PG_STATE/hold.json"; return 1; }; deny verify_backend; [[ -f $PG_STATE/hold.json ]]; deny pg_admit')

    def test_held_release_cannot_be_retried_through_ssm(self):
        self.shell('hold; deny deploy_backend "$DIGEST"; [[ ! -e $PG_RUN/permit.json ]]; [[ ! -e $PG_RUN/calls ]]')

    def test_pull_failure_preserves_running_release(self):
        self.shell('verified "$PREVIOUS"; select_image "$PREVIOUS"; touch "$PG_RUN/active"; pg_pull() { return 1; }; deny deploy_backend "$DIGEST"; [[ -f $PG_RUN/active && ! -e $PG_STATE/hold.json ]]')

    def test_hold_is_written_before_service_stop(self):
        self.shell('systemctl() { [[ -f $PG_STATE/hold.json ]]; return 1; }; deny deploy_backend "$DIGEST"; [[ -f $PG_STATE/hold.json ]]')

    def test_candidate_exit_before_http_stays_held(self):
        # Exit-code propagation and Goose failures are verified separately on
        # Linux / in Go; the host verifier rejects any exited container.
        self.shell('select_image "$DIGEST"; hold; permit; pg_admit; pg_container() { return 1; }; docker() { echo exited; }; deny verify_backend; [[ -f $PG_STATE/hold.json ]]; deny pg_admit')

    def test_running_but_unhealthy_candidate_times_out(self):
        self.shell('select_image "$DIGEST"; hold; permit; pg_admit; curl() { return 1; }; sleep() { SECONDS=10000; }; deny verify_backend; [[ -f $PG_STATE/hold.json ]]; deny pg_admit')

    def test_failed_health_stops_candidate_and_preserves_data(self):
        database = self.root / "data/pulsegrid.db"
        database.write_bytes(b"existing-database-not-a-test-sqlite-file")
        self.shell('pg_container() { return 1; }; docker() { echo exited; }; deny deploy_backend "$DIGEST"; (trap deploy_cleanup EXIT; exit 1) && exit 81; [[ -f $PG_STATE/hold.json && ! -e $PG_RUN/active ]]; deny pg_admit')
        self.assertEqual(database.read_bytes(), b"existing-database-not-a-test-sqlite-file")

    def test_success_record_before_hold_clear_still_blocks_restart(self):
        self.shell('select_image "$DIGEST"; hold; permit; pg_admit; pg_clear_hold() { return 1; }; deny verify_backend; [[ $(pg_field "$PG_STATE/verified.json" digest) == "$DIGEST" ]]; [[ -f $PG_STATE/hold.json ]]; deny pg_admit; deny deploy_backend "$DIGEST"')

    def test_interruption_cleanup_retains_hold_and_removes_permit(self):
        result = subprocess.run(
            ["/bin/bash", "-c", self.prefix + 'select_image "$DIGEST"; hold; permit; PG_ATTEMPT=1; PG_DOCKER_CONFIG=; touch "$PG_RUN/active"; trap deploy_cleanup EXIT; trap "exit 143" TERM; kill -TERM "$$"'],
            check=False, capture_output=True, text=True,
        )
        self.assertEqual(result.returncode, 143, result.stdout + result.stderr)
        self.shell('[[ -f $PG_STATE/hold.json && ! -e $PG_RUN/permit.json && ! -e $PG_RUN/active ]]; deny pg_admit')

    def test_commit_without_admission_or_health_cannot_succeed(self):
        self.shell('select_image "$DIGEST"; hold; pg_selection; deny pg_commit container-one; [[ ! -e $PG_STATE/verified.json && -f $PG_STATE/hold.json ]]')

    def test_atomic_record_failure_does_not_clear_hold(self):
        self.shell('select_image "$DIGEST"; hold; permit; pg_admit; pg_atomic() { return 1; }; deny verify_backend; [[ -f $PG_STATE/hold.json ]]')

    def test_verified_crash_recovery_rechecks_storage_each_time(self):
        self.shell('select_image "$DIGEST"; verified "$DIGEST"; pg_admit; verify_backend; INVOCATION_ID=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa; pg_admit; verify_backend; [[ $(grep -c storage "$PG_RUN/calls") == 6 ]]')

    def test_reset_failed_never_overrides_hold(self):
        self.shell('select_image "$DIGEST"; hold; systemctl reset-failed pulsegrid-backend.service; deny pg_admit')

    def test_operator_review_can_authorize_controlled_recovery(self):
        self.shell('select_image "$DIGEST"; hold; deny deploy_backend "$DIGEST"; systemctl stop pulsegrid-backend.service; pg_atomic remove "$PG_STATE/hold.json"; deploy_backend "$DIGEST"; [[ ! -e $PG_STATE/hold.json ]]')

    def test_concurrent_deployment_is_rejected(self):
        import fcntl
        with (self.root / "run/deployment.lock").open("w") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            self.shell('deny deploy_backend "$DIGEST"; [[ ! -e $PG_STATE/hold.json ]]')

    def test_symlink_state_is_rejected(self):
        (self.root / "state/selected.env").symlink_to(self.root / "outside")
        self.shell('deny pg_admit')

    def test_atomic_records_are_private_and_complete(self):
        self.shell('verified "$DIGEST"')
        record = self.root / "state/verified.json"
        self.assertEqual(record.stat().st_mode & 0o777, 0o600)
        self.assertEqual(json.loads(record.read_text())["digest"], DIGEST)
        self.assertEqual([p.name for p in record.parent.iterdir()], ["verified.json"])

    def test_private_configuration_permissions(self):
        self.shell('eval "$PG_REAL_PRIVATE"; touch "$PG_STATE/config"; stat() { echo "${PERMS:-0:0:600}"; }; pg_private "$PG_STATE/config" file; PERMS=10001:10001:600; deny pg_private "$PG_STATE/config" file; PERMS=0:0:644; deny pg_private "$PG_STATE/config" file')

    def test_permit_process_identity_rejects_zombies_and_nonroot(self):
        wrapper = """
eval "$PG_REAL_PROCESS_START"
python3() {
    command python3 -c '
import io, os, sys
from types import SimpleNamespace
from unittest.mock import patch
program = sys.stdin.read()
sys.argv = ["-", sys.argv[1]]
fields = [os.environ["PROCESS_STATE"]] + ["0"] * 18 + ["123"]
stat = "42 (deployer with spaces) " + " ".join(fields)
with patch("pathlib.Path.stat", return_value=SimpleNamespace(st_uid=int(os.environ["PROCESS_UID"]))):
    with patch("pathlib.Path.read_text", return_value=stat):
        exec(program)
' "$2"
}
"""
        self.shell(wrapper + 'export PROCESS_STATE=S PROCESS_UID=0; [[ $(pg_process_start 42) == 123 ]]')
        for state, uid in (("Z", "0"), ("X", "0"), ("S", "10001")):
            self.shell(wrapper + f'export PROCESS_STATE={state} PROCESS_UID={uid}; deny pg_process_start 42')

    def test_runtime_configuration_rejects_unknown_fields(self):
        self.shell('PG_CONFIG="$PG_STATE/config"; printf "PULSEGRID_EXPECTED_AWS_ACCOUNT_ID=123456789012\\nPULSEGRID_EBS_VOLUME_ID=vol-0123456789abcdef0\\n" > "$PG_CONFIG"; pg_load_config; [[ $PG_REPOSITORY == 123456789012.dkr.ecr.ap-southeast-2.amazonaws.com/pulsegrid-production-backend ]]; printf "EVIL=not-a-runtime-key\\n" > "$PG_CONFIG"; deny pg_load_config')

    def test_container_identity_and_bind_device_checks(self):
        image = f"123456789012.dkr.ecr.ap-southeast-2.amazonaws.com/pulsegrid-production-backend@{DIGEST}"
        good = {
            "Id": "container-one",
            "State": {"Running": True, "Restarting": False, "Pid": 123},
            "Config": {"Image": image, "User": "10001:10001"},
            "HostConfig": {"ReadonlyRootfs": True, "PortBindings": {}, "RestartPolicy": {"Name": "no"}},
            "Mounts": [{"Destination": "/app/data", "Source": str(self.root / "data"), "Type": "bind", "RW": True}],
            "NetworkSettings": {"Networks": {"pulsegrid-production": {"IPAddress": "172.18.0.2"}}},
        }
        # Run the real embedded parser with only /proc file I/O substituted.
        wrapper = """
eval "$PG_REAL_CONTAINER"
PG_IMAGE="$PG_REPOSITORY@$DIGEST"
docker() { cat "$PG_STATE/container.json"; }
findmnt() { echo 259:1; }
python3() {
    command python3 -c '
import builtins, io, sys
from unittest.mock import patch
program = sys.argv[1]
sys.argv = ["-c", *sys.argv[2:]]
real_open = builtins.open
def fixture_open(path, *args, **kwargs):
    if path == "/proc/123/mountinfo":
        return io.StringIO("1 2 259:1 / /app/data rw - ext4 /dev/nvme1n1 rw\\n")
    return real_open(path, *args, **kwargs)
with patch("builtins.open", fixture_open):
    exec(program)
' "$2" "${@:3}"
}
"""
        fixture = self.root / "state/container.json"
        fixture.write_text(json.dumps([good]))
        self.shell(wrapper + 'pg_container')
        mutations = (
            lambda c: c["Config"].update(Image="unapproved"),
            lambda c: c["HostConfig"].update(PortBindings={"8080/tcp": [{}]}),
            lambda c: c["HostConfig"]["RestartPolicy"].update(Name="always"),
            lambda c: c["Mounts"][0].update(Source="/root-disk"),
            lambda c: c["Mounts"][0].update(RW=False),
            lambda c: c["State"].update(Running=False),
        )
        for mutate in mutations:
            altered = json.loads(json.dumps(good))
            mutate(altered)
            fixture.write_text(json.dumps([altered]))
            self.shell(wrapper + 'deny pg_container')
        fixture.write_text(json.dumps([good]))
        self.shell(wrapper + 'findmnt() { echo 259:2; }; deny pg_container')

    def test_storage_checks_reject_each_mismatch(self):
        # Restore the real function while mocking Linux utilities. Real block
        # devices, namespace identity, and setpriv remain Linux acceptance tests.
        storage = (RUNTIME / "check-backend-start.sh").read_text().split("pg_storage() {", 1)[1].split("\npg_admit()", 1)[0]
        fixture = "pg_storage() {" + storage + """
mountpoint() { return "${MOUNT_EXIT:-0}"; }
readlink() { printf '%s\\n' "$PG_DATA"; }
pg_device_serial() { echo "${SERIAL:-vol0123456789abcdef0}"; }
stat() { echo "${OWNER:-10001:10001:700}"; }
setpriv() { return "${WRITE_EXIT:-0}"; }
findmnt() {
    case "${!#}" in
        TARGET) echo "$PG_DATA" ;;
        UUID) echo "${UUID:-$PG_UUID}" ;;
        FSTYPE) echo "${FSTYPE:-ext4}" ;;
        OPTIONS) echo "${OPTIONS:-rw,relatime}" ;;
        SOURCE) echo /dev/nvme1n1 ;;
    esac
}
pg_storage
"""
        self.shell(fixture)
        for key, value in (("MOUNT_EXIT", "1"), ("UUID", "wrong"), ("FSTYPE", "xfs"), ("OPTIONS", "ro,relatime"), ("SERIAL", "volwrong"), ("OWNER", "0:0:700"), ("OWNER", "10001:10001:755"), ("WRITE_EXIT", "1")):
            with self.subTest(key=key, value=value):
                self.shell(fixture.rsplit("pg_storage", 1)[0] + f'{key}={value}; deny pg_storage')

    def test_caddy_deny_all_fixture_is_not_production_configuration(self):
        self.shell(f"deny pg_caddy_auth '{RUNTIME}/origin-auth.caddy.example'")

    def test_caddy_accepts_private_single_value_and_rotation_matchers(self):
        path = self.root / "origin-auth.caddy"
        for values in (("a" * 64,), ("a" * 64, "b" * 64)):
            path.write_text("@approved_api {\n path /api/*\n" + "".join(f" header X-PulseGrid-Origin-Verify {value}\n" for value in values) + "}\n")
            self.shell(f"pg_caddy_auth '{path}'")

    def test_caddy_rejects_broad_placeholder_or_duplicate_matchers(self):
        path = self.root / "origin-auth.caddy"
        cases = (
            "@approved_api {\n path /*\n header X-PulseGrid-Origin-Verify " + "a" * 64 + "\n}\n",
            "@approved_api {\n path /api/*\n header X-PulseGrid-Origin-Verify REPLACE_ME\n}\n",
            "@approved_api {\n path /api/*\n header X-PulseGrid-Origin-Verify *\n}\n",
            "@approved_api {\n path /api/*\n" + (" header X-PulseGrid-Origin-Verify " + "a" * 64 + "\n") * 2 + "}\n",
        )
        for content in cases:
            path.write_text(content)
            self.shell(f"deny pg_caddy_auth '{path}'")


if __name__ == "__main__":
    unittest.main(verbosity=2)
