#!/bin/bash
# Shared host primitives live here to keep the three runtime executables small.
# Sourcing defines functions only; executable entry points set fixed paths.

pg_initialize() {
    PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
    export PATH
    umask 077
    [[ $EUID == 0 ]] || { pg_error 'root execution required'; return 1; }
    PG_CONFIG=/etc/pulsegrid/runtime.env
    PG_STATE=/var/lib/pulsegrid-runtime
    PG_RUN=/run/pulsegrid
    PG_DATA=/var/lib/pulsegrid
    PG_UUID=9fcff7d8-57f2-4ce4-b667-eebda409636b
    PG_CONTAINER=pulsegrid-production-backend
    PG_REGION=ap-southeast-2
    # /run is ephemeral. Recreate only this fixed root-owned runtime directory;
    # never create or repair the application data directory.
    mkdir -p -m 0700 "$PG_RUN" || return 1
    pg_private "$PG_CONFIG" file || return 1
    pg_private "$PG_STATE" directory || return 1
    pg_private "$PG_RUN" directory || return 1
    pg_load_config
}

pg_error() { printf 'pulsegrid: %s\n' "$*" >&2; }
pg_digest() { [[ $1 =~ ^sha256:[0-9a-f]{64}$ ]]; }

pg_private() {
    local path=$1 kind=$2 expected=0:0:600
    [[ ! -L $path ]] || return 1
    if [[ $kind == directory ]]; then
        [[ -d $path ]] || return 1
        expected=0:0:700
    else
        [[ -f $path ]] || return 1
    fi
    [[ $(stat -c '%u:%g:%a' "$path") == "$expected" ]]
}

pg_load_config() {
    local key value
    PG_ACCOUNT= PG_VOLUME=
    while IFS='=' read -r key value || [[ -n $key ]]; do
        case "$key" in
            ''|'#'*) continue ;;
            PULSEGRID_EXPECTED_AWS_ACCOUNT_ID) PG_ACCOUNT=$value ;;
            PULSEGRID_EBS_VOLUME_ID) PG_VOLUME=$value ;;
            *) pg_error 'unexpected runtime configuration key'; return 1 ;;
        esac
    done < "$PG_CONFIG"
    [[ $PG_ACCOUNT =~ ^[0-9]{12}$ && $PG_VOLUME =~ ^vol-[0-9a-f]{17}$ ]] || {
        pg_error 'invalid expected account or EBS volume'; return 1;
    }
    PG_REPOSITORY="$PG_ACCOUNT.dkr.ecr.$PG_REGION.amazonaws.com/pulsegrid-production-backend"
}

