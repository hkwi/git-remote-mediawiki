package remotehelper

import (
	"bytes"
	"strings"
	"testing"

	"git-remote-mediawiki/client"
)

func TestDoImportSingleCommitRootsWhenRefMissing(t *testing.T) {
	oldGetAll := getAllPagesContentFunc
	oldGit := gitExecWithStdin
	defer func() {
		getAllPagesContentFunc = oldGetAll
		gitExecWithStdin = oldGit
	}()

	getAllPagesContentFunc = func(apiURL string, namespace, limit int) ([]client.Page, error) {
		return []client.Page{{PageID: 1, Title: "Test Page", Content: "Hello World"}}, nil
	}
	gitExecWithStdin = func(stdin string, args ...string) (string, string, error) {
		if len(args) >= 3 && args[0] == "config" && args[1] == "--get" {
			switch args[2] {
			case "remote.origin.shallow", "mediawiki.shallow":
				return "true", "", nil
			}
		}
		return "", "", assertAnError{}
	}

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if err := doImport(out, errOut, "origin", "http://example.com/api.php", "http://example.com", nil); err != nil {
		t.Fatalf("doImport failed: %v", err)
	}

	got := out.String()
	if strings.Contains(got, "from refs/mediawiki/origin/master^0") {
		t.Fatalf("unexpected parent on initial import: %q", got)
	}
}

func TestDoImportSingleCommitContinuesExistingRef(t *testing.T) {
	oldGetAll := getAllPagesContentFunc
	oldGit := gitExecWithStdin
	defer func() {
		getAllPagesContentFunc = oldGetAll
		gitExecWithStdin = oldGit
	}()

	getAllPagesContentFunc = func(apiURL string, namespace, limit int) ([]client.Page, error) {
		return []client.Page{{PageID: 1, Title: "Test Page", Content: "Hello World"}}, nil
	}
	gitExecWithStdin = func(stdin string, args ...string) (string, string, error) {
		if len(args) >= 3 && args[0] == "config" && args[1] == "--get" {
			switch args[2] {
			case "remote.origin.shallow", "mediawiki.shallow":
				return "true", "", nil
			}
		}
		if len(args) >= 3 && args[0] == "rev-parse" && args[1] == "--verify" && args[2] == "refs/mediawiki/origin/master^0" {
			return "deadbeef", "", nil
		}
		return "", "", nil
	}

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if err := doImport(out, errOut, "origin", "http://example.com/api.php", "http://example.com", nil); err != nil {
		t.Fatalf("doImport failed: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "commit refs/mediawiki/origin/master\nfrom refs/mediawiki/origin/master^0\n") {
		t.Fatalf("missing parent on incremental import: %q", got)
	}
}

type assertAnError struct{}

func (assertAnError) Error() string { return "expected error" }
