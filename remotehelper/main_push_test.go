package remotehelper

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

func TestPushCallsEdit(t *testing.T) {
	// Override git and file helpers
	oldList := listFilesFunc
	oldShow := showFileFunc
	oldEdit := editPage
	oldDelete := deletePage
	oldGit := gitExecWithStdin
	oldUpdate := updatePushMetadataFunc
	oldDeletedFiles := deletedMWFilesFunc
	oldChangedFiles := changedMWFilesFunc
	defer func() {
		listFilesFunc = oldList
		showFileFunc = oldShow
		editPage = oldEdit
		deletePage = oldDelete
		updatePushMetadataFunc = oldUpdate
		deletedMWFilesFunc = oldDeletedFiles
		changedMWFilesFunc = oldChangedFiles
	}()

	defer func() { gitExecWithStdin = oldGit }()

	gitExecWithStdin = func(stdin string, args ...string) (string, string, error) {
		// For rev-parse return a placeholder commit
		if len(args) >= 1 && args[0] == "rev-parse" {
			return "deadbeef", "", nil
		}
		return "", "", nil
	}

	listFilesFunc = func(commit string) ([]string, error) { return []string{"Test_Page.mw"}, nil }
	changedMWFilesFunc = func(base, commit string) ([]string, error) { return []string{"Test_Page.mw"}, nil }
	showFileFunc = func(commit, path string) (string, error) {
		return "Hello Push", nil
	}

	var gotTitle, gotContent string
	editPage = func(httpClient *http.Client, apiURL, title, content, summary string, minor bool) (int64, error) {
		gotTitle = title
		gotContent = content
		if minor {
			t.Fatal("minor should be false")
		}
		return 123, nil
	}
	var gotCommit string
	var gotRevid int64
	updatePushMetadataFunc = func(remotename, commit string, revid int64) error {
		gotCommit = commit
		gotRevid = revid
		return nil
	}
	deletedMWFilesFunc = func(base, commit string) ([]string, error) { return nil, nil }

	in := bytes.NewBufferString("push refs/heads/master:refs/heads/master\n\n")
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if err := Run(in, out, errOut, "origin", "http://example.com/w"); err != nil {
		t.Fatalf("Run failed: %v; stderr=%s", err, errOut.String())
	}

	if gotTitle != "Test Page" {
		t.Fatalf("unexpected title: %q", gotTitle)
	}
	if gotContent != "Hello Push" {
		t.Fatalf("unexpected content: %q", gotContent)
	}
	if gotCommit != "deadbeef" || gotRevid != 123 {
		t.Fatalf("unexpected metadata update args: commit=%q revid=%d", gotCommit, gotRevid)
	}
	if !strings.Contains(out.String(), "ok refs/heads/master") {
		t.Fatalf("missing push status: %q", out.String())
	}
}

func TestPushDumbPushSkipsMetadataUpdate(t *testing.T) {
	oldList := listFilesFunc
	oldShow := showFileFunc
	oldEdit := editPage
	oldDelete := deletePage
	oldGit := gitExecWithStdin
	oldUpdate := updatePushMetadataFunc
	oldDeletedFiles := deletedMWFilesFunc
	oldChangedFiles := changedMWFilesFunc
	defer func() {
		listFilesFunc = oldList
		showFileFunc = oldShow
		editPage = oldEdit
		deletePage = oldDelete
		gitExecWithStdin = oldGit
		updatePushMetadataFunc = oldUpdate
		deletedMWFilesFunc = oldDeletedFiles
		changedMWFilesFunc = oldChangedFiles
	}()

	gitExecWithStdin = func(stdin string, args ...string) (string, string, error) {
		if len(args) >= 1 && args[0] == "rev-parse" {
			return "deadbeef", "", nil
		}
		if len(args) >= 3 && args[0] == "config" && args[1] == "--get" && args[2] == "remote.origin.dumbPush" {
			return "true", "", nil
		}
		return "", "", nil
	}
	listFilesFunc = func(commit string) ([]string, error) {
		return []string{"Test_Page.mw"}, nil
	}
	changedMWFilesFunc = func(base, commit string) ([]string, error) { return []string{"Test_Page.mw"}, nil }
	showFileFunc = func(commit, path string) (string, error) {
		return "Hello Push", nil
	}
	editPage = func(httpClient *http.Client, apiURL, title, content, summary string, minor bool) (int64, error) {
		return 123, nil
	}
	deletedMWFilesFunc = func(base, commit string) ([]string, error) { return nil, nil }

	called := false
	updatePushMetadataFunc = func(remotename, commit string, revid int64) error {
		called = true
		return nil
	}

	in := bytes.NewBufferString("push refs/heads/master:refs/heads/master\n\n")
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if err := Run(in, out, errOut, "origin", "http://example.com/w"); err != nil {
		t.Fatalf("Run failed: %v; stderr=%s", err, errOut.String())
	}
	if called {
		t.Fatal("metadata update should be skipped for dumbPush")
	}
	if !strings.Contains(errOut.String(), "dumbPush") {
		t.Fatalf("missing dumbPush notice: %q", errOut.String())
	}
}

