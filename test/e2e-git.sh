#!/usr/bin/env bash
set -euo pipefail

. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/e2e-lib.sh"

e2e_init
e2e_start_stack
e2e_wait_for_api
e2e_ensure_helper
e2e_run_core_scenario "E2E"
e2e_run_tracked_subset_scenario "E2E"
e2e_run_shallow_clone_scenario "E2E"

echo "E2E git test passed. Cleaning up..."
e2e_cleanup
