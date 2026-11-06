package remotehelper

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Exercise the Git remote-helper and fast-import processes with an import
// that writes a commit before encountering a missing revision.
func TestGitFetchMissingRevisions(t *testing.T) {
	binDir := t.TempDir()
	build := exec.Command("go", "build", "-o", filepath.Join(binDir, "git-remote-mediawiki"), "..")
	build.Env = append(os.Environ(), "GOPROXY=off", "GOSUMDB=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v\n%s", err, output)
	}

	for _, tc := range []struct {
		name       string
		lookup     string
		wantError  bool
		wantOutput string
	}{
		{"deleted", `{"query":{"pages":[{"title":"Example","deletedrevisions":[{"revid":52}]}]}}`, false, `warning: skipping deleted revision(s): 52="Example"`},
		{"unknown", `{"error":{"code":"drvnosuchrevid"}}`, true, "failed to retrieve revision(s): [52]"},
		{"permission denied", `{"error":{"code":"drvpermissiondenied"}}`, true, "insufficient permissions to inspect deleted revisions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.FormValue("list") == "recentchanges":
					fmt.Fprint(w, `{"query":{"recentchanges":[{"revid":53}]}}`)
				case r.FormValue("prop") == "revisions":
					ids := r.FormValue("revids")
					switch {
					case strings.HasPrefix(ids, "2|"):
						gaps := make(map[string]interface{})
						for id := 3; id <= 51; id++ {
							gaps[strconv.Itoa(id)] = map[string]interface{}{"revid": id, "missing": true}
						}
						json.NewEncoder(w).Encode(map[string]interface{}{"query": map[string]interface{}{
							"badrevids": gaps,
							"pages": []interface{}{map[string]interface{}{"title": "Example", "revisions": []interface{}{
								map[string]interface{}{"revid": 2, "content": "Before gap", "timestamp": "2020-01-01T00:00:00Z", "user": "Example"},
							}}},
						}})
					case ids == "52|53":
						fmt.Fprint(w, `{"query":{"pages":[{"title":"Example","revisions":[{"revid":53,"content":"After gap","timestamp":"2020-01-02T00:00:00Z","user":"Example"}]}]}}`)
					default:
						fmt.Fprint(w, `{"query":{"pages":[]}}`)
					}
				case r.FormValue("prop") == "deletedrevisions":
					fmt.Fprint(w, tc.lookup)
				case r.FormValue("list") == "logevents":
					fmt.Fprint(w, `{"query":{"logevents":[]}}`)
				default:
					http.Error(w, "unexpected request", http.StatusBadRequest)
				}
			}))
			defer server.Close()

			repo := t.TempDir()
			env := append(os.Environ(),
				"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull,
				"GIT_TERMINAL_PROMPT=0", "NO_PROXY=*", "no_proxy=*",
				"GIT_REMOTE_MEDIAWIKI_DEBUG=", "GIT_REMOTE_MEDIAWIKI_MEDIAIMPORT=",
			)
			runGit := func(args ...string) string {
				t.Helper()
				cmd := exec.Command("git", args...)
				cmd.Dir = repo
				cmd.Env = env
				output, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, output)
				}
				return string(output)
			}
			runGit("init", "-q")
			runGit("config", "user.name", "Example")
			runGit("config", "user.email", "example@example.invalid")
			runGit("commit", "--allow-empty", "-qm", "Initial")
			runGit("update-ref", "refs/mediawiki/origin/master", "HEAD")
			runGit("notes", "--ref=origin/mediawiki", "add", "-m", "mediawiki_revision: 1", "HEAD")
			runGit("remote", "add", "origin", "mediawiki::"+server.URL)
			runGit("config", "remote.origin.page", "Example")
			before := runGit("rev-parse", "refs/mediawiki/origin/master")
			beforeNotes := runGit("rev-parse", "refs/notes/origin/mediawiki")

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			fetch := exec.CommandContext(ctx, "git", "fetch", "origin")
			fetch.Dir = repo
			fetch.Env = env
			fetch.WaitDelay = time.Second
			output, err := fetch.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("git fetch hung: %s", output)
			}
			if (err != nil) != tc.wantError {
				t.Fatalf("git fetch error = %v, wantError = %t: %s", err, tc.wantError, output)
			}
			if !strings.Contains(string(output), tc.wantOutput) {
				t.Fatalf("missing diagnostic %q: %s", tc.wantOutput, output)
			}
			after := runGit("rev-parse", "refs/mediawiki/origin/master")
			afterNotes := runGit("rev-parse", "refs/notes/origin/mediawiki")
			if tc.wantError {
				if after != before || afterNotes != beforeNotes {
					t.Fatalf("failed import advanced refs: commit %s -> %s; notes %s -> %s", before, after, beforeNotes, afterNotes)
				}
				return
			}
			if after == before || afterNotes == beforeNotes {
				t.Fatal("successful import did not advance commit and notes refs")
			}
			if got := runGit("show", "refs/mediawiki/origin/master:Example.mw"); got != "After gap\n" {
				t.Fatalf("unexpected content after skipped revision: %q", got)
			}
			if got := runGit("notes", "--ref=origin/mediawiki", "show", "refs/mediawiki/origin/master"); strings.TrimSpace(got) != "mediawiki_revision: 53" {
				t.Fatalf("unexpected last imported revision: %q", got)
			}
		})
	}
}
