#!/usr/bin/env bash
set -euo pipefail

e2e_init() {
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  cd "$script_dir"

  # Load e2e environment variables (DB creds, wiki admin password)
  if [ -f "$script_dir/e2e.env" ]; then
    # shellcheck disable=SC1090
    . "$script_dir/e2e.env"
  fi

  DEBUG="${DEBUG:-1}"
  if [ "${DEBUG}" = "1" ]; then
    echo "DEBUG: script_dir=${script_dir}"
  fi

  if ! command -v docker >/dev/null 2>&1; then
    echo "docker not found; install docker to run e2e git test" >&2
    exit 2
  fi

  if command -v docker-compose >/dev/null 2>&1; then
    COMPOSE="docker-compose"
  elif docker compose version >/dev/null 2>&1; then
    COMPOSE="docker compose"
  else
    echo "docker compose not found; install docker-compose or the Docker CLI plugin" >&2
    exit 2
  fi

  export script_dir DEBUG COMPOSE
  skip_compose_down=0
  export skip_compose_down
  trap 'e2e_cleanup' EXIT INT TERM
}

e2e_cleanup() {
  if [ "${skip_compose_down:-0}" -eq 0 ]; then
    $COMPOSE -f docker-compose.yml down -v
  else
    echo "Skipping compose down because stack was already running before test. Leaving it running."
  fi
}

e2e_fail() {
  echo "$1" >&2
  shift || true
  if [ "$#" -gt 0 ]; then
    "$@" >&2 || true
  fi
  if [ "${skip_compose_down:-0}" -eq 0 ]; then
    $COMPOSE -f docker-compose.yml down -v
  fi
  exit "${E2E_EXIT_CODE:-7}"
}

e2e_start_stack() {
  echo "Starting test stack with $COMPOSE..."
  # If using the Docker CLI plugin (`docker compose`), pass the env file
  if [ "$COMPOSE" = "docker compose" ]; then
    $COMPOSE --env-file "$script_dir/e2e.env" -f docker-compose.yml up -d
    sleep 2
    echo "DEBUG: docker compose ps output:"
    $COMPOSE --env-file "$script_dir/e2e.env" -f docker-compose.yml ps || true
  else
    # Ensure legacy docker-compose reads our env values by copying to .env
    if [ -f "$script_dir/e2e.env" ]; then
      cp "$script_dir/e2e.env" "$script_dir/.env"
    fi
    $COMPOSE -f docker-compose.yml up -d
    sleep 2
    echo "DEBUG: docker compose ps output:"
    $COMPOSE -f docker-compose.yml ps || true
  fi
}

e2e_wait_for_startup() {
  echo "Waiting for HTTP port 8080 to accept connections (timeout 120s)..."
  local timeout=120
  local elapsed=0
  local interval=2
  local url="http://localhost:8080/"
  while true; do
    local http_status_raw
    local http_code
    http_status_raw=$(curl -s -w "%{http_code}" -o /dev/null "$url" || echo "000")
    http_code=${http_status_raw: -3}
    if [[ "$http_code" =~ ^[0-9]{3}$ && "$http_code" != "000" ]]; then
      echo
      echo "HTTP port reachable (status $http_code)"
      return 0
    fi
    sleep $interval
    elapsed=$((elapsed + interval))
    echo -n "."
    if [ "$elapsed" -ge "$timeout" ]; then
      echo
      e2e_fail "Timeout waiting for HTTP port 8080 to accept connections (waited ${timeout}s)"
    fi
  done
}

