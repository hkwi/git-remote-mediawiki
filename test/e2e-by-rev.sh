#!/usr/bin/env bash
set -euo pipefail

. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/e2e-lib.sh"

e2e_init
e2e_start_stack
e2e_wait_for_api
e2e_exec_in_git_container "e2e-by-rev-inner.sh"

echo "E2E by_rev git test passed. Cleaning up..."
e2e_cleanup