func TestPushPropagatesDeletedPage(t *testing.T) {
	oldList := listFilesFunc
	oldShow := showFileFunc
	oldEdit := editPage
	oldDelete := deletePage
	oldGit := gitExecWithStdin
	oldUpdate := updatePushMetadataFunc
	oldDeletedFiles := deletedMWFilesFunc
	oldChangedFiles := changedMWFilesFunc
	defer func() {
		listFilesFunc = oldList
		showFileFunc = oldShow
		editPage = oldEdit
		deletePage = oldDelete
		gitExecWithStdin = oldGit
		updatePushMetadataFunc = oldUpdate
		deletedMWFilesFunc = oldDeletedFiles
		changedMWFilesFunc = oldChangedFiles
	}()

	gitExecWithStdin = func(stdin string, args ...string) (string, string, error) {
		if len(args) >= 1 && args[0] == "rev-parse" {
			if len(args) >= 2 && args[1] == "refs/mediawiki/origin/master^0" {
				return "basebeef", "", nil
			}
			return "deadbeef", "", nil
		}
		return "", "", nil
	}
	listFilesFunc = func(commit string) ([]string, error) {
		return nil, nil
	}
	changedMWFilesFunc = func(base, commit string) ([]string, error) { return nil, nil }
	showFileFunc = func(commit, path string) (string, error) {
		return "", nil
	}
	editPage = func(httpClient *http.Client, apiURL, title, content, summary string, minor bool) (int64, error) {
		t.Fatalf("editPage should not be called")
		return 0, nil
	}
	deletedMWFilesFunc = func(base, commit string) ([]string, error) {
		if base != "basebeef" || commit != "deadbeef" {
			t.Fatalf("unexpected diff args: base=%q commit=%q", base, commit)
		}
		return []string{"Foo.mw"}, nil
	}

	var gotTitle string
	deletePage = func(httpClient *http.Client, apiURL, title, reason string) (int64, error) {
		gotTitle = title
		return 456, nil
	}

	var gotCommit string
	var gotRevid int64
	updatePushMetadataFunc = func(remotename, commit string, revid int64) error {
		gotCommit = commit
		gotRevid = revid
		return nil
	}

	in := bytes.NewBufferString("push refs/heads/master:refs/heads/master\n\n")
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if err := Run(in, out, errOut, "origin", "http://example.com/w"); err != nil {
		t.Fatalf("Run failed: %v; stderr=%s", err, errOut.String())
	}

	if gotTitle != "Foo" {
		t.Fatalf("unexpected deleted title: %q", gotTitle)
	}
	if gotCommit != "deadbeef" || gotRevid != 456 {
		t.Fatalf("unexpected metadata update args: commit=%q revid=%d", gotCommit, gotRevid)
	}
	if !strings.Contains(out.String(), "ok refs/heads/master") {
		t.Fatalf("missing push status: %q", out.String())
	}
}

