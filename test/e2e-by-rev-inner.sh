#!/usr/bin/env bash
set -euo pipefail

. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/e2e-lib.sh"

e2e_ensure_helper
# Keep the by_rev variant focused on incremental import behavior so it is
# meaningfully different from the default clone/push scenario.
e2e_run_incremental_fetch_scenario "E2E_by_rev" -c mediawiki.fetchStrategy=by_rev
