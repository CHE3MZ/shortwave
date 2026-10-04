#!/usr/bin/env bash

set -o pipefail

# Always run from the repo root regardless of where the script is invoked.
cd "$(dirname "$0")/../.."

mkdir -p logs

failed=0

run_check() {
    local name="$1"
    shift

    echo "==> Running $name"

    if "$@" 2>&1 | tee "logs/go-seccheck-${name}.txt"; then
        echo "    $name: OK"
    else
        echo "    $name: FAILED"
        failed=1
    fi

    echo
}

run_check gosec gosec ./...

exit "$failed"