#!/bin/bash

deploy_cleanup() {
    local status=$?
    trap - EXIT HUP INT TERM
    if [[ ${PG_ATTEMPT:-0} == 1 ]]; then
        # Startup verification belongs to the unit, so explicitly cancel it too.
        systemctl stop pulsegrid-backend.service || true
        pg_atomic remove "$PG_RUN/permit.json" || true
        pg_error 'deployment incomplete: backend stopped; review durable release records'
    fi
    [[ -z ${PG_DOCKER_CONFIG:-} ]] || rm -rf -- "$PG_DOCKER_CONFIG"
    exit "$status"
}

pg_pull() {
    PG_DOCKER_CONFIG=$(mktemp -d "$PG_RUN/ecr-login.XXXXXX") || return 1
    export DOCKER_CONFIG=$PG_DOCKER_CONFIG
    # A clean AWS environment excludes profiles, web-identity credentials,
    # custom endpoints, and static keys; only the host instance role is used.
    env -i PATH="$PATH" AWS_CONFIG_FILE=/dev/null AWS_SHARED_CREDENTIALS_FILE=/dev/null \
        AWS_EC2_METADATA_DISABLED=false AWS_EC2_METADATA_V1_DISABLED=true AWS_MAX_ATTEMPTS=3 \
        aws ecr get-login-password --region "$PG_REGION" --cli-connect-timeout 10 --cli-read-timeout 60 |
        docker --config "$PG_DOCKER_CONFIG" login --username AWS --password-stdin "${PG_REPOSITORY%%/*}" >/dev/null || return 1
    timeout 300s docker --config "$PG_DOCKER_CONFIG" pull --platform linux/arm64 "$PG_REPOSITORY@$PG_CANDIDATE" || return 1
    [[ $(docker image inspect --format '{{.Os}}/{{.Architecture}}' "$PG_REPOSITORY@$PG_CANDIDATE") == linux/arm64 ]]
}

deploy_backend() {
    [[ $# == 1 ]] && pg_digest "$1" || { pg_error 'exactly one sha256 digest is required'; return 1; }
    PG_CANDIDATE=$1
    PG_ATTEMPT=0
    PG_DOCKER_CONFIG=
    exec 9>"$PG_RUN/deployment.lock"
    flock -n 9 || { pg_error 'another deployment is active'; return 1; }
    if pg_exists "$PG_STATE/hold.json"; then
        pg_error 'unresolved deployment hold: manual review required'
        return 1
    fi
    local previous= process_start boot deployment
    if pg_exists "$PG_STATE/verified.json"; then
        previous=$(pg_field "$PG_STATE/verified.json" digest) || return 1
        pg_digest "$previous" || return 1
        if [[ $previous == "$PG_CANDIDATE" ]]; then
            previous=$(pg_field "$PG_STATE/verified.json" previous) || return 1
            [[ -z $previous ]] || pg_digest "$previous" || return 1
        fi
    fi
    pg_storage || { pg_error 'storage check failed before deployment'; return 1; }
    pg_pull || return 1
    process_start=$(pg_process_start "$$") || return 1
    boot=$(pg_boot_id) || return 1
    deployment="$boot:$$:$process_start"
    # All preflight operations leave the running release untouched. From here,
    # the durable hold precedes service stop, selection, and any Goose migration.
    pg_atomic json "$PG_STATE/hold.json" candidate "$PG_CANDIDATE" previous "$previous" deployment "$deployment" || return 1
    PG_ATTEMPT=1
    systemctl stop pulsegrid-backend.service || return 1
    if systemctl is-active --quiet pulsegrid-backend.service; then
        pg_error 'previous backend did not stop'; return 1
    fi
    # Confirm absence of a surviving writer, independently of systemd's state.
    [[ $(docker inspect --format '{{.State.Running}}' "$PG_CONTAINER" 2>/dev/null || true) != true ]] || return 1
    pg_storage || return 1
    pg_atomic text "$PG_STATE/selected.env" "PULSEGRID_BACKEND_IMAGE=$PG_REPOSITORY@$PG_CANDIDATE" || return 1
    pg_atomic json "$PG_RUN/permit.json" candidate "$PG_CANDIDATE" deployment "$deployment" boot "$boot" pid "$$" started "$process_start" || return 1
    # This exact-name glob skips an unloaded unit, which has no failure state.
    # --all also resets start counters on loaded inactive units. Real manager
    # and reset errors still abort; [p] cannot match another service name.
    systemctl reset-failed --all '[p]ulsegrid-backend.service' || return 1
    # systemctl waits for ExecStartPost, which verifies and commits the release.
    systemctl start pulsegrid-backend.service || return 1
    systemctl is-active --quiet pulsegrid-backend.service || return 1
    [[ $(pg_field "$PG_STATE/verified.json" digest) == "$PG_CANDIDATE" ]] || return 1
    if pg_exists "$PG_STATE/hold.json"; then
        pg_error 'release commit did not complete'; return 1
    fi
    PG_ATTEMPT=0
    printf 'pulsegrid: deployed %s\n' "$PG_CANDIDATE"
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
    set -euo pipefail
    source /usr/local/libexec/pulsegrid/check-backend-start.sh
    pg_initialize
    trap deploy_cleanup EXIT
    trap 'exit 129' HUP
    trap 'exit 130' INT
    trap 'exit 143' TERM
    deploy_backend "$@"
fi
