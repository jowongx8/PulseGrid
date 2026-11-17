#!/bin/bash

verify_backend() {
    local deadline=$((SECONDS + 120)) container_id address observed
    local previous_container_id candidate_id='' current_id status inspected
    pg_selection || return 1
    [[ $(pg_field "$PG_RUN/admitted.json" digest) == "$PG_DIGEST" ]] || return 1
    [[ $(pg_field "$PG_RUN/admitted.json" invocation) == "$INVOCATION_ID" ]] || return 1
    previous_container_id=$(pg_field "$PG_RUN/admitted.json" previous_container_id) || return 1
    [[ -z $previous_container_id || $previous_container_id =~ ^[0-9a-f]{64}$ ]] || return 1
    pg_storage || return 1
    while (( SECONDS < deadline )); do
        # Inspect identity and status together. The stopped predecessor can
        # remain under this name until Compose's force-recreate completes.
        if inspected=$(docker inspect --type container --format '{{.Id}} {{.State.Status}}' "$PG_CONTAINER" 2>/dev/null); then
            read -r current_id status <<< "$inspected"
            [[ $current_id =~ ^[0-9a-f]{64}$ && -n $status ]] || return 1
            if [[ $current_id != "$previous_container_id" ]]; then
                if [[ -z $candidate_id ]]; then
                    candidate_id=$current_id
                elif [[ $current_id != "$candidate_id" ]]; then
                    pg_error 'backend container identity changed during startup'; return 1
                fi
                if [[ $status =~ ^(exited|dead)$ ]]; then
                    pg_error 'backend replacement exited before startup verification'; return 1
                fi
                if observed=$(pg_container); then
                    read -r container_id address <<< "$observed"
                    [[ $container_id == "$candidate_id" ]] || return 1
                    if curl --noproxy '*' --fail --silent --show-error --connect-timeout 2 --max-time 3 \
                        "http://$address:8080/health" >/dev/null 2>&1; then
                        # Require the same verified replacement after HTTP health.
                        [[ $(pg_container) == "$observed" ]] || return 1
                        pg_storage || return 1
                        pg_commit "$container_id" || return 1
                        printf 'pulsegrid: backend startup verified (%s)\n' "$PG_DIGEST" >&2
                        return 0
                    fi
                fi
            elif [[ -n $candidate_id ]]; then
                pg_error 'backend container reverted to its predecessor'; return 1
            fi
        elif [[ -n $candidate_id ]]; then
            pg_error 'backend replacement disappeared during startup'; return 1
        fi
        sleep 2
    done
    pg_error 'backend failed its 120-second startup verification'
    return 1
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
    set -euo pipefail
    source /usr/local/libexec/pulsegrid/check-backend-start.sh
    pg_initialize
    # A failed ExecStartPost makes systemd stop the container. A pending hold
    # remains unless health verification AND the durable commit both succeeded.
    verify_backend
fi
