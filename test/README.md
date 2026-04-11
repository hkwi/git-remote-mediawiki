# E2E Test Flow

This directory contains the end-to-end test setup for `git-remote-mediawiki`.

## Overview

The e2e suite is split into two layers:

- Host-side scripts start Docker Compose services and wait for MediaWiki to become ready through the internal Compose network.
- Container-side scripts run `git` and `git-remote-mediawiki` inside the `git` service, so tests do not depend on a helper installed on the host system.

## Services

`docker-compose.yml` defines three services:

- `db`: MariaDB for MediaWiki
- `mediawiki`: the wiki under test
- `git`: a Go + Git environment used to build and run `git-remote-mediawiki`

## Entry Points

- `e2e.sh`
  Runs the full suite:
  - `e2e-git.sh`
  - `e2e-by-rev.sh`

- `e2e-git.sh`
  Host-side wrapper for the main Git scenario.
  It:
  1. starts Compose
  2. waits for the MediaWiki API
  3. executes `e2e-git-inner.sh` inside the `git` container

- `e2e-by-rev.sh`
  Host-side wrapper for the incremental fetch/pull scenario.
  It:
  1. starts Compose
  2. waits for the MediaWiki API
  3. executes `e2e-by-rev-inner.sh` inside the `git` container

## Inner Scripts

- `e2e-git-inner.sh`
  Runs the main scenarios inside the `git` container:
  - clone/import
  - page push/delete
  - media upload/import/delete
  - tracked subset clone
  - shallow clone

- `e2e-by-rev-inner.sh`
  Runs the incremental import scenario inside the `git` container:
  - out-of-band change
  - `git fetch`
  - `git pull`

## Shared Logic

`e2e-lib.sh` contains common helpers for:

- starting and stopping Compose
- waiting for the MediaWiki API
- building and installing the helper in the `git` container
- cloning and preparing repositories
- verifying MediaWiki API results and local clone contents

## Why The Inner Scripts Exist

The outer scripts manage infrastructure.
The inner scripts perform Git operations.

This split ensures the helper used during e2e is the one built from the current source tree, not a helper that happens to be installed on the host machine.
It also means the suite no longer depends on publishing MediaWiki on a host port such as `localhost:8080`.

## Typical Usage

Run the full suite:

```bash
cd test
bash e2e.sh
```

Run only the main Git scenario:

```bash
cd test
bash e2e-git.sh
```

Run only the incremental by-revision scenario:

```bash
cd test
bash e2e-by-rev.sh
```