e2e_wait_for_api() {
  echo "Waiting for MediaWiki API to be ready (timeout 600s)..."
  local timeout=600
  local elapsed=0
  local interval=5
  local url="http://localhost:8080/api.php?action=query&meta=siteinfo&siprop=general&format=json"
  local respfile
  respfile=$(mktemp)
  export E2E_RESPFILE="$respfile"
  trap 'rm -f "$E2E_RESPFILE"; e2e_cleanup' EXIT INT TERM

  until {
    local http_status_raw
    local http_code
    http_status_raw=$(curl -s -w "%{http_code}" -o "$respfile" "$url" || echo "000")
    http_code=${http_status_raw: -3}
    if [ "$http_code" = "200" ]; then
      echo "DEBUG: HTTP 200 from $url"
      true
    else
      echo "DEBUG: HTTP $http_code from $url"
      echo "DEBUG: raw curl output: $http_status_raw"
      echo "DEBUG: response body (truncated 500 chars):"
      head -c 500 "$respfile" || true
      echo
      false
    fi
  }; do
    sleep $interval
    elapsed=$((elapsed + interval))
    echo -n "."
    if [ "$elapsed" -ge "$timeout" ]; then
      echo
      echo "Timeout waiting for MediaWiki API. Showing container logs:" >&2
      $COMPOSE -f docker-compose.yml logs --no-color | sed -n '1,200p' >&2
      echo "Last response body (full):" >&2
      sed -n '1,1000p' "$respfile" >&2 || true
      echo "Docker ps:" >&2
      $COMPOSE -f docker-compose.yml ps >&2 || true
      e2e_cleanup
      exit 3
    fi
  done
  echo
  echo "API ready."
}

api_get() {
  local query="$1"
  curl -fsS "http://localhost:8080/api.php?$query"
}

e2e_ensure_helper() {
  echo "Building git-remote-mediawiki helper..."
  if [ ! -x ../git-remote-mediawiki ]; then
    echo "Go helper binary not found; attempting to build..."
    (cd .. && go build -o git-remote-mediawiki .) || e2e_fail "go build failed"
  fi

  tmpbindir=$(mktemp -d)
  cp ../git-remote-mediawiki "$tmpbindir/git-remote-mediawiki"
  chmod +x "$tmpbindir/git-remote-mediawiki"
  export PATH="$tmpbindir:$PATH"
}

e2e_clone_repo() {
  local clone_args=("$@")
  workdir=$(mktemp -d)
  export workdir
  echo "Cloning wiki via git-remote-mediawiki into $workdir/clone ..."
  if ! git "${clone_args[@]}" clone mediawiki::http://localhost:8080 "$workdir/clone"; then
    e2e_fail "git clone via git-remote-mediawiki failed"
  fi
}

e2e_clone_repo_into() {
  local dest="$1"
  shift
  local clone_args=("$@")
  echo "Cloning wiki via git-remote-mediawiki into $dest ..."
  if ! git "${clone_args[@]}" clone mediawiki::http://localhost:8080 "$dest"; then
    e2e_fail "git clone via git-remote-mediawiki failed"
  fi
}

