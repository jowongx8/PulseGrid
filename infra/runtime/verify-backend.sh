#!/bin/bash

verify_backend() {
    local deadline=$((SECONDS + 120)) container_id address observed
    pg_selection || return 1
    [[ $(pg_field "$PG_RUN/admitted.json" digest) == "$PG_DIGEST" ]] || return 1
    [[ $(pg_field "$PG_RUN/admitted.json" invocation) == "$INVOCATION_ID" ]] || return 1
    pg_storage || return 1
    while (( SECONDS < deadline )); do
        if observed=$(pg_container); then
            read -r container_id address <<< "$observed"
            if curl --noproxy '*' --fail --silent --show-error --connect-timeout 2 --max-time 3 \
                "http://$address:8080/health" >/dev/null 2>&1; then
                # Recheck identity after the HTTP request; a replacement is not ready.
                [[ $(pg_container) == "$observed" ]] || return 1
                pg_storage || return 1
                pg_commit "$container_id" || return 1
                printf 'pulsegrid: backend startup verified (%s)\n' "$PG_DIGEST" >&2
                return 0
            fi
        fi
        # Docker may still be creating the container. Actual exits fail immediately.
        if [[ $(docker inspect --format '{{.State.Status}}' "$PG_CONTAINER" 2>/dev/null || true) =~ ^(exited|dead)$ ]]; then
            pg_error 'backend exited before startup verification'
            return 1
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
