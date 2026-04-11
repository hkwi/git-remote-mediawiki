package remotehelper

import (
	"bytes"
	"strings"
	"testing"

	"git-remote-mediawiki/client"
)

func TestCapabilities(t *testing.T) {
	in := bytes.NewBufferString("capabilities\n")
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if err := Run(in, out, errOut, "origin", "http://example.com/w"); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "import") || !strings.Contains(got, "list") || !strings.Contains(got, "push") || !strings.Contains(got, "option") {
		t.Fatalf("capabilities missing in output: %q", got)
	}
}

func TestList(t *testing.T) {
	in := bytes.NewBufferString("list\n")
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if err := Run(in, out, errOut, "origin", "http://example.com/w"); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "refs/heads/master") {
		t.Fatalf("list output unexpected: %q", got)
	}
}

func TestImportProducesCommit(t *testing.T) {
	oldProgress := progressEnabled
	defer func() { progressEnabled = oldProgress }()

	// Override page fetcher
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
			case "remote.origin.shallow":
				return "true", "", nil
			case "mediawiki.shallow":
				return "true", "", nil
			}
		}
		return "", "", nil
	}
	in := bytes.NewBufferString("import HEAD\n\n")
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if err := Run(in, out, errOut, "origin", "http://example.com/w"); err != nil {
		t.Fatalf("Run failed: %v; stderr=%s", err, errOut.String())
	}
	got := out.String()
	if !strings.Contains(got, "commit refs/mediawiki/origin/master") {
		t.Fatalf("import did not produce commit header: %q", got)
	}
	if !strings.Contains(got, "M 644 inline \"Test_Page.mw\"") {
		t.Fatalf("import did not include file modification: %q", got)
	}
}

func TestOptionProgressEnablesProgress(t *testing.T) {
	oldProgress := progressEnabled
	oldGetAll := getAllPagesContentFunc
	oldGit := gitExecWithStdin
	defer func() {
		progressEnabled = oldProgress
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
		return "", "", nil
	}

	in := bytes.NewBufferString("option progress true\nimport HEAD\n\n")
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if err := Run(in, out, errOut, "origin", "http://example.com/w"); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "ok\nprogress Importing snapshot with 1 pages\n") {
		t.Fatalf("progress was not enabled: %q", got)
	}
}

func TestDebugfDisabledByDefault(t *testing.T) {
	t.Setenv("GIT_REMOTE_MEDIAWIKI_DEBUG", "")

	errOut := &bytes.Buffer{}
	debugf(errOut, "hidden")
	if errOut.Len() != 0 {
		t.Fatalf("unexpected debug output: %q", errOut.String())
	}
}

func TestDebugfEnabledByEnv(t *testing.T) {
	t.Setenv("GIT_REMOTE_MEDIAWIKI_DEBUG", "1")

	errOut := &bytes.Buffer{}
	debugf(errOut, "visible %d", 42)
	got := errOut.String()
	if !strings.Contains(got, "DEBUG: visible 42") {
		t.Fatalf("missing debug output: %q", got)
	}
}