pg_selection() {
    pg_private "$PG_STATE/selected.env" file || return 1
    local line
    line=$(cat "$PG_STATE/selected.env") || return 1
    PG_IMAGE=${line#PULSEGRID_BACKEND_IMAGE=}
    PG_DIGEST=${PG_IMAGE#"$PG_REPOSITORY@"}
    [[ $line == "PULSEGRID_BACKEND_IMAGE=$PG_REPOSITORY@$PG_DIGEST" ]] && pg_digest "$PG_DIGEST"
}

# Atomic replacement and parent-directory fsync are required on the root disk.
# Arguments contain fixed paths and digest metadata, never the origin secret.
pg_atomic() {
    python3 - "$@" <<'PY'
import json, os, pathlib, sys, tempfile
operation, filename, *values = sys.argv[1:]
path = pathlib.Path(filename)
if operation == 'remove':
    path.unlink(missing_ok=True)
else:
    if operation == 'json':
        if len(values) % 2:
            raise ValueError('record requires key/value pairs')
        content = json.dumps(dict(zip(values[::2], values[1::2])), sort_keys=True) + '\n'
    elif operation == 'text':
        content = values[0] + '\n'
    else:
        raise ValueError('unknown atomic operation')
    fd, temporary = tempfile.mkstemp(prefix='.' + path.name + '.', dir=path.parent)
    try:
        with os.fdopen(fd, 'w') as stream:
            os.fchmod(stream.fileno(), 0o600)
            stream.write(content)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)
fd = os.open(path.parent, os.O_RDONLY | getattr(os, 'O_DIRECTORY', 0))
try:
    os.fsync(fd)
finally:
    os.close(fd)
PY
}

pg_field() {
    pg_private "$1" file || return 1
    python3 - "$1" "$2" <<'PY'
import json, sys
try:
    value = json.load(open(sys.argv[1]))[sys.argv[2]]
    if not isinstance(value, str) or '\n' in value:
        raise ValueError('invalid record field')
    print(value)
except (OSError, ValueError, KeyError):
    sys.exit(1)
PY
}

pg_exists() { [[ -e $1 || -L $1 ]]; }
pg_boot_id() { cat /proc/sys/kernel/random/boot_id; }
pg_process_start() {
    python3 - "$1" <<'PY'
import os, pathlib, sys
try:
    path = pathlib.Path('/proc') / str(int(sys.argv[1]))
    if path.stat().st_uid != 0:
        raise ValueError('deployment process is not root')
    fields = (path / 'stat').read_text().rsplit(') ', 1)[1].split()
    if fields[0] in ('Z', 'X', 'x'):
        raise ValueError('deployment process has exited')
    print(fields[19])
except (OSError, ValueError, IndexError):
    sys.exit(1)
PY
}

pg_device_serial() {
    [[ $1 == /dev/* && -b $1 ]] || return 1
    lsblk -dn -o SERIAL "$1"
}

pg_storage() {
    local source serial options
    [[ ! -L $PG_DATA && $(readlink -f "$PG_DATA") == "$PG_DATA" ]] || return 1
    mountpoint -q "$PG_DATA" || return 1
    [[ $(findmnt -rn -M "$PG_DATA" -o TARGET) == "$PG_DATA" ]] || return 1
    [[ $(findmnt -rn -M "$PG_DATA" -o UUID) == "$PG_UUID" ]] || return 1
    [[ $(findmnt -rn -M "$PG_DATA" -o FSTYPE) == ext4 ]] || return 1
    options=$(findmnt -rn -M "$PG_DATA" -o OPTIONS) || return 1
    [[ ,$options, == *,rw,* && ,$options, != *,ro,* ]] || return 1
    source=$(findmnt -rn -M "$PG_DATA" -o SOURCE) || return 1
    serial=$(pg_device_serial "$source") || return 1
    serial=${serial//[[:space:]]/}
    [[ ${serial//-/} == "${PG_VOLUME//-/}" ]] || return 1
    [[ $(stat -c '%u:%g:%a' "$PG_DATA") == 10001:10001:700 ]] || return 1
    setpriv --reuid=10001 --regid=10001 --clear-groups --no-new-privs \
        /bin/sh -c '
            umask 077
            probe=$(mktemp "$1/.pulsegrid-write-check.XXXXXX") || exit 1
            trap '\''rm -f -- "$probe"'\'' EXIT HUP INT TERM
            printf check > "$probe" || exit 1
            rm -- "$probe" || exit 1
        ' sh "$PG_DATA"
}

pg_admit() {
    local candidate boot pid started deployment=
    pg_selection || { pg_error 'no approved image selection'; return 1; }
    [[ ${INVOCATION_ID:-} =~ ^[0-9a-f]{32}$ ]] || { pg_error 'systemd invocation required'; return 1; }
    # Hold wins even if a crash occurred after writing the new success record.
    if pg_exists "$PG_STATE/hold.json"; then
        candidate=$(pg_field "$PG_STATE/hold.json" candidate) || return 1
        deployment=$(pg_field "$PG_STATE/hold.json" deployment) || return 1
        [[ -n $deployment ]] || return 1
        [[ $candidate == "$PG_DIGEST" ]] || return 1
        [[ $(pg_field "$PG_RUN/permit.json" deployment) == "$deployment" ]] || return 1
        [[ $(pg_field "$PG_RUN/permit.json" candidate) == "$candidate" ]] || {
            pg_error 'deployment hold: operator review required'; return 1;
        }
        boot=$(pg_field "$PG_RUN/permit.json" boot) || return 1
        pid=$(pg_field "$PG_RUN/permit.json" pid) || return 1
        started=$(pg_field "$PG_RUN/permit.json" started) || return 1
        [[ $boot == "$(pg_boot_id)" && $pid =~ ^[0-9]+$ ]] || return 1
        [[ $started == "$(pg_process_start "$pid")" ]] || return 1
        # Spend the permit before any remaining startup operation can fail.
        pg_atomic remove "$PG_RUN/permit.json" || return 1
    else
        [[ $(pg_field "$PG_STATE/verified.json" digest) == "$PG_DIGEST" ]] || {
            pg_error 'selected image has never been verified'; return 1;
        }
    fi
    pg_storage || { pg_error 'persistent EBS verification failed'; return 1; }
    pg_atomic json "$PG_RUN/admitted.json" digest "$PG_DIGEST" invocation "$INVOCATION_ID" deployment "$deployment"
}

pg_container() {
    # The host can reach the bridge IP without publishing a backend port.
    docker inspect "$PG_CONTAINER" | python3 -c '
import ipaddress, json, os, sys
def require(condition):
    if not condition:
        raise ValueError("container verification failed")
try:
    c = json.load(sys.stdin)[0]
    expected, data, device = sys.argv[1:]
    require(c["State"]["Running"] and not c["State"]["Restarting"])
    require(c["Config"]["Image"] == expected and c["Config"]["User"] == "10001:10001")
    require(c["HostConfig"]["ReadonlyRootfs"] and not c["HostConfig"]["PortBindings"])
    require(c["HostConfig"]["RestartPolicy"]["Name"] == "no")
    mounts = [m for m in c["Mounts"] if m["Destination"] == "/app/data"]
    require(len(mounts) == 1 and mounts[0]["Type"] == "bind")
    require(mounts[0]["Source"] == data and mounts[0]["RW"])
    pid = int(c["State"]["Pid"])
    lines = open(f"/proc/{pid}/mountinfo").read().splitlines()
    require(any(l.split()[2] == device and l.split()[4] == "/app/data"
                and l.split(" - ", 1)[1].split()[0] == "ext4" for l in lines))
    ip = c["NetworkSettings"]["Networks"]["pulsegrid-production"]["IPAddress"]
    require(isinstance(ipaddress.ip_address(ip), ipaddress.IPv4Address))
    print(c["Id"], ip)
except (KeyError, IndexError, ValueError, OSError, AssertionError):
    sys.exit(1)
' "$PG_IMAGE" "$PG_DATA" "$(findmnt -rn -M "$PG_DATA" -o MAJ:MIN)"
}

pg_clear_hold() { pg_atomic remove "$PG_STATE/hold.json"; }

pg_commit() {
    local container_id=$1 previous deployment
    [[ $(pg_field "$PG_RUN/admitted.json" digest) == "$PG_DIGEST" ]] || return 1
    [[ $(pg_field "$PG_RUN/admitted.json" invocation) == "$INVOCATION_ID" ]] || return 1
    if pg_exists "$PG_STATE/hold.json"; then
        [[ $(pg_field "$PG_STATE/hold.json" candidate) == "$PG_DIGEST" ]] || return 1
        deployment=$(pg_field "$PG_STATE/hold.json" deployment) || return 1
        [[ -n $deployment && $(pg_field "$PG_RUN/admitted.json" deployment) == "$deployment" ]] || return 1
        previous=$(pg_field "$PG_STATE/hold.json" previous) || return 1
        # This order is deliberate. Failure between these writes remains held.
        pg_atomic json "$PG_STATE/verified.json" digest "$PG_DIGEST" previous "$previous" container_id "$container_id" || return 1
        if ! pg_clear_hold; then
            # An unlink whose directory fsync failed must not leave this live
            # host admitting the candidate as though commit had succeeded.
            pg_atomic json "$PG_STATE/hold.json" candidate "$PG_DIGEST" previous "$previous" deployment "$deployment" || true
            return 1
        fi
    else
        [[ $(pg_field "$PG_STATE/verified.json" digest) == "$PG_DIGEST" ]] || return 1
    fi
}

pg_caddy_auth() {
    pg_private "$1" file || return 1
    # Validate without adapting/printing Caddy JSON, which would expose secrets.
    # A normal matcher accepts one value; a rotation accepts two exact values.
    python3 - "$1" <<'PY'
import re, sys
try:
    with open(sys.argv[1]) as stream:
        lines = [line.strip() for line in stream if line.strip() and not line.lstrip().startswith('#')]
    if len(lines) not in (4, 5):
        raise ValueError('unexpected matcher structure')
    if not re.fullmatch(r'@approved_api\s*\{', lines[0]) or not re.fullmatch(r'path\s+/api/\*', lines[1]) or lines[-1] != '}':
        raise ValueError('unexpected matcher scope')
    values = []
    for line in lines[2:-1]:
        match = re.fullmatch(r'header\s+X-PulseGrid-Origin-Verify\s+([0-9a-f]{64})', line)
        if not match:
            raise ValueError('private origin matcher is not configured')
        values.append(match[1])
    if len(set(values)) != len(values):
        raise ValueError('duplicate rotation value')
except (OSError, ValueError):
    sys.exit(1)
PY
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
    set -euo pipefail
    PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
    export PATH
    if [[ $# != 0 ]]; then
        if [[ $# == 1 && $1 == --caddy-auth && $EUID == 0 ]] && pg_caddy_auth /etc/pulsegrid/caddy/origin-auth.caddy; then
            exit 0
        fi
        pg_error 'Caddy origin configuration denied; install the private matcher after Package B verification'
        exit 1
    fi
    # Expected denials use ExecCondition status 1 (no automatic restart).
    pg_initialize && pg_admit && exit 0
    pg_error 'backend start denied; inspect storage and release records'
    exit 1
fi