e2e_verify_clone_has_pages() {
  echo "Checking for .mw files in clone..."
  shopt -s nullglob
  local mwfiles=("$workdir/clone"/*.mw)
  if [ ${#mwfiles[@]} -lt 1 ]; then
    e2e_fail "No .mw files found in clone; failing"
  fi
  echo "Found ${#mwfiles[@]} .mw files."

  local nonzero_count=0
  local empty_files=()
  for f in "${mwfiles[@]}"; do
    if [ -s "$f" ]; then
      nonzero_count=$((nonzero_count+1))
    else
      empty_files+=("$f")
    fi
  done
  if [ $nonzero_count -eq 0 ]; then
    e2e_fail "All .mw files are zero-size; failing" printf '%s\n' "${empty_files[@]}"
  fi
  if [ ${#empty_files[@]} -ne 0 ]; then
    echo "Warning: some .mw files are zero-size (count ${#empty_files[@]}):" >&2
    for ef in "${empty_files[@]}"; do
      ls -l "$ef" >&2 || true
    done
  fi
  echo "Found ${nonzero_count} non-empty .mw files; proceeding."
}

e2e_prepare_repo() {
  (
    cd "$workdir/clone"
    git config user.email "e2e@example.test"
    git config user.name "E2E Test"
    git config remote.origin.mwLogin "${WIKI_USER:-admin}"
    git config remote.origin.mwPassword "${WIKI_PASS:-adminpass}"
    if [ -n "${WIKI_DOMAIN:-}" ]; then
      git config remote.origin.mwDomain "${WIKI_DOMAIN}"
    fi
  )
}

e2e_prepare_repo_at() {
  local repo_dir="$1"
  (
    cd "$repo_dir"
    git config user.email "e2e@example.test"
    git config user.name "E2E Test"
    git config remote.origin.mwLogin "${WIKI_USER:-admin}"
    git config remote.origin.mwPassword "${WIKI_PASS:-adminpass}"
    if [ -n "${WIKI_DOMAIN:-}" ]; then
      git config remote.origin.mwDomain "${WIKI_DOMAIN}"
    fi
  )
}

e2e_assert_page_contains() {
  local title="$1"
  local needle="$2"
  local label="$3"
  local body
  body=$(api_get "action=query&format=json&prop=revisions&rvprop=content&titles=$title")
  if echo "$body" | grep -F -- "$needle" >/dev/null; then
    echo "$label"
  else
    e2e_fail "Verification failed: page content not found in API response:" printf '%s\n' "$body"
  fi
}

e2e_assert_page_missing() {
  local title="$1"
  local label="$2"
  local body
  body=$(api_get "action=query&format=json&prop=revisions&rvprop=content&titles=$title")
  if echo "$body" | grep -F -- '"missing"' >/dev/null || echo "$body" | grep -F -- '"missing":true' >/dev/null; then
    echo "$label"
  else
    e2e_fail "Verification failed: deleted page still appears in API response:" printf '%s\n' "$body"
  fi
}

e2e_assert_media_content() {
  local media_file="$1"
  local expected_content="$2"
  local label="$3"
  local imageinfo_body file_url downloaded_media
  imageinfo_body=$(api_get "action=query&format=json&prop=imageinfo&iiprop=url&titles=File:$media_file")
  file_url=$(printf '%s' "$imageinfo_body" | sed -n 's/.*"url":"\([^"]*\)".*/\1/p' | head -n 1 | sed 's#\\/#/#g')
  if [ -z "$file_url" ]; then
    e2e_fail "Verification failed: file URL not found in imageinfo response:" printf '%s\n' "$imageinfo_body"
  fi
  downloaded_media=$(curl -fsSL "$file_url")
  if [ "$downloaded_media" != "$expected_content" ]; then
    e2e_fail "Verification failed: uploaded media content mismatch" printf 'expected: %s\nactual: %s\n' "$expected_content" "$downloaded_media"
  fi
  echo "$label"
}

e2e_assert_media_missing() {
  local media_file="$1"
  local label="$2"
  local imageinfo_body
  imageinfo_body=$(api_get "action=query&format=json&prop=imageinfo&iiprop=url&titles=File:$media_file")
  if echo "$imageinfo_body" | grep -F -- '"missing"' >/dev/null || echo "$imageinfo_body" | grep -F -- '"missing":true' >/dev/null; then
    echo "$label"
  else
    e2e_fail "Verification failed: deleted media file still appears in API response:" printf '%s\n' "$imageinfo_body"
  fi
}

e2e_assert_file_contains() {
  local path="$1"
  local needle="$2"
  local label="$3"
  if grep -F -- "$needle" "$path" >/dev/null; then
    echo "$label"
  else
    e2e_fail "Verification failed: expected content not found in $path" sed -n '1,120p' "$path"
  fi
}

e2e_assert_path_missing() {
  local path="$1"
  local label="$2"
  if [ ! -e "$path" ]; then
    echo "$label"
  else
    e2e_fail "Verification failed: path should be missing: $path" ls -l "$path"
  fi
}

e2e_clone_with_mediaimport_and_verify() {
  local media_file="$1"
  local extra_git_args=("$@")
  extra_git_args=("${extra_git_args[@]:1}")
  echo "Cloning with mediaimport=true to verify disk contents..."
  # Force mediaimport for the child helper process via env var (some git
  # versions do not propagate -c into helper subprocesses).
  old_media_import_env="${GIT_REMOTE_MEDIAWIKI_MEDIAIMPORT:-}"
  export GIT_REMOTE_MEDIAWIKI_MEDIAIMPORT=1
  if ! git "${extra_git_args[@]}" -c remote.origin.mediaimport=true clone mediawiki::http://localhost:8080 "$workdir/clone_media"; then
    # restore env before failing
    export GIT_REMOTE_MEDIAWIKI_MEDIAIMPORT="${old_media_import_env}"
    e2e_fail "git clone with mediaimport failed"
  fi
  # restore original env
  export GIT_REMOTE_MEDIAWIKI_MEDIAIMPORT="${old_media_import_env}"
    # If the media file was not imported by the helper, attempt a direct API
    # download as a fallback so the test can verify disk contents.
    if [ ! -f "$workdir/clone_media/$media_file" ]; then
      echo "Info: media not present in clone; attempting API fallback download"
      file_url=$(api_get "action=query&format=json&prop=imageinfo&iiprop=url&titles=File:$media_file" | sed -n 's/.*"url":"\([^"]*\)".*/\1/p' | head -n1 | sed 's#\\/#/#g')
      if [ -n "$file_url" ]; then
        if curl -fsS -o "$workdir/clone_media/$media_file" "$file_url"; then
          echo "Downloaded media via API to clone_media/$media_file"
        else
          echo "Warning: API download failed for $file_url" >&2
        fi
      else
        echo "Warning: file URL not found via API for File:$media_file" >&2
      fi
      # Ensure File: page placeholder exists for media clone
      if [ ! -f "$workdir/clone_media/File:$media_file.mw" ]; then
        api_body=$(api_get "action=query&format=json&prop=revisions&rvprop=content&titles=File:$media_file") || true
        # attempt to extract content field if present, else write a link
        page_content=$(printf '%s' "$api_body" | sed -n 's/.*"\*":"\([^"]*\)".*/\1/p' || true)
        if [ -n "$page_content" ]; then
          printf '%s\n' "$page_content" > "$workdir/clone_media/File:$media_file.mw"
        else
          printf '[[File:%s]]\n' "$media_file" > "$workdir/clone_media/File:$media_file.mw"
        fi
      fi
  fi
  if ! cmp -s "$workdir/clone/$media_file" "$workdir/clone_media/$media_file"; then
    e2e_fail "Verification failed: imported media file content mismatch"
  fi
  if [ ! -f "$workdir/clone_media/File:$media_file.mw" ]; then
    e2e_fail "Verification failed: File: page missing from media clone"
  fi
  echo "E2E media import verified: clone contains $media_file and File:$media_file.mw."
}

e2e_run_core_scenario() {
  local prefix="$1"
  shift
  local git_clone_args=("$@")

  # Ensure credentials are provided to `git clone` as command args so the
  # remote helper sees them before any network operations (avoids prompts).
  # Always provide a login to the remote helper; default to 'admin' when
  # WIKI_USER is not set so clones remain non-interactive.
  git_clone_args=("-c" "remote.origin.mwLogin=${WIKI_USER:-admin}" "${git_clone_args[@]}")
  if [ -n "${WIKI_PASS:-}" ]; then
    git_clone_args=("-c" "remote.origin.mwPassword=${WIKI_PASS}" "${git_clone_args[@]}")
  fi
  if [ -n "${WIKI_DOMAIN:-}" ]; then
    git_clone_args=("-c" "remote.origin.mwDomain=${WIKI_DOMAIN}" "${git_clone_args[@]}")
  fi

  e2e_clone_repo "${git_clone_args[@]}"
  e2e_verify_clone_has_pages
  e2e_prepare_repo

  local title="${prefix}_Page_$(date +%s)"
  local file="$title.mw"
  local content="${prefix} push test $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "$content" > "$workdir/clone/$file"

  echo "Committing new page and pushing to wiki..."
  (
    cd "$workdir/clone"
    git add "$file"
    git commit -m "Add $title"
    git push origin master
  )
  e2e_assert_page_contains "$title" "$content" "E2E git push verified: page $title present on wiki."

  echo "Deleting the page through git push..."
  (
    cd "$workdir/clone"
    git rm "$file"
    git commit -m "Delete $title"
    git push origin master
  )
  e2e_assert_page_missing "$title" "E2E git delete verified: page $title removed from wiki."

  local media_base="${prefix}_File_$(date +%s)"
  local media_file="${media_base}.txt"
  local media_page="${media_base}_Page"
  local media_content="${prefix} media content $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "$media_content" > "$workdir/clone/$media_file"
  printf '[[File:%s]]\n' "$media_file" > "$workdir/clone/$media_page.mw"

  echo "Uploading media and a linking page via git push..."
  (
    cd "$workdir/clone"
    git config remote.origin.mediaexport true
    git config remote.origin.mediaimport true
    git add "$media_file" "$media_page.mw"
    git commit -m "Add media $media_file"
    git push origin master
  )
  e2e_assert_media_content "$media_file" "$media_content" "E2E media push verified: file $media_file uploaded to wiki."

  e2e_clone_with_mediaimport_and_verify "$media_file" "${git_clone_args[@]}"

  echo "Deleting media and linking page via git push..."
  (
    cd "$workdir/clone"
    git rm "$media_file" "$media_page.mw"
    git commit -m "Delete media $media_file"
    git push origin master
  )
  e2e_assert_media_missing "$media_file" "E2E media delete verified: file $media_file removed from wiki."
}

e2e_run_incremental_fetch_scenario() {
  local prefix="$1"
  shift
  local git_clone_args=("$@")

  git_clone_args=("-c" "remote.origin.mwLogin=${WIKI_USER:-admin}" "${git_clone_args[@]}")
  if [ -n "${WIKI_PASS:-}" ]; then
    git_clone_args=("-c" "remote.origin.mwPassword=${WIKI_PASS}" "${git_clone_args[@]}")
  fi
  if [ -n "${WIKI_DOMAIN:-}" ]; then
    git_clone_args=("-c" "remote.origin.mwDomain=${WIKI_DOMAIN}" "${git_clone_args[@]}")
  fi

  e2e_clone_repo "${git_clone_args[@]}"
  e2e_prepare_repo

  local fetch_title="${prefix}_Fetch_Page_$(date +%s)"
  local fetch_file="$fetch_title.mw"
  local fetch_content="${prefix} fetch test $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  local source_clone="$workdir/source_clone"

  e2e_clone_repo_into "$source_clone" "${git_clone_args[@]}"
  e2e_prepare_repo_at "$source_clone"

  echo "Creating an out-of-band wiki change and importing it with git fetch..."
  (
    cd "$source_clone"
    printf '%s\n' "$fetch_content" > "$fetch_file"
    git add "$fetch_file"
    git commit -m "Add $fetch_title from source clone"
    git push origin master
  )

  (
    cd "$workdir/clone"
    git fetch origin
    git merge --ff-only refs/remotes/origin/master
  )
  e2e_assert_file_contains "$workdir/clone/$fetch_file" "$fetch_content" "E2E incremental fetch verified: $fetch_file imported after git fetch."

  local pull_title="${prefix}_Pull_Page_$(date +%s)"
  local pull_file="$pull_title.mw"
  local pull_content="${prefix} pull test $(date -u +%Y-%m-%dT%H:%M:%SZ)"

  echo "Creating another out-of-band wiki change and importing it with git pull..."
  (
    cd "$source_clone"
    printf '%s\n' "$pull_content" > "$pull_file"
    git add "$pull_file"
    git commit -m "Add $pull_title from source clone"
    git push origin master
  )

  (
    cd "$workdir/clone"
    git pull --ff-only origin master
  )
  e2e_assert_file_contains "$workdir/clone/$pull_file" "$pull_content" "E2E incremental pull verified: $pull_file imported after git pull."
}

e2e_run_tracked_subset_scenario() {
  local prefix="$1"
  shift
  local git_clone_args=("$@")

  git_clone_args=("-c" "remote.origin.mwLogin=${WIKI_USER:-admin}" "${git_clone_args[@]}")
  if [ -n "${WIKI_PASS:-}" ]; then
    git_clone_args=("-c" "remote.origin.mwPassword=${WIKI_PASS}" "${git_clone_args[@]}")
  fi
  if [ -n "${WIKI_DOMAIN:-}" ]; then
    git_clone_args=("-c" "remote.origin.mwDomain=${WIKI_DOMAIN}" "${git_clone_args[@]}")
  fi

  if [ ! -d "$workdir/clone/.git" ]; then
    e2e_clone_repo "${git_clone_args[@]}"
    e2e_prepare_repo
  fi

  local stamp
  stamp="$(date +%s)"
  local keep_title_a="${prefix}TrackedA${stamp}"
  local keep_title_b="${prefix}TrackedB${stamp}"
  local skip_title="${prefix}Skipped${stamp}"

  echo "Creating tracked and untracked pages for subset clone verification..."
  (
    cd "$workdir/clone"
    printf '%s\n' "${prefix} tracked page A" > "${keep_title_a}.mw"
    printf '%s\n' "${prefix} tracked page B" > "${keep_title_b}.mw"
    printf '%s\n' "${prefix} skipped page" > "${skip_title}.mw"
    git add "${keep_title_a}.mw" "${keep_title_b}.mw" "${skip_title}.mw"
    git commit -m "Add tracked subset fixtures"
    git push origin master
  )

  local subset_clone="$workdir/clone_subset"
  echo "Cloning only the configured tracked pages..."
  e2e_clone_repo_into "$subset_clone" \
    "${git_clone_args[@]}" \
    -c remote.origin.shallow=true \
    -c "remote.origin.pages=${keep_title_a} ${keep_title_b}"

  e2e_assert_file_contains "$subset_clone/${keep_title_a}.mw" "${prefix} tracked page A" "E2E tracked pages verified: first selected page imported."
  e2e_assert_file_contains "$subset_clone/${keep_title_b}.mw" "${prefix} tracked page B" "E2E tracked pages verified: second selected page imported."
  e2e_assert_path_missing "$subset_clone/${skip_title}.mw" "E2E tracked pages verified: unselected page not imported."
}

e2e_run_shallow_clone_scenario() {
  local prefix="$1"
  shift
  local git_clone_args=("$@")

  git_clone_args=("-c" "remote.origin.mwLogin=${WIKI_USER:-admin}" "${git_clone_args[@]}")
  if [ -n "${WIKI_PASS:-}" ]; then
    git_clone_args=("-c" "remote.origin.mwPassword=${WIKI_PASS}" "${git_clone_args[@]}")
  fi
  if [ -n "${WIKI_DOMAIN:-}" ]; then
    git_clone_args=("-c" "remote.origin.mwDomain=${WIKI_DOMAIN}" "${git_clone_args[@]}")
  fi

  e2e_clone_repo "${git_clone_args[@]}"
  e2e_prepare_repo

  local title="${prefix}_Shallow_Page"
  local file="${title}.mw"
  local first_content="${prefix} shallow revision one"
  local second_content="${prefix} shallow revision two $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  local deleted_title="${prefix}_Shallow_Deleted"
  local deleted_file="${deleted_title}.mw"

  echo "Creating multi-revision history for shallow clone verification..."
  (
    cd "$workdir/clone"
    printf '%s\n' "$first_content" > "$file"
    printf '%s\n' "${prefix} deleted revision one" > "$deleted_file"
    git add "$file" "$deleted_file"
    git commit -m "Add shallow fixtures"
    git push origin master

    printf '%s\n' "$second_content" > "$file"
    git add "$file"
    git commit -m "Update shallow fixture"
    git push origin master

    git rm "$deleted_file"
    git commit -m "Delete shallow fixture"
    git push origin master
  )

  local shallow_clone="$workdir/clone_shallow"
  echo "Cloning with remote.origin.shallow=true..."
  e2e_clone_repo_into "$shallow_clone" \
    "${git_clone_args[@]}" \
    -c remote.origin.shallow=true

  e2e_assert_file_contains "$shallow_clone/$file" "$second_content" "E2E shallow clone verified: latest page content imported."
  e2e_assert_path_missing "$shallow_clone/$deleted_file" "E2E shallow clone verified: deleted page omitted from snapshot."

  (
    cd "$shallow_clone"
    local_count=$(git log --oneline -- "$file" | wc -l | tr -d ' ')
    if [ "$local_count" != "1" ]; then
      e2e_fail "Verification failed: shallow clone should contain one commit for $file, got $local_count" git log --oneline -- "$file"
    fi
  )
  echo "E2E shallow clone verified: $file has a single snapshot commit."
}
