#!/bin/bash
set -euo pipefail

# Reviewed upstream release checksums. No installer is piped into a shell.
version=1.7.12
case "$(uname -s)/$(uname -m)" in
    Linux/x86_64)
        platform=linux_amd64
        checksum=8aca8db96f1b94770f1b0d72b6dddcb1ebb8123cb3712530b08cc387b349a3d8
        ;;
    Darwin/arm64)
        platform=darwin_arm64
        checksum=aba9ced2dee8d27fecca3dc7feb1a7f9a52caefa1eb46f3271ea66b6e0e6953f
        ;;
    *) echo 'Unsupported actionlint validation platform.' >&2; exit 1 ;;
esac
lint_dir=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/pulsegrid-actionlint.XXXXXX")
trap 'rm -rf -- "$lint_dir"' EXIT
archive="$lint_dir/actionlint.tar.gz"
curl --fail --silent --show-error --location --connect-timeout 10 --max-time 60 \
    "https://github.com/rhysd/actionlint/releases/download/v$version/actionlint_${version}_${platform}.tar.gz" \
    --output "$archive"
python3 - "$archive" "$checksum" "$lint_dir/actionlint" <<'PY'
import hashlib
from pathlib import Path
import sys
import tarfile

archive, expected, destination = sys.argv[1:]
if hashlib.sha256(Path(archive).read_bytes()).hexdigest() != expected:
    sys.exit('actionlint checksum mismatch')
with tarfile.open(archive) as source:
    Path(destination).write_bytes(source.extractfile('actionlint').read())
Path(destination).chmod(0o700)
PY
"$lint_dir/actionlint" -color
