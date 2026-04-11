#!/usr/bin/env bash
set -euo pipefail

# Entry point to run all e2e scripts in this directory.
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Default: enable helper debug output during e2e (can be overridden externally)
GIT_REMOTE_MEDIAWIKI_DEBUG="${GIT_REMOTE_MEDIAWIKI_DEBUG:-1}"
export GIT_REMOTE_MEDIAWIKI_DEBUG

echo "Running e2e: git scenario..."
bash "$script_dir/e2e-git.sh"

echo "Running e2e: by_rev scenario..."
bash "$script_dir/e2e-by-rev.sh"

echo "All e2e scripts completed."
