Simple MediaWiki API client (Go)

Minimal MediaWiki API client and helper tools implemented in Go.

This project is a Go reimplementation of [Git-Mediawiki/Git-Mediawiki](https://github.com/Git-Mediawiki/Git-Mediawiki).

Prerequisites
-------------

- Go 1.20 or newer
- Docker and Docker Compose (required for end-to-end tests)

Build
-----

Build the command-line binaries from the project root:

```bash
# Build the git remote helper
go build -o git-remote-mediawiki .
```

Run unit tests
--------------

```bash
go test ./...
```

End-to-end tests
----------------

An end-to-end test suite that brings up a MediaWiki container via Docker Compose is provided in `test`.

From the project root:

```bash
cd test
chmod +x e2e.sh e2e-git.sh
# The e2e runner will build the binaries if they are missing.
WIKI_USER=admin WIKI_PASS=hogehogehoge ./e2e.sh
```

Run only the git helper integration test:

```bash
cd test
WIKI_USER=admin WIKI_PASS=hogehogehoge ./e2e-git.sh
```

Notes
-----

- `e2e.sh` will automatically build the Go binaries if they are not present.
- Pass `WIKI_USER`/`WIKI_PASS` to tests if your test wiki uses non-default credentials.
- When MediaWiki returns HTTP 429 with a `Retry-After` header, the helper retries automatically if the delay is 5 minutes or less; longer delays are treated as errors.

Install as a Git remote helper
-----------------------------

You can install the `git-remote-mediawiki` binary so `git` can use it as a remote helper.

Build the helper (from project root):

```bash
go build -o git-remote-mediawiki .
chmod +x git-remote-mediawiki
```

Install system-wide (requires sudo):

```bash
# Install into Git's exec path (preferred)
sudo cp git-remote-mediawiki "$(git --exec-path)/git-remote-mediawiki"

# Or install to a location on your $PATH
sudo mv git-remote-mediawiki /usr/local/bin/
```

Install for a single user:

```bash
mkdir -p "$HOME/bin"
mv git-remote-mediawiki "$HOME/bin/"
export PATH="$HOME/bin:$PATH"
```

Notes and verification
----------------------

- The helper must be named exactly `git-remote-mediawiki` (no suffix) and be executable.
- Verify installation by cloning a wiki URL handled by the helper, for example:

```bash
git clone mediawiki::http://localhost:8080 my_wiki_clone
```

- To provide wiki credentials (optional), prefer passing them at clone time with `-c`:

```bash
git \
  -c remote.origin.mwLogin=admin \
  -c remote.origin.mwPassword=hogehogehoge \
  clone mediawiki::http://localhost:8080 my_wiki_clone
```

- OAuth and two-factor authentication (2FA) are not supported.
- For MediaWiki environments that require stronger operational controls, this tool is intended to be used together with MediaWiki's built-in bot functionality.

- To track specific pages, categories, or namespaces, the legacy plural keys
  (`remote.<name>.pages`, `categories`, `namespaces`) still accept
  whitespace-separated values.
- For titles that contain spaces, prefer the singular multi-value keys
  (`remote.<name>.page`, `category`, `namespace`) and add them multiple times:

```bash
git config --add remote.origin.page "Spaced page"
git config --add remote.origin.page "Another Page"
git config --add remote.origin.category "Real Time Strategy Games"
```

- To mark a pushed commit as a minor edit, add a Git note on
  `refs/notes/mediawiki-options` before pushing:

```bash
git notes --ref=mediawiki-options add -m "minor: true" HEAD
git push origin master
```

See the root entrypoint: [main.go](main.go).
