#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
cd "$repo_root"

cover_out="${COVER_OUT:-coverage.out}"
cover_txt="${COVER_TXT:-${cover_out%.out}.txt}"
cover_html="${COVER_HTML:-${cover_out%.out}.html}"
gocache_dir="${GOCACHE:-/tmp/go-build-cache}"
include_e2e="${INCLUDE_E2E:-1}"

tmp_dir="$(mktemp -d)"
covdata_bin="$tmp_dir/covdata"
unit_covdir="$tmp_dir/unit-cov"
e2e_covdir="$tmp_dir/e2e-cov"
merged_covdir="$tmp_dir/merged-cov"

cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

mkdir -p "$unit_covdir" "$e2e_covdir" "$merged_covdir"

pkgs=(
  .
  ./remotehelper
  ./client
)

mapfile -t pkg_imports < <(go list "${pkgs[@]}")
coverpkg_list="$(printf '%s,' "${pkg_imports[@]}")"
coverpkg_list="${coverpkg_list%,}"

echo "Building local covdata helper..."
env GOCACHE="$gocache_dir" go build -o "$covdata_bin" /home/kawai/goroot/src/cmd/covdata

echo "Running Go tests with raw coverage collection for: ${pkgs[*]}"
for pkg in "${pkgs[@]}"; do
  pkg_import="$(go list "$pkg")"
  pkg_dir="$(go list -f '{{.Dir}}' "$pkg")"
  test_go_files="$(go list -f '{{len .TestGoFiles}}' "$pkg")"
  xtest_go_files="$(go list -f '{{len .XTestGoFiles}}' "$pkg")"
  if [ "$test_go_files" = "0" ] && [ "$xtest_go_files" = "0" ]; then
    echo "Skipping unit coverage binary for $pkg_import because it has no Go test files"
    continue
  fi
  pkg_name="$(printf '%s' "$pkg_import" | sed 's#[^A-Za-z0-9_.-]#_#g')"
  test_bin="$tmp_dir/${pkg_name}.test"

  env GOCACHE="$gocache_dir" go test -c \
    -covermode=atomic \
    -coverpkg="$coverpkg_list" \
    -o "$test_bin" \
    "$pkg"

  (
    cd "$pkg_dir"
    "$test_bin" -test.paniconexit0 -test.gocoverdir="$unit_covdir"
  )

  echo "Collected unit coverage for $pkg_import"
done

if [ "$include_e2e" = "1" ]; then
  echo "Building coverage-instrumented helper for e2e..."
  env GOCACHE="$gocache_dir" go build \
    -covermode=atomic \
    -coverpkg="$coverpkg_list" \
    -o git-remote-mediawiki \
    .

  echo "Running e2e coverage via test/e2e.sh..."
  (
    cd "$script_dir"
    GOCOVERDIR="$e2e_covdir" bash ./e2e.sh
  )
else
  echo "Skipping e2e coverage because INCLUDE_E2E=$include_e2e"
fi

merge_inputs="$unit_covdir"
if [ "$include_e2e" = "1" ]; then
  merge_inputs="$merge_inputs,$e2e_covdir"
fi

echo "Merging raw coverage data..."
"$covdata_bin" merge -i="$merge_inputs" -o="$merged_covdir"
"$covdata_bin" textfmt -i="$merged_covdir" -o="$cover_out"

echo "Generating summaries..."
go tool cover -func="$cover_out" | tee "$cover_txt"
go tool cover -html="$cover_out" -o "$cover_html"

echo "Coverage profile: $cover_out"
echo "Coverage summary: $cover_txt"
echo "Coverage HTML: $cover_html"
if [ "$include_e2e" = "1" ]; then
  echo "Coverage includes unit tests and test/e2e.sh."
else
  echo "Coverage includes unit tests only. Set INCLUDE_E2E=1 to include test/e2e.sh."
fi