func TestPushOnlySendsChangedFiles(t *testing.T) {
	oldList := listFilesFunc
	oldShow := showFileFunc
	oldEdit := editPage
	oldDelete := deletePage
	oldGit := gitExecWithStdin
	oldUpdate := updatePushMetadataFunc
	oldDeletedFiles := deletedMWFilesFunc
	oldChangedFiles := changedMWFilesFunc
	defer func() {
		listFilesFunc = oldList
		showFileFunc = oldShow
		editPage = oldEdit
		deletePage = oldDelete
		gitExecWithStdin = oldGit
		updatePushMetadataFunc = oldUpdate
		deletedMWFilesFunc = oldDeletedFiles
		changedMWFilesFunc = oldChangedFiles
	}()

	gitExecWithStdin = func(stdin string, args ...string) (string, string, error) {
		if len(args) >= 1 && args[0] == "rev-parse" {
			if len(args) >= 2 && args[1] == "refs/mediawiki/origin/master^0" {
				return "basebeef", "", nil
			}
			return "deadbeef", "", nil
		}
		return "", "", nil
	}
	listFilesFunc = func(commit string) ([]string, error) {
		return []string{"Unchanged.mw", "Changed.mw"}, nil
	}
	changedMWFilesFunc = func(base, commit string) ([]string, error) {
		if base != "basebeef" || commit != "deadbeef" {
			t.Fatalf("unexpected changed diff args: base=%q commit=%q", base, commit)
		}
		return []string{"Changed.mw"}, nil
	}
	deletedMWFilesFunc = func(base, commit string) ([]string, error) { return nil, nil }

	var shown []string
	showFileFunc = func(commit, path string) (string, error) {
		shown = append(shown, path)
		return "content", nil
	}
	var edited []string
	editPage = func(httpClient *http.Client, apiURL, title, content, summary string, minor bool) (int64, error) {
		edited = append(edited, title)
		return 111, nil
	}
	deletePage = func(httpClient *http.Client, apiURL, title, reason string) (int64, error) {
		t.Fatalf("deletePage should not be called")
		return 0, nil
	}
	updatePushMetadataFunc = func(remotename, commit string, revid int64) error { return nil }

	in := bytes.NewBufferString("push refs/heads/master:refs/heads/master\n\n")
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if err := Run(in, out, errOut, "origin", "http://example.com/w"); err != nil {
		t.Fatalf("Run failed: %v; stderr=%s", err, errOut.String())
	}

	if len(shown) != 1 || shown[0] != "Changed.mw" {
		t.Fatalf("unexpected shown files: %#v", shown)
	}
	if len(edited) != 1 || edited[0] != "Changed" {
		t.Fatalf("unexpected edited titles: %#v", edited)
	}
}

func TestPushUsesMinorFlagFromNotes(t *testing.T) {
	oldShow := showFileFunc
	oldEdit := editPage
	oldDelete := deletePage
	oldGit := gitExecWithStdin
	oldUpdate := updatePushMetadataFunc
	oldDeletedFiles := deletedMWFilesFunc
	oldChangedFiles := changedMWFilesFunc
	defer func() {
		showFileFunc = oldShow
		editPage = oldEdit
		deletePage = oldDelete
		gitExecWithStdin = oldGit
		updatePushMetadataFunc = oldUpdate
		deletedMWFilesFunc = oldDeletedFiles
		changedMWFilesFunc = oldChangedFiles
	}()

	gitExecWithStdin = func(stdin string, args ...string) (string, string, error) {
		if len(args) >= 1 && args[0] == "rev-parse" {
			return "deadbeef", "", nil
		}
		if len(args) >= 4 && args[0] == "notes" && args[1] == "--ref=mediawiki-options" && args[2] == "show" && args[3] == "deadbeef" {
			return "minor: true\n", "", nil
		}
		return "", "", nil
	}
	changedMWFilesFunc = func(base, commit string) ([]string, error) { return []string{"Test_Page.mw"}, nil }
	deletedMWFilesFunc = func(base, commit string) ([]string, error) { return nil, nil }
	showFileFunc = func(commit, path string) (string, error) { return "Hello Push", nil }

	called := false
	editPage = func(httpClient *http.Client, apiURL, title, content, summary string, minor bool) (int64, error) {
		called = true
		if !minor {
			t.Fatal("expected minor=true")
		}
		return 123, nil
	}
	deletePage = func(httpClient *http.Client, apiURL, title, reason string) (int64, error) {
		t.Fatalf("deletePage should not be called")
		return 0, nil
	}
	updatePushMetadataFunc = func(remotename, commit string, revid int64) error { return nil }

	in := bytes.NewBufferString("push refs/heads/master:refs/heads/master\n\n")
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if err := Run(in, out, errOut, "origin", "http://example.com/w"); err != nil {
		t.Fatalf("Run failed: %v; stderr=%s", err, errOut.String())
	}
	if !called {
		t.Fatal("expected editPage to be called")
	}
}
