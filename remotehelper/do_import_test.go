package remotehelper

import (
	"bytes"
	"os/exec"
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

func TestDoImportSingleCommitSupportsMultipleFiles(t *testing.T) {
	oldGetAll := getAllPagesContentFunc
	oldGit := gitExecWithStdin
	defer func() {
		getAllPagesContentFunc = oldGetAll
		gitExecWithStdin = oldGit
	}()

	getAllPagesContentFunc = func(apiURL string, namespace, limit int) ([]client.Page, error) {
		return []client.Page{
			{PageID: 1, Title: "First Page", Content: "Hello One"},
			{PageID: 2, Title: "Second Page", Content: "Hello Two"},
		}, nil
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

	repo := t.TempDir()
	cmd := exec.Command("git", "init", repo)
	if got, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, got)
	}

	fastImport := exec.Command("git", "fast-import", "--quiet")
	fastImport.Dir = repo
	fastImport.Stdin = strings.NewReader(out.String())
	if got, err := fastImport.CombinedOutput(); err != nil {
		t.Fatalf("git fast-import rejected stream: %v\n%s\nstream:\n%s", err, got, out.String())
	}

	firstShow := exec.Command("git", "show", "refs/mediawiki/origin/master:First_Page.mw")
	firstShow.Dir = repo
	firstOut, err := firstShow.CombinedOutput()
	if err != nil || !strings.Contains(string(firstOut), "Hello One") {
		t.Fatalf("missing first imported page: err=%v data=%q", err, string(firstOut))
	}
	secondShow := exec.Command("git", "show", "refs/mediawiki/origin/master:Second_Page.mw")
	secondShow.Dir = repo
	secondOut, err := secondShow.CombinedOutput()
	if err != nil || !strings.Contains(string(secondOut), "Hello Two") {
		t.Fatalf("missing second imported page: err=%v data=%q", err, string(secondOut))
	}
}

type assertAnError struct{}

func (assertAnError) Error() string { return "expected error" }
