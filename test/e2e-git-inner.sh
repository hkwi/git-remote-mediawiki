#!/usr/bin/env bash
set -euo pipefail

. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/e2e-lib.sh"

e2e_ensure_helper
e2e_run_core_scenario "E2E"
e2e_run_tracked_subset_scenario "E2E"
e2e_run_shallow_clone_scenario "E2E"
e2e_run_multiple_remotes_scenario "E2E"
